---
subcategory: "Network"
layout: "azurerm"
page_title: "Azure Resource Manager: azurerm_subnet"
description: |-
  Manages a subnet.
---

# azurerm_subnet

Manages a subnet.

## Example Usage

```hcl
resource "azurerm_subnet" "example" {
  name                 = "example-subnet"
  resource_group_name  = azurerm_resource_group.example.name
  virtual_network_name = azurerm_virtual_network.example.name
  address_prefixes     = ["10.0.1.0/24"]
}
```

## Argument Reference

The following arguments are supported:

* `name` - (Required) The name of the subnet. Changing this forces a new resource to be created.

* `resource_group_name` - (Required) The name of the resource group in which to create the subnet. Changing this forces a new resource to be created.

* `virtual_network_name` - (Required) The name of the virtual network to which to attach the subnet. Changing this forces a new resource to be created.

* `address_prefixes` - (Required) The address prefixes to use for the subnet.

* `delegation` - (Optional) One or more `delegation` blocks as defined below.

* `service_endpoints` - (Optional) The list of Service endpoints to associate with the subnet.

* `service_endpoint_policy_ids` - (Optional) The list of IDs of Service Endpoint Policies to associate with the subnet.

---

A `delegation` block supports the following:

* `name` - (Required) A name for this delegation.

* `service_delegation` - (Required) A `service_delegation` block as defined below.

---

A `service_delegation` block supports the following:

* `name` - (Required) The name of service to delegate to.

* `actions` - (Optional) A list of Actions which should be delegated.

## Attributes Reference

In addition to the Arguments listed above - the following Attributes are exported:

* `id` - The subnet ID.
