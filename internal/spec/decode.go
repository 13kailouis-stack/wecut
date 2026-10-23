package spec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Decode parses a project. Before the typed decode it walks the raw JSON
// against the Go types, so an agent gets every unknown field and wrong type
// at once, each with its JSON pointer and a "did you mean" when one fits.
func Decode(data []byte) (*Project, []Issue) {
	var raw any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, []Issue{{Code: "E_JSON", Message: "project is not valid JSON: " + err.Error(), Hint: "Check for trailing commas, unquoted keys or comments."}}
	}
	if dec.More() {
		return nil, []Issue{{Code: "E_JSON", Message: "extra data after the project object", Hint: "Send exactly one JSON object."}}
	}
	var issues []Issue
	checkShape(reflect.TypeOf(Project{}), raw, "", &issues)
	if len(issues) > 0 {
		return nil, issues
	}
	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, []Issue{{Code: "E_SCHEMA", Message: err.Error()}}
	}
	return &p, nil
}

var rawMessageType = reflect.TypeOf(json.RawMessage{})

func checkShape(t reflect.Type, v any, path string, out *[]Issue) {
	if v == nil {
		return // null means "not set"
	}
	if t == rawMessageType {
		return
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := v.(map[string]any)
		if !ok {
			*out = append(*out, typeIssue(path, "an object", v))
			return
		}
		fields := jsonFields(t)
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			ft, ok := fields[k]
			if !ok {
				iss := Issue{Code: "E_SCHEMA", Path: path + "/" + escPtr(k), Message: fmt.Sprintf("unknown field %q", k)}
				if s := closest(k, fields); s != "" {
					iss.Hint = fmt.Sprintf("Did you mean %q?", s)
				} else {
					iss.Hint = "Allowed here: " + strings.Join(sortedKeys(fields), ", ")
				}
				*out = append(*out, iss)
				continue
			}
			checkShape(ft, obj[k], path+"/"+escPtr(k), out)
		}
	case reflect.Slice:
		arr, ok := v.([]any)
		if !ok {
			*out = append(*out, typeIssue(path, "an array", v))
			return
		}
		for i, e := range arr {
			checkShape(t.Elem(), e, path+"/"+strconv.Itoa(i), out)
		}
	case reflect.String:
		if _, ok := v.(string); !ok {
			*out = append(*out, typeIssue(path, "a string", v))
		}
	case reflect.Bool:
		if _, ok := v.(bool); !ok {
			*out = append(*out, typeIssue(path, "true or false", v))
		}
	case reflect.Float64:
		n, ok := v.(json.Number)
		if !ok {
			*out = append(*out, typeIssue(path, "a number", v))
			return
		}
		if f, err := n.Float64(); err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			*out = append(*out, Issue{Code: "E_SCHEMA", Path: path, Message: "number out of range"})
		}
	case reflect.Int:
		n, ok := v.(json.Number)
		if !ok {
			*out = append(*out, typeIssue(path, "a whole number", v))
			return
		}
		if _, err := strconv.ParseInt(n.String(), 10, 32); err != nil {
			*out = append(*out, Issue{Code: "E_SCHEMA", Path: path, Message: fmt.Sprintf("expected a whole number, got %s", n.String())})
		}
	}
}

func typeIssue(path, want string, got any) Issue {
	iss := Issue{Code: "E_SCHEMA", Path: path, Message: fmt.Sprintf("expected %s, got %s", want, kindOf(got))}
	if s, ok := got.(string); ok && strings.Contains(want, "number") {
		if _, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			iss.Hint = fmt.Sprintf("Write %s without quotes.", strings.TrimSpace(s))
		}
	}
	return iss
}

func kindOf(v any) string {
	switch x := v.(type) {
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case string:
		return fmt.Sprintf("the string %q", x)
	case bool:
		return "a boolean"
	case json.Number:
		return "the number " + x.String()
	}
	return "something else"
}

// jsonFields maps JSON names to field types, flattening embedded structs the
// way encoding/json does.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if f.Anonymous && name == "" {
			for k, v := range jsonFields(f.Type) {
				out[k] = v
			}
			continue
		}
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = f.Type
	}
	return out
}

// JSONFieldNames is used by tests to keep schema.json in step with the types.
func JSONFieldNames(v any) []string {
	return sortedKeys(jsonFields(reflect.TypeOf(v)))
}

func sortedKeys(m map[string]reflect.Type) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func escPtr(s string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(s)
}

// closest returns the known field nearest to k, if it is near enough to be a
// likely typo (or differs only in case).
func closest(k string, fields map[string]reflect.Type) string {
	best, bestD := "", 1<<30
	for name := range fields {
		if strings.EqualFold(name, k) {
			return name
		}
		d := levenshtein(strings.ToLower(k), strings.ToLower(name))
		if d < bestD || (d == bestD && name < best) {
			best, bestD = name, d
		}
	}
	if bestD <= 2 || (bestD <= 3 && len(k) >= 7) {
		return best
	}
	return ""
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
