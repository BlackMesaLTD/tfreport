package blocks

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseArgs converts a variadic list of alternating (key, value) pairs, as
// received from Go text/template function calls, into a map[string]any.
//
// Templates invoke parameterized blocks like:
//
//	{{ summary_table "group" "module_type" "max" 10 }}
//
// Go text/template doesn't support named args, so the convention is positional
// (k1, v1, k2, v2, ...). Keys must be strings; values may be any type. Odd
// argument counts or non-string keys produce an error annotated with the block
// name supplied by the caller.
//
// Values of type string are lightly type-coerced: "true"/"false" → bool,
// integer literals → int. This lets templates pass everything as strings
// (the common case with Sprig output) without every block re-coercing.
func ParseArgs(blockName string, args ...any) (map[string]any, error) {
	if len(args)%2 != 0 {
		return nil, fmt.Errorf("%s: expected key=value pairs, got %d arguments", blockName, len(args))
	}
	out := make(map[string]any, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			return nil, fmt.Errorf("%s: argument %d: key must be a string, got %T", blockName, i, args[i])
		}
		out[key] = coerce(args[i+1])
	}
	return out, nil
}

// coerce applies lightweight type coercion on values passed as strings.
func coerce(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	lower := strings.ToLower(s)
	if lower == "true" {
		return true
	}
	if lower == "false" {
		return false
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return s
}

// ArgString returns a string argument with a default fallback.
func ArgString(args map[string]any, key, def string) string {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ArgInt returns an integer argument with a default fallback.
func ArgInt(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case string:
		if parsed, err := strconv.Atoi(n); err == nil {
			return parsed
		}
	}
	return def
}

// ArgBool returns a boolean argument with a default fallback.
func ArgBool(args map[string]any, key string, def bool) bool {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		lower := strings.ToLower(b)
		if lower == "true" {
			return true
		}
		if lower == "false" {
			return false
		}
	}
	return def
}

// ArgCSV returns a string argument parsed as a comma-separated list. Returns
// nil when key is absent or value is empty. Trims whitespace around each item.
func ArgCSV(args map[string]any, key string) []string {
	s := ArgString(args, key, "")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
