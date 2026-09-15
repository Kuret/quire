package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rickl/quire/backend/probe/prober"
	"github.com/rickl/quire/backend/theme"
)

// ReprobeCooldown is the shortest gap between two automatic re-probes of the
// same source.
//
// A re-probe is six stages of requests against a site that may already be
// struggling, and PLAN §7.4 classifies it as *discovery* — the politest
// category we have. Doing it every time a listing comes back empty would turn
// one bad afternoon on a site into a small crawl.
const ReprobeCooldown = 15 * time.Minute

// reprobeReason is why a re-probe was triggered, for the log. It is not shown
// to the user: what they see is the verdict.
type reprobeReason string

const (
	reasonEmptyListing  reprobeReason = "a listing came back empty"
	reasonEmptyChapters reprobeReason = "a series came back with no chapters"
)

// reprobeState tracks when each source was last re-probed.
type reprobeState struct {
	mu   sync.Mutex
	last map[string]time.Time
}

// allow reports whether a source may be re-probed now, and records the attempt.
func (r *reprobeState) allow(sourceID string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last == nil {
		r.last = map[string]time.Time{}
	}
	if at, ok := r.last[sourceID]; ok && now.Sub(at) < ReprobeCooldown {
		return false
	}
	r.last[sourceID] = now
	return true
}

// maybeReprobe re-runs the probe when a source has started coming back empty,
// and reports what it found in plain language.
//
// # The trigger, and what is deliberately not one
//
// PLAN §6 M7 says "when a source starts returning empty results". The useful
// reading of that is narrower than it sounds, because "empty" and "broken" are
// only the same thing for some requests:
//
//   - **A listing with no rows triggers it.** Browse is the site's own
//     front page of series. A site that has one and returns nothing has
//     changed shape, started challenging, or moved.
//   - **A series with no chapters triggers it.** The series page resolved, so
//     the site answered; a series with zero chapters is a parse that no longer
//     matches the page.
//   - **A *search* with no matches does not.** Searching for something a site
//     genuinely does not have is the single most common way to get zero rows,
//     and treating the user's typing as evidence of a broken site would
//     re-probe constantly and cry wolf.
//   - **A transport or HTTP error does not.** That is the blip case: wifi
//     dropped, the tablet woke up mid-request, the site 503'd for a second.
//     The existing retry and backoff in backend/fetch own that, and a re-probe
//     on every flake would be exactly the "don't re-probe on a single
//     transport blip" this is told not to do.
//
// The probe is run headless: automatic re-probing must never put a question on
// screen, because the user did not ask for this and may not be looking.
func (s *Service) maybeReprobe(ctx context.Context, out Sender, src *theme.Source, why reprobeReason) {
	if src == nil || s.reg == nil {
		return
	}
	if !s.reprobes.allow(src.ID, s.now()) {
		s.log.Debug("re-probe skipped, still in cooldown", "source", src.ID, "why", why)
		return
	}

	s.log.Info("re-probing a source that stopped returning results",
		"source", src.ID, "url", src.BaseURL, "why", why)

	p := prober.New(prober.Options{
		Fetcher:  s.fetch,
		Registry: s.reg,
		Guard:    s.guard,
		Now:      s.now,
	})
	res, err := p.Run(ctx, src.BaseURL, headlessUI{})
	if err != nil {
		// A re-probe that cannot complete tells us nothing new, and the user
		// already has the empty result in front of them.
		s.log.Warn("re-probe failed", "source", src.ID, "err", err)
		return
	}

	stored := &theme.ProbeResult{
		Verdict:     res.Verdict,
		Detail:      res.Detail,
		ThemeScores: res.ThemeScores,
		At:          s.now(),
	}
	if err := s.store.SetProbe(src.ID, stored); err != nil {
		s.log.Warn("could not record the re-probe", "source", src.ID, "err", err)
	}

	// A verdict of "still fine" is not worth interrupting anyone with: the user
	// asked for a listing, got nothing, and the honest answer is that the site
	// really does have nothing to show. Anything else is a real explanation for
	// an empty screen and beats "no results found".
	if res.Verdict == theme.VerdictOK {
		s.log.Info("re-probe found nothing wrong", "source", src.ID)
		_ = s.sendSources(out)
		return
	}

	headline := VerdictHeadline(res.Verdict)
	detail := res.Detail
	if detail == "" {
		detail = headline
	}
	s.log.Warn("re-probe explains the empty result",
		"source", src.ID, "verdict", res.Verdict, "detail", detail)

	_ = s.sendError(out, "source_changed", fmt.Sprintf(
		"%s isn’t working the way it used to — %s. %s", src.Name, lower(headline), detail))
	_ = s.sendSources(out)
}

// lower lowercases the first rune of a headline so it reads inside a sentence.
func lower(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] = r[0] - 'A' + 'a'
	}
	return string(r)
}

// headlessUI runs a probe with nobody watching: progress is dropped and any
// question is declined.
//
// Declining is the safe answer for both of PLAN §7.5's questions. One asks
// whether a redirect off the typed domain is acceptable and the other asks
// which of two close themes is right — and a re-probe the user did not request
// has no business answering either on their behalf, still less silently
// following a redirect somewhere they never typed.
type headlessUI struct{}

func (headlessUI) Progress(prober.Progress) {}

func (headlessUI) Ask(context.Context, prober.Question) (prober.Answer, error) {
	return prober.Answer{}, fmt.Errorf("re-probe: not asking the user during an automatic check")
}
