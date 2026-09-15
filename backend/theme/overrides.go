package theme

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// PLAN §7.2: "`overrides` is a per-theme, schema-declared map. Themes declare
// which keys they accept and sensible defaults; unknown keys are a validation
// error, not silently ignored."
//
// This file is that declaration mechanism. It is shared rather than reinvented
// per theme because the failure mode it guards against — a typo'd key that
// does nothing and is never reported — is exactly what makes adding site #3 to
// an existing theme feel like guesswork.

// Kind is the type of an override value.
type Kind int

const (
	KindString Kind = iota
	KindBool
	KindInt
)

func (k Kind) String() string {
	switch k {
	case KindBool:
		return "boolean"
	case KindInt:
		return "integer"
	default:
		return "string"
	}
}

// OverrideDoc describes one accepted override key. The Why field is not
// decoration: docs/THEME-NOTES.md is generated from the same information, and
// PLAN §7.3 asks each theme to record "which overrides keys exist and why".
type OverrideDoc struct {
	Key     string
	Kind    Kind
	Default any

	// Enum, when non-empty, is the closed set of permitted string values.
	Enum []string

	// Why explains what site variation this key absorbs.
	Why string
}

// OverrideSpec is a theme's complete set of accepted keys.
type OverrideSpec struct {
	// ThemeID is only used in error messages, so a user staring at a rejected
	// import knows which source entry is at fault.
	ThemeID string
	Keys    []OverrideDoc
}

// Overrides is a validated, defaults-applied override set. Themes read their
// configuration through it, so a missing key and a defaulted key behave
// identically.
type Overrides struct {
	values map[string]any
}

// Validate checks raw against the spec without building an Overrides. It is
// what Registry.Validate calls, and what makes an unknown key an error.
func (sp OverrideSpec) Validate(raw map[string]any) error {
	_, err := sp.Resolve(raw)
	return err
}

// Resolve validates raw and returns it with defaults filled in.
func (sp OverrideSpec) Resolve(raw map[string]any) (Overrides, error) {
	out := Overrides{values: make(map[string]any, len(sp.Keys))}
	for _, k := range sp.Keys {
		out.values[k.Key] = k.Default
	}

	// Iterate the raw keys in sorted order so the *first* error reported for a
	// config with several mistakes is stable across runs.
	rawKeys := make([]string, 0, len(raw))
	for k := range raw {
		rawKeys = append(rawKeys, k)
	}
	slices.Sort(rawKeys)

	for _, key := range rawKeys {
		doc, ok := sp.find(key)
		if !ok {
			return Overrides{}, fmt.Errorf(
				"theme %s: unknown override key %q; accepted keys are %s",
				sp.ThemeID, key, strings.Join(sp.keyNames(), ", "))
		}
		v, err := coerce(doc, raw[key])
		if err != nil {
			return Overrides{}, fmt.Errorf("theme %s: override %q: %w", sp.ThemeID, key, err)
		}
		out.values[key] = v
	}
	return out, nil
}

func (sp OverrideSpec) find(key string) (OverrideDoc, bool) {
	for _, k := range sp.Keys {
		if k.Key == key {
			return k, true
		}
	}
	return OverrideDoc{}, false
}

func (sp OverrideSpec) keyNames() []string {
	names := make([]string, 0, len(sp.Keys))
	for _, k := range sp.Keys {
		names = append(names, k.Key)
	}
	slices.Sort(names)
	return names
}

// Docs returns the accepted keys, for OverrideValidator.OverrideKeys.
func (sp OverrideSpec) Docs() []OverrideDoc { return slices.Clone(sp.Keys) }

// coerce converts a JSON-decoded value to the declared kind. JSON numbers
// arrive as float64, which is why an integer override needs a conversion
// rather than a type assertion.
func coerce(doc OverrideDoc, v any) (any, error) {
	switch doc.Kind {
	case KindString:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("want a string, got %T", v)
		}
		if len(doc.Enum) > 0 && !slices.Contains(doc.Enum, s) {
			return nil, fmt.Errorf("want one of %s, got %q", strings.Join(doc.Enum, ", "), s)
		}
		return s, nil
	case KindBool:
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("want a boolean, got %T", v)
		}
		return b, nil
	case KindInt:
		switch n := v.(type) {
		case float64:
			if n != float64(int(n)) {
				return nil, fmt.Errorf("want a whole number, got %v", n)
			}
			return int(n), nil
		case int:
			return n, nil
		case string:
			i, err := strconv.Atoi(n)
			if err != nil {
				return nil, fmt.Errorf("want an integer, got %q", n)
			}
			return i, nil
		default:
			return nil, fmt.Errorf("want an integer, got %T", v)
		}
	}
	return nil, fmt.Errorf("unhandled override kind %v", doc.Kind)
}

// String returns the string value of key, or "" if it is not a string one.
func (o Overrides) String(key string) string {
	s, _ := o.values[key].(string)
	return s
}

// Bool returns the boolean value of key.
func (o Overrides) Bool(key string) bool {
	b, _ := o.values[key].(bool)
	return b
}

// Int returns the integer value of key.
func (o Overrides) Int(key string) int {
	i, _ := o.values[key].(int)
	return i
}

// PathSegment returns a string override cleaned up for use as a URL path
// segment: no leading or trailing slashes, so both "manga" and "/manga/"
// written by a user produce the same URL.
func (o Overrides) PathSegment(key string) string {
	return strings.Trim(o.String(key), "/")
}
