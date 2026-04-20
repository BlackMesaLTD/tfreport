---
subcategory: "Network"
layout: "azurerm"
page_title: "Azure Resource Manager: azurerm_virtual_network"
description: |-
  Manages a virtual network.
---

# azurerm_virtual_network

Manages a virtual network including any configured subnets.

## Argument Reference

The following arguments are supported:

* `name` - (Required) The name of the virtual network. Changing this forces a new resource to be created.

* `resource_group_name` - (Required) The name of the resource group in which to create the virtual network. Changing this forces a new resource to be created.

* `address_space` - (Required) The address space that is used the virtual network.

* `location` - (Required) The location/region where the virtual network is created. Changing this forces a new resource to be created.

* `dns_servers` - (Optional) List of IP addresses of DNS servers.

* `tags` - (Optional) A mapping of tags to assign to the resource.

## Attributes Reference

* `id` - The virtual network ID.
