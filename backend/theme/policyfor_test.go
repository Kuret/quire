package theme_test

import (
	"net/http"
	"testing"

	"github.com/rickl/quire/backend/theme"
)

// headeredTheme answers SourceHeaders and nothing else.
type headeredTheme struct {
	plainTheme
	headers http.Header
}

func (h headeredTheme) SourceHeaders(*theme.Source) http.Header { return h.headers }

// cookieTheme answers CookieUser and nothing else.
type cookieTheme struct {
	plainTheme
	use bool
}

func (c cookieTheme) UsesCookies(*theme.Source) bool { return c.use }

func TestPolicyForWithoutEitherInterfaceIsPlainPolicy(t *testing.T) {
	s := &theme.Source{BaseURL: "https://example.invalid"}
	p, err := theme.PolicyFor(plainTheme{}, s)
	if err != nil {
		t.Fatalf("PolicyFor: %v", err)
	}
	if p.Headers != nil {
		t.Fatalf("a theme with no SourceHeaders must yield no Headers, got %v", p.Headers)
	}
	if p.Cookies != nil {
		t.Fatalf("a theme with no CookieUser must yield no Cookies jar, got %v", p.Cookies)
	}
}

func TestPolicyForCarriesSourceHeaders(t *testing.T) {
	s := &theme.Source{BaseURL: "https://example.invalid"}
	h := http.Header{"X-Gc-Client": []string{"public-id"}}
	th := headeredTheme{headers: h}

	p, err := theme.PolicyFor(th, s)
	if err != nil {
		t.Fatalf("PolicyFor: %v", err)
	}
	if got := p.Headers.Get("X-Gc-Client"); got != "public-id" {
		t.Fatalf("Headers[X-Gc-Client] = %q, want %q", got, "public-id")
	}
}

func TestPolicyForOnlyAttachesCookiesWhenTheThemeAsksForThem(t *testing.T) {
	s := &theme.Source{BaseURL: "https://example.invalid"}

	p, err := theme.PolicyFor(cookieTheme{use: false}, s)
	if err != nil {
		t.Fatalf("PolicyFor: %v", err)
	}
	if p.Cookies != nil {
		t.Fatalf("UsesCookies() == false must yield no jar, got %v", p.Cookies)
	}

	p, err = theme.PolicyFor(cookieTheme{use: true}, s)
	if err != nil {
		t.Fatalf("PolicyFor: %v", err)
	}
	if p.Cookies == nil {
		t.Fatal("UsesCookies() == true must yield a jar")
	}
}

// TestPolicyForCookieJarIsStableAndNeverSharedAcrossSources is the mutation
// (c) test: a cookie must never be able to cross from one source's jar to
// another's. PolicyFor is the only thing that hands a theme a jar, so the
// property to pin is that (1) the same Source always gets back the same jar
// instance, so a session survives across calls, and (2) two different
// Sources — even driven by the identical theme value — never get back the
// same jar instance, so nothing set on one can ever be read from the other.
func TestPolicyForCookieJarIsStableAndNeverSharedAcrossSources(t *testing.T) {
	th := cookieTheme{use: true}
	s1 := &theme.Source{ID: "s1", BaseURL: "https://one.example.invalid"}
	s2 := &theme.Source{ID: "s2", BaseURL: "https://two.example.invalid"}

	p1a, err := theme.PolicyFor(th, s1)
	if err != nil {
		t.Fatalf("PolicyFor(s1): %v", err)
	}
	p1b, err := theme.PolicyFor(th, s1)
	if err != nil {
		t.Fatalf("PolicyFor(s1) again: %v", err)
	}
	if p1a.Cookies != p1b.Cookies {
		t.Fatal("the same source must get back the same jar on a later call, or no session ever survives across requests")
	}

	p2, err := theme.PolicyFor(th, s2)
	if err != nil {
		t.Fatalf("PolicyFor(s2): %v", err)
	}
	if p2.Cookies == p1a.Cookies {
		t.Fatal("two different sources must never share one cookie jar instance")
	}
}
