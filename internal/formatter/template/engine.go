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
	"text/template"

	sprig "github.com/Masterminds/sprig/v3"
	"github.com/tfreport/tfreport/internal/core"
	"github.com/tfreport/tfreport/internal/formatter/blocks"
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

	return funcs
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
