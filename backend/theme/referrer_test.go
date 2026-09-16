package theme_test

import (
	"context"
	"testing"

	"github.com/rickl/quire/backend/fetch"
	"github.com/rickl/quire/backend/probe"
	"github.com/rickl/quire/backend/theme"
)

// plainTheme implements theme.Theme and nothing else. It is the "this theme has
// no page to name" case PLAN §7.6 turns on: no PageReferrer, therefore no
// header.
type plainTheme struct{}

func (plainTheme) ID() string                  { return "plain" }
func (plainTheme) Fingerprint(*probe.Page) int { return 0 }
func (plainTheme) Search(context.Context, *theme.Source, string, int) ([]theme.SeriesStub, error) {
	return nil, nil
}
func (plainTheme) Series(context.Context, *theme.Source, string) (*theme.Series, error) {
	return nil, nil
}
func (plainTheme) Chapters(context.Context, *theme.Source, string) ([]theme.Chapter, error) {
	return nil, nil
}
func (plainTheme) Pages(context.Context, *theme.Source, string) ([]string, error) { return nil, nil }
func (plainTheme) AllowedHosts() []string                                         { return nil }
func (plainTheme) SuggestedName() string                                          { return "" }

// referringTheme answers per chapter, so "the right chapter's referrer" is
// something a test can tell apart from "some chapter's referrer".
type referringTheme struct {
	plainTheme
	byChapter map[string]string
}

func (r referringTheme) PageReferer(_ *theme.Source, chapterID string) string {
	return r.byChapter[chapterID]
}

func TestPageRefererForWithoutTheInterfaceSendsNothing(t *testing.T) {
	ref, err := theme.PageRefererFor(plainTheme{}, &theme.Source{BaseURL: "https://example.invalid"}, "c1")
	if err != nil {
		t.Fatalf("PageRefererFor: %v", err)
	}
	if !ref.IsZero() {
		t.Fatalf("a theme with no PageReferrer must yield the zero Referrer, got %q", ref.String())
	}
}

func TestPageRefererForEmptyAnswerSendsNothing(t *testing.T) {
	th := referringTheme{byChapter: map[string]string{"c1": ""}}
	ref, err := theme.PageRefererFor(th, &theme.Source{}, "c1")
	if err != nil {
		t.Fatalf("PageRefererFor: %v", err)
	}
	if !ref.IsZero() {
		t.Fatalf(`a theme answering "" must yield the zero Referrer, got %q`, ref.String())
	}
}

func TestPageRefererForNamesThatChaptersPage(t *testing.T) {
	th := referringTheme{byChapter: map[string]string{
		"c1": "https://example.invalid/viewer/1",
		"c2": "https://example.invalid/viewer/2",
	}}
	for id, want := range th.byChapter {
		ref, err := theme.PageRefererFor(th, &theme.Source{}, id)
		if err != nil {
			t.Fatalf("PageRefererFor(%s): %v", id, err)
		}
		if ref.String() != want {
			t.Fatalf("PageRefererFor(%s) = %q, want %q", id, ref.String(), want)
		}
	}
}

func TestPageRefererForRejectsAnUnusableAnswer(t *testing.T) {
	th := referringTheme{byChapter: map[string]string{"c1": "/viewer/1"}}
	ref, err := theme.PageRefererFor(th, &theme.Source{}, "c1")
	if err == nil {
		t.Fatal("a relative page URL must be reported, not sent")
	}
	if !ref.IsZero() {
		t.Fatalf("a rejected answer must still yield the zero Referrer, got %q", ref.String())
	}
}

func TestDiscoveryFetcherKeepsTheReferrerWhileDowngradingTheKind(t *testing.T) {
	rec := &kindRecorder{}
	f := theme.DiscoveryFetcher(rec)

	ref, err := fetch.PageReferrer("https://example.invalid/viewer/7")
	if err != nil {
		t.Fatalf("PageReferrer: %v", err)
	}
	if _, err := f.GetRetrievalFrom(context.Background(), nil, "https://cdn.example.invalid/1.jpg", ref); err != nil {
		t.Fatalf("GetRetrievalFrom: %v", err)
	}

	if want := []string{"discovery"}; len(rec.kinds) != 1 || rec.kinds[0] != want[0] {
		t.Fatalf("kinds = %v, want %v", rec.kinds, want)
	}
	if len(rec.referrers) != 1 || rec.referrers[0].String() != ref.String() {
		t.Fatalf("referrers = %v, want [%q]", rec.referrers, ref.String())
	}
}
