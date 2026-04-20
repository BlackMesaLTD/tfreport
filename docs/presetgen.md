# Preset Generator (presetgen)

`presetgen` generates enriched tfreport preset JSON files from Terraform provider documentation markdown.

## What It Does

Terraform provider docs (`website/docs/r/*.html.markdown`) contain per-attribute metadata that isn't available from `terraform providers schema -json`:

- **Descriptions** — human-readable attribute descriptions
- **ForceNew** — detected via text pattern "forces a new resource to be created"

`presetgen` parses these docs and merges the extracted data with existing preset files and optional schema data.

## Installation

```bash
go install github.com/BlackMesaLTD/tfreport/cmd/presetgen@latest

# Or build from source
make build-presetgen
```

## Usage

### Basic — Parse docs directory

```bash
presetgen --provider azurerm \
  --docs-dir ./terraform-provider-azurerm/website/docs/r/ \
  --output azurerm.json
```

### Merge with existing preset

Preserves display names and manual entries from the existing preset:

```bash
presetgen --provider azurerm \
  --docs-dir ./website/docs/r/ \
  --existing-preset ./internal/presets/builtin/azurerm.json \
  --version "4.46.0" \
  --output enriched-azurerm.json
```

### Filter specific resources

Only include networking-related types:

```bash
presetgen --provider azurerm \
  --docs-dir ./website/docs/r/ \
  --resources "azurerm_subnet,azurerm_virtual_network,azurerm_route_table" \
  --output networking.json
```

### With terraform schema overlay

For structural data (computed, sensitive) not in docs:

```bash
terraform providers schema -json > schema.json

presetgen --provider azurerm \
  --docs-dir ./website/docs/r/ \
  --schema-file schema.json \
  --output full-azurerm.json
```

## CLI Reference

```
presetgen [flags]

Flags:
      --provider string          Provider name prefix, e.g. azurerm, aws, google (required)
      --docs-dir string          Path to provider docs/r/ directory (required)
  -o, --output string            Output path for generated preset JSON (required)
      --schema-file string       Path to terraform providers schema -json output (optional)
      --existing-preset string   Merge with existing preset JSON, preserves display_names (optional)
      --resources string         Comma-separated resource types to include, default: all (optional)
      --version string           Provider version string for the preset metadata (optional)
```

## How It Works

### Doc Parsing

The parser processes `*.html.markdown` files from the `## Argument Reference` section:

1. **Attribute extraction** — regex: `` * `attr_name` - (Required/Optional) Description. ``
2. **ForceNew detection** — text contains "forces a new resource" or "changing this forces"
3. **Nested blocks** — tracks section context to prefix nested attributes (e.g., `delegation.name`)
4. **Resource type derivation** — filename: `subnet.html.markdown` → `azurerm_subnet`

### Merge Strategy

When combining data sources:

- **Existing preset** — loaded first as base (preserves display_names, manual entries)
- **Doc-parsed data** — overlaid on top (fills in missing descriptions, force_new)
- **Schema data** — fills any remaining gaps (description from schema if docs didn't have one)

Existing entries are never overwritten — new data only fills in empty fields.

### Output Format

The generated JSON matches the standard preset schema:

```json
{
  "provider": "azurerm",
  "version": "4.46.0",
  "resources": {
    "azurerm_subnet": {
      "display_name": "subnet",
      "attributes": {
        "name": {
          "description": "The name of the subnet",
          "force_new": true
        },
        "address_prefixes": {
          "description": "The address prefixes to use for the subnet.",
          "force_new": false
        },
        "delegation.name": {
          "description": "A name for this delegation."
        }
      }
    }
  }
}
```

Nested block attributes use dot notation (`delegation.name`) to avoid name collisions with top-level attributes.

## Supported Providers

Any provider following the HashiCorp docs convention works:

- **azurerm** — tested, 54 resource types in bundled preset
- **aws** — same doc format, untested
- **google** — same doc format, untested

## Workflow: Updating Presets on Provider Upgrade

```bash
# 1. Clone provider at new version
git clone --depth 1 --branch v4.47.0 \
  https://github.com/hashicorp/terraform-provider-azurerm.git

# 2. Generate enriched preset
presetgen --provider azurerm \
  --docs-dir ./terraform-provider-azurerm/website/docs/r/ \
  --existing-preset ./internal/presets/builtin/azurerm.json \
  --version "4.47.0" \
  --output ./internal/presets/builtin/azurerm.json

# 3. Run tests
go test ./internal/presets/

# 4. Commit
git add internal/presets/builtin/azurerm.json
git commit -m "chore: update azurerm preset to v4.47.0"
```
