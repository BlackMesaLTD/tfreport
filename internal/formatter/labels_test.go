package formatter

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

func TestLabelsFormatter_GetRegistration(t *testing.T) {
	f, err := Get("labels")
	if err != nil {
		t.Fatalf("Get(\"labels\") error: %v", err)
	}
	if _, ok := f.(*LabelsFormatter); !ok {
		t.Fatalf("Get(\"labels\") returned %T, want *LabelsFormatter", f)
	}

	mf, ok := f.(MultiReportFormatter)
	if !ok {
		t.Fatalf("LabelsFormatter does not implement MultiReportFormatter")
	}
	_ = mf

	if !slices.Contains(ValidTargets(), "labels") {
		t.Errorf("ValidTargets() does not include \"labels\": %v", ValidTargets())
	}
}

func TestLabelsFormatter_SingleReport(t *testing.T) {
	r := &core.Report{
		Label: "sub-eastus",
		ModuleGroups: []core.ModuleGroup{
			{Changes: []core.ResourceChange{{Action: core.ActionCreate}}},
		},
	}
	out, err := (&LabelsFormatter{}).Format(r)
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}
	specs := unmarshalSpecs(t, out)
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d (%s)", len(specs), out)
	}
	if specs[0].Name != "[ + ] sub-eastus" {
		t.Errorf("Name = %q, want %q", specs[0].Name, "[ + ] sub-eastus")
	}
	if specs[0].Color != core.LabelColorGreen {
		t.Errorf("Color = %q, want %q", specs[0].Color, core.LabelColorGreen)
	}
	if specs[0].Description != core.LabelDescriptionText+core.LabelMarker {
		t.Errorf("Description = %q", specs[0].Description)
	}
}

func TestLabelsFormatter_SingleReport_EmptyLabel_EmitsEmptyArray(t *testing.T) {
	r := &core.Report{
		Label: "", // skipped by DeriveLabel
		ModuleGroups: []core.ModuleGroup{
			{Changes: []core.ResourceChange{{Action: core.ActionCreate}}},
		},
	}
	out, err := (&LabelsFormatter{}).Format(r)
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}
	specs := unmarshalSpecs(t, out)
	if len(specs) != 0 {
		t.Errorf("expected empty array, got %d specs", len(specs))
	}
}

func TestLabelsFormatter_SingleReport_AllNoOp_EmitsEmptyArray(t *testing.T) {
	r := &core.Report{
		Label: "sub-eastus",
		ModuleGroups: []core.ModuleGroup{
			{Changes: []core.ResourceChange{{Action: core.ActionNoOp}, {Action: core.ActionNoOp}}},
		},
	}
	out, err := (&LabelsFormatter{}).Format(r)
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}
	specs := unmarshalSpecs(t, out)
	if len(specs) != 0 {
		t.Errorf("expected empty array (no chars contributed), got %d specs", len(specs))
	}
}

func TestLabelsFormatter_MultiReport(t *testing.T) {
	a := &core.Report{
		Label:        "sub-a",
		ModuleGroups: []core.ModuleGroup{{Changes: []core.ResourceChange{{Action: core.ActionCreate}}}},
	}
	b := &core.Report{
		Label:        "sub-b",
		ModuleGroups: []core.ModuleGroup{{Changes: []core.ResourceChange{{Action: core.ActionDelete}}}},
	}
	out, err := (&LabelsFormatter{}).FormatMulti([]*core.Report{a, b})
	if err != nil {
		t.Fatalf("FormatMulti error: %v", err)
	}
	specs := unmarshalSpecs(t, out)
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs, got %d (%s)", len(specs), out)
	}
	if specs[0].Name != "[ + ] sub-a" || specs[0].Color != core.LabelColorGreen {
		t.Errorf("specs[0] = %+v", specs[0])
	}
	if specs[1].Name != "[ - ] sub-b" || specs[1].Color != core.LabelColorRed {
		t.Errorf("specs[1] = %+v", specs[1])
	}
}

func TestLabelsFormatter_MultiReport_SkipsEmptyLabelReports(t *testing.T) {
	a := &core.Report{
		Label:        "sub-a",
		ModuleGroups: []core.ModuleGroup{{Changes: []core.ResourceChange{{Action: core.ActionCreate}}}},
	}
	b := &core.Report{
		Label:        "", // should be skipped
		ModuleGroups: []core.ModuleGroup{{Changes: []core.ResourceChange{{Action: core.ActionDelete}}}},
	}
	c := &core.Report{
		Label:        "sub-c",
		ModuleGroups: []core.ModuleGroup{{Changes: []core.ResourceChange{{Action: core.ActionUpdate}}}},
	}
	out, err := (&LabelsFormatter{}).FormatMulti([]*core.Report{a, b, c})
	if err != nil {
		t.Fatalf("FormatMulti error: %v", err)
	}
	specs := unmarshalSpecs(t, out)
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs (b skipped), got %d (%s)", len(specs), out)
	}
	if specs[0].Name != "[ + ] sub-a" {
		t.Errorf("specs[0].Name = %q", specs[0].Name)
	}
	if specs[1].Name != "[ ~ ] sub-c" {
		t.Errorf("specs[1].Name = %q", specs[1].Name)
	}
}

func TestLabelsFormatter_OutputIsValidJSON(t *testing.T) {
	r := &core.Report{
		Label:        "sub-eastus",
		ModuleGroups: []core.ModuleGroup{{Changes: []core.ResourceChange{{Action: core.ActionReplace}}}},
	}
	out, err := (&LabelsFormatter{}).Format(r)
	if err != nil {
		t.Fatalf("Format error: %v", err)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("output is not valid JSON: %q", out)
	}
}

func unmarshalSpecs(t *testing.T, raw string) []core.LabelSpec {
	t.Helper()
	var specs []core.LabelSpec
	if err := json.Unmarshal([]byte(raw), &specs); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, raw)
	}
	return specs
}
