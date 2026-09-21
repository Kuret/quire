package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickl/quire/backend/probe/prober"
	"github.com/rickl/quire/backend/theme"
)

// No test in this file makes a network request. probeOne and runAll are
// exercised against a fake runner; everything else is pure parsing or
// formatting logic.

func writeTemp(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sites.txt")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadSites(t *testing.T) {
	path := writeTemp(t, strings.Join([]string{
		"# a comment",
		"",
		"Weeb Central\thttps://weebcentral.com",
		"https://example.com/some/path",
		"   ",
		"  Trimmed\t  https://trimmed.example  ",
	}, "\n"))

	sites, err := readSites(path)
	if err != nil {
		t.Fatalf("readSites: %v", err)
	}
	want := []site{
		{Name: "Weeb Central", URL: "https://weebcentral.com"},
		{Name: "https://example.com/some/path", URL: "https://example.com/some/path"},
		{Name: "Trimmed", URL: "https://trimmed.example"},
	}
	if len(sites) != len(want) {
		t.Fatalf("got %d sites, want %d: %+v", len(sites), len(want), sites)
	}
	for i := range want {
		if sites[i] != want[i] {
			t.Errorf("site %d: got %+v, want %+v", i, sites[i], want[i])
		}
	}
}

func TestReadSitesEmptyURL(t *testing.T) {
	path := writeTemp(t, "onlyname\t\n")
	if _, err := readSites(path); err == nil {
		t.Fatal("expected an error for a line with no URL")
	}
}

func TestReadSitesMissingFile(t *testing.T) {
	if _, err := readSites(filepath.Join(t.TempDir(), "nope.txt")); err == nil {
		t.Fatal("expected an error reading a missing file")
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		verdict string
		want    string
	}{
		{theme.VerdictOK, groupWorking},
		{theme.VerdictPartial, groupDegraded},
		{theme.VerdictUnrecognised, groupUnrec},
		{theme.VerdictBlockedChallenge, groupChallenge},
		{theme.VerdictUnreachable, groupUnreachable},
		{theme.VerdictBlockedAddress, groupUnreachable},
		{theme.VerdictRobotsDenied, groupUnreachable},
		{theme.VerdictInvalidURL, groupUnreachable},
		{"", groupUnreachable}, // cancelled, or a run error/panic/timeout
	}
	for _, c := range cases {
		r := &result{Verdict: c.verdict}
		if got := classify(r); got != c.want {
			t.Errorf("classify(%q) = %q, want %q", c.verdict, got, c.want)
		}
	}
}

// fakeRunner lets a test control exactly what "the prober" does, without a
// real Prober or any network path.
type fakeRunner struct {
	res     prober.Result
	err     error
	panic   any
	block   bool // ignores ctx and only returns when the context is done
	askKind string
}

func (f fakeRunner) Run(ctx context.Context, rawurl string, ui prober.UI) (prober.Result, error) {
	if f.askKind != "" {
		_, _ = ui.Ask(ctx, prober.Question{Kind: f.askKind, Text: "a question"})
	}
	if f.panic != nil {
		panic(f.panic)
	}
	if f.block {
		<-ctx.Done()
		return prober.Result{}, ctx.Err()
	}
	return f.res, f.err
}

func TestProbeOneHappyPath(t *testing.T) {
	fr := fakeRunner{
		res: prober.Result{
			Verdict:     theme.VerdictOK,
			ThemeID:     "madara",
			ThemeScores: map[string]int{"madara": 90, "generic": 10},
			Detail:      "worked fine",
		},
	}
	r := probeOne(context.Background(), fr, site{Name: "n", URL: "https://n.example"}, time.Second, make(map[string]bool))
	if r.Verdict != theme.VerdictOK || r.ThemeID != "madara" || r.Group != groupWorking {
		t.Fatalf("unexpected result: %+v", r)
	}
	if r.RunError != "" {
		t.Fatalf("expected no run error, got %q", r.RunError)
	}
}

func TestProbeOneRecordsQuestion(t *testing.T) {
	fr := fakeRunner{
		askKind: "selfhosted",
		res:     prober.Result{Verdict: theme.VerdictUnreachable},
	}
	r := probeOne(context.Background(), fr, site{Name: "n", URL: "https://n.example"}, time.Second, make(map[string]bool))
	if len(r.Questions) != 1 || r.Questions[0].Kind != "selfhosted" {
		t.Fatalf("expected one selfhosted question recorded, got %+v", r.Questions)
	}
	if r.Questions[0].Answered != "no" {
		t.Fatalf("expected question to be declined by default, got %q", r.Questions[0].Answered)
	}
}

func TestProbeOnePanicIsRecovered(t *testing.T) {
	fr := fakeRunner{panic: "boom"}
	r := probeOne(context.Background(), fr, site{Name: "n", URL: "https://n.example"}, time.Second, make(map[string]bool))
	if r.RunError == "" || !strings.Contains(r.RunError, "boom") {
		t.Fatalf("expected the panic to be recorded, got %+v", r)
	}
	if r.Group != groupUnreachable {
		t.Fatalf("a panicked site should classify as unreachable, got %q", r.Group)
	}
}

func TestProbeOneTimeout(t *testing.T) {
	fr := fakeRunner{block: true}
	start := time.Now()
	r := probeOne(context.Background(), fr, site{Name: "slow", URL: "https://slow.example"}, 20*time.Millisecond, make(map[string]bool))
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("probeOne did not return promptly on timeout: %s", elapsed)
	}
	if r.RunError == "" || !strings.Contains(r.RunError, "timed out") {
		t.Fatalf("expected a timeout to be recorded, got %+v", r)
	}
	if r.Group != groupUnreachable {
		t.Fatalf("a timed-out site should classify as unreachable, got %q", r.Group)
	}
}

// TestRunAllDoesNotAbortOnOneFailure is the "bulletproof" requirement: one
// site panicking, one timing out, and one failing outright must not stop the
// others' results from coming back.
func TestRunAllDoesNotAbortOnOneFailure(t *testing.T) {
	sites := []site{
		{Name: "good", URL: "https://good.example"},
		{Name: "panics", URL: "https://panics.example"},
		{Name: "slow", URL: "https://slow.example"},
		{Name: "erroring", URL: "https://erroring.example"},
	}

	// A single fakeRunner can't answer differently per URL, so drive runAll
	// through a small dispatcher keyed on the site's host.
	dispatch := map[string]fakeRunner{
		"good.example":     {res: prober.Result{Verdict: theme.VerdictOK, ThemeID: "generic"}},
		"panics.example":   {panic: "kaboom"},
		"slow.example":     {block: true},
		"erroring.example": {err: context.DeadlineExceeded},
	}
	rr := dispatchRunner{byHost: dispatch}

	results := runAll(context.Background(), rr, sites, 20*time.Millisecond, 4, make(map[string]bool))
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
	if results[0].Group != groupWorking {
		t.Errorf("good: got group %q", results[0].Group)
	}
	if results[1].RunError == "" || results[1].Group != groupUnreachable {
		t.Errorf("panics: got %+v", results[1])
	}
	if results[2].RunError == "" || results[2].Group != groupUnreachable {
		t.Errorf("slow: got %+v", results[2])
	}
	if results[3].RunError == "" || results[3].Group != groupUnreachable {
		t.Errorf("erroring: got %+v", results[3])
	}
}

// dispatchRunner picks a fakeRunner by the URL's host, so runAll's several
// concurrent jobs can each behave differently in one test.
type dispatchRunner struct {
	byHost map[string]fakeRunner
}

func (d dispatchRunner) Run(ctx context.Context, rawurl string, ui prober.UI) (prober.Result, error) {
	fr, ok := d.byHost[hostOf(rawurl)]
	if !ok {
		return prober.Result{}, nil
	}
	return fr.Run(ctx, rawurl, ui)
}

func TestHostOf(t *testing.T) {
	cases := map[string]string{
		"https://Example.com/foo": "example.com",
		"http://example.com:8080": "example.com:8080",
		"example.com/bar?x=1":     "example.com",
		"https://a.example.com/":  "a.example.com",
	}
	for in, want := range cases {
		if got := hostOf(in); got != want {
			t.Errorf("hostOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrintTableAndSummary(t *testing.T) {
	results := []result{
		{Name: "Madara Site", Verdict: theme.VerdictOK, ThemeID: "madara",
			ThemeScores: map[string]int{"madara": 90, "generic": 5}, Group: groupWorking},
		{Name: "Themesia Site", Verdict: theme.VerdictOK, ThemeID: "mangathemesia",
			ThemeScores: map[string]int{"mangathemesia": 95}, Group: groupWorking},
		{Name: "Degraded Site", Verdict: theme.VerdictPartial, ThemeID: "madara",
			Detail: "some pages missing", Group: groupDegraded},
		{Name: "Mystery Site", Verdict: theme.VerdictUnrecognised, Group: groupUnrec},
		{Name: "Blocked Site", Verdict: theme.VerdictBlockedChallenge, Group: groupChallenge},
		{Name: "Dead Site", RunError: "dial tcp: no route to host", Group: groupUnreachable},
	}

	var tbl bytes.Buffer
	printTable(&tbl, results)
	out := tbl.String()
	for _, want := range []string{"Madara Site", "Themesia Site", "Degraded Site", "Mystery Site", "Blocked Site", "Dead Site", "ERROR"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}

	var sum bytes.Buffer
	printSummary(&sum, results)
	summary := sum.String()
	for _, want := range []string{
		groupWorking + " (2)", groupDegraded + " (1)", groupUnrec + " (1)",
		groupChallenge + " (1)", groupUnreachable + " (1)",
		"madara", "mangathemesia", "Mystery Site", "Blocked Site", "Dead Site",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q:\n%s", want, summary)
		}
	}
}

func TestTopScores(t *testing.T) {
	scores := map[string]int{"a": 10, "b": 90, "c": 50, "d": 90}
	top := topScores(scores, 2)
	if len(top) != 2 {
		t.Fatalf("got %d scores, want 2", len(top))
	}
	// b and d tie at 90; the tiebreak is alphabetical, so b comes first.
	if top[0].ThemeID != "b" || top[0].Score != 90 {
		t.Errorf("top[0] = %+v", top[0])
	}
	if top[1].ThemeID != "d" || top[1].Score != 90 {
		t.Errorf("top[1] = %+v", top[1])
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("got %q", got)
	}
	if got := truncate("a very long string indeed", 10); len([]rune(got)) > 10 {
		t.Errorf("truncate did not bound length: %q", got)
	}
	if got := truncate("line1\nline2\ttabbed", 100); strings.ContainsAny(got, "\n\t") {
		t.Errorf("truncate left control characters in: %q", got)
	}
}

func TestRunRejectsMissingInFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("got exit code %d, want 2", code)
	}
}

func TestRunUnreadableInputFileIsNonZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-in", filepath.Join(t.TempDir(), "missing.txt")}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected a non-zero exit when the input file cannot be read")
	}
}

func TestParseAcceptEmpty(t *testing.T) {
	kinds := parseAccept("")
	if len(kinds) != 0 {
		t.Fatalf("empty accept flag should produce empty set, got %v", kinds)
	}
}

func TestParseAcceptAll(t *testing.T) {
	kinds := parseAccept("all")
	if !kinds["all"] {
		t.Fatalf("parseAccept('all') should set kinds['all'], got %v", kinds)
	}
}

func TestParseAcceptSingleKind(t *testing.T) {
	kinds := parseAccept("redirect")
	if !kinds["redirect"] {
		t.Fatalf("parseAccept('redirect') should set kinds['redirect'], got %v", kinds)
	}
	if len(kinds) != 1 {
		t.Fatalf("parseAccept('redirect') should produce exactly one kind, got %d", len(kinds))
	}
}

func TestParseAcceptMultipleKinds(t *testing.T) {
	kinds := parseAccept("redirect,imagehost,selfhosted")
	if !kinds["redirect"] || !kinds["imagehost"] || !kinds["selfhosted"] {
		t.Fatalf("parseAccept should parse all kinds, got %v", kinds)
	}
	if len(kinds) != 3 {
		t.Fatalf("expected 3 kinds, got %d", len(kinds))
	}
}

func TestParseAcceptUnknownKind(t *testing.T) {
	kinds := parseAccept("unknown,redirect")
	if !kinds["unknown"] || !kinds["redirect"] {
		t.Fatalf("parseAccept should accept unknown kinds, got %v", kinds)
	}
}

// TestProbeOneDefaultDeclinesAllQuestions tests that by default, all questions
// are declined. This is the mutation test baseline.
func TestProbeOneDefaultDeclinesAllQuestions(t *testing.T) {
	fr := fakeRunner{
		askKind: "redirect",
		res:     prober.Result{Verdict: theme.VerdictUnreachable},
	}
	r := probeOne(context.Background(), fr, site{Name: "n", URL: "https://n.example"}, time.Second, make(map[string]bool))
	if len(r.Questions) != 1 {
		t.Fatalf("expected one question, got %d", len(r.Questions))
	}
	if r.Questions[0].Answered != "no" {
		t.Fatalf("expected question to be declined by default, got %q", r.Questions[0].Answered)
	}
}

// TestProbeOneAcceptsSpecificKind tests that when a specific kind is in the
// accept set, questions of that kind are auto-accepted.
func TestProbeOneAcceptsSpecificKind(t *testing.T) {
	fr := fakeRunner{
		askKind: "redirect",
		res: prober.Result{
			Verdict: theme.VerdictOK,
			ThemeID: "generic",
		},
	}
	acceptKinds := map[string]bool{"redirect": true}
	r := probeOne(context.Background(), fr, site{Name: "n", URL: "https://n.example"}, time.Second, acceptKinds)
	if len(r.Questions) != 1 {
		t.Fatalf("expected one question, got %d", len(r.Questions))
	}
	if r.Questions[0].Answered != "auto-accept" {
		t.Fatalf("expected question to be auto-accepted, got %q", r.Questions[0].Answered)
	}
}

// TestProbeOneAcceptsAllKinds tests that when "all" is in the accept set,
// questions of any kind are auto-accepted.
func TestProbeOneAcceptsAllKinds(t *testing.T) {
	fr := fakeRunner{
		askKind: "imagehost",
		res:     prober.Result{Verdict: theme.VerdictOK, ThemeID: "generic"},
	}
	acceptKinds := map[string]bool{"all": true}
	r := probeOne(context.Background(), fr, site{Name: "n", URL: "https://n.example"}, time.Second, acceptKinds)
	if len(r.Questions) != 1 {
		t.Fatalf("expected one question, got %d", len(r.Questions))
	}
	if r.Questions[0].Answered != "auto-accept" {
		t.Fatalf("expected all questions to be auto-accepted, got %q", r.Questions[0].Answered)
	}
}

// TestProbeOneIgnoresUnwantedKinds tests that when a specific kind is in the
// accept set, questions of other kinds are still declined.
func TestProbeOneIgnoresUnwantedKinds(t *testing.T) {
	fr := fakeRunner{
		askKind: "imagehost",
		res:     prober.Result{Verdict: theme.VerdictUnreachable},
	}
	acceptKinds := map[string]bool{"redirect": true}
	r := probeOne(context.Background(), fr, site{Name: "n", URL: "https://n.example"}, time.Second, acceptKinds)
	if len(r.Questions) != 1 {
		t.Fatalf("expected one question, got %d", len(r.Questions))
	}
	if r.Questions[0].Answered != "no" {
		t.Fatalf("expected unwanted question to be declined, got %q", r.Questions[0].Answered)
	}
}

// TestTableShowsAnswerStatus tests that the table output shows whether each
// question was declined or auto-accepted.
func TestTableShowsAnswerStatus(t *testing.T) {
	results := []result{
		{
			Name:     "Site1",
			Verdict:  theme.VerdictOK,
			ThemeID:  "madara",
			Group:    groupWorking,
			Questions: []question{
				{Kind: "redirect", Text: "redirect?", Answered: "no"},
				{Kind: "imagehost", Text: "images?", Answered: "auto-accept"},
			},
		},
	}

	var tbl bytes.Buffer
	printTable(&tbl, results)
	out := tbl.String()

	// The table should show both questions with their answers
	if !strings.Contains(out, "redirect(no)") {
		t.Errorf("table missing 'redirect(no)':\n%s", out)
	}
	if !strings.Contains(out, "imagehost(yes)") {
		t.Errorf("table missing 'imagehost(yes)':\n%s", out)
	}
}

// TestJSONOutputIncludesAnswerStatus tests that the JSON output includes the
// Answered field for each question.
func TestJSONOutputIncludesAnswerStatus(t *testing.T) {
	results := []result{
		{
			Name:    "Site1",
			Verdict: theme.VerdictOK,
			ThemeID: "madara",
			Group:   groupWorking,
			Questions: []question{
				{Kind: "redirect", Text: "redirect?", Answered: "no"},
				{Kind: "imagehost", Text: "images?", Answered: "auto-accept"},
			},
		},
	}

	b, err := json.Marshal(results)
	if err != nil {
		t.Fatalf("failed to marshal results: %v", err)
	}

	// Unmarshal to check the structure
	var unmarshalled []result
	if err := json.Unmarshal(b, &unmarshalled); err != nil {
		t.Fatalf("failed to unmarshal results: %v", err)
	}

	if len(unmarshalled[0].Questions) != 2 {
		t.Fatalf("expected 2 questions in unmarshalled result")
	}
	if unmarshalled[0].Questions[0].Answered != "no" {
		t.Errorf("expected 'no', got %q", unmarshalled[0].Questions[0].Answered)
	}
	if unmarshalled[0].Questions[1].Answered != "auto-accept" {
		t.Errorf("expected 'auto-accept', got %q", unmarshalled[0].Questions[1].Answered)
	}
}
