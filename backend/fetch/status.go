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
	case 429, 444:
		// 444 is nginx closing the connection without answering, which in the
		// wild is a rate limit or a cheap bot filter almost every time.
		return subject + " is refusing requests for now, which almost always means it is rate limiting Quire; waiting a few minutes and trying again usually works"
	case 403:
		// Not a challenge — those are caught earlier. This is a plain refusal
		// of this request, and saying more would assert what we did not see.
		return subject + " refused this request; if the page opens in a browser, it is turning Quire away specifically, and there is nothing Quire can change about that"
	case 502, 503, 504:
		return subject + " is having trouble at its end right now; it is worth trying again later"
	case 404:
		// This one points at us. Saying so is what makes it reportable.
		return "the address Quire asked " + subject + " for isn't there, which is more likely a problem with Quire's theme for this site than with the site itself"
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
