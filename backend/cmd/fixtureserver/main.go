// Command fixtureserver runs Quire's fixture HTTP server by hand.
//
// It serves the committed synthetic fixtures over a real socket and simulates
// the awkward responses — 429 with Retry-After, 5xx, slow, oversized, a
// redirect chain, a robots.txt that disallows a path. The automated version of
// the same thing is the integration test in backend/fixtures; this binary is
// for the times when what you want is curl and a terminal.
//
//	go run ./backend/cmd/fixtureserver -dir backend/theme/mangadex/testdata
//	curl -i localhost:8099/robots.txt
//	curl -i 'localhost:8099/sim/status/429?retryAfter=5'
//	curl -i 'localhost:8099/sim/redirect?n=3'
//	curl -sD- -o/dev/null 'localhost:8099/sim/large?bytes=20000000'
//
// It serves only files under -dir, and nothing in the shipped app imports the
// package it wraps.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rickl/quire/backend/fixtures"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8099", "address to listen on")
	dir := flag.String("dir", ".", "directory fixture files are served from")
	robots := flag.String("robots", "", "robots.txt body; empty allows everything")
	robotsFile := flag.String("robots-file", "", "read the robots.txt body from a file")
	flag.Parse()

	body := *robots
	if *robotsFile != "" {
		b, err := os.ReadFile(*robotsFile)
		if err != nil {
			log.Fatalf("fixtureserver: %v", err)
		}
		body = string(b)
	}

	routes, err := autoRoutes(*dir)
	if err != nil {
		log.Fatalf("fixtureserver: %v", err)
	}

	srv := fixtures.New(fixtures.Options{Dir: *dir, Routes: routes, Robots: body})

	fmt.Printf("fixtureserver listening on http://%s\n", *addr)
	fmt.Printf("  fixtures from %s\n", *dir)
	if body == "" {
		fmt.Println("  robots.txt: empty (everything allowed)")
	} else {
		fmt.Printf("  robots.txt:\n%s\n", indent(body))
	}
	keys := make([]string, 0, len(routes))
	for k := range routes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  route %s\n", k)
	}
	fmt.Println("  simulations: /sim/ok /sim/status/{code} /sim/slow /sim/large /sim/redirect")

	log.Fatal(http.ListenAndServe(*addr, srv))
}

// autoRoutes publishes every file in dir at its own name, so pointing the
// server at a theme's testdata directory is enough to browse it. A test that
// wants a specific URL shape builds its own route table instead.
func autoRoutes(dir string) (map[string]fixtures.Route, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	routes := map[string]fixtures.Route{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		routes["/"+name] = fixtures.Route{File: name}
		routes["/"+strings.TrimSuffix(name, filepath.Ext(name))] = fixtures.Route{File: name}
	}
	return routes, nil
}

func indent(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
