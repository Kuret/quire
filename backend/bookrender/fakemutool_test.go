package bookrender

// A fake mutool for offline tests: it speaks render.js's protocol without a
// real mutool binary or a real book, following the classic Go trick of
// re-executing the test binary itself as a subprocess (see os/exec's own
// TestHelperProcess). Behaviour is driven entirely by environment variables
// the test sets on the *exec.Cmd, so one helper covers every case: a normal
// exchange, a hang (for the timeout-kills-the-child mutation check) and a
// crash-once (for the crash-restart mutation check).

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const (
	envFakeMutool   = "QUIRE_BOOKRENDER_FAKE_MUTOOL"
	envHangOnCmd    = "QUIRE_FAKE_HANG_ON_CMD"
	envCrashOnCmd   = "QUIRE_FAKE_CRASH_ON_CMD"
	envCrashMarker  = "QUIRE_FAKE_CRASH_MARKER"
	envOpenFails    = "QUIRE_FAKE_OPEN_FAILS"
	envFixedLayout  = "QUIRE_FAKE_FIXED_LAYOUT"
	envOutlineTitle = "QUIRE_FAKE_OUTLINE_TITLE"
)

// fakeSpawn returns a spawn hook that launches this test binary in "fake
// mutool" mode with extraEnv layered on top of the current environment.
func fakeSpawn(extraEnv ...string) func() *exec.Cmd {
	return func() *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperMutool", "-test.v=false")
		cmd.Env = append(append([]string{}, os.Environ()...), envFakeMutool+"=1")
		cmd.Env = append(cmd.Env, extraEnv...)
		return cmd
	}
}

// TestHelperMutool is not a real test: run under `go test`, with
// QUIRE_BOOKRENDER_FAKE_MUTOOL unset, it does nothing. fakeSpawn re-executes
// the test binary with that variable set, at which point this becomes the
// fake mutool child process instead of a test.
func TestHelperMutool(t *testing.T) {
	if os.Getenv(envFakeMutool) != "1" {
		return
	}
	runFakeMutool()
}

func runFakeMutool() {
	hangOn := os.Getenv(envHangOnCmd)
	crashOn := os.Getenv(envCrashOnCmd)
	crashMarker := os.Getenv(envCrashMarker)
	openFails := os.Getenv(envOpenFails) == "1"
	fixedLayout := os.Getenv(envFixedLayout) == "1"
	outlineTitle := os.Getenv(envOutlineTitle)

	// Read exactly as `mutool run`'s readline() does — fgets into a 256-byte
	// buffer, so a longer line comes back as several reads — and unframe the
	// way render.js does. A request sent unframed, or framed in pieces too long
	// for that buffer, fails here the way it fails on the device.
	in := bufio.NewReader(os.Stdin)
	pending := ""
	for {
		raw, err := fgets256(in)
		if err != nil {
			return
		}
		if strings.HasPrefix(raw, ">") {
			pending += raw[1:]
			continue
		}
		if raw != "." {
			continue
		}
		line := pending
		pending = ""
		if line == "" {
			continue
		}
		var req map[string]any
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			fmt.Println(`{"ok":false,"error":"bad request"}`)
			continue
		}
		cmd, _ := req["cmd"].(string)

		if cmd == crashOn && crashMarker != "" {
			if _, err := os.Stat(crashMarker); err != nil {
				// First time: touch the marker and die without answering,
				// simulating a crash mid-request.
				_ = os.WriteFile(crashMarker, []byte("1"), 0o644)
				os.Exit(1)
			}
			// Second time (after the restart replayed open/layout): behave.
		}

		if cmd == hangOn {
			select {} // block forever; the caller's timeout must kill us.
		}

		switch cmd {
		case "open":
			if openFails {
				fmt.Println(`{"ok":false,"error":"that file is not a book Quire can open"}`)
				continue
			}
			resp := map[string]any{"ok": true, "fixedLayout": fixedLayout, "title": "Fake Book"}
			b, _ := json.Marshal(resp)
			fmt.Println(string(b))
		case "layout":
			// A stylesheet ending in /*LEN*/ lays out to as many pages as it
			// has bytes, so a test can prove the whole request arrived intact.
			css, _ := req["css"].(string)
			if strings.HasSuffix(css, "/*LEN*/") {
				fmt.Printf("{\"pages\":%d}\n", len(css))
				break
			}
			fmt.Println(`{"pages":10}`)
		case "render":
			out, _ := req["out"].(string)
			if out != "" {
				_ = os.WriteFile(out, []byte("fake-png"), 0o644)
			}
			fmt.Println(`{"ok":true}`)
		case "text":
			page := 0
			if p, ok := req["page"].(float64); ok {
				page = int(p)
			}
			resp := map[string]any{"text": fmt.Sprintf("snippet for page %d", page)}
			b, _ := json.Marshal(resp)
			fmt.Println(string(b))
		case "outline":
			title := outlineTitle
			if title == "" {
				title = "Chapter 1"
			}
			resp := map[string]any{"toc": []map[string]any{{"title": title, "page": 0, "level": 0}}}
			b, _ := json.Marshal(resp)
			fmt.Println(string(b))
		default:
			fmt.Println(`{"ok":false,"error":"unknown command"}`)
		}
	}
}

// fgets256 returns what one fgets(line, 256, stdin) call followed by murun's
// trailing-newline strip would: at most 255 bytes, ending early at a newline.
func fgets256(in *bufio.Reader) (string, error) {
	var b []byte
	for len(b) < 255 {
		c, err := in.ReadByte()
		if err != nil {
			if len(b) > 0 {
				return string(b), nil
			}
			return "", err
		}
		b = append(b, c)
		if c == '\n' {
			return string(b[:len(b)-1]), nil
		}
	}
	return string(b), nil
}
