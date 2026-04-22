package template

import (
	"fmt"
	"slices"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/formatter/blocks"
	"github.com/BlackMesaLTD/tfreport/internal/preserve"
)

// preserveFuncs builds the state-preservation template helpers bound to a
// BlockContext so the `prior` / `has_prior` lookups can consult the
// parsed previous body carried on ctx.PriorRegions.
//
// Exposed helpers:
//
//	{{ preserve "id" "kind" [kind-args...] }}
//	    Inline wrap — emits begin + default body + end. kind dictates the
//	    default rendering and validates kind-args (options list for radio,
//	    default tick for checkbox, default string for text).
//
//	{{ preserve_begin "id" "kind" [kind-args...] }}
//	{{ preserve_end "id" }}
//	    Paired form — emits only the open/close markers. Author writes
//	    whatever default body they want between them. Required for kind=block.
//
//	{{ prior "id" }}
//	    Returns the prior region body (verbatim, including any surrounding
//	    whitespace) or "" when unknown. Escape hatch for authors who want
//	    template-time validation logic.
//
//	{{ has_prior "id" }}
//	    Boolean: true when `id` appears in PriorRegions.
func preserveFuncs(ctx *blocks.BlockContext) map[string]any {
	return map[string]any{
		"preserve":       preserveInline(ctx),
		"preserve_begin": preserveBegin,
		"preserve_end":   preserveEnd,
		"prior":          priorFn(ctx),
		"has_prior":      hasPriorFn(ctx),
	}
}

// preserveInline renders begin + default body + end in one call. Body
// is derived from kind:
//   - checkbox: `[ ]`, or the first extra arg if it's a literal default tick.
//   - radio: builds a task-list block from an options list (first extra arg);
//     optional second arg is the default-selected label.
//   - text: empty, or the first extra arg as literal default text.
//   - block: REJECTED — block kind requires preserve_begin/preserve_end so
//     the author can compose arbitrary multi-line content with template
//     control flow.
func preserveInline(ctx *blocks.BlockContext) func(args ...any) (string, error) {
	return func(args ...any) (string, error) {
		id, kind, extra, err := parsePreserveArgs(args)
		if err != nil {
			return "", err
		}
		if kind == "block" {
			return "", fmt.Errorf("preserve id=%q: kind=\"block\" is not supported inline; use {{ preserve_begin }}...{{ preserve_end }}", id)
		}

		markerAttrs, body, err := buildKindBody(id, kind, extra, ctx)
		if err != nil {
			return "", err
		}
		begin, err := preserve.RenderBegin(id, kind, markerAttrs)
		if err != nil {
			return "", err
		}
		end, err := preserve.RenderEnd(id)
		if err != nil {
			return "", err
		}
		return begin + body + end, nil
	}
}

// preserveBegin emits only the opening marker. Kind-args become marker
// attributes so the reconciler can introspect them without the template
// needing to be re-rendered.
func preserveBegin(args ...any) (string, error) {
	id, kind, extra, err := parsePreserveArgs(args)
	if err != nil {
		return "", err
	}
	attrs, _, err := buildKindBody(id, kind, extra, nil)
	if err != nil {
		return "", err
	}
	return preserve.RenderBegin(id, kind, attrs)
}

// preserveEnd emits only the closing marker.
func preserveEnd(id string) (string, error) {
	return preserve.RenderEnd(id)
}

// priorFn returns the prior body for id, or "" when no prior is known.
func priorFn(ctx *blocks.BlockContext) func(id string) string {
	return func(id string) string {
		if ctx == nil || ctx.PriorRegions == nil {
			return ""
		}
		if r, ok := ctx.PriorRegions[id]; ok {
			return r.Body
		}
		return ""
	}
}

// hasPriorFn returns true when id has a prior region in scope.
func hasPriorFn(ctx *blocks.BlockContext) func(id string) bool {
	return func(id string) bool {
		if ctx == nil || ctx.PriorRegions == nil {
			return false
		}
		_, ok := ctx.PriorRegions[id]
		return ok
	}
}

// parsePreserveArgs extracts (id, kind, extras) from a preserve/preserve_begin
// call. id is required; kind defaults to empty (treated as block by the
// reconciler but rejected for inline preserve).
func parsePreserveArgs(args []any) (id, kind string, extra []any, err error) {
	if len(args) == 0 {
		return "", "", nil, fmt.Errorf("preserve: id argument is required")
	}
	idStr, ok := args[0].(string)
	if !ok {
		return "", "", nil, fmt.Errorf("preserve: id must be a string, got %T", args[0])
	}
	if err := preserve.ValidateID(idStr); err != nil {
		return "", "", nil, err
	}
	id = idStr
	if len(args) >= 2 {
		kStr, ok := args[1].(string)
		if !ok {
			return "", "", nil, fmt.Errorf("preserve id=%q: kind must be a string, got %T", id, args[1])
		}
		kind = kStr
	}
	if len(args) > 2 {
		extra = args[2:]
	}
	return id, kind, extra, nil
}

// buildKindBody returns (marker attrs, default body) for the given kind.
// ctx may be nil — when called from preserve_begin we don't need the
// default body since the template provides one between the tags.
func buildKindBody(id, kind string, extra []any, _ *blocks.BlockContext) (map[string]string, string, error) {
	attrs := map[string]string{}
	switch kind {
	case "checkbox":
		def := "[ ]"
		if len(extra) >= 1 {
			s, ok := extra[0].(string)
			if !ok {
				return nil, "", fmt.Errorf("preserve id=%q kind=checkbox: default must be a string, got %T", id, extra[0])
			}
			if s != "[ ]" && s != "[x]" && s != "[X]" {
				return nil, "", fmt.Errorf("preserve id=%q kind=checkbox: default must be \"[ ]\" or \"[x]\", got %q", id, s)
			}
			def = s
			if def == "[X]" {
				def = "[x]"
			}
		}
		return attrs, def, nil

	case "radio":
		opts, err := coerceOptions(id, extra)
		if err != nil {
			return nil, "", err
		}
		if len(opts) == 0 {
			return nil, "", fmt.Errorf("preserve id=%q kind=radio: options list is required", id)
		}
		attrs["options"] = strings.Join(opts, ",")
		selected := ""
		if len(extra) >= 2 {
			s, ok := extra[1].(string)
			if !ok {
				return nil, "", fmt.Errorf("preserve id=%q kind=radio: default-selected must be a string, got %T", id, extra[1])
			}
			selected = s
		}
		return attrs, renderRadioBody(opts, selected), nil

	case "text":
		def := ""
		if len(extra) >= 1 {
			s, ok := extra[0].(string)
			if !ok {
				return nil, "", fmt.Errorf("preserve id=%q kind=text: default must be a string, got %T", id, extra[0])
			}
			def = s
		}
		return attrs, def, nil

	case "block", "":
		return attrs, "", nil

	default:
		return nil, "", fmt.Errorf("preserve id=%q: unknown kind %q (valid: %s)", id, kind, strings.Join(preserve.KnownKinds(), ", "))
	}
}

// coerceOptions extracts the radio options list from the first extra arg.
// Accepts []string, []any (stringified), or a csv string.
func coerceOptions(id string, extra []any) ([]string, error) {
	if len(extra) < 1 {
		return nil, fmt.Errorf("preserve id=%q kind=radio: first extra arg must be the options list", id)
	}
	switch v := extra[0].(type) {
	case []string:
		return slices.Clone(v), nil
	case []any:
		out := make([]string, 0, len(v))
		for i, el := range v {
			s, ok := el.(string)
			if !ok {
				return nil, fmt.Errorf("preserve id=%q kind=radio: option %d must be a string, got %T", id, i, el)
			}
			out = append(out, s)
		}
		return out, nil
	case string:
		out := make([]string, 0)
		for p := range strings.SplitSeq(v, ",") {
			if t := strings.TrimSpace(p); t != "" {
				out = append(out, t)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("preserve id=%q kind=radio: options must be a list or csv string, got %T", id, extra[0])
	}
}

// renderRadioBody emits the default (all-unticked, or one-ticked if a
// default-selected label was supplied) task-list body.
func renderRadioBody(opts []string, selected string) string {
	var b strings.Builder
	b.WriteString("\n")
	for _, o := range opts {
		if o == selected {
			b.WriteString("- [x] ")
		} else {
			b.WriteString("- [ ] ")
		}
		b.WriteString(o)
		b.WriteString("\n")
	}
	return b.String()
}
