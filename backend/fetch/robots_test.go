package fetch_test

import (
	"testing"

	"github.com/rickl/quire/backend/fetch"
)

func TestRobotsAllowDeny(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		body  string
		paths map[string]bool // path -> allowed
	}{
		{
			name: "empty file allows everything",
			body: "",
			paths: map[string]bool{
				"/":          true,
				"/manga/abc": true,
			},
		},
		{
			name: "wildcard group blanket deny",
			body: "User-agent: *\nDisallow: /\n",
			paths: map[string]bool{
				"/":          false,
				"/manga/abc": false,
			},
		},
		{
			name: "empty disallow means allow all",
			body: "User-agent: *\nDisallow:\n",
			paths: map[string]bool{
				"/":          true,
				"/wp-admin/": true,
			},
		},
		{
			name: "prefix deny with a narrower allow",
			body: "User-agent: *\nDisallow: /wp-admin/\nAllow: /wp-admin/admin-ajax.php\n",
			paths: map[string]bool{
				"/wp-admin/":               false,
				"/wp-admin/options.php":    false,
				"/wp-admin/admin-ajax.php": true,
				"/manga/abc":               true,
			},
		},
		{
			name: "longest match wins regardless of order",
			body: "User-agent: *\nAllow: /a/b/c\nDisallow: /a/\nDisallow: /a/b/\n",
			paths: map[string]bool{
				"/a/x":   false,
				"/a/b/x": false,
				"/a/b/c": true,
			},
		},
		{
			name: "star wildcard inside a pattern",
			body: "User-agent: *\nDisallow: /*/private\n",
			paths: map[string]bool{
				"/manga/private": false,
				"/x/y/private/z": false,
				"/manga/public":  true,
			},
		},
		{
			name: "dollar anchors the end of the path",
			body: "User-agent: *\nDisallow: /*.pdf$\n",
			paths: map[string]bool{
				"/files/a.pdf":      false,
				"/files/a.pdf?x=1":  true,
				"/files/a.pdf.html": true,
			},
		},
		{
			name: "query strings participate in matching",
			body: "User-agent: *\nDisallow: /*?s=\n",
			paths: map[string]bool{
				"/?s=hello":    false,
				"/page/2/?s=x": false,
				"/manga/abc":   true,
			},
		},
		{
			name: "a group naming Quire wins over the wildcard group",
			body: "User-agent: *\nDisallow: /\n\nUser-agent: quire\nDisallow: /secret/\n",
			paths: map[string]bool{
				"/manga/abc": true,
				"/secret/x":  false,
			},
		},
		{
			name: "a group for someone else does not apply to us",
			body: "User-agent: somebot\nDisallow: /\n",
			paths: map[string]bool{
				"/manga/abc": true,
			},
		},
		{
			name: "consecutive user-agent lines share one group",
			body: "User-agent: somebot\nUser-agent: quire\nDisallow: /nope/\n",
			paths: map[string]bool{
				"/nope/x": false,
				"/fine/x": true,
			},
		},
		{
			name: "comments and blank lines are ignored",
			body: "# a comment\n\nUser-agent: *   # trailing\nDisallow: /x/ # why\n",
			paths: map[string]bool{
				"/x/1": false,
				"/y/1": true,
			},
		},
		{
			name: "rules before any user-agent line belong to nobody",
			body: "Disallow: /\nUser-agent: *\nAllow: /\n",
			paths: map[string]bool{
				"/anything": true,
			},
		},
		{
			name: "unparseable junk does not deny by accident",
			body: "this is not robots.txt\n<html><body>404</body></html>\n",
			paths: map[string]bool{
				"/manga/abc": true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rules := fetch.ParseRobots(tc.body)
			for path, want := range tc.paths {
				got := true
				if rules != nil {
					got = fetch.RobotsAllows(rules, path)
				}
				if got != want {
					t.Errorf("allowed(%q) = %v, want %v", path, got, want)
				}
			}
		})
	}
}
