package formatter

import (
	"github.com/tfreport/tfreport/internal/core"
	"github.com/tfreport/tfreport/internal/formatter/blocks"
	"github.com/tfreport/tfreport/internal/formatter/template"
	"github.com/tfreport/tfreport/internal/formatter/templates"
)

// TemplateFormatter renders a report (or multiple reports) against a Go
// text/template. The template is either user-supplied via config or the
// embedded default shipped with tfreport. One formatter instance powers all
// four markdown-flavor targets (markdown, github-pr-body, github-pr-comment,
// github-step-summary); target grammar lives in the blocks that back each
// template variable, not in the formatter.
//
// Zero value is not usable; construct via NewTemplateFormatter.
type TemplateFormatter struct {
	Target       string
	UserTemplate string // empty → use embedded default
	Engine       *template.Engine
	Context      *blocks.BlockContext
}

// NewTemplateFormatter creates a formatter for the given target with the
// default block registry. Callers configure Context and (optionally)
// UserTemplate before calling Format or FormatMulti.
func NewTemplateFormatter(target string) *TemplateFormatter {
	return &TemplateFormatter{
		Target: target,
		Engine: template.New(blocks.Default()),
	}
}

// Format renders a single report.
func (f *TemplateFormatter) Format(report *core.Report) (string, error) {
	if f.Context == nil {
		f.Context = &blocks.BlockContext{Target: f.Target}
	}
	f.Context.Target = f.Target
	f.Context.Report = report
	f.Context.Reports = nil
	return f.Engine.Render(f.resolveTemplate(false), f.Context)
}

// FormatMulti renders an aggregated view over multiple reports.
func (f *TemplateFormatter) FormatMulti(reports []*core.Report) (string, error) {
	if f.Context == nil {
		f.Context = &blocks.BlockContext{Target: f.Target}
	}
	f.Context.Target = f.Target
	f.Context.Report = nil
	f.Context.Reports = reports
	return f.Engine.Render(f.resolveTemplate(true), f.Context)
}

func (f *TemplateFormatter) resolveTemplate(multi bool) string {
	if f.UserTemplate != "" {
		return f.UserTemplate
	}
	return templates.Default(f.Target, multi)
}
