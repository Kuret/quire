// Command probe-batch is a dev-only tool. It runs Quire's existing source
// probe (backend/probe/prober) against a list of URLs supplied by the caller
// and prints a verdict table, so a developer can answer "which of these sites
// can Quire support, and which theme family does each belong to" by measuring
// against the real probe instead of guessing.
//
// It is not wired into `make all`. Run it by hand:
//
//	go run ./tools/probe-batch -in sites.txt [-out results.json] [-timeout 90s] [-parallel 4] [-accept kinds]
//
// The input file is never part of this repository — see README.md's promise
// that Quire ships no source URLs, no bundled index and no default catalogue.
// This tool takes the list from wherever the caller keeps it, outside the
// tree.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe/prober"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/asurascans"
	"github.com/rickl/quire/backend/theme/comick"
	"github.com/rickl/quire/backend/theme/doujinreader"
	"github.com/rickl/quire/backend/theme/fanfox"
	"github.com/rickl/quire/backend/theme/generic"
	"github.com/rickl/quire/backend/theme/globalcomix"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/mangadex"
	"github.com/rickl/quire/backend/theme/mangakakalot"
	"github.com/rickl/quire/backend/theme/mangathemesia"
	"github.com/rickl/quire/backend/theme/shelfmark"
	"github.com/rickl/quire/backend/theme/webtoons"
	"github.com/rickl/quire/backend/theme/weebcentral"
)

// site is one line of the input file.
type site struct {
	Name string
	URL  string
}

// question is a record that the probe stopped to ask something.
// By default, all questions are declined, but -accept can change this.
// That a question was asked — and what it asked — is itself a finding: e.g.
// "it asked about a login wall" or "it asked about a redirect".
type question struct {
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	Answered string `json:"answered"` // "yes", "no", or "auto-accept" for auto-accepted questions
}

// result is one site's outcome, bulletproofed against anything the probe
// itself can do wrong: a panic, a hang or a plain error all land here rather
// than aborting the run.
type result struct {
	Name        string         `json:"name"`
	URL         string         `json:"url"`
	Group       string         `json:"group"`
	Verdict     string         `json:"verdict,omitempty"`
	Cancelled   bool           `json:"cancelled,omitempty"`
	ThemeID     string         `json:"theme,omitempty"`
	ThemeScores map[string]int `json:"themeScores,omitempty"`
	Warnings    []string       `json:"warnings,omitempty"`
	Detail      string         `json:"detail,omitempty"`
	Questions   []question     `json:"questions,omitempty"`
	RunError    string         `json:"runError,omitempty"`
	DurationMS  int64          `json:"durationMs"`
}

// Outcome groups, in the order the summary is printed.
const (
	groupWorking     = "matched-and-working"
	groupDegraded    = "matched-but-degraded"
	groupUnrec       = "unrecognised"
	groupChallenge   = "challenge-blocked"
	groupUnreachable = "unreachable"
)

var groupOrder = []string{groupWorking, groupDegraded, groupUnrec, groupChallenge, groupUnreachable}

// classify maps a result onto one of the five summary buckets. Everything
// that is not a clean match, a degraded match, an unrecognised layout or a
// challenge refusal — a transport failure, a blocked address, a robots
// refusal, an invalid URL, a cancelled probe, a panic, or a timeout — is
// "unreachable": in every one of those cases the site could not be measured
// as a candidate at all.
func classify(r *result) string {
	switch r.Verdict {
	case theme.VerdictOK:
		return groupWorking
	case theme.VerdictPartial:
		return groupDegraded
	case theme.VerdictUnrecognised:
		return groupUnrec
	case theme.VerdictBlockedChallenge:
		return groupChallenge
	default:
		return groupUnreachable
	}
}

// recordingUI records questions and answers them according to the acceptKinds
// set (or declines all if the set is empty).
type recordingUI struct {
	mu           sync.Mutex
	questions    []question
	acceptKinds  map[string]bool // kinds to answer "yes" to
	acceptString string          // for JSON output: how this UI was configured
}

func (u *recordingUI) Progress(prober.Progress) {}

func (u *recordingUI) Ask(ctx context.Context, q prober.Question) (prober.Answer, error) {
	// Determine how to answer: accept all kinds, accept specific kind, or decline all
	var answered string
	var answer prober.Answer

	if u.acceptKinds[q.Kind] || u.acceptKinds["all"] {
		// Accept this question
		answered = "auto-accept"
		// Answer yes to the question based on its kind
		answer = u.answerYes(q)
	} else {
		// Decline this question
		answered = "no"
		answer = prober.Answer{ID: "cancel"}
	}

	u.mu.Lock()
	u.questions = append(u.questions, question{Kind: q.Kind, Text: q.Text, Answered: answered})
	u.mu.Unlock()
	return answer, nil
}

// answerYes returns the appropriate "yes" answer for the given question kind.
func (u *recordingUI) answerYes(q prober.Question) prober.Answer {
	switch q.Kind {
	case "redirect", "imagehost", "selfhosted":
		// These questions accept a "continue" option
		return prober.Answer{ID: "continue"}
	case "theme":
		// For theme questions, pick the first option that is not "Stop"/"cancel"
		for _, opt := range q.Options {
			if opt.ID != "cancel" && opt.ID != "stop" {
				return prober.Answer{ID: opt.ID}
			}
		}
		// Fallback: if somehow no other option exists, decline
		return prober.Answer{ID: "cancel"}
	default:
		// Unknown kind: accept with "continue" if it exists, else decline
		for _, opt := range q.Options {
			if opt.ID == "continue" {
				return prober.Answer{ID: "continue"}
			}
		}
		// Fallback
		return prober.Answer{ID: "cancel"}
	}
}

// readSites parses the -in file: blank lines and lines starting with # are
// skipped, and each remaining line is either "name<TAB>url" or just "url"
// (the URL itself becomes the name).
func readSites(path string) ([]site, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var sites []site
	sc := bufio.NewScanner(f)
	// Long lines should not truncate silently; 1MiB is generous for a URL list.
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		var name, url string
		if i := strings.IndexByte(line, '\t'); i >= 0 {
			name = strings.TrimSpace(line[:i])
			url = strings.TrimSpace(line[i+1:])
		} else {
			url = trimmed
		}
		if url == "" {
			return nil, fmt.Errorf("%s:%d: no URL on this line", path, lineNo)
		}
		if name == "" {
			name = url
		}
		sites = append(sites, site{Name: name, URL: url})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return sites, nil
}

// themeRegistry mirrors backend/cmd/quired/main.go's themeRegistry: every
// theme this build ships, over one guarded client. Kept in step with that
// list by hand — see OPEN in the tool's report if it drifts.
func themeRegistry(client *fetch.Client) *theme.Registry {
	reg := theme.NewRegistry()
	reg.MustRegister(madara.New(client))
	reg.MustRegister(mangathemesia.New(client))
	reg.MustRegister(mangakakalot.New(client))
	reg.MustRegister(mangadex.New(client))
	reg.MustRegister(weebcentral.New(client))
	reg.MustRegister(webtoons.New(client))
	reg.MustRegister(fanfox.New(client))
	reg.MustRegister(comick.New(client))
	reg.MustRegister(doujinreader.New(client))
	reg.MustRegister(shelfmark.New(client))
	reg.MustRegister(asurascans.New(client))
	reg.MustRegister(globalcomix.New(client))
	reg.MustRegister(generic.New(client))
	return reg
}

// runner is the slice of *prober.Prober that probeOne needs. It exists so a
// test can exercise the panic and timeout handling below against a fake that
// never touches the network, without needing a real Prober to misbehave.
type runner interface {
	Run(ctx context.Context, rawurl string, ui prober.UI) (prober.Result, error)
}

// parseAccept parses the -accept flag value into a set of kinds to accept.
// It accepts "all" (to accept all questions), a comma-separated list of kinds,
// or an empty string (default: accept nothing).
// Unknown kinds are still accepted if they appear; the flag does not validate
// against a closed list.
func parseAccept(acceptFlag string) map[string]bool {
	kinds := make(map[string]bool)
	if acceptFlag == "" {
		return kinds // empty: accept nothing
	}
	if acceptFlag == "all" {
		kinds["all"] = true
		return kinds
	}
	// Comma-separated list of kinds
	parts := strings.Split(acceptFlag, ",")
	for _, part := range parts {
		kind := strings.TrimSpace(part)
		if kind != "" {
			kinds[kind] = true
		}
	}
	return kinds
}

// probeOne runs a single site through the prober, bulletproofed: a panic
// anywhere in the probe is recovered and turned into a result rather than
// taking the whole run down, and the call is raced against timeout so one
// hanging host cannot stall the others.
func probeOne(ctx context.Context, p runner, s site, timeout time.Duration, acceptKinds map[string]bool) result {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ui := &recordingUI{
		acceptKinds: acceptKinds,
	}
	type outcome struct {
		res prober.Result
		err error
	}
	done := make(chan outcome, 1)

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				done <- outcome{err: fmt.Errorf("panic: %v\n%s", rec, debug.Stack())}
			}
		}()
		res, err := p.Run(ctx, s.URL, ui)
		done <- outcome{res: res, err: err}
	}()

	var r result
	r.Name = s.Name
	r.URL = s.URL

	select {
	case o := <-done:
		if o.err != nil {
			r.RunError = o.err.Error()
		} else {
			r.Verdict = o.res.Verdict
			r.Cancelled = o.res.Cancelled
			r.ThemeID = o.res.ThemeID
			r.ThemeScores = o.res.ThemeScores
			r.Warnings = o.res.Warnings
			r.Detail = o.res.Detail
		}
	case <-ctx.Done():
		// The goroutine above is abandoned; it will finish (or not) on its own.
		// It still holds no lock the rest of the run needs, since host
		// serialization is applied by the caller before probeOne is invoked.
		r.RunError = fmt.Sprintf("timed out after %s", timeout)
	}

	ui.mu.Lock()
	r.Questions = append([]question(nil), ui.questions...)
	ui.mu.Unlock()

	r.DurationMS = time.Since(start).Milliseconds()
	r.Group = classify(&r)
	return r
}

// runAll probes every site, at most parallel different hosts at once, and
// never two probes against the same host concurrently — that would subvert
// the client's own per-host rate limiting, which is the whole point of
// measuring through the real fetcher. Results are returned in input order.
func runAll(ctx context.Context, p runner, sites []site, timeout time.Duration, parallel int, acceptKinds map[string]bool) []result {
	results := make([]result, len(sites))

	type job struct {
		idx int
		s   site
	}
	jobs := make(chan job)

	var hostMu sync.Map // host string -> *sync.Mutex
	lockFor := func(host string) *sync.Mutex {
		m := &sync.Mutex{}
		actual, _ := hostMu.LoadOrStore(host, m)
		return actual.(*sync.Mutex)
	}

	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				host := hostOf(j.s.URL)
				lk := lockFor(host)
				lk.Lock()
				results[j.idx] = probeOne(ctx, p, j.s, timeout, acceptKinds)
				lk.Unlock()
			}
		}()
	}

	for i, s := range sites {
		jobs <- job{idx: i, s: s}
	}
	close(jobs)
	wg.Wait()

	return results
}

// hostOf is a best-effort host key for serializing same-host probes. It does
// not need to be a fully correct URL parse: two sites sharing a host is the
// only thing it has to catch, and a wrong guess only costs extra
// serialization, never correctness of the probe itself.
func hostOf(rawurl string) string {
	u := rawurl
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	return strings.ToLower(u)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("probe-batch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := fs.String("in", "", "input file: one `name<TAB>url` or `url` per line (required)")
	out := fs.String("out", "", "optional path to write the full results as JSON")
	timeout := fs.Duration("timeout", 90*time.Second, "per-site timeout")
	parallel := fs.Int("parallel", 4, "number of different hosts to probe concurrently")
	accept := fs.String("accept", "", "comma-separated question kinds to auto-accept (e.g., 'redirect,imagehost' or 'all'); default: none")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" {
		fmt.Fprintln(stderr, "probe-batch: -in is required")
		fs.Usage()
		return 2
	}
	if *parallel < 1 {
		*parallel = 1
	}

	acceptKinds := parseAccept(*accept)

	sites, err := readSites(*in)
	if err != nil {
		fmt.Fprintf(stderr, "probe-batch: reading %s: %v\n", *in, err)
		return 1
	}
	if len(sites) == 0 {
		fmt.Fprintf(stderr, "probe-batch: %s has no sites to probe\n", *in)
		return 1
	}

	// A quiet logger: this tool's whole point is the table on stdout, and the
	// client's own info-level chatter about robots consultation would bury it.
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	client := fetch.NewClient(fetch.Options{Version: "probe-batch/dev", Logger: log})
	reg := themeRegistry(client)
	p := prober.New(prober.Options{Fetcher: client, Registry: reg})

	results := runAll(context.Background(), p, sites, *timeout, *parallel, acceptKinds)

	printTable(stdout, results)
	printSummary(stdout, results)

	if *out != "" {
		b, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "probe-batch: marshalling results: %v\n", err)
			return 0 // the run itself completed; only writing the dump failed
		}
		if err := os.WriteFile(*out, b, 0o644); err != nil {
			fmt.Fprintf(stderr, "probe-batch: writing %s: %v\n", *out, err)
		}
	}

	// The run completed; individual site failures are the data, not a reason
	// to exit non-zero.
	return 0
}

// topScores returns up to n theme IDs from scores, highest first.
func topScores(scores map[string]int, n int) []theme.Score {
	out := make([]theme.Score, 0, len(scores))
	for id, sc := range scores {
		out = append(out, theme.Score{ThemeID: id, Score: sc})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ThemeID < out[j].ThemeID
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\t", " ")
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func printTable(w io.Writer, results []result) {
	fmt.Fprintf(w, "%-28s %-18s %-14s %-32s %-30s %s\n",
		"NAME", "VERDICT", "THEME", "TOP SCORES", "QUESTIONS", "DETAIL")
	for _, r := range results {
		verdict := r.Verdict
		switch {
		case r.RunError != "":
			verdict = "ERROR"
		case r.Cancelled:
			verdict = "cancelled"
		case verdict == "":
			verdict = "-"
		}

		var scoreParts []string
		for _, s := range topScores(r.ThemeScores, 3) {
			scoreParts = append(scoreParts, fmt.Sprintf("%s:%d", s.ThemeID, s.Score))
		}
		scores := strings.Join(scoreParts, " ")

		// Show questions with their answers
		questions := "-"
		if len(r.Questions) > 0 {
			parts := make([]string, 0, len(r.Questions))
			for _, q := range r.Questions {
				// Show kind(answer)
				if q.Answered == "auto-accept" {
					parts = append(parts, q.Kind+"(yes)")
				} else {
					parts = append(parts, q.Kind+"(no)")
				}
			}
			questions = strings.Join(parts, " ")
		}

		detail := r.Detail
		if r.RunError != "" {
			detail = r.RunError
		}

		themeID := r.ThemeID
		if themeID == "" {
			themeID = "-"
		}

		fmt.Fprintf(w, "%-28s %-18s %-14s %-32s %-30s %s\n",
			truncate(r.Name, 28), truncate(verdict, 18), truncate(themeID, 14),
			truncate(scores, 32), truncate(questions, 30), truncate(detail, 100))
	}
}

func printSummary(w io.Writer, results []result) {
	byGroup := map[string][]result{}
	for _, r := range results {
		byGroup[r.Group] = append(byGroup[r.Group], r)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "SUMMARY")
	for _, g := range groupOrder {
		rs := byGroup[g]
		fmt.Fprintf(w, "  %s (%d)\n", g, len(rs))
		if len(rs) == 0 {
			continue
		}

		if g == groupWorking || g == groupDegraded {
			// Grouping by matched theme is the useful cut here: "these N are
			// Madara, these M are MangaThemesia" is the answer this tool exists
			// to produce.
			byTheme := map[string][]string{}
			for _, r := range rs {
				byTheme[r.ThemeID] = append(byTheme[r.ThemeID], r.Name)
			}
			themes := make([]string, 0, len(byTheme))
			for t := range byTheme {
				themes = append(themes, t)
			}
			sort.Strings(themes)
			for _, t := range themes {
				fmt.Fprintf(w, "    %-16s %s\n", t, strings.Join(byTheme[t], ", "))
			}
			continue
		}

		names := make([]string, 0, len(rs))
		for _, r := range rs {
			names = append(names, r.Name)
		}
		sort.Strings(names)
		fmt.Fprintf(w, "    %s\n", strings.Join(names, ", "))
	}
}
