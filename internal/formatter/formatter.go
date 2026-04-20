package formatter

import (
	"fmt"

	"github.com/tfreport/tfreport/internal/core"
)

// Formatter formats a Report into a string for a specific output target.
type Formatter interface {
	Format(report *core.Report) (string, error)
}

// MultiReportFormatter formats multiple reports into a single aggregated
// output. Since the template engine unifies rendering, every markdown-flavor
// target now supports this interface.
type MultiReportFormatter interface {
	Formatter
	FormatMulti(reports []*core.Report) (string, error)
}

// Get returns a formatter for the given target name. The four markdown-flavor
// targets share a single TemplateFormatter; json stays on its own dedicated
// implementation because it's the canonical interchange format.
func Get(target string) (Formatter, error) {
	switch target {
	case "markdown", "github-pr-body", "github-pr-comment", "github-step-summary":
		return NewTemplateFormatter(target), nil
	case "json":
		return &JSONFormatter{}, nil
	default:
		return nil, fmt.Errorf("unknown target: %q (valid: %v)", target, ValidTargets())
	}
}

// ValidTargets returns the list of valid target names.
func ValidTargets() []string {
	return []string{"markdown", "github-pr-body", "github-pr-comment", "github-step-summary", "json"}
}
