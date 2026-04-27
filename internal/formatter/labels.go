package formatter

import (
	"encoding/json"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// LabelsFormatter renders a manifest of GitHub label specs derived from one
// or more reports. The manifest is consumed by `scripts/gh_api.py --labels`
// to reconcile PR labels (JIT upsert + stale-removal). See
// internal/core/labels.go for the derivation rule.
//
// Output is a JSON array of LabelSpec objects. Reports that don't yield a
// spec (empty Label, all-no-op, all-read) are silently skipped — an empty
// array is a valid manifest meaning "no tfreport labels desired", which the
// applier reconciles by removing every prior tfreport-marked label.
type LabelsFormatter struct{}

func (f *LabelsFormatter) Format(report *core.Report) (string, error) {
	specs := []core.LabelSpec{}
	if spec, ok := core.DeriveLabel(report); ok {
		specs = append(specs, spec)
	}
	return marshalSpecs(specs)
}

func (f *LabelsFormatter) FormatMulti(reports []*core.Report) (string, error) {
	specs := make([]core.LabelSpec, 0, len(reports))
	for _, r := range reports {
		if spec, ok := core.DeriveLabel(r); ok {
			specs = append(specs, spec)
		}
	}
	return marshalSpecs(specs)
}

func marshalSpecs(specs []core.LabelSpec) (string, error) {
	data, err := json.MarshalIndent(specs, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}
