// Package template wires tfreport's block registry into a Go text/template
// engine, so user-facing output can be composed declaratively.
//
// The engine exposes three composition tiers:
//
//  1. Pre-rendered properties — `{{ .Title }}`, `{{ .PlanCounts }}`,
//     `{{ .KeyChanges }}`, `{{ .SummaryTable }}`, `{{ .DeployChecklist }}`,
//     `{{ .Footer }}`, `{{ .CrossSubTable }}`. Zero-arg, grammar-aware.
//  2. Parameterized functions — `{{ summary_table group="module" }}`,
//     `{{ key_changes max=10 }}`, `{{ instance_detail show="diff" }}`,
//     `{{ text_plan addresses="..." }}`, etc.
//  3. Raw-data escape hatch — `{{ .Report }}`, `{{ .Reports }}`, `{{ .Target }}`
//     with Sprig functions available for any non-trivial logic.
package template

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"text/template"

	sprig "github.com/Masterminds/sprig/v3"
	"github.com/BlackMesaLTD/tfreport/internal/core"
	"github.com/BlackMesaLTD/tfreport/internal/formatter/blocks"
)

// Engine renders user templates against the block registry.
type Engine struct {
	registry *blocks.Registry
	include  func(string) (string, error) // injected; nil means disabled
}

// New constructs an Engine backed by the supplied registry. Pass
// blocks.Default() for the default registry populated with every built-in.
func New(registry *blocks.Registry) *Engine {
	return &Engine{registry: registry}
}

// WithIncludeFunc returns a copy with the supplied include implementation. The
// include func is scoped per render (tied to a specific configDir) — callers
// typically build a fresh engine per invocation.
func (e *Engine) WithIncludeFunc(fn func(string) (string, error)) *Engine {
	cp := *e
	cp.include = fn
	return &cp
}

// SingleReportData is the template scope for single-report renders.
type SingleReportData struct {
	Target          string
	Report          *core.Report
	Title           string
	PlanCounts      string
	KeyChanges      string
	SummaryTable    string
	Footer          string
	DeployChecklist string
	CrossSubTable   string
}

// MultiReportData is the template scope for multi-report renders.
type MultiReportData struct {
	Target          string
	Reports         []*core.Report
	Title           string
	PlanCounts      string
	KeyChanges      string
	SummaryTable    string
	Footer          string
	DeployChecklist string
	CrossSubTable   string
}

// Render parses tmplText, evaluates it against ctx, and returns the result.
func (e *Engine) Render(tmplText string, ctx *blocks.BlockContext) (string, error) {
	funcs := e.buildFuncMap(ctx)
	tmpl, err := template.New("tfreport").Funcs(funcs).Parse(tmplText)
	if err != nil {
		return "", fmt.Errorf("template parse: %w", err)
	}

	data, err := e.buildData(ctx)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template execute: %w", err)
	}
	return buf.String(), nil
}

// buildFuncMap composes Sprig's function map with tfreport-specific helpers.
// Order matters: Sprig first, then tfreport overrides so our `include`
// supersedes Sprig's.
func (e *Engine) buildFuncMap(ctx *blocks.BlockContext) template.FuncMap {
	funcs := sprig.TxtFuncMap()

	// Parameterized block helpers. Each calls into the registry.
	blockFunc := func(name string) func(args ...any) (string, error) {
		return func(args ...any) (string, error) {
			parsed, err := blocks.ParseArgs(name, args...)
			if err != nil {
				return "", err
			}
			return e.registry.Render(name, ctx, parsed)
		}
	}

	funcs["summary_table"] = blockFunc("summary_table")
	funcs["key_changes"] = blockFunc("key_changes")
	funcs["instance_detail"] = blockFunc("instance_detail")
	funcs["module_details"] = blockFunc("module_details")
	funcs["modules_table"] = blockFunc("modules_table")
	funcs["text_plan"] = blockFunc("text_plan")
	funcs["changed_resources_table"] = blockFunc("changed_resources_table")
	funcs["deploy_checklist"] = blockFunc("deploy_checklist")
	funcs["title"] = blockFunc("title")
	funcs["plan_counts"] = blockFunc("plan_counts")
	funcs["footer"] = blockFunc("footer")
	funcs["risk_histogram"] = blockFunc("risk_histogram")
	funcs["diff_groups"] = blockFunc("diff_groups")
	funcs["fleet_homogeneity"] = blockFunc("fleet_homogeneity")
	funcs["glossary"] = blockFunc("glossary")
	funcs["per_report"] = blockFunc("per_report")
	funcs["imports_list"] = blockFunc("imports_list")
	funcs["banner"] = blockFunc("banner")
	funcs["attribute_diff"] = blockFunc("attribute_diff")
	funcs["submodule_group"] = blockFunc("submodule_group")

	// Sandboxed include. Falls back to an error if no include func was bound.
	if e.include != nil {
		funcs["include"] = e.include
	} else {
		funcs["include"] = func(string) (string, error) {
			return "", fmt.Errorf("include: no config directory bound (pass --config to enable template includes)")
		}
	}

	// Small data helpers for templates that drop into raw-data mode.
	funcs["action_emoji"] = func(a string) string { return core.ActionEmoji(core.Action(a)) }
	funcs["impact_emoji"] = func(i string) string { return core.ImpactEmoji(core.Impact(i)) }
	funcs["resource_label"] = func(rc core.ResourceChange) string { return core.ResourceDisplayLabel(rc) }
	funcs["module_type"] = func(mg core.ModuleGroup, r *core.Report) string { return core.ModuleTypeForGroup(mg, r) }

	// Predicate helpers that stringify their args, so users can write
	// `{{ if impact_is "critical" $rc.Impact }}` without the
	// `(printf "%s" $rc.Impact)` dance that pure {{ eq }} requires.
	funcs["impact_is"] = func(wanted, got any) bool { return fmt.Sprint(got) == fmt.Sprint(wanted) }
	funcs["action_is"] = func(wanted, got any) bool { return fmt.Sprint(got) == fmt.Sprint(wanted) }

	// sample returns the first n items of a slice. Uses reflection so it
	// accepts any []T. Returns the original slice when n >= length.
	funcs["sample"] = sampleFn

	// Typo-safe action counters. action_count aggregates across all reports
	// (single or multi). import_count tallies IsImport=true resources.
	funcs["action_count"] = func(action string) int {
		total := 0
		for _, r := range reportsForCtx(ctx) {
			total += r.ActionCounts[core.Action(action)]
		}
		return total
	}
	funcs["import_count"] = func() int {
		total := 0
		for _, r := range reportsForCtx(ctx) {
			for _, mg := range r.ModuleGroups {
				for _, rc := range mg.Changes {
					if rc.IsImport {
						total++
					}
				}
			}
		}
		return total
	}

	// count_where / resources — predicate-based filtering helpers.
	// Multi-predicate AND semantics; csv values on `impact`, `action` are OR.
	// Predicates: action, impact, module, module_type, resource_type, is_import.
	funcs["count_where"] = func(args ...any) (int, error) {
		preds, err := parseWherePredicates(args)
		if err != nil {
			return 0, err
		}
		count := 0
		for _, r := range reportsForCtx(ctx) {
			for _, mg := range r.ModuleGroups {
				for _, rc := range mg.Changes {
					if whereMatch(r, mg, rc, preds) {
						count++
					}
				}
			}
		}
		return count, nil
	}
	funcs["resources"] = func(args ...any) ([]core.ResourceChange, error) {
		preds, err := parseWherePredicates(args)
		if err != nil {
			return nil, err
		}
		var out []core.ResourceChange
		for _, r := range reportsForCtx(ctx) {
			for _, mg := range r.ModuleGroups {
				for _, rc := range mg.Changes {
					if whereMatch(r, mg, rc, preds) {
						out = append(out, rc)
					}
				}
			}
		}
		return out, nil
	}

	return funcs
}

// wherePredicates is the parsed form of count_where/resources args.
type wherePredicates struct {
	actions       map[core.Action]struct{}
	impacts       map[core.Impact]struct{}
	modules       map[string]struct{}
	moduleTypes   map[string]struct{}
	resourceTypes map[string]struct{}
	isImport      *bool
}

// parseWherePredicates converts positional k,v args into a predicate set.
// Keys must be strings; values are csv-split where appropriate (action,
// impact, module, module_type, resource_type) or parsed as bool for
// is_import. Returns an error on odd argument counts, non-string keys, or
// unknown predicate names.
func parseWherePredicates(args []any) (wherePredicates, error) {
	var p wherePredicates
	if len(args)%2 != 0 {
		return p, fmt.Errorf("count_where/resources: expected key=value pairs, got %d arguments", len(args))
	}
	for i := 0; i < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			return p, fmt.Errorf("count_where/resources: argument %d: key must be a string, got %T", i, args[i])
		}
		val := fmt.Sprint(args[i+1])
		switch key {
		case "action":
			if p.actions == nil {
				p.actions = map[core.Action]struct{}{}
			}
			for _, v := range splitCSV(val) {
				p.actions[core.Action(v)] = struct{}{}
			}
		case "impact":
			if p.impacts == nil {
				p.impacts = map[core.Impact]struct{}{}
			}
			for _, v := range splitCSV(val) {
				p.impacts[core.Impact(v)] = struct{}{}
			}
		case "module":
			if p.modules == nil {
				p.modules = map[string]struct{}{}
			}
			for _, v := range splitCSV(val) {
				p.modules[strings.ToLower(v)] = struct{}{}
			}
		case "module_type":
			if p.moduleTypes == nil {
				p.moduleTypes = map[string]struct{}{}
			}
			for _, v := range splitCSV(val) {
				p.moduleTypes[strings.ToLower(v)] = struct{}{}
			}
		case "resource_type":
			if p.resourceTypes == nil {
				p.resourceTypes = map[string]struct{}{}
			}
			for _, v := range splitCSV(val) {
				p.resourceTypes[v] = struct{}{}
			}
		case "is_import":
			switch strings.ToLower(val) {
			case "true":
				b := true
				p.isImport = &b
			case "false":
				b := false
				p.isImport = &b
			default:
				return p, fmt.Errorf("count_where/resources: is_import must be true or false, got %q", val)
			}
		default:
			return p, fmt.Errorf("count_where/resources: unknown predicate %q (valid: action, impact, module, module_type, resource_type, is_import)", key)
		}
	}
	return p, nil
}

func splitCSV(s string) []string {
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

// whereMatch reports whether rc (inside mg of r) satisfies every predicate.
func whereMatch(r *core.Report, mg core.ModuleGroup, rc core.ResourceChange, p wherePredicates) bool {
	if p.actions != nil {
		if _, ok := p.actions[rc.Action]; !ok {
			return false
		}
	}
	if p.impacts != nil {
		if _, ok := p.impacts[rc.Impact]; !ok {
			return false
		}
	}
	if p.modules != nil {
		top := core.TopLevelModuleName(mg.Path)
		if _, ok := p.modules[strings.ToLower(top)]; !ok {
			if _, nameOk := p.modules[strings.ToLower(mg.Name)]; !nameOk {
				return false
			}
		}
	}
	if p.moduleTypes != nil {
		top := core.TopLevelModuleName(mg.Path)
		mt := core.ResolveModuleType(top, r.ModuleSources, mg.Name)
		if _, ok := p.moduleTypes[strings.ToLower(mt)]; !ok {
			return false
		}
	}
	if p.resourceTypes != nil {
		if _, ok := p.resourceTypes[rc.ResourceType]; !ok {
			return false
		}
	}
	if p.isImport != nil && rc.IsImport != *p.isImport {
		return false
	}
	return true
}

// sampleFn returns the first n elements of a slice (any element type).
// Non-slice input returns the input unchanged.
func sampleFn(n int, in any) any {
	v := reflect.ValueOf(in)
	if !v.IsValid() || v.Kind() != reflect.Slice {
		return in
	}
	if n >= v.Len() {
		return in
	}
	return v.Slice(0, n).Interface()
}

// reportsForCtx returns all reports in scope (single-report wrapped as slice).
func reportsForCtx(ctx *blocks.BlockContext) []*core.Report {
	if len(ctx.Reports) > 0 {
		return ctx.Reports
	}
	if ctx.Report != nil {
		return []*core.Report{ctx.Report}
	}
	return nil
}

// buildData pre-renders zero-arg blocks into struct fields for cheap access
// via {{ .Title }} etc.
func (e *Engine) buildData(ctx *blocks.BlockContext) (any, error) {
	title, err := e.registry.Render("title", ctx, nil)
	if err != nil {
		return nil, err
	}
	planCounts, err := e.registry.Render("plan_counts", ctx, nil)
	if err != nil {
		return nil, err
	}
	keyChanges, err := e.registry.Render("key_changes", ctx, nil)
	if err != nil {
		return nil, err
	}
	summaryTable, err := e.registry.Render("summary_table", ctx, nil)
	if err != nil {
		return nil, err
	}
	footer, err := e.registry.Render("footer", ctx, nil)
	if err != nil {
		return nil, err
	}
	deployChecklist, err := e.registry.Render("deploy_checklist", ctx, nil)
	if err != nil {
		return nil, err
	}
	crossSubTable := ""
	if len(ctx.Reports) > 1 {
		crossSubTable, err = e.registry.Render("summary_table", ctx, map[string]any{"group": "subscription"})
		if err != nil {
			return nil, err
		}
	}

	if len(ctx.Reports) > 1 {
		return &MultiReportData{
			Target:          ctx.Target,
			Reports:         ctx.Reports,
			Title:           title,
			PlanCounts:      planCounts,
			KeyChanges:      keyChanges,
			SummaryTable:    summaryTable,
			Footer:          footer,
			DeployChecklist: deployChecklist,
			CrossSubTable:   crossSubTable,
		}, nil
	}
	return &SingleReportData{
		Target:          ctx.Target,
		Report:          ctx.Report,
		Title:           title,
		PlanCounts:      planCounts,
		KeyChanges:      keyChanges,
		SummaryTable:    summaryTable,
		Footer:          footer,
		DeployChecklist: deployChecklist,
		CrossSubTable:   crossSubTable,
	}, nil
}
