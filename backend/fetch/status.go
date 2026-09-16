package fetch

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Plain language for the HTTP statuses a refusal actually arrives as.
//
// PLAN §6 M3: "verdicts surface as plain language, not error codes", and a
// verdict is meant to be a complete, final answer. `HTTP 444` is neither — it
// is nginx's "close the connection without responding", it is not in Go's
// http.StatusText so it renders as a bare number, and a user who is told only
// that has been handed a puzzle instead of an answer.
//
// This is deliberately *not* a table of every status. Each case below is one
// that occurs in practice, and each says two things: what it means, and what
// the reader can do about it. Anything else keeps its number but is wrapped in
// a sentence rather than presented naked.
//
// CORRECTION 2026-09-16 — **every sentence states what was observed, never a
// cause that was inferred.** The first version of this file said 444 "almost
// always means it is rate limiting", on the strength of what 444 usually is in
// the wild. It was measured the same day: comick.art answers 444 to any search
// query shorter than three characters and 200 to longer ones, deterministically
// and with no rate limit anywhere in it. A user followed our advice, waited
// several minutes, retried and failed again — a confident false explanation is
// worse than the bare code it replaced, because it sends someone off to do
// something useless. So 429, which is the server *saying* "too many requests",
// names rate limiting; 444, which is the server saying nothing at all, does
// not. This is PLAN §7.5's rule about challenge markers one layer down: the
// observation is the fact, the cause is the inference, and only the first may
// be asserted.
//
// Nothing here retries. PLAN §7.4's client already honours Retry-After and
// backs off on 429/5xx; a probe that silently retried a rate limit would make
// the rate limit worse. Telling the user to come back in a few minutes leaves
// the control where it belongs.

// StatusSentence explains an HTTP status in plain language, as a clause about
// subject ("the site", "the image host") with no leading capital and no
// trailing full stop, so callers can compose it.
//
// It is only for refusals. A browser challenge is detected before this is
// reached and is worded by its own path (PLAN §7.5 stage 3, §7.6): a challenge
// is a challenge, and "maybe it's rate limiting" must never leak into it.
func StatusSentence(subject string, code int) string {
	if strings.TrimSpace(subject) == "" {
		subject = "the site"
	}
	switch code {
	case 429:
		// The only case where rate limiting is a *fact*: 429 is the server
		// saying "too many requests" in so many words.
		return subject + " says Quire is asking too often; waiting a few minutes and trying again usually works"
	case 444:
		// 444 is nginx closing the connection without sending a response, and
		// that is the whole of what it tells us. It does not say why. A rate
		// limit is one cause; a filter on the request, a rule about the path,
		// or a query the site won't accept are others, and a user who was told
		// "rate limiting" waited several minutes for nothing.
		return subject + " closed the connection without answering, which can mean anything from a busy moment to a request it won't accept; trying again later is worth one attempt, but if it keeps happening the site is refusing this request rather than delaying it"
	case 403:
		// Not a challenge — those are caught earlier. This is a plain refusal
		// of this request, and saying more would assert what we did not see.
		return subject + " refused this request; if the page opens in a browser, it is refusing Quire's request in particular, and there is nothing Quire can change about that"
	case 502, 503, 504:
		// Observed: these three are the server's own statement that it could
		// not serve the request, so "later" is advice, not a diagnosis.
		return subject + " answered that it couldn't serve the request just now; trying again later is worth it"
	case 404:
		// Observed: the address is not there. "Which side is at fault" is the
		// inference, so it is offered as a possibility, not stated.
		return "the address Quire asked " + subject + " for isn't there; that may mean the site has moved it, or that Quire's theme for this site is out of date"
	}
	return fmt.Sprintf("%s answered with HTTP %d, which Quire couldn't use", subject, code)
}

// trailingStatus matches a bare "HTTP 444" at the end of a message, which is
// what a theme's error boils down to once Go's wrapping has been trimmed off.
var trailingStatus = regexp.MustCompile(`(?i)\bHTTP[ /]?(\d{3})\.?\s*$`)

// ExplainStatus rewrites a message that ends in a bare status code so that it
// ends in StatusSentence instead. A message with no bare code is returned
// unchanged — this expands what is already there, it does not guess.
func ExplainStatus(msg, subject string) string {
	m := trailingStatus.FindStringSubmatchIndex(msg)
	if m == nil {
		return msg
	}
	code, err := strconv.Atoi(msg[m[2]:m[3]])
	if err != nil {
		return msg
	}
	prefix := strings.TrimRight(strings.TrimSpace(msg[:m[0]]), ":;,- ")
	sentence := StatusSentence(subject, code)
	if prefix == "" {
		return sentence
	}
	return prefix + ": " + sentence
}
