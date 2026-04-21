package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/BlackMesaLTD/tfreport/internal/config"
	"github.com/BlackMesaLTD/tfreport/internal/core"
	"github.com/BlackMesaLTD/tfreport/internal/formatter"
	"github.com/BlackMesaLTD/tfreport/internal/formatter/blocks"
	"github.com/BlackMesaLTD/tfreport/internal/formatter/template"
	"github.com/BlackMesaLTD/tfreport/internal/formatter/templates"
	"github.com/BlackMesaLTD/tfreport/internal/presets"
)

var (
	version = "dev"

	flagTarget       string
	flagConfig       string
	flagPlanFile     string
	flagTextPlanFile string
	flagReportFiles  []string
	flagLabel        string
	flagCustom       []string
	flagChangedOnly  bool
	flagQuiet        bool
)

var rootCmd = &cobra.Command{
	Use:   "tfreport",
	Short: "Transform Terraform plans into human-readable reports",
	Long: `tfreport is a general-purpose Terraform reporting tool that transforms
plan output into structured, human-readable reports for CI/CD pipelines.

Best results — both JSON and text plan (step-summary and pr-comment targets
render native per-resource text blocks only when text is supplied):
  terraform show -json     plan.out > plan.show.json
  terraform show -no-color plan.out > plan.show.txt
  tfreport --plan-file plan.show.json --text-plan-file plan.show.txt --target github-step-summary

Shortcut — the bundled wrapper does both terraform show calls for you:
  tfreport-from-plan plan.out --target github-step-summary

JSON-only (tables and counts; no per-resource text blocks):
  terraform show -json plan.out | tfreport --target github-pr-body
  tfreport --plan-file plan.show.json --target json

Re-ingest a previously exported report (for cross-step pipeline composition):
  tfreport --report-file report.json --target github-step-summary`,
	Version:       version,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          run,
}

func init() {
	rootCmd.Flags().StringVarP(&flagTarget, "target", "t", "markdown", "output target (markdown, github-pr-body, github-pr-comment, github-step-summary, json)")
	rootCmd.Flags().StringVarP(&flagConfig, "config", "c", "", "path to .tfreport.yml config file")
	rootCmd.Flags().StringVarP(&flagPlanFile, "plan-file", "f", "", "read plan JSON from file instead of stdin")
	rootCmd.Flags().StringVar(&flagTextPlanFile, "text-plan-file", "", "path to terraform text plan output (terraform show -no-color plan.out)")
	rootCmd.Flags().StringSliceVar(&flagReportFiles, "report-file", nil, "read previously exported tfreport JSON (repeatable for multi-report aggregation)")
	rootCmd.Flags().StringVar(&flagLabel, "label", "", "subscription/environment label (stored in JSON export)")
	rootCmd.Flags().StringArrayVar(&flagCustom, "custom", nil, "custom key=value metadata (repeatable). Accessible in templates as {{ $r.Custom.<key> }}")
	rootCmd.Flags().BoolVar(&flagChangedOnly, "changed-only", false, "show only changed resources (exclude no-ops)")
	rootCmd.Flags().BoolVarP(&flagQuiet, "quiet", "q", false, "suppress non-essential output")
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

// SetVersion sets the version string (used by goreleaser).
func SetVersion(v string) {
	version = v
	rootCmd.Version = v
}

func run(cmd *cobra.Command, args []string) error {
	cfg, configDir, err := config.Load(flagConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(flagReportFiles) > 1 {
		if len(flagCustom) > 0 {
			return fmt.Errorf("--custom is not supported in multi-report mode; set custom metadata per report at prepare time (when the plan JSON is first ingested)")
		}
		return runMultiReport(cfg, configDir, flagReportFiles, flagTarget)
	}

	var report *core.Report
	if len(flagReportFiles) == 1 {
		report, err = loadReport(flagReportFiles[0])
	} else {
		report, err = buildReportFromPlan(cfg)
		if err == nil && flagLabel != "" {
			report.Label = flagLabel
		}
	}
	if err != nil {
		return err
	}

	if err := applyCustomFlags(report, flagCustom); err != nil {
		return err
	}

	moduleTypeDescriptions, err := loadModuleTypeDescriptions(cfg)
	if err != nil {
		return err
	}

	f, err := formatter.Get(flagTarget)
	if err != nil {
		return err
	}

	if tf, ok := f.(*formatter.TemplateFormatter); ok {
		if err := configureTemplateFormatter(tf, cfg, configDir, report, nil, moduleTypeDescriptions); err != nil {
			return err
		}
	}

	output, err := f.Format(report)
	if err != nil {
		return fmt.Errorf("formatting report: %w", err)
	}

	fmt.Print(output)
	return nil
}

func runMultiReport(cfg config.Config, configDir string, paths []string, target string) error {
	f, err := formatter.Get(target)
	if err != nil {
		return err
	}

	mf, ok := f.(formatter.MultiReportFormatter)
	if !ok {
		return fmt.Errorf("target %q does not support multiple report files", target)
	}

	reports, err := loadReports(paths)
	if err != nil {
		return err
	}

	moduleTypeDescriptions, err := loadModuleTypeDescriptions(cfg)
	if err != nil {
		return err
	}

	if tf, ok := f.(*formatter.TemplateFormatter); ok {
		if err := configureTemplateFormatter(tf, cfg, configDir, nil, reports, moduleTypeDescriptions); err != nil {
			return err
		}
	}

	output, err := mf.FormatMulti(reports)
	if err != nil {
		return fmt.Errorf("formatting multi-report: %w", err)
	}

	fmt.Print(output)
	return nil
}

// configureTemplateFormatter fills in the BlockContext, wires the sandboxed
// include function, and resolves the user template override (inline/file/sections).
func configureTemplateFormatter(
	tf *formatter.TemplateFormatter,
	cfg config.Config,
	configDir string,
	report *core.Report,
	reports []*core.Report,
	moduleTypeDescriptions map[string]string,
) error {
	effOut := cfg.EffectiveOutput(tf.Target)
	budget := &blocks.TextPlanBudget{Remaining: stepSummaryBudgetBytes(effOut)}
	ctx := &blocks.BlockContext{
		Target:                 tf.Target,
		Report:                 report,
		Reports:                reports,
		Output:                 outputOptions(effOut),
		NoteResolver:           cfg.AttributeNoteResolver(),
		ModuleTypeDescriptions: moduleTypeDescriptions,
		TextBudget:             budget,
		ConfigDir:              configDir,
	}
	if report != nil {
		ctx.DisplayNames = report.DisplayNames
	} else if len(reports) > 0 && reports[0] != nil {
		ctx.DisplayNames = reports[0].DisplayNames
	}

	// Preset resolvers are only available when building from plan (not from
	// already-serialized report JSON). Populate them when we have the presets.
	if len(flagReportFiles) == 0 && len(cfg.Presets) > 0 {
		var loaded []*presets.Preset
		for _, name := range cfg.Presets {
			if p, _ := presets.Load(name); p != nil {
				loaded = append(loaded, p)
			}
		}
		if len(loaded) > 0 {
			ctx.ForceNewResolver = presets.ForceNewResolver(loaded...)
			ctx.DescriptionResolver = presets.DescriptionResolver(loaded...)
		}
	}

	tf.Context = ctx
	tf.Engine = template.New(blocks.Default()).
		WithIncludeFunc(template.MakeIncludeFunc(configDir))

	// Resolve user template override for this target.
	tc, ok := cfg.Output.Targets[tf.Target]
	if !ok || tc.IsZero() {
		return nil
	}
	override, err := resolveUserTemplate(tc, configDir, tf.Target, len(reports) > 1)
	if err != nil {
		return err
	}
	tf.UserTemplate = override
	return nil
}

// resolveUserTemplate produces the template text that should override the
// embedded default for a target. Returns empty string to fall back to the
// default (e.g. when only sections filtering is requested and markers
// aren't present).
func resolveUserTemplate(tc config.TargetConfig, configDir, target string, multi bool) (string, error) {
	switch {
	case tc.Template != "":
		return tc.Template, nil
	case tc.TemplateFile != "":
		abs, err := resolveTemplatePath(configDir, tc.TemplateFile)
		if err != nil {
			return "", err
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return "", fmt.Errorf("reading template_file: %w", err)
		}
		return string(data), nil
	case !tc.Sections.IsZero():
		defaultText := templates.Default(target, multi)
		return template.ApplySections(defaultText, template.SectionSelector{
			Show: tc.Sections.Show,
			Hide: tc.Sections.Hide,
		})
	}
	return "", nil
}

// resolveTemplatePath applies the same sandbox rules as the include function:
// relative to configDir, no absolute paths, no traversal outside configDir.
func resolveTemplatePath(configDir, relPath string) (string, error) {
	if configDir == "" {
		return "", fmt.Errorf("template_file: no config directory available")
	}
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("template_file: absolute paths not permitted: %q", relPath)
	}
	abs, err := filepath.Abs(configDir)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	joined := filepath.Join(abs, filepath.Clean(relPath))
	sep := string(os.PathSeparator)
	if joined != abs && !strings.HasPrefix(joined, abs+sep) {
		return "", fmt.Errorf("template_file: path escapes config directory: %q", relPath)
	}
	return joined, nil
}

// applyCustomFlags parses --custom entries and merges them onto
// report.Custom. CLI values take precedence over any Custom map already
// present on the report (relevant when loaded from a --report-file whose
// JSON already contains custom metadata). A nil or empty flag list is a
// no-op. Parse errors bubble up with the offending entry.
func applyCustomFlags(report *core.Report, flags []string) error {
	custom, err := parseCustomFlags(flags)
	if err != nil {
		return err
	}
	if len(custom) == 0 {
		return nil
	}
	if report.Custom == nil {
		report.Custom = custom
		return nil
	}
	for k, v := range custom {
		report.Custom[k] = v
	}
	return nil
}

// parseCustomFlags converts a slice of "key=value" strings from the
// --custom flag into a map. Empty input returns nil. Entries without an
// `=` return a helpful error naming the offending entry; empty keys are
// rejected. Later occurrences of the same key overwrite earlier ones.
func parseCustomFlags(entries []string) (map[string]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		eq := strings.IndexByte(entry, '=')
		if eq < 0 {
			return nil, fmt.Errorf("--custom %q: expected key=value", entry)
		}
		key := strings.TrimSpace(entry[:eq])
		if key == "" {
			return nil, fmt.Errorf("--custom %q: key must not be empty", entry)
		}
		out[key] = entry[eq+1:]
	}
	return out, nil
}

func outputOptions(out config.OutputConfig) blocks.OutputOptions {
	return blocks.OutputOptions{
		MaxResourcesInSummary: out.MaxResourcesInSummary,
		GroupSubmodules:       out.GroupSubmodules,
		SubmoduleDepth:        out.SubmoduleDepth,
		StepSummaryMaxKB:      out.StepSummaryMaxKB,
		CodeFormat:            out.CodeFormat,
		ChangedAttrsDisplay:   out.ChangedAttrsDisplay,
	}
}

func stepSummaryBudgetBytes(out config.OutputConfig) int {
	kb := out.StepSummaryMaxKB
	if kb <= 0 {
		kb = 800
	}
	return kb * 1024
}

func loadModuleTypeDescriptions(cfg config.Config) (map[string]string, error) {
	if cfg.ModuleDescriptionsFile == "" {
		return nil, nil
	}
	data, err := os.ReadFile(cfg.ModuleDescriptionsFile)
	if err != nil {
		return nil, fmt.Errorf("reading module descriptions file: %w", err)
	}
	out := make(map[string]string)
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parsing module descriptions file: %w", err)
	}
	return out, nil
}

func loadReports(paths []string) ([]*core.Report, error) {
	reports := make([]*core.Report, 0, len(paths))
	for _, p := range paths {
		r, err := loadReport(p)
		if err != nil {
			return nil, err
		}
		if r.Label == "" {
			r.Label = labelFromFilename(p)
		}
		reports = append(reports, r)
	}
	return reports, nil
}

func labelFromFilename(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".json")
	base = strings.TrimPrefix(base, "report-")
	base = strings.TrimPrefix(base, "report_")
	return base
}

func loadReport(path string) (*core.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading report file: %w", err)
	}
	report, err := core.UnmarshalReport(data)
	if err != nil {
		return nil, fmt.Errorf("parsing report file: %w", err)
	}
	return report, nil
}

func buildReportFromPlan(cfg config.Config) (*core.Report, error) {
	planJSON, err := readPlanJSON()
	if err != nil {
		return nil, fmt.Errorf("reading plan JSON: %w", err)
	}

	if !json.Valid(planJSON) {
		return nil, fmt.Errorf("input is not valid JSON")
	}

	opts := core.ReportOptions{
		ChangedOnly:     flagChangedOnly,
		ImpactOverrides: cfg.ImpactOverrides(),
	}

	if len(cfg.Presets) > 0 {
		var loadedPresets []*presets.Preset
		for _, name := range cfg.Presets {
			p, err := presets.Load(name)
			if err != nil {
				return nil, fmt.Errorf("loading preset %q: %w", name, err)
			}
			loadedPresets = append(loadedPresets, p)
		}
		opts.DisplayNames = presets.DisplayNames(loadedPresets...)
		opts.DescriptionResolver = presets.DescriptionResolver(loadedPresets...)

		forceNewResolver := presets.ForceNewResolver(loadedPresets...)
		opts.AttributeResolver = chainResolvers(cfg, forceNewResolver)
	} else {
		opts.AttributeResolver = chainResolvers(cfg, nil)
	}

	if opts.DisplayNames == nil {
		opts.DisplayNames = make(map[string]string)
	}
	for resType := range cfg.Resources {
		if dn := cfg.ResourceDisplayName(resType); dn != "" {
			opts.DisplayNames[resType] = dn
		}
	}

	if len(cfg.Modules) > 0 {
		opts.ModuleDescriptions = make(map[string]string)
		for name, mc := range cfg.Modules {
			opts.ModuleDescriptions[name] = mc.Description
		}
	}

	report, err := core.GenerateReport(planJSON, opts)
	if err != nil {
		return nil, fmt.Errorf("generating report: %w", err)
	}

	if flagTextPlanFile != "" {
		textData, err := os.ReadFile(flagTextPlanFile)
		if err != nil {
			return nil, fmt.Errorf("reading text plan file: %w", err)
		}
		report.TextPlanBlocks = core.ParseTextPlan(string(textData))
	}

	return report, nil
}

func chainResolvers(cfg config.Config, forceNewResolver func(string, string) (bool, bool)) core.AttributeResolver {
	return func(resourceType, attrName string) (core.Impact, bool) {
		if impact, ok := cfg.AttributeImpact(resourceType, attrName); ok {
			return impact, true
		}
		if forceNewResolver != nil {
			if forceNew, found := forceNewResolver(resourceType, attrName); found && forceNew {
				return core.ImpactCritical, true
			}
		}
		return "", false
	}
}

func readPlanJSON() ([]byte, error) {
	if flagPlanFile != "" {
		return os.ReadFile(flagPlanFile)
	}

	stat, err := os.Stdin.Stat()
	if err != nil {
		return nil, fmt.Errorf("checking stdin: %w", err)
	}

	if (stat.Mode() & os.ModeCharDevice) != 0 {
		return nil, fmt.Errorf("no input: pipe plan JSON via stdin or use --plan-file / --report-file")
	}

	return io.ReadAll(os.Stdin)
}
