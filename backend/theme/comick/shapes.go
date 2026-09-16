package comick

// Decoding an API that does not keep its own shapes.
//
// # The bug this exists because of
//
// On 2026-09-16 the series screen failed outright on many series with
//
//	cannot unmarshal object into Go struct field comicData.md_titles of type []comick.altTitle
//
// The endpoint sends `md_titles` as an **array** for some series and as a bare
// **object** for others. Because the whole payload is decoded in one call, one
// field of the wrong cardinality failed `Series()`, and because the series
// screen errors as a unit the user saw **no chapters at all** — `Chapters()`
// was fine throughout and returned 18, 51, 10 and 1 for four series sampled by
// hand the same day.
//
// # Why this is a file rather than one patched field
//
// This is an undocumented application backend (see the package comment), not a
// versioned public API. A backend that varies one field's cardinality is a
// backend whose serialiser drops the array wrapper around any to-many relation
// that happens to hold one row — so the next field to do it would have failed
// in exactly the same way, and just as totally. Every to-many relation in
// these DTOs is therefore decoded through [list], and every to-one through
// [one], so the shape a given response happens to use stops being load-bearing.
//
// # Where the tolerance stops, deliberately
//
// Tolerance is for *cardinality*, which this API demonstrably varies. It is not
// a licence to accept anything: a field that can only sensibly be one thing
// keeps its plain type, so encoding/json refuses it and **names it** in the
// error, exactly as the message above did. That message was a good one — it
// said which field and which shape — and losing it in a mush of `any` would
// make the next instance of this harder to diagnose, not easier.
//
// PLAN §9: the fixtures now carry **both** shapes, because a test that only
// ever sees the array proves nothing about the object. That is the durable
// half of this fix.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// list decodes a to-many relation sent as an array of T, as an **object whose
// values are T**, as a single bare T, or as null.
//
// The object case is the one that broke `md_titles`, and its exact shape
// matters. Measured against 60 live series on 2026-09-16, a series whose
// alternate titles are non-empty sends
//
//	"md_titles": {"1": {"id": 462545, "comic_id": 142241, "title": "…"}}
//
// — a **map keyed by position**, not a bare single object. That is what a PHP
// collection serialises to once its keys stop being a 0-based run, and it is
// why this branch decodes values rather than the object itself: reading
// `{"1": {…}}` as one altTitle yields an altTitle with no title in it, which
// would have replaced a hard failure with a quiet wrong answer — every
// alternate title silently gone, and nothing red anywhere to say so.
//
// Document order is preserved, because a map in Go has none and the site's
// order is the one the user is shown.
//
// null and absent both yield an empty list, which is what every caller here
// wants: a series with no authors listed is not an error.
type list[T any] []T

// UnmarshalJSON implements json.Unmarshaler.
func (l *list[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case len(b) == 0 || bytes.Equal(b, []byte("null")):
		*l = nil
		return nil
	case b[0] == '[':
		var many []T
		if err := json.Unmarshal(b, &many); err != nil {
			return err
		}
		*l = many
		return nil
	case b[0] == '{':
		values, err := objectValuesInOrder(b)
		if err != nil {
			return err
		}
		many := make([]T, 0, len(values))
		for _, raw := range values {
			var v T
			if err := json.Unmarshal(raw, &v); err != nil {
				// Not a map of T. The remaining reading is that the object is
				// one T itself — the shape a different serialiser produces for
				// the same relation — so try that before giving up.
				var single T
				if err2 := json.Unmarshal(b, &single); err2 != nil {
					return fmt.Errorf("expected an array, an object of %T, or one %T: %w", single, single, err)
				}
				*l = []T{single}
				return nil
			}
			many = append(many, v)
		}
		*l = many
		return nil
	default:
		// A scalar where a relation was expected: still a cardinality
		// question, so a bare string in a string list is one element. For any
		// other T this fails, with the element type named.
		var single T
		if err := json.Unmarshal(b, &single); err != nil {
			return fmt.Errorf("expected an array or a single %T, got %s", single, kindOf(b))
		}
		*l = []T{single}
		return nil
	}
}

// one decodes a to-one relation sent as an object, or as an array holding it,
// or as null.
//
// It is [list] the other way round and exists for the same reason: a
// serialiser that drops an array wrapper around one row can equally add one.
type one[T any] struct{ V T }

// UnmarshalJSON implements json.Unmarshaler.
func (o *one[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	var zero T
	switch {
	case len(b) == 0 || bytes.Equal(b, []byte("null")):
		o.V = zero
		return nil
	case b[0] == '[':
		var many []T
		if err := json.Unmarshal(b, &many); err != nil {
			return err
		}
		if len(many) == 0 {
			o.V = zero
			return nil
		}
		// The first, because the field is declared to hold one thing: taking a
		// later element would be choosing on the caller's behalf.
		o.V = many[0]
		return nil
	default:
		return json.Unmarshal(b, &o.V)
	}
}

// flexString decodes a field sent as a string or as a number.
//
// Used only where the site's own data makes both plausible: a chapter number
// and a volume number are text here ("10.5", "Extra") but are the fields an
// API of this kind most often emits unquoted. A number is rendered back as
// text rather than parsed, so 10.5 stays "10.5" and the ordering contract in
// order.go sees what it would have seen.
//
// It refuses objects and arrays. A title that arrives as an object is not a
// cardinality question and guessing which of its fields was meant would be the
// invention this file is otherwise arguing against.
type flexString string

// UnmarshalJSON implements json.Unmarshaler.
func (f *flexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case len(b) == 0 || bytes.Equal(b, []byte("null")):
		*f = ""
		return nil
	case b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
		return nil
	case b[0] == '-' || (b[0] >= '0' && b[0] <= '9'):
		// Kept verbatim: round-tripping through float64 turns 10.50 into 10.5
		// and a long identifier into exponent notation.
		if _, err := strconv.ParseFloat(string(b), 64); err != nil {
			return fmt.Errorf("expected text or a number, got %s", b)
		}
		*f = flexString(b)
		return nil
	default:
		return fmt.Errorf("expected text or a number, got %s", kindOf(b))
	}
}

// String renders the value.
func (f flexString) String() string { return string(f) }

// objectValuesInOrder returns a JSON object's values in the order they were
// written.
//
// The order is the point: the site sends its keyed collections as {"1": …,
// "2": …} and the user is shown them in that order, which decoding into a Go
// map would lose. Reading the keys rather than sorting them also means a
// collection keyed by something other than a position still comes out in the
// order it arrived.
func objectValuesInOrder(b []byte) ([]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("expected an object, got %s", kindOf(b))
	}
	var out []json.RawMessage
	for dec.More() {
		// The key, which is read and discarded: it is a position, and the
		// position is already carried by the order of the result.
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, nil
}

// kindOf names the JSON shape of a payload, for error messages that say what
// arrived rather than only what was wanted.
func kindOf(b []byte) string {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return "nothing"
	}
	switch b[0] {
	case '{':
		return "an object"
	case '[':
		return "an array"
	case '"':
		return "a string"
	case 't', 'f':
		return "a boolean"
	case 'n':
		return "null"
	default:
		return "a number"
	}
}
