package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/BlackMesaLTD/tfreport/internal/presetgen"
	"github.com/BlackMesaLTD/tfreport/internal/presets"
)

var (
	flagProvider       string
	flagDocsDir        string
	flagSchemaFile     string
	flagExistingPreset string
	flagOutput         string
	flagResources      string
	flagVersion        string
)

var rootCmd = &cobra.Command{
	Use:   "presetgen",
	Short: "Generate enriched tfreport presets from provider documentation",
	Long: `presetgen parses Terraform provider documentation markdown files to extract
per-attribute metadata (descriptions, force_new indicators) and generates
enriched preset JSON files for use with tfreport.

Usage:
  presetgen --provider azurerm --docs-dir ./website/docs/r/ --output azurerm.json
  presetgen --provider azurerm --docs-dir ./docs/r/ --existing-preset azurerm.json --output enriched.json`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          run,
}

func init() {
	rootCmd.Flags().StringVar(&flagProvider, "provider", "", "provider name prefix (e.g., azurerm, aws, google)")
	rootCmd.Flags().StringVar(&flagDocsDir, "docs-dir", "", "path to provider docs/r/ directory with .html.markdown files")
	rootCmd.Flags().StringVar(&flagSchemaFile, "schema-file", "", "optional: path to terraform providers schema -json output")
	rootCmd.Flags().StringVar(&flagExistingPreset, "existing-preset", "", "optional: merge with existing preset JSON (preserves display_names)")
	rootCmd.Flags().StringVarP(&flagOutput, "output", "o", "", "output path for generated preset JSON")
	rootCmd.Flags().StringVar(&flagResources, "resources", "", "optional: comma-separated resource types to include (default: all)")
	rootCmd.Flags().StringVar(&flagVersion, "version", "", "provider version string for the preset")

	_ = rootCmd.MarkFlagRequired("provider")
	_ = rootCmd.MarkFlagRequired("docs-dir")
	_ = rootCmd.MarkFlagRequired("output")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Build resource filter
	var resourceFilter map[string]bool
	if flagResources != "" {
		resourceFilter = make(map[string]bool)
		for _, r := range strings.Split(flagResources, ",") {
			r = strings.TrimSpace(r)
			if r != "" {
				resourceFilter[r] = true
			}
		}
	}

	// Parse docs
	fmt.Fprintf(os.Stderr, "Parsing docs in %s for provider %s...\n", flagDocsDir, flagProvider)
	parsed, err := presetgen.ParseDocsDir(flagDocsDir, flagProvider, resourceFilter)
	if err != nil {
		return fmt.Errorf("parsing docs: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Parsed %d resource types\n", len(parsed))

	// Load existing preset if provided
	var existing *presets.Preset
	if flagExistingPreset != "" {
		existing, err = presets.Load(flagExistingPreset)
		if err != nil {
			return fmt.Errorf("loading existing preset: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Loaded existing preset with %d resources\n", len(existing.Resources))
	}

	// Merge
	result, err := presetgen.Merge(presetgen.MergeOptions{
		Provider:       flagProvider,
		Version:        flagVersion,
		ExistingPreset: existing,
		DocsParsed:     parsed,
		SchemaFile:     flagSchemaFile,
	})
	if err != nil {
		return fmt.Errorf("merging preset data: %w", err)
	}

	// Print stats
	totalAttrs := 0
	forceNewCount := 0
	for _, rp := range result.Resources {
		for _, ap := range rp.Attributes {
			totalAttrs++
			if ap.ForceNew {
				forceNewCount++
			}
		}
	}
	fmt.Fprintf(os.Stderr, "Result: %d resources, %d attributes (%d force_new)\n",
		len(result.Resources), totalAttrs, forceNewCount)

	// Write output
	data, err := presetgen.MarshalPreset(result)
	if err != nil {
		return fmt.Errorf("marshaling preset: %w", err)
	}

	if err := os.WriteFile(flagOutput, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Written to %s\n", flagOutput)
	return nil
}
