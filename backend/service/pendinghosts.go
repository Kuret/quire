package service

import (
	"strings"
	"sync"
	"time"

	"github.com/rickl/quire/backend/fetch"
)

// pendingHostCap bounds how many distinct hosts one source may have pending
// review at once.
//
// A hostile or merely broken source is exactly the actor schema/source.
// schema.json's allowedHosts is worried about: it supplies URLs Quire
// fetches unattended, and without a bound it could spray enough distinct
// off-domain hostnames to flood both memory and the owner's review list —
// the same shape of problem the fetch-layer limiter already exists for, one
// level up.
//
// Eight, because the owner is a person reading a short list on a source
// list row, not paging through a report: an honest CDN needs one host, or a
// handful when it fronts several kinds of asset, and a source asking for
// more than eight distinct off-domain names in its lifetime has already said
// more about itself than any individual entry would.
//
// The discard rule, once a source is at the cap: a *new* distinct host is
// dropped silently, and every host already pending stays. Dropping the old
// ones to make room would throw away exactly what the owner already has to
// decide about to make space for a name that arrived more recently and is no
// more trustworthy for it; keeping the newest and discarding the rest would
// reward whichever host happened to be sprayed last. Neither is more
// principled than "the first eight distinct hosts get a turn, and that is
// all one source gets."
const pendingHostCap = 8

// pendingHost is one host recorded as refused for being off a source's
// registrable domain — PLAN's record-and-offer design, deliberately not
// prompt-on-first-sight: a fetch runs unattended, in the background, and a
// modal at the moment of refusal would either appear with nobody watching or
// appear so often that "yes" becomes reflex. See fetch.GuardError.OffDomain.
type pendingHost struct {
	Host string `json:"host"`

	// Purpose is what Quire was trying to fetch when the host was refused —
	// "cover" or "page" — so the owner can judge the request rather than a
	// bare hostname. It is derived from fetch.Kind at the point of recording
	// (KindRetrieval is a page image the download queue fetched by name;
	// everything else observed here is a cover, the one KindDiscovery fetch
	// that legitimately reaches outside a source's own domain). It is an
	// approximation of "what for", not a promise that every KindDiscovery
	// refusal is literally a cover — the two paths this ever fires from
	// today are covers.Cache (discovery) and the download queue's page
	// fetch (retrieval), and nothing else in Quire fetches a URL a source's
	// own content supplied.
	Purpose string `json:"purpose"`

	// FirstSeen is when this host was first refused. It is not sent as a
	// deadline or a threat — pending hosts do not expire — only so the owner
	// can tell an old, ignored refusal from one that just happened.
	FirstSeen time.Time `json:"firstSeen"`
}

// purposeForKind maps a fetch.Kind to the plain word pendingHost.Purpose
// carries. See pendingHost.Purpose for what the approximation is and is not.
func purposeForKind(k fetch.Kind) string {
	if k == fetch.KindRetrieval {
		return "page"
	}
	return "cover"
}

// pendingHosts is Quire's in-memory record of hosts refused for being
// off-domain, one list per source.
//
// **It is in-memory only, and deliberately not persisted.** A restart loses
// whatever was pending, and the next background fetch that hits the same
// refusal re-records it — the guard runs on every request, so nothing about
// the refusal itself depends on this list surviving. That self-healing
// property is worth more here than surviving a restart would be: a pending
// list is a *notice*, not a decision the owner has made, and there is
// nothing to lose by asking again except a little memory that a restart
// already reclaims for everything else in this struct. Persisting it would
// also mean a second file, or a second shape squeezed into sources.json,
// carrying data that is only ever a symptom of the guard doing its job — one
// more thing that could disagree with what the guard would say today.
type pendingHosts struct {
	mu    sync.Mutex
	bySrc map[string][]pendingHost
}

func newPendingHosts() *pendingHosts {
	return &pendingHosts{bySrc: map[string][]pendingHost{}}
}

// record notes that sourceID's fetch of kind was refused for host being
// off-domain. Called only from Service.RecordOffDomainHost, which is the
// callback fetch.Client.SetOffDomainHook is given — see there for why the
// guard itself never calls this directly.
//
// A host already pending is left alone rather than re-timestamped: FirstSeen
// answers "how long has this been sitting here", and a source that keeps
// failing the same way should not look freshly discovered on every retry.
func (p *pendingHosts) record(sourceID, host string, kind fetch.Kind, now time.Time) {
	sourceID = strings.TrimSpace(sourceID)
	host = strings.ToLower(strings.TrimSpace(host))
	if sourceID == "" || host == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	list := p.bySrc[sourceID]
	for _, e := range list {
		if e.Host == host {
			return
		}
	}
	if len(list) >= pendingHostCap {
		return
	}
	p.bySrc[sourceID] = append(list, pendingHost{
		Host: host, Purpose: purposeForKind(kind), FirstSeen: now,
	})
}

// forSource returns a copy of sourceID's pending hosts, oldest first. Never
// nil, so a caller can range over it without a guard.
func (p *pendingHosts) forSource(sourceID string) []pendingHost {
	p.mu.Lock()
	defer p.mu.Unlock()
	list := p.bySrc[sourceID]
	out := make([]pendingHost, len(list))
	copy(out, list)
	return out
}

// clearHost removes one host from sourceID's pending list — called once it
// has been allowed or the owner otherwise no longer needs to be asked about
// it. Absent is not an error: the caller does not have to check first.
func (p *pendingHosts) clearHost(sourceID, host string) {
	host = strings.ToLower(strings.TrimSpace(host))
	p.mu.Lock()
	defer p.mu.Unlock()
	list := p.bySrc[sourceID]
	for i, e := range list {
		if e.Host == host {
			p.bySrc[sourceID] = append(list[:i:i], list[i+1:]...)
			return
		}
	}
}

// clearSource drops every pending host for sourceID — called when the source
// itself is removed, so a deleted source's stale refusals do not linger for
// an id nothing can act on any more.
func (p *pendingHosts) clearSource(sourceID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.bySrc, sourceID)
}

// RecordOffDomainHost is the sink fetch.Client.SetOffDomainHook is given. It
// is the only place the fetch layer's news that a request was refused for
// being off-domain reaches the service, and it does the one thing PLAN's
// record-and-offer design asks of it: note the fact and change nothing else.
// The refused fetch has already failed and stays failed — nothing here
// retries it, and nothing here grants anything. See pendingHosts.record for
// the cap and pendingHosts' own doc comment for why this is in memory only.
//
// It is exported because main.go wires it in after both the fetch.Client and
// the Service exist — see fetch.Client.SetOffDomainHook for why that has to
// happen in that order.
func (s *Service) RecordOffDomainHost(sourceID, host string, kind fetch.Kind) {
	s.pending.record(sourceID, host, kind, s.now())
}
