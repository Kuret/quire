package prober_test

import (
	"context"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe/prober"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/themetest"
)

// Stage 1 used to end here. A base URL resolving into a private or CGNAT range
// was `blocked_address` and that was that, which left hand-editing
// sources.json as the only way to add a service on your own network — the
// reason theme.Source.SelfHosted had no caller.
//
// These tests are about the offer that replaced the refusal, and about the
// three things that keep the offer from being a way in: the address is named,
// the default is no, and the guard still has the last word.

// The address the probe "resolves" to: CGNAT, which is where a tailnet lives
// and what the 2026-09-20 measurement was against.
const resolvedAddr = "100.79.171.1"

// homeGuard is a stage 1 guard shaped like the real one: it refuses the host's
// address, and it can say which address that was. The crucial half is that it
// *stops* refusing only when the policy carries the confirmation — so a test
// that saw the probe continue saw the guard being asked again, not the probe
// helping itself to a yes.
type homeGuard struct {
	addr  netip.Addr
	calls int
}

func newHomeGuard() *homeGuard {
	return &homeGuard{addr: netip.MustParseAddr(resolvedAddr)}
}

func (g *homeGuard) CheckURL(_ context.Context, u *url.URL, p *fetch.Policy) error {
	g.calls++
	if p != nil && p.SelfHostedHost == u.Hostname() {
		return nil
	}
	return &fetch.GuardError{
		URL:    u.Redacted(),
		Reason: "private addresses are not reachable sources",
		Kind:   fetch.ErrBlockedAddress,
	}
}

func (g *homeGuard) SelfHostableTarget(context.Context, *url.URL) (netip.Addr, bool) {
	return g.addr, true
}

// silentGuard refuses the same way and says nothing else: a guard that does not
// implement the side interface makes no offer. That is the old behaviour, and
// it is what an unrecognised address still gets.
type silentGuard struct{ asked int }

func (g *silentGuard) CheckURL(_ context.Context, u *url.URL, _ *fetch.Policy) error {
	g.asked++
	return &fetch.GuardError{
		URL:    u.Redacted(),
		Reason: "private addresses are not reachable sources",
		Kind:   fetch.ErrBlockedAddress,
	}
}

// answerUI answers the one question with a fixed choice and a fixed typed
// value, and keeps what it was asked.
type answerUI struct {
	id        string
	text      string
	questions []prober.Question
}

func (u *answerUI) Progress(prober.Progress) {}

func (u *answerUI) Ask(_ context.Context, q prober.Question) (prober.Answer, error) {
	u.questions = append(u.questions, q)
	return prober.Answer{ID: u.id, Text: u.text}, nil
}

func withGuard(g prober.AddressGuard) func(*prober.Options) {
	return func(o *prober.Options) { o.Guard = g }
}

// TestPrivateAddressIsOfferedRatherThanRefused is the question itself: what it
// says, what it offers, and which way it leans.
func TestPrivateAddressIsOfferedRatherThanRefused(t *testing.T) {
	ui := &answerUI{id: "cancel"}
	res := run(t, madaraRoutes(), ui, withGuard(newHomeGuard()))

	if len(ui.questions) != 1 {
		t.Fatalf("the probe asked %d questions, want exactly one: %+v", len(ui.questions), ui.questions)
	}
	q := ui.questions[0]
	if q.Kind != "selfhosted" {
		t.Errorf("question kind = %q, want %q", q.Kind, "selfhosted")
	}
	// The address is named plainly. Agreeing to something unnamed is not
	// agreeing to anything.
	if !strings.Contains(q.Text, resolvedAddr) {
		t.Errorf("the question does not name the address it resolved to: %q", q.Text)
	}
	if !strings.Contains(q.Text, "example.invalid") {
		t.Errorf("the question does not name the host asked about: %q", q.Text)
	}
	if !strings.Contains(strings.ToLower(q.Text), "private address") {
		t.Errorf("the question does not say what kind of address it is: %q", q.Text)
	}
	// No is first, because no is the answer already in force.
	if len(q.Options) != 2 || q.Options[0].ID != "cancel" || q.Options[1].ID != "continue" {
		t.Fatalf("options = %+v, want cancel first then continue", q.Options)
	}
	// The proxy rides on the same question: the person adding a host on their
	// own network is the one whose device may not be able to route to it.
	if q.Input == nil || q.Input.Label == "" {
		t.Fatalf("the question offers no field for a proxy: %+v", q.Input)
	}

	// Declining leaves the refusal standing *as itself*: the same verdict and
	// the same sentence the probe ended with before the offer existed, so a
	// user who simply mistyped still gets the wizard's "Edit the address".
	if res.Verdict != theme.VerdictBlockedAddress {
		t.Errorf("declining gave verdict %q, want the refusal that was already in force (%q)",
			res.Verdict, theme.VerdictBlockedAddress)
	}
	if res.Addable || res.Draft != nil {
		t.Fatalf("a declined offer produced an addable source: %+v", res)
	}
}

// TestAnUnansweredOfferIsANo pins the default. silentUI answers "cancel" for an
// unattended probe, and an empty or unrecognised ID means the same thing.
func TestAnUnansweredOfferIsANo(t *testing.T) {
	for _, id := range []string{"", "yes", "continue-ish"} {
		ui := &answerUI{id: id}
		res := run(t, madaraRoutes(), ui, withGuard(newHomeGuard()))
		if id == "continue" {
			continue
		}
		if res.Verdict != theme.VerdictBlockedAddress || res.Addable {
			t.Errorf("answer %q was taken as a yes: %+v", id, res)
		}
	}
}

// TestAGuardThatMakesNoOfferStillRefuses: silence is the old behaviour, which
// is the safe direction for a guard to get by saying nothing.
func TestAGuardThatMakesNoOfferStillRefuses(t *testing.T) {
	ui := &answerUI{id: "continue"} // would say yes, if it were ever asked
	g := &silentGuard{}
	res := run(t, madaraRoutes(), ui, withGuard(g))

	if len(ui.questions) != 0 {
		t.Errorf("a guard that cannot say what the address is still asked the user: %+v", ui.questions)
	}
	if res.Verdict != theme.VerdictBlockedAddress {
		t.Fatalf("verdict = %q, want %q", res.Verdict, theme.VerdictBlockedAddress)
	}
	if res.Addable {
		t.Error("a refused address was addable")
	}
}

// TestAcceptingRecordsTheAddressTheProbeResolved is the point of the whole
// thing: the confirmation carries the evidence of the act, which is the address
// the probe actually saw and the moment it was agreed.
func TestAcceptingRecordsTheAddressTheProbeResolved(t *testing.T) {
	ui := &answerUI{id: "continue"}
	g := newHomeGuard()
	res := run(t, madaraRoutes(), ui, withGuard(g))

	if res.Verdict != theme.VerdictOK || !res.Addable || res.Draft == nil {
		t.Fatalf("accepting did not let the probe finish: verdict=%q addable=%v detail=%q",
			res.Verdict, res.Addable, res.Detail)
	}
	sh := res.Draft.SelfHosted
	if sh == nil {
		t.Fatal("the draft carries no confirmation after the user gave one")
	}
	if sh.ConfirmedAddr != resolvedAddr {
		t.Errorf("ConfirmedAddr = %q, want the address the probe resolved (%s)", sh.ConfirmedAddr, resolvedAddr)
	}
	if !sh.ConfirmedAt.Equal(fixedNow) {
		t.Errorf("ConfirmedAt = %v, want the probe clock", sh.ConfirmedAt)
	}
	// No proxy was typed, so none is recorded. An empty field answers the
	// question just as completely.
	if res.Draft.Proxy != "" {
		t.Errorf("draft proxy = %q, want none", res.Draft.Proxy)
	}
	// The guard was asked twice: once to refuse, once with the confirmation on
	// the policy. The exemption is granted by the guard, never by the probe.
	if g.calls < 2 {
		t.Errorf("the guard was consulted %d time(s); the confirmation must be re-checked by it", g.calls)
	}
	// And the draft is a source like any other.
	if err := registry(nil).Validate(res.Draft); err != nil {
		t.Errorf("the draft does not validate: %v", err)
	}
}

// TestAcceptingWithAProxyRecordsIt: the same question carries the proxy,
// because sending the user back through the flow for one field would be a
// second trip.
func TestAcceptingWithAProxyRecordsIt(t *testing.T) {
	ui := &answerUI{id: "continue", text: " http://localhost:1055 "}
	res := run(t, madaraRoutes(), ui, withGuard(newHomeGuard()))

	if !res.Addable || res.Draft == nil {
		t.Fatalf("the probe did not finish: %+v", res)
	}
	if res.Draft.Proxy != "http://localhost:1055" {
		t.Errorf("draft proxy = %q, want the one typed (trimmed)", res.Draft.Proxy)
	}
	if res.Draft.SelfHosted == nil {
		t.Error("the confirmation went missing when a proxy came with it")
	}
	if err := registry(nil).Validate(res.Draft); err != nil {
		t.Errorf("the draft does not validate: %v", err)
	}
}

// TestAnUnusableProxyEndsTheProbe. Refused in the words of the field it was
// typed into, rather than carried into a request that would fail as a timeout
// on a network the user knows is reachable.
func TestAnUnusableProxyEndsTheProbe(t *testing.T) {
	for _, raw := range []string{"localhost:1055", "socks4://127.0.0.1:1080", "file:///etc/passwd"} {
		ui := &answerUI{id: "continue", text: raw}
		res := run(t, madaraRoutes(), ui, withGuard(newHomeGuard()))

		if res.Verdict != theme.VerdictInvalidURL {
			t.Errorf("proxy %q: verdict = %q, want %q (%s)", raw, res.Verdict, theme.VerdictInvalidURL, res.Detail)
		}
		if res.Addable || res.Draft != nil {
			t.Errorf("proxy %q: an unusable proxy still produced a source: %+v", raw, res)
		}
		if !strings.Contains(res.Detail, "proxy") {
			t.Errorf("proxy %q: the detail does not say what was wrong: %q", raw, res.Detail)
		}
	}
}

// TestAProxyIsNotOfferedWithoutTheQuestion: an ordinary site, reachable on a
// public address, is never asked any of this. The flow that was added must not
// appear in the flow that already worked.
func TestAProxyIsNotOfferedWithoutTheQuestion(t *testing.T) {
	ui := &answerUI{id: "continue"}
	res := run(t, madaraRoutes(), ui) // the default allowGuard: nothing is refused

	if len(ui.questions) != 0 {
		t.Fatalf("an ordinary site was asked %d question(s): %+v", len(ui.questions), ui.questions)
	}
	if res.Draft == nil {
		t.Fatal("the ordinary path stopped producing a draft")
	}
	if res.Draft.SelfHosted != nil || res.Draft.Proxy != "" {
		t.Errorf("an ordinary source carries a confirmation or a proxy: %+v", res.Draft)
	}
}

// TestARedirectOffTheHostDropsWhatWasConfirmed.
//
// A confirmation is given for the host the user typed, and a proxy is a route
// to that host. A redirect that lands somewhere else is the *site* choosing a
// host, so carrying either of them across would hand content the exemption —
// the one thing none of this may do. They are dropped, and the new host can be
// asked about on its own terms.
func TestARedirectOffTheHostDropsWhatWasConfirmed(t *testing.T) {
	r := madaraRoutes()
	r["GET /"] = themetest.Route{File: "home-madara.html", FinalURL: "https://elsewhere.invalid/"}
	// The listing stage 5 lands on differs once the base host has changed, so
	// the same two fixtures are routed under the series it picks there.
	r["GET /manga/salt-and-cedar/"] = themetest.Route{File: "series.html"}
	r["POST /manga/salt-and-cedar/ajax/chapters/"] = themetest.Route{File: "chapters-ajax.html"}
	r["GET /manga/the-lantern-keeper/chapter-3-5/"] = themetest.Route{File: "reader.html"}

	// "continue" answers both questions: yes it is mine, and yes use the host
	// it redirected to.
	ui := &answerUI{id: "continue", text: "http://localhost:1055"}
	res := run(t, r, ui, withGuard(newHomeGuard()))

	if len(ui.questions) != 2 {
		t.Fatalf("questions asked = %d, want the self-hosted offer and the redirect: %+v",
			len(ui.questions), ui.questions)
	}
	if res.Draft == nil {
		t.Fatalf("no draft: %+v", res)
	}
	if res.Draft.SelfHosted != nil {
		t.Errorf("the confirmation followed the redirect to another host: %+v", res.Draft.SelfHosted)
	}
	if res.Draft.Proxy != "" {
		t.Errorf("the proxy followed the redirect to another host: %q", res.Draft.Proxy)
	}
}

// unresolvableGuard is the device, as measured on 2026-09-20: the host does not
// resolve here and never will, because the name lives inside a mesh the kernel
// has no route into. It relents once the confirmation is on the policy — which
// for the real guard is the point at which it stops resolving the name at all
// and hands it to the proxy.
type unresolvableGuard struct {
	calls int
	// proxied records whether the policy carried a proxy when the guard finally
	// said yes. Without one the real guard would still be trying to resolve.
	proxied bool
}

func (g *unresolvableGuard) CheckURL(_ context.Context, u *url.URL, p *fetch.Policy) error {
	g.calls++
	if p != nil && p.SelfHostedHost == u.Hostname() && p.Proxy != nil {
		g.proxied = true
		return nil
	}
	return &fetch.GuardError{
		URL:        u.Redacted(),
		Reason:     `cannot resolve "example.invalid": no such host`,
		Kind:       fetch.ErrInvalidURL,
		Unresolved: true,
	}
}

// TestALookupFailureIsOfferedToo. The refusal that actually happens on the
// device is not "that address is private" but "that name does not resolve": the
// source is a mesh name, the tablet's resolver is public DNS, and the userspace
// VPN installs no resolver because there is no TUN device to install one for.
// Offering the question only for a private address meant the probe died with a
// lookup error and the proxy that makes it work was never asked for.
func TestALookupFailureIsOfferedToo(t *testing.T) {
	ui := &answerUI{id: "cancel"}
	res := run(t, madaraRoutes(), ui, withGuard(&unresolvableGuard{}))

	// Declining leaves the lookup failure standing, exactly as before the offer
	// existed — a mistyped domain lands here too, and "Edit the address" is
	// what that user needs.
	if res.Verdict != theme.VerdictInvalidURL || res.Addable {
		t.Errorf("declining gave %+v, want the lookup failure it started as", res)
	}

	if len(ui.questions) != 1 {
		t.Fatalf("a lookup failure asked %d questions, want one: %+v", len(ui.questions), ui.questions)
	}
	q := ui.questions[0]
	if q.Kind != "selfhosted" {
		t.Errorf("question kind = %q, want selfhosted", q.Kind)
	}
	// It says what happened, and does not claim an address nobody resolved.
	if !strings.Contains(q.Text, "look up") || !strings.Contains(q.Text, "example.invalid") {
		t.Errorf("the question does not say the name could not be looked up: %q", q.Text)
	}
	if strings.Contains(q.Text, "resolves to") {
		t.Errorf("the question claims an address for a name that resolved to nothing: %q", q.Text)
	}
	if q.Input == nil || !strings.Contains(q.Input.Label, "proxy") {
		t.Fatalf("no proxy was asked for, which is the only way through here: %+v", q.Input)
	}
}

// TestALookupFailureAcceptedWithAProxyProceeds, and records the confirmation
// that names no address because there was none to name.
func TestALookupFailureAcceptedWithAProxyProceeds(t *testing.T) {
	ui := &answerUI{id: "continue", text: "http://localhost:1055"}
	g := &unresolvableGuard{}
	res := run(t, madaraRoutes(), ui, withGuard(g))

	if res.Verdict != theme.VerdictOK || !res.Addable || res.Draft == nil {
		t.Fatalf("the probe did not finish: verdict=%q detail=%q", res.Verdict, res.Detail)
	}
	if !g.proxied {
		t.Error("the guard was re-asked without the proxy on the policy")
	}
	sh := res.Draft.SelfHosted
	if sh == nil || !sh.ViaProxy {
		t.Fatalf("the draft's confirmation = %+v, want one recorded as reached through a proxy", sh)
	}
	if sh.ConfirmedAddr != "" {
		t.Errorf("ConfirmedAddr = %q; nothing resolved, so nothing may be recorded", sh.ConfirmedAddr)
	}
	if !sh.ConfirmedAt.Equal(fixedNow) {
		t.Errorf("ConfirmedAt = %v, want the probe clock", sh.ConfirmedAt)
	}
	if res.Draft.Proxy != "http://localhost:1055" {
		t.Errorf("draft proxy = %q", res.Draft.Proxy)
	}
	if err := registry(nil).Validate(res.Draft); err != nil {
		t.Errorf("the draft does not validate: %v", err)
	}
}

// TestALookupFailureAcceptedWithoutAProxyStops. Saying yes does not make a name
// resolvable. The honest answer is the lookup failure, with the one thing that
// would fix it named — and nothing is recorded, because nothing was established.
func TestALookupFailureAcceptedWithoutAProxyStops(t *testing.T) {
	ui := &answerUI{id: "continue"} // yes, but no proxy typed
	res := run(t, madaraRoutes(), ui, withGuard(&unresolvableGuard{}))

	if res.Verdict != theme.VerdictInvalidURL {
		t.Fatalf("verdict = %q, want %q (%s)", res.Verdict, theme.VerdictInvalidURL, res.Detail)
	}
	if res.Addable || res.Draft != nil {
		t.Fatalf("a source was offered with no way to reach it: %+v", res)
	}
	if !strings.Contains(res.Detail, "proxy") {
		t.Errorf("the detail does not name what would fix it: %q", res.Detail)
	}
}

// TestAPrivateAddressAcceptedWithoutAProxyStillWorks: the case that already
// worked must keep working on its own terms — a resolvable, private address
// needs no proxy, and the confirmation records the address as before.
func TestAPrivateAddressAcceptedWithoutAProxyStillWorks(t *testing.T) {
	ui := &answerUI{id: "continue"}
	res := run(t, madaraRoutes(), ui, withGuard(newHomeGuard()))

	if !res.Addable || res.Draft == nil || res.Draft.SelfHosted == nil {
		t.Fatalf("a confirmed private address no longer adds: %+v", res)
	}
	if res.Draft.SelfHosted.ConfirmedAddr != resolvedAddr {
		t.Errorf("ConfirmedAddr = %q, want %q", res.Draft.SelfHosted.ConfirmedAddr, resolvedAddr)
	}
	if res.Draft.SelfHosted.ViaProxy {
		t.Error("an address was resolved, so the record must not claim it was reached through a proxy")
	}
	if res.Draft.Proxy != "" {
		t.Errorf("draft proxy = %q, want none", res.Draft.Proxy)
	}
}
