package core

import "testing"

func TestDeriveLabel(t *testing.T) {
	cases := []struct {
		name     string
		report   *Report
		wantOK   bool
		wantName string
		wantCol  string
	}{
		{
			name:   "nil report",
			report: nil,
			wantOK: false,
		},
		{
			name: "empty label skipped",
			report: &Report{
				Label:        "",
				ModuleGroups: groupsFromActions(ActionCreate),
			},
			wantOK: false,
		},
		{
			name: "all-no-op skipped",
			report: &Report{
				Label:        "sub-eastus",
				ModuleGroups: groupsFromActions(ActionNoOp, ActionNoOp),
			},
			wantOK: false,
		},
		{
			name: "all-read skipped",
			report: &Report{
				Label:        "sub-eastus",
				ModuleGroups: groupsFromActions(ActionRead),
			},
			wantOK: false,
		},
		{
			name: "create only -> [ + ] green",
			report: &Report{
				Label:        "sub-eastus",
				ModuleGroups: groupsFromActions(ActionCreate, ActionCreate),
			},
			wantOK:   true,
			wantName: "[ + ] sub-eastus",
			wantCol:  LabelColorGreen,
		},
		{
			name: "update only -> [ ~ ] amber",
			report: &Report{
				Label:        "sub-eastus",
				ModuleGroups: groupsFromActions(ActionUpdate),
			},
			wantOK:   true,
			wantName: "[ ~ ] sub-eastus",
			wantCol:  LabelColorAmber,
		},
		{
			name: "delete only -> [ - ] red",
			report: &Report{
				Label:        "sub-eastus",
				ModuleGroups: groupsFromActions(ActionDelete),
			},
			wantOK:   true,
			wantName: "[ - ] sub-eastus",
			wantCol:  LabelColorRed,
		},
		{
			name: "replace only -> [ + - ] red (dual char)",
			report: &Report{
				Label:        "sub-eastus",
				ModuleGroups: groupsFromActions(ActionReplace),
			},
			wantOK:   true,
			wantName: "[ + - ] sub-eastus",
			wantCol:  LabelColorRed,
		},
		{
			name: "create + update + delete -> [ + ~ - ] red",
			report: &Report{
				Label:        "sub-eastus",
				ModuleGroups: groupsFromActions(ActionCreate, ActionUpdate, ActionDelete),
			},
			wantOK:   true,
			wantName: "[ + ~ - ] sub-eastus",
			wantCol:  LabelColorRed,
		},
		{
			name: "create + update -> [ + ~ ] amber (red doesn't fire)",
			report: &Report{
				Label:        "sub-eastus",
				ModuleGroups: groupsFromActions(ActionCreate, ActionUpdate),
			},
			wantOK:   true,
			wantName: "[ + ~ ] sub-eastus",
			wantCol:  LabelColorAmber,
		},
		{
			name: "pure import (no-op + IsImport) -> [ ~ ] amber",
			report: &Report{
				Label:        "sub-eastus",
				ModuleGroups: groupsFromImports(true, ActionNoOp),
			},
			wantOK:   true,
			wantName: "[ ~ ] sub-eastus",
			wantCol:  LabelColorAmber,
		},
		{
			name: "import + create -> [ + ~ ] (import contributes ~ on top of create's +)",
			report: &Report{
				Label: "sub-eastus",
				ModuleGroups: []ModuleGroup{
					{
						Changes: []ResourceChange{
							{Action: ActionCreate},
							{Action: ActionNoOp, IsImport: true},
						},
					},
				},
			},
			wantOK:   true,
			wantName: "[ + ~ ] sub-eastus",
			wantCol:  LabelColorAmber,
		},
		{
			name: "import on a non-no-op action does NOT double-count",
			report: &Report{
				Label: "sub-eastus",
				ModuleGroups: []ModuleGroup{
					{
						Changes: []ResourceChange{
							{Action: ActionCreate, IsImport: true},
						},
					},
				},
			},
			wantOK:   true,
			wantName: "[ + ] sub-eastus",
			wantCol:  LabelColorGreen,
		},
		{
			name: "char ordering is + before ~ before - regardless of input order",
			report: &Report{
				Label:        "sub-eastus",
				ModuleGroups: groupsFromActions(ActionDelete, ActionUpdate, ActionCreate),
			},
			wantOK:   true,
			wantName: "[ + ~ - ] sub-eastus",
			wantCol:  LabelColorRed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DeriveLabel(tc.report)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
			if got.Color != tc.wantCol {
				t.Errorf("Color = %q, want %q", got.Color, tc.wantCol)
			}
			wantDesc := LabelDescriptionText + LabelMarker
			if got.Description != wantDesc {
				t.Errorf("Description = %q, want %q", got.Description, wantDesc)
			}
		})
	}
}

func TestDeriveLabel_DescriptionMarkerSuffix(t *testing.T) {
	r := &Report{
		Label:        "sub-eastus",
		ModuleGroups: groupsFromActions(ActionCreate),
	}
	spec, ok := DeriveLabel(r)
	if !ok {
		t.Fatal("expected spec to emit")
	}
	if got, want := spec.Description[len(spec.Description)-len(LabelMarker):], LabelMarker; got != want {
		t.Errorf("description does not end with marker: %q", spec.Description)
	}
}

// groupsFromActions builds a single ModuleGroup whose Changes carry the
// supplied actions in order. IsImport is false on all entries.
func groupsFromActions(actions ...Action) []ModuleGroup {
	changes := make([]ResourceChange, len(actions))
	for i, a := range actions {
		changes[i] = ResourceChange{Action: a}
	}
	return []ModuleGroup{{Changes: changes}}
}

// groupsFromImports builds a single ModuleGroup whose Changes carry the
// supplied actions, all with IsImport=isImport.
func groupsFromImports(isImport bool, actions ...Action) []ModuleGroup {
	changes := make([]ResourceChange, len(actions))
	for i, a := range actions {
		changes[i] = ResourceChange{Action: a, IsImport: isImport}
	}
	return []ModuleGroup{{Changes: changes}}
}
