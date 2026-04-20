package core

import (
	"strings"
	"testing"
)

func TestSummarizeTagUpdates(t *testing.T) {
	changes := []ResourceChange{
		{
			ResourceType:      "azurerm_subnet",
			ResourceName:      "app",
			Action:            ActionUpdate,
			ChangedAttributes: []ChangedAttribute{{Key: "tags"}},
		},
		{
			ResourceType:      "azurerm_subnet",
			ResourceName:      "db",
			Action:            ActionUpdate,
			ChangedAttributes: []ChangedAttribute{{Key: "tags"}},
		},
		{
			ResourceType:      "azurerm_route_table",
			ResourceName:      "main",
			Action:            ActionUpdate,
			ChangedAttributes: []ChangedAttribute{{Key: "tags"}},
		},
	}

	sentences := Summarize(changes, nil)

	if len(sentences) != 1 {
		t.Fatalf("expected 1 sentence (merged across types), got %d: %v", len(sentences), sentences)
	}

	s := sentences[0].Text
	// Should mention "Tags updates across 3 subnet and route table"
	if !strings.Contains(s, "Tags") && !strings.Contains(s, "tags") {
		t.Errorf("expected sentence to mention tags: %q", s)
	}
	if !strings.Contains(s, "3") {
		t.Errorf("expected sentence to mention count 3: %q", s)
	}
}

func TestSummarizeMixedActions(t *testing.T) {
	changes := []ResourceChange{
		{
			ResourceType:      "azurerm_subnet",
			ResourceName:      "app",
			Action:            ActionUpdate,
			ChangedAttributes: []ChangedAttribute{{Key: "tags"}},
		},
		{
			ResourceType:      "azurerm_private_endpoint",
			ResourceName:      "web",
			Action:            ActionCreate,
			ChangedAttributes: []ChangedAttribute{{Key: "name"}, {Key: "location"}},
		},
		{
			ResourceType:      "azurerm_route",
			ResourceName:      "legacy-1",
			Action:            ActionDelete,
			ChangedAttributes: []ChangedAttribute{{Key: "name"}},
		},
		{
			ResourceType:      "azurerm_route",
			ResourceName:      "legacy-2",
			Action:            ActionDelete,
			ChangedAttributes: []ChangedAttribute{{Key: "name"}},
		},
	}

	sentences := Summarize(changes, nil)

	if len(sentences) != 3 {
		t.Fatalf("expected 3 sentences, got %d: %v", len(sentences), sentences)
	}

	// Check each sentence type
	hasUpdate := false
	hasCreate := false
	hasDelete := false
	for _, kc := range sentences {
		s := kc.Text
		if strings.Contains(s, "update") || strings.Contains(s, "Tags") {
			hasUpdate = true
		}
		if strings.Contains(s, "New") || strings.Contains(s, "new") {
			hasCreate = true
		}
		if strings.Contains(s, "Removing") {
			hasDelete = true
		}
	}

	if !hasUpdate {
		t.Error("missing update sentence")
	}
	if !hasCreate {
		t.Error("missing create sentence")
	}
	if !hasDelete {
		t.Error("missing delete sentence")
	}
}

func TestSummarizeReplace(t *testing.T) {
	changes := []ResourceChange{
		{
			ResourceType: "azurerm_subnet",
			ResourceName: "app",
			Action:       ActionReplace,
		},
	}

	sentences := Summarize(changes, nil)

	if len(sentences) != 1 {
		t.Fatalf("expected 1 sentence, got %d", len(sentences))
	}

	if !strings.Contains(sentences[0].Text, "Replacing") {
		t.Errorf("expected 'Replacing' in sentence: %q", sentences[0])
	}
	if !strings.Contains(sentences[0].Text, "destroy + recreate") {
		t.Errorf("expected 'destroy + recreate' in sentence: %q", sentences[0])
	}
}

func TestSummarizeSingleCreate(t *testing.T) {
	changes := []ResourceChange{
		{
			ResourceType:      "azurerm_private_endpoint",
			ResourceName:      "pe-web",
			Action:            ActionCreate,
			ChangedAttributes: []ChangedAttribute{{Key: "name"}},
		},
	}

	sentences := Summarize(changes, nil)

	if len(sentences) != 1 {
		t.Fatalf("expected 1 sentence, got %d", len(sentences))
	}

	s := sentences[0].Text
	if !strings.Contains(s, "New") {
		t.Errorf("expected 'New' in sentence: %q", s)
	}
	if !strings.Contains(s, "private endpoint") {
		t.Errorf("expected 'private endpoint' in sentence: %q", s)
	}
	if !strings.Contains(s, "pe-web") {
		t.Errorf("expected resource name 'pe-web' in sentence: %q", s)
	}
}

func TestSummarizeUsesActualName(t *testing.T) {
	changes := []ResourceChange{
		{
			ResourceType:      "azurerm_network_security_group",
			ResourceName:      "main",
			Action:            ActionCreate,
			After:             map[string]any{"name": "nsg-web-prod-001"},
			ChangedAttributes: []ChangedAttribute{{Key: "name"}},
		},
	}

	sentences := Summarize(changes, nil)

	if len(sentences) != 1 {
		t.Fatalf("expected 1 sentence, got %d", len(sentences))
	}
	// Should use the actual name "nsg-web-prod-001" not the TF name "main"
	if !strings.Contains(sentences[0].Text, "nsg-web-prod-001") {
		t.Errorf("expected actual name in sentence: %q", sentences[0].Text)
	}
	if strings.Contains(sentences[0].Text, ": main") {
		t.Errorf("should not use TF resource name 'main': %q", sentences[0].Text)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	sentences := Summarize(nil, nil)
	if sentences != nil {
		t.Errorf("expected nil, got %v", sentences)
	}
}

func TestSummarizeNoOpSkipped(t *testing.T) {
	changes := []ResourceChange{
		{ResourceType: "azurerm_subnet", ResourceName: "app", Action: ActionNoOp},
	}

	sentences := Summarize(changes, nil)
	if len(sentences) != 0 {
		t.Errorf("expected 0 sentences for no-op, got %d: %v", len(sentences), sentences)
	}
}

func TestSummarizeMultipleAttrs(t *testing.T) {
	changes := []ResourceChange{
		{
			ResourceType:      "azurerm_subnet",
			ResourceName:      "app",
			Action:            ActionUpdate,
			ChangedAttributes: []ChangedAttribute{{Key: "tags"}, {Key: "address_prefixes"}},
		},
	}

	sentences := Summarize(changes, nil)

	if len(sentences) != 1 {
		t.Fatalf("expected 1 sentence, got %d", len(sentences))
	}

	s := sentences[0].Text
	if !strings.Contains(s, "address_prefixes") || !strings.Contains(s, "tags") {
		t.Errorf("expected both attrs in sentence: %q", s)
	}
}

func TestDisplayNameFallback(t *testing.T) {
	// Unknown resource type should strip provider prefix
	got := displayName("azurerm_some_new_resource", nil)
	if got != "some new resource" {
		t.Errorf("displayName = %q, want %q", got, "some new resource")
	}
}

func TestDisplayNameFromPreset(t *testing.T) {
	names := map[string]string{"azurerm_subnet": "subnet"}
	got := displayName("azurerm_subnet", names)
	if got != "subnet" {
		t.Errorf("displayName = %q, want %q", got, "subnet")
	}
}

func TestPluralizeSingleChar(t *testing.T) {
	// Ensure pluralize doesn't panic on single-character strings
	got := pluralize("y")
	if got != "ys" {
		t.Errorf("pluralize(\"y\") = %q, want %q", got, "ys")
	}

	got = pluralize("s")
	if got != "ses" {
		t.Errorf("pluralize(\"s\") = %q, want %q", got, "ses")
	}

	got = pluralize("")
	if got != "s" {
		t.Errorf("pluralize(\"\") = %q, want %q", got, "s")
	}
}
