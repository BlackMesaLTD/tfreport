package presetgen

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/presets"
)

var (
	// Matches: * `attr_name` - (Required/Optional) Description text.
	attrPattern = regexp.MustCompile("^\\* `([^`]+)` - \\((Required|Optional)\\) (.+)$")

	// Matches: * `attr_name` - (Required) or just * `attr_name` - Description
	attrLoosePattern = regexp.MustCompile("^\\* `([^`]+)` - (.+)$")

	// Matches section headers like: ## Argument Reference, ## Attributes Reference
	sectionPattern = regexp.MustCompile("^##\\s+(.+)$")

	// Matches block headers like: ---\nA `delegation` block supports:
	blockPattern = regexp.MustCompile("^(?:A|An|The) `([^`]+)` block (?:supports|exports|contains)")
)

// ParsedAttribute holds data extracted from a provider doc for a single attribute.
type ParsedAttribute struct {
	Name        string
	Description string
	Required    bool
	ForceNew    bool
	Block       string // parent block name, empty for top-level
}

// ParsedResource holds all attributes parsed from one resource doc file.
type ParsedResource struct {
	ResourceType string
	Attributes   []ParsedAttribute
}

// ParseDocFile parses a single provider doc markdown file and extracts
// attribute metadata including descriptions and force_new indicators.
func ParseDocFile(path string, providerPrefix string) (*ParsedResource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening doc file %s: %w", path, err)
	}
	defer f.Close()

	// Derive resource type from filename: subnet.html.markdown -> azurerm_subnet
	base := filepath.Base(path)
	resourceName := strings.TrimSuffix(strings.TrimSuffix(base, ".html.markdown"), ".markdown")
	resourceName = strings.TrimSuffix(resourceName, ".html.md")
	resourceName = strings.TrimSuffix(resourceName, ".md")
	resourceType := providerPrefix + "_" + resourceName

	var attrs []ParsedAttribute
	scanner := bufio.NewScanner(f)

	inArgSection := false
	currentBlock := ""

	for scanner.Scan() {
		line := scanner.Text()

		// Track section headers
		if m := sectionPattern.FindStringSubmatch(line); m != nil {
			header := strings.TrimSpace(m[1])
			lower := strings.ToLower(header)
			inArgSection = strings.Contains(lower, "argument") && strings.Contains(lower, "reference")
			if !inArgSection {
				// Also catch "Arguments Reference" variants
				inArgSection = strings.Contains(lower, "arguments")
			}
			// Reset block tracking on new section
			if !inArgSection {
				currentBlock = ""
			}
			continue
		}

		if !inArgSection {
			continue
		}

		// Track nested block sections
		if m := blockPattern.FindStringSubmatch(line); m != nil {
			currentBlock = m[1]
			continue
		}

		// Parse attribute lines
		if m := attrPattern.FindStringSubmatch(line); m != nil {
			name := m[1]
			required := m[2] == "Required"
			desc := m[3]
			forceNew := isForceNew(desc)

			attrs = append(attrs, ParsedAttribute{
				Name:        name,
				Description: cleanDescription(desc),
				Required:    required,
				ForceNew:    forceNew,
				Block:       currentBlock,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading doc file %s: %w", path, err)
	}

	return &ParsedResource{
		ResourceType: resourceType,
		Attributes:   attrs,
	}, nil
}

// ParseDocsDir parses all doc files in a directory for a given provider prefix.
// Only files matching the provider prefix naming convention are parsed.
func ParseDocsDir(dir string, providerPrefix string, resourceFilter map[string]bool) (map[string]*ParsedResource, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading docs directory %s: %w", dir, err)
	}

	results := make(map[string]*ParsedResource)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !isDocFile(name) {
			continue
		}

		path := filepath.Join(dir, name)
		parsed, err := ParseDocFile(path, providerPrefix)
		if err != nil {
			continue // skip unparseable files
		}

		// Apply resource filter if provided
		if resourceFilter != nil && !resourceFilter[parsed.ResourceType] {
			continue
		}

		if len(parsed.Attributes) > 0 {
			results[parsed.ResourceType] = parsed
		}
	}

	return results, nil
}

// ToPresetResources converts parsed doc data into preset ResourcePreset entries.
// Top-level attributes are included directly. Nested block attributes use
// "block.attr" as the key to avoid name collisions.
func ToPresetResources(parsed map[string]*ParsedResource) map[string]presets.ResourcePreset {
	resources := make(map[string]presets.ResourcePreset)

	for resType, pr := range parsed {
		attrs := make(map[string]presets.AttributePreset)
		for _, a := range pr.Attributes {
			key := a.Name
			if a.Block != "" {
				key = a.Block + "." + a.Name
			}
			attrs[key] = presets.AttributePreset{
				Description: a.Description,
				ForceNew:    a.ForceNew,
			}
		}

		resources[resType] = presets.ResourcePreset{
			Attributes: attrs,
		}
	}

	return resources
}

// isForceNew checks if a description indicates the attribute forces replacement.
func isForceNew(desc string) bool {
	lower := strings.ToLower(desc)
	return strings.Contains(lower, "forces a new resource") ||
		strings.Contains(lower, "forces a new") ||
		strings.Contains(lower, "forceNew") || //nolint:goconst
		strings.Contains(lower, "changing this forces")
}

// cleanDescription removes trailing force_new indicators and trims whitespace.
func cleanDescription(desc string) string {
	desc = strings.TrimSpace(desc)
	// Remove trailing period-separated force_new clause
	lower := strings.ToLower(desc)
	for _, pattern := range []string{
		"changing this forces a new resource to be created.",
		"forces a new resource to be created.",
		"changing this forces a new resource to be created",
		"forces a new resource to be created",
	} {
		idx := strings.Index(lower, pattern)
		if idx > 0 {
			desc = strings.TrimSpace(desc[:idx])
			desc = strings.TrimRight(desc, ".")
			break
		}
	}
	return desc
}

func isDocFile(name string) bool {
	return strings.HasSuffix(name, ".html.markdown") ||
		strings.HasSuffix(name, ".markdown") ||
		strings.HasSuffix(name, ".html.md") ||
		strings.HasSuffix(name, ".md")
}
