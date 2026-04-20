package core

import "testing"

func TestClassifyImpactDefaults(t *testing.T) {
	changes := []ResourceChange{
		{Action: ActionCreate},
		{Action: ActionUpdate},
		{Action: ActionDelete},
		{Action: ActionReplace},
		{Action: ActionRead},
		{Action: ActionNoOp},
	}

	ClassifyImpact(changes, nil, nil)

	expected := []Impact{ImpactLow, ImpactMedium, ImpactHigh, ImpactCritical, ImpactLow, ImpactNone}
	for i, rc := range changes {
		if rc.Impact != expected[i] {
			t.Errorf("changes[%d].Impact = %q, want %q", i, rc.Impact, expected[i])
		}
	}
}

func TestClassifyImpactWithOverrides(t *testing.T) {
	changes := []ResourceChange{
		{Action: ActionCreate},
		{Action: ActionUpdate},
	}

	overrides := map[Action]Impact{
		ActionCreate: ImpactMedium, // override default (low -> medium)
	}

	ClassifyImpact(changes, overrides, nil)

	if changes[0].Impact != ImpactMedium {
		t.Errorf("create impact = %q, want %q", changes[0].Impact, ImpactMedium)
	}
	if changes[1].Impact != ImpactMedium {
		t.Errorf("update impact = %q, want %q (default)", changes[1].Impact, ImpactMedium)
	}
}

func TestClassifyImpactWithAttributeResolver(t *testing.T) {
	resolver := func(resourceType, attrName string) (Impact, bool) {
		if attrName == "tags" {
			return ImpactNone, true
		}
		if resourceType == "azurerm_virtual_network" && attrName == "name" {
			return ImpactCritical, true
		}
		return "", false
	}

	t.Run("all attrs resolved - tags only update is none impact", func(t *testing.T) {
		changes := []ResourceChange{
			{
				Action:       ActionUpdate,
				ResourceType: "azurerm_subnet",
				ChangedAttributes: []ChangedAttribute{
					{Key: "tags"},
				},
			},
		}
		ClassifyImpact(changes, nil, resolver)
		if changes[0].Impact != ImpactNone {
			t.Errorf("impact = %q, want none (tags-only update)", changes[0].Impact)
		}
	})

	t.Run("mixed attrs - highest wins when all resolved", func(t *testing.T) {
		changes := []ResourceChange{
			{
				Action:       ActionUpdate,
				ResourceType: "azurerm_virtual_network",
				ChangedAttributes: []ChangedAttribute{
					{Key: "tags"},
					{Key: "name"},
				},
			},
		}
		ClassifyImpact(changes, nil, resolver)
		if changes[0].Impact != ImpactCritical {
			t.Errorf("impact = %q, want critical (name override)", changes[0].Impact)
		}
	})

	t.Run("unresolved attr falls back to action default", func(t *testing.T) {
		changes := []ResourceChange{
			{
				Action:       ActionUpdate,
				ResourceType: "azurerm_subnet",
				ChangedAttributes: []ChangedAttribute{
					{Key: "tags"},
					{Key: "address_prefixes"}, // not in resolver
				},
			},
		}
		ClassifyImpact(changes, nil, resolver)
		if changes[0].Impact != ImpactMedium {
			t.Errorf("impact = %q, want medium (action default)", changes[0].Impact)
		}
	})

	t.Run("non-update actions skip attribute resolution", func(t *testing.T) {
		changes := []ResourceChange{
			{
				Action:       ActionCreate,
				ResourceType: "azurerm_subnet",
				ChangedAttributes: []ChangedAttribute{
					{Key: "tags"},
				},
			},
		}
		ClassifyImpact(changes, nil, resolver)
		if changes[0].Impact != ImpactLow {
			t.Errorf("impact = %q, want low (create default)", changes[0].Impact)
		}
	})
}

func TestMaxImpactForGroup(t *testing.T) {
	group := ModuleGroup{
		Changes: []ResourceChange{
			{Impact: ImpactLow},
			{Impact: ImpactHigh},
			{Impact: ImpactMedium},
		},
	}

	if got := MaxImpactForGroup(group); got != ImpactHigh {
		t.Errorf("max impact = %q, want %q", got, ImpactHigh)
	}
}

func TestMaxImpactOverall(t *testing.T) {
	groups := []ModuleGroup{
		{Changes: []ResourceChange{{Impact: ImpactLow}}},
		{Changes: []ResourceChange{{Impact: ImpactCritical}}},
		{Changes: []ResourceChange{{Impact: ImpactMedium}}},
	}

	if got := MaxImpactOverall(groups); got != ImpactCritical {
		t.Errorf("max impact = %q, want %q", got, ImpactCritical)
	}
}

func TestMaxImpactEmptyGroup(t *testing.T) {
	group := ModuleGroup{}
	if got := MaxImpactForGroup(group); got != ImpactNone {
		t.Errorf("max impact = %q, want %q", got, ImpactNone)
	}
}
