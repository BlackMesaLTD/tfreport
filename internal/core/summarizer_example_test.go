package core

import "fmt"

func ExampleSummarize() {
	changes := []ResourceChange{
		{ResourceType: "azurerm_subnet", ResourceName: "app", Action: ActionUpdate, ChangedAttributes: []ChangedAttribute{{Key: "tags"}}},
		{ResourceType: "azurerm_subnet", ResourceName: "db", Action: ActionUpdate, ChangedAttributes: []ChangedAttribute{{Key: "tags"}}},
		{ResourceType: "azurerm_route_table", ResourceName: "main", Action: ActionUpdate, ChangedAttributes: []ChangedAttribute{{Key: "tags"}}},
		{ResourceType: "azurerm_private_endpoint", ResourceName: "pe-web", Action: ActionCreate, ChangedAttributes: []ChangedAttribute{{Key: "name"}}},
		{ResourceType: "azurerm_route", ResourceName: "legacy-1", Action: ActionDelete, ChangedAttributes: []ChangedAttribute{{Key: "name"}}},
		{ResourceType: "azurerm_route", ResourceName: "legacy-2", Action: ActionDelete, ChangedAttributes: []ChangedAttribute{{Key: "name"}}},
		{ResourceType: "azurerm_subnet", ResourceName: "critical", Action: ActionReplace},
	}

	sentences := Summarize(changes, nil)
	for _, s := range sentences {
		fmt.Println(s.Text)
	}
	// Output:
	// ⚠️ Tags updates across 3 subnet and route table
	// ✅ New private endpoint: pe-web
	// ❗ Removing 2 routes
	// ❗ Replacing subnet: critical (destroy + recreate)
}
