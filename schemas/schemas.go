// Package schemas embeds the JSON Schemas (draft 2020-12) of every JSON document kubestrata prints
// or writes, and checks documents against them for the tests.
//
// Check understands the keywords these schemas use: $ref to #/$defs, type (one or a list), const,
// enum, properties, required, additionalProperties, items, minimum, maximum and pattern; format
// and the annotations are not checked. It is stricter than the schemas on one point: an object
// with properties and no additionalProperties keyword rejects a property the schema does not
// list, so a field added to the code without its schema fails the tests. The published schemas
// allow unknown properties, the additive changes of schema_version 1.
package schemas

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

//go:embed *.schema.json
var files embed.FS

// Names lists the schemas: snapshot, contexts, collect, cache-ls, render, compare, diff, summary.
func Names() []string {
	entries, _ := files.ReadDir(".")
	var out []string
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".schema.json"))
	}
	sort.Strings(out)
	return out
}

// Check validates doc against the schema name and returns every problem found, joined.
func Check(name string, doc []byte) error {
	raw, err := files.ReadFile(name + ".schema.json")
	if err != nil {
		return fmt.Errorf("schema %s: %w", name, err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("schema %s: %w", name, err)
	}
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("%s document: %w", name, err)
	}
	c := &checker{root: root}
	c.check(root, v, "$")
	if len(c.errs) > 0 {
		if len(c.errs) > 20 {
			c.errs = append(c.errs[:20], fmt.Sprintf("... and %d more", len(c.errs)-20))
		}
		return fmt.Errorf("%s: %s", name, strings.Join(c.errs, "; "))
	}
	return nil
}

type checker struct {
	root map[string]any
	errs []string
}

func (c *checker) fail(path, format string, a ...any) {
	c.errs = append(c.errs, path+": "+fmt.Sprintf(format, a...))
}

func (c *checker) resolve(s map[string]any) map[string]any {
	for {
		r, ok := s["$ref"].(string)
		if !ok {
			return s
		}
		name, ok := strings.CutPrefix(r, "#/$defs/")
		defs, _ := c.root["$defs"].(map[string]any)
		next, found := defs[name].(map[string]any)
		if !ok || !found {
			c.fail("$ref", "cannot resolve %s", r)
			return map[string]any{}
		}
		s = next
	}
}

func typeOf(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number:
		if _, err := x.Int64(); err == nil {
			return "integer"
		}
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

func (c *checker) check(s map[string]any, v any, path string) {
	s = c.resolve(s)
	if t, ok := s["type"]; ok {
		var allowed []string
		switch x := t.(type) {
		case string:
			allowed = []string{x}
		case []any:
			for _, e := range x {
				allowed = append(allowed, fmt.Sprint(e))
			}
		}
		got, match := typeOf(v), false
		for _, a := range allowed {
			match = match || a == got || (a == "number" && got == "integer")
		}
		if !match {
			c.fail(path, "type %s, want %s", got, strings.Join(allowed, " or "))
			return
		}
	}
	if want, ok := s["const"]; ok && fmt.Sprint(want) != fmt.Sprint(v) {
		c.fail(path, "%v, want %v", v, want)
	}
	if enum, ok := s["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			found = found || fmt.Sprint(e) == fmt.Sprint(v)
		}
		if !found {
			c.fail(path, "%v is not one of %v", v, enum)
		}
	}
	if n, ok := v.(json.Number); ok {
		f, _ := n.Float64()
		if m, ok := s["minimum"].(float64); ok && f < m {
			c.fail(path, "%v below the minimum %v", f, m)
		}
		if m, ok := s["maximum"].(float64); ok && f > m {
			c.fail(path, "%v above the maximum %v", f, m)
		}
		if math.IsNaN(f) {
			c.fail(path, "not a number")
		}
	}
	if p, ok := s["pattern"].(string); ok {
		if str, isStr := v.(string); isStr && !regexp.MustCompile(p).MatchString(str) {
			c.fail(path, "%q does not match %s", str, p)
		}
	}
	switch x := v.(type) {
	case map[string]any:
		props, _ := s["properties"].(map[string]any)
		for _, r := range asStrings(s["required"]) {
			if _, ok := x[r]; !ok {
				c.fail(path, "missing required property %q", r)
			}
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		extra, hasExtra := s["additionalProperties"]
		for _, k := range keys {
			if ps, ok := props[k].(map[string]any); ok {
				c.check(ps, x[k], path+"."+k)
				continue
			}
			switch e := extra.(type) {
			case bool:
				if !e {
					c.fail(path, "property %q not allowed", k)
				}
			case map[string]any:
				c.check(e, x[k], path+"."+k)
			default:
				if !hasExtra && props != nil {
					c.fail(path, "property %q is not in the schema", k)
				}
			}
		}
	case []any:
		if items, ok := s["items"].(map[string]any); ok {
			for i, e := range x {
				c.check(items, e, fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
}

func asStrings(v any) []string {
	var out []string
	for _, e := range asSlice(v) {
		out = append(out, fmt.Sprint(e))
	}
	return out
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}
