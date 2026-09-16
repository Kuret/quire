package theme_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/theme"
	"github.com/rickl/quire/backend/theme/generic"
	"github.com/rickl/quire/backend/theme/madara"
	"github.com/rickl/quire/backend/theme/mangadex"
	"github.com/rickl/quire/backend/theme/mangathemesia"
)

// kindRecorder is a theme.Fetcher that records which PLAN §7.4 kind it was
// asked for, and nothing else.
type kindRecorder struct{ kinds []string }

func (k *kindRecorder) Get(context.Context, *fetch.Policy, string) (*fetch.Response, error) {
	k.kinds = append(k.kinds, "discovery")
	return &fetch.Response{StatusCode: 200}, nil
}

func (k *kindRecorder) GetRetrieval(context.Context, *fetch.Policy, string) (*fetch.Response, error) {
	k.kinds = append(k.kinds, "retrieval")
	return &fetch.Response{StatusCode: 200}, nil
}

func (k *kindRecorder) PostForm(context.Context, *fetch.Policy, string, url.Values) (*fetch.Response, error) {
	k.kinds = append(k.kinds, "post")
	return &fetch.Response{StatusCode: 200}, nil
}

// PLAN §12.2: a watch check is discovery even for a resource that would be
// retrieval had the user tapped it. This is the mechanism that makes that true.
func TestDiscoveryFetcherDowngradesRetrieval(t *testing.T) {
	rec := &kindRecorder{}
	f := theme.DiscoveryFetcher(rec)

	ctx := context.Background()
	if _, err := f.Get(ctx, nil, "https://example.invalid/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.GetRetrieval(ctx, nil, "https://example.invalid/b"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.PostForm(ctx, nil, "https://example.invalid/c", nil); err != nil {
		t.Fatal(err)
	}

	want := []string{"discovery", "discovery", "post"}
	if len(rec.kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", rec.kinds, want)
	}
	for i := range want {
		if rec.kinds[i] != want[i] {
			t.Errorf("call %d was %s, want %s", i, rec.kinds[i], want[i])
		}
	}

	// Wrapping twice is the same wrapper, so a theme that hands its already
	// downgraded fetcher on does not pay for a second layer.
	if theme.DiscoveryFetcher(f) != f {
		t.Error("DiscoveryFetcher wrapped an already-downgraded fetcher")
	}
	if theme.DiscoveryFetcher(nil) != nil {
		t.Error("DiscoveryFetcher(nil) must stay nil")
	}
}

// Every shipped theme has to answer the question, because the watched-series
// check refuses to run against a theme that has not — see
// theme.DiscoveryClassifier. A new theme that forgets this fails here rather
// than silently going unchecked on the device.
func TestEveryShippedThemeCanBeDiscoveryOnly(t *testing.T) {
	rec := &kindRecorder{}
	themes := []theme.Theme{
		madara.New(rec),
		mangathemesia.New(rec),
		mangadex.New(rec),
		generic.New(rec),
	}
	for _, th := range themes {
		dc, ok := th.(theme.DiscoveryClassifier)
		if !ok {
			t.Errorf("%s does not implement theme.DiscoveryClassifier", th.ID())
			continue
		}
		only := dc.DiscoveryOnly()
		if only == nil {
			t.Errorf("%s.DiscoveryOnly() is nil", th.ID())
			continue
		}
		if only == th {
			t.Errorf("%s.DiscoveryOnly() returned the receiver, so the original was mutated", th.ID())
		}
		if only.ID() != th.ID() {
			t.Errorf("%s.DiscoveryOnly().ID() = %q", th.ID(), only.ID())
		}
	}
}
