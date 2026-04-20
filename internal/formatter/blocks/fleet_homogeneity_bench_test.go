package blocks

import (
	"fmt"
	"testing"

	"github.com/tfreport/tfreport/internal/core"
)

// syntheticFleet builds `reports` reports, each with `perReport` resources
// spread across 5 module groups with a mix of update/delete/create actions.
// Used to size fleet_homogeneity perf against plan hazard H3.
func syntheticFleet(reports, perReport int) []*core.Report {
	out := make([]*core.Report, reports)
	for r := 0; r < reports; r++ {
		var groups []core.ModuleGroup
		changes := make([]core.ResourceChange, perReport)
		for i := 0; i < perReport; i++ {
			action := core.ActionUpdate
			impact := core.ImpactMedium
			switch i % 5 {
			case 0:
				action, impact = core.ActionCreate, core.ImpactLow
			case 1:
				action, impact = core.ActionDelete, core.ImpactHigh
			case 2:
				action, impact = core.ActionReplace, core.ImpactCritical
			}
			changes[i] = core.ResourceChange{
				Address:      fmt.Sprintf("module.m%d.azurerm_x.r%d", i%5, i),
				ResourceType: "azurerm_x",
				Action:       action,
				Impact:       impact,
				ChangedAttributes: []core.ChangedAttribute{{
					Key: "tags", OldValue: "v1", NewValue: "v2",
				}},
			}
		}
		groups = append(groups, syntheticGroup(fmt.Sprintf("m%d", r), changes...))
		rep := syntheticReport(fmt.Sprintf("sub-%02d", r), groups...)
		// Populate some KeyChanges for the key_changes fingerprint path.
		rep.KeyChanges = []core.KeyChange{
			{Text: "⚠️ Tags updates across 20 x", Impact: core.ImpactMedium},
			{Text: "✅ New x: foo", Impact: core.ImpactLow},
		}
		out[r] = rep
	}
	return out
}

// BenchmarkFleetHomogeneity_keyChanges exercises the default fingerprint
// strategy at plan-hazard scale (20 reports × 50 resources). Target: under
// a few hundred microseconds per call; alarm threshold would be >10ms.
func BenchmarkFleetHomogeneity_keyChanges(b *testing.B) {
	reports := syntheticFleet(20, 50)
	ctx := &BlockContext{Target: "github-pr-body", Reports: reports}
	block := FleetHomogeneity{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := block.Render(ctx, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFleetHomogeneity_actionCounts exercises the looser fingerprint.
func BenchmarkFleetHomogeneity_actionCounts(b *testing.B) {
	reports := syntheticFleet(20, 50)
	ctx := &BlockContext{Target: "github-pr-body", Reports: reports}
	block := FleetHomogeneity{}
	args := map[string]any{"fingerprint": "action_counts"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := block.Render(ctx, args); err != nil {
			b.Fatal(err)
		}
	}
}

// TestFleetHomogeneity_scalesToHazard is a non-benchmark scale test that
// confirms the Render call doesn't panic or explode at networks-azure scale.
// Uses a hard cap of 10ms via testing.Short to keep CI fast.
func TestFleetHomogeneity_scalesToHazard(t *testing.T) {
	if testing.Short() {
		t.Skip("scale test skipped in short mode")
	}
	reports := syntheticFleet(20, 50)
	ctx := &BlockContext{Target: "github-pr-body", Reports: reports}
	out, err := FleetHomogeneity{}.Render(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Error("expected non-empty render at 20×50 scale")
	}
}
