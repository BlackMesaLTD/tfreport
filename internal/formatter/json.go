package formatter

import (
	"github.com/tfreport/tfreport/internal/core"
)

// JSONFormatter outputs the full report as structured JSON.
// This JSON is the canonical tfreport interchange format and can be
// re-imported via --report-file.
type JSONFormatter struct{}

func (f *JSONFormatter) Format(report *core.Report) (string, error) {
	data, err := core.MarshalReport(report)
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}
