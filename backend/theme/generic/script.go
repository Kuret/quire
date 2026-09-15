package generic

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// The goja hook, deliberately minimal.
//
// The runtime gets almost no host bindings: no fetch, no file access, no
// console, no timers, and nothing from Node. A script sees two strings — the
// HTML we already fetched, and the URL we fetched it from — and returns an
// array. That is the entire contract.
//
// This is not paranoia about a user scripting their own device; it is about
// where the §7.4 fetch invariants live. Every request Quire makes goes through
// one rate-limited, robots-respecting, SSRF-guarded client. A script that
// could fetch would be a second HTTP path with none of that, reachable from a
// config file that a user might have imported from someone else.
//
// The only two things Quire puts into the runtime are atob and btoa — see
// installBase64 for why those two and nothing else.
//
// A script may export either or both of:
//
//	function pages(html, url)    -> [imageUrl, ...]
//	function chapters(html, url) -> [href, ...] or [{id, title}, ...]
//
// A function that is absent, or that returns null/undefined, means "I have no
// opinion" and the selector path runs instead.

// installBase64 adds atob and btoa, and nothing else.
//
// These are the only two functions Quire puts into the runtime. They earn
// their place because base64 is how a one-off site most commonly hides a page
// list from selectors, and because they are *pure*: string in, string out, no
// I/O, no clock, no ambient authority. A script that can decode a blob still
// cannot reach the network, so the §7.4 fetch invariants stand.
//
// If a future site needs something more than this, the answer is a theme, not
// a bigger sandbox.
func installBase64(vm *goja.Runtime) error {
	if err := vm.Set("atob", func(s string) (string, error) {
		// Sites emit both the padded and the URL-safe alphabet; accept either
		// rather than making the user normalise it in their script.
		s = strings.TrimSpace(s)
		enc := base64.StdEncoding
		if strings.ContainsAny(s, "-_") {
			enc = base64.URLEncoding
		}
		if n := len(s) % 4; n != 0 {
			enc = enc.WithPadding(base64.NoPadding)
		}
		b, err := enc.DecodeString(s)
		if err != nil {
			return "", fmt.Errorf("atob: %w", err)
		}
		return string(b), nil
	}); err != nil {
		return err
	}
	return vm.Set("btoa", func(s string) string {
		return base64.StdEncoding.EncodeToString([]byte(s))
	})
}

// scriptItem is one returned entry, from either accepted shape.
type scriptItem struct {
	ID    string
	Title string
}

// compile parses a script once, so a syntax error is reported when the source
// is added rather than when a chapter is opened.
func compile(src string) (*goja.Program, error) {
	return goja.Compile("source-script.js", src, true)
}

// runHook calls fn in a fresh runtime. The second return value is false when
// the script has no such function, or returned nothing — meaning "fall back to
// the selectors".
func runHook(src, fn, html, pageURL string, timeoutMs int) ([]scriptItem, bool, error) {
	prog, err := compile(src)
	if err != nil {
		return nil, false, err
	}
	if timeoutMs <= 0 {
		timeoutMs = 2000
	}

	// A fresh runtime per call. Sharing one would let a script keep state
	// between chapters, which is a debugging problem nobody needs and which
	// would make the hook's behaviour depend on browse order.
	//
	// A bare goja.Runtime has only the ECMAScript standard library: no Node,
	// no DOM, no fetch, no timers. TestScriptHasNoHostBindings asserts it
	// stays that way.
	vm := goja.New()
	if err := installBase64(vm); err != nil {
		return nil, false, err
	}

	// A wall-clock interrupt, so a runaway loop cannot hang the backend on a
	// device with one core to spare.
	timer := time.AfterFunc(time.Duration(timeoutMs)*time.Millisecond, func() {
		vm.Interrupt(fmt.Sprintf("script exceeded its %dms budget", timeoutMs))
	})
	defer timer.Stop()

	if _, err := vm.RunProgram(prog); err != nil {
		return nil, false, scriptError(err)
	}

	v := vm.Get(fn)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil, false, nil
	}
	callable, ok := goja.AssertFunction(v)
	if !ok {
		return nil, false, fmt.Errorf("%s is defined but is not a function", fn)
	}

	res, err := callable(goja.Undefined(), vm.ToValue(html), vm.ToValue(pageURL))
	if err != nil {
		return nil, false, scriptError(err)
	}
	if res == nil || goja.IsUndefined(res) || goja.IsNull(res) {
		return nil, false, nil
	}

	items, err := toItems(vm, res)
	if err != nil {
		return nil, false, err
	}
	if len(items) == 0 {
		// An empty array is a real answer ("this chapter has no pages") only
		// in theory; in practice it is a script that did not work. Treat it as
		// no opinion so the selectors get their turn.
		return nil, false, nil
	}
	return items, true, nil
}

// toItems accepts an array of strings or of {id,title} objects.
func toItems(vm *goja.Runtime, v goja.Value) ([]scriptItem, error) {
	obj := v.ToObject(vm)
	if obj == nil {
		return nil, fmt.Errorf("script returned %s, want an array", v.ExportType())
	}
	lenVal := obj.Get("length")
	if lenVal == nil || goja.IsUndefined(lenVal) {
		return nil, fmt.Errorf("script returned a non-array value")
	}
	n := int(lenVal.ToInteger())
	if n < 0 || n > 100_000 {
		return nil, fmt.Errorf("script returned %d entries, which is not a page list", n)
	}

	out := make([]scriptItem, 0, n)
	for i := range n {
		el := obj.Get(fmt.Sprint(i))
		if el == nil || goja.IsUndefined(el) || goja.IsNull(el) {
			continue
		}
		if eo, ok := el.Export().(map[string]any); ok {
			id, _ := eo["id"].(string)
			title, _ := eo["title"].(string)
			if id == "" {
				continue
			}
			out = append(out, scriptItem{ID: id, Title: title})
			continue
		}
		if s := el.String(); s != "" {
			out = append(out, scriptItem{ID: s})
		}
	}
	return out, nil
}

// scriptError turns goja's own error types into something a user can read. An
// interrupt is reported as a timeout rather than as a Go panic value.
func scriptError(err error) error {
	var ie *goja.InterruptedError
	if ok := asInterrupted(err, &ie); ok {
		return fmt.Errorf("script was interrupted: %v", ie.Value())
	}
	var ex *goja.Exception
	if ok := asException(err, &ex); ok {
		return fmt.Errorf("script threw: %v", ex.Value())
	}
	return err
}

func asInterrupted(err error, target **goja.InterruptedError) bool {
	ie, ok := err.(*goja.InterruptedError)
	if ok {
		*target = ie
	}
	return ok
}

func asException(err error, target **goja.Exception) bool {
	ex, ok := err.(*goja.Exception)
	if ok {
		*target = ex
	}
	return ok
}
