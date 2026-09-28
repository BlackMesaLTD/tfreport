package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/BlackMesaLTD/tfreport/internal/config"
	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// `tfreport inspect` — a local workbench over the same engine CI uses.
//
//	tfreport inspect plan.out                    breakdown by type/action, attrs driving updates, replacements, creates, destroys
//	tfreport inspect plan.show.json plan.txt     the two files CI has
//	tfreport inspect report.json                 a prepare artifact
//	tfreport inspect plan.out --attr tags        every resource touching an attribute
//	tfreport inspect plan.out --show nsg["app"]  per-rule diff + raw block for one resource
//	tfreport inspect plan.out --rules            NSG / route-table rule delta: net-losing sets, removed-not-re-added
//	… --json                                     machine-readable form of any view
//
// Inputs are parsed once and cached under $XDG_CACHE_HOME/tfreport keyed on
// file identity, so repeated questions against one plan are instant.
// `inspect` never grows parsing of its own: if a question cannot be answered
// from the report, the fix belongs in core so CI benefits too.

var (
	inspectAttr     string
	inspectShow     string
	inspectRules    bool
	inspectJSON     bool
	inspectNoColor  bool
	inspectNoCache  bool
	inspectSamples  int
	inspectType     string
	inspectAction   string
	inspectContext  int
	inspectLines    int
	inspectDiffOnly bool
	inspectConfig   string
)

var inspectCmd = &cobra.Command{
	Use:   "inspect <plan.out | plan.json [plan.txt] | report.json>",
	Short: "Interrogate a plan locally: breakdown, attribute usage, per-rule diffs, rule deltas",
	Long: `Inspect a terraform plan on your machine with the same analysis CI renders.

Accepts a binary plan (runs terraform show twice), the plan JSON with an
optional text plan, or a tfreport report JSON (the prepare artifact).
Parsed input is cached so repeated questions are instant.

Views (default is the breakdown):
  --attr NAME    resources whose changed attributes match NAME (base key or prefix)
  --show ADDR    the resource whose address contains ADDR: per-rule diff, then raw block
  --rules        NSG / route-table rule delta across the plan
  --json         machine-readable output for any view`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runInspect,
}

func init() {
	f := inspectCmd.Flags()
	f.StringVar(&inspectAttr, "attr", "", "show every resource touching this attribute")
	f.StringVar(&inspectShow, "show", "", "show the resource whose address contains this")
	f.BoolVar(&inspectRules, "rules", false, "rule delta for NSG security rules and route-table routes")
	f.BoolVar(&inspectJSON, "json", false, "emit the view as JSON")
	f.BoolVar(&inspectNoColor, "no-color", false, "disable ANSI colour (also honours NO_COLOR)")
	f.BoolVar(&inspectNoCache, "no-cache", false, "re-parse inputs even when a cached report exists")
	f.IntVar(&inspectSamples, "samples", 3, "sample resources to print per group")
	f.StringVar(&inspectType, "type", "", "filter to resource types containing this")
	f.StringVar(&inspectAction, "action", "", "filter to one action: create, update, delete, replace, read")
	f.IntVar(&inspectContext, "context", -1, "with --show: unchanged lines kept either side of a change in the raw block (-1 = verbatim)")
	f.IntVar(&inspectLines, "lines", 200, "with --show: cap on raw-block lines printed")
	f.BoolVar(&inspectDiffOnly, "diff-only", false, "with --show: shorthand for --context 0")
	f.StringVarP(&inspectConfig, "config", "c", "", "path to .tfreport.yml (default: ./.tfreport.yml when present)")
	rootCmd.AddCommand(inspectCmd)
}

func runInspect(cmd *cobra.Command, args []string) error {
	cfg, _, err := config.Load(inspectConfig)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	report, err := loadInspectReport(cfg, inspectConfig, args, inspectNoCache, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	filtered := filterResources(report, inspectType, inspectAction)

	p := newPainter(!inspectNoColor && os.Getenv("NO_COLOR") == "" && isTerminal(os.Stdout))
	out := cmd.OutOrStdout()

	var view any
	var text string
	switch {
	case inspectShow != "":
		ctx := inspectContext
		if inspectDiffOnly {
			ctx = 0
		}
		v := showView(report, filtered, inspectShow, ctx, inspectLines)
		view, text = v, renderShow(v, p)
	case inspectAttr != "":
		v := attrView(filtered, inspectAttr, inspectSamples)
		view, text = v, renderAttr(v, p)
	case inspectRules:
		v := rulesView(report, filtered, inspectSamples)
		view, text = v, renderRules(v, p)
	default:
		v := breakdownView(report, filtered)
		view, text = v, renderBreakdown(v, p)
	}

	if inspectJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}
	_, err = fmt.Fprint(out, text)
	return err
}

// ---------------------------------------------------------------------------
// Input resolution + cache
// ---------------------------------------------------------------------------

// loadInspectReport turns the positional inputs into a *core.Report:
//
//	one arg, JSON with "module_groups"   → report JSON, loaded as-is
//	one arg, JSON with "format_version"  → plan JSON (no text blocks)
//	one arg, anything else               → binary plan: terraform show -json / -no-color
//	two args                             → plan JSON + text plan, either order
//
// changed-only is always on: inspect is about what the plan will do.
func loadInspectReport(cfg config.Config, configPath string, args []string, noCache bool, stderr io.Writer) (*core.Report, error) {
	var jsonPath, textPath string
	switch len(args) {
	case 1:
		jsonPath = args[0]
	case 2:
		jsonPath, textPath = args[0], args[1]
		if strings.HasSuffix(strings.ToLower(jsonPath), ".txt") || strings.HasSuffix(strings.ToLower(textPath), ".json") {
			jsonPath, textPath = textPath, jsonPath
		}
	}

	key, err := cacheKey(configPath, jsonPath, textPath)
	if err != nil {
		return nil, err
	}
	if !noCache {
		if r, ok := readCache(key); ok {
			return r, nil
		}
	}

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", jsonPath, err)
	}
	var report *core.Report
	switch inputKind(data) {
	case "report":
		report, err = core.UnmarshalReport(data)
		if err != nil {
			return nil, fmt.Errorf("parsing report %s: %w", jsonPath, err)
		}
	case "plan":
		report, err = buildReportFromBytes(cfg, data, textPath, true, nil)
		if err != nil {
			return nil, err
		}
	default:
		if textPath != "" {
			return nil, fmt.Errorf("%s is not JSON; pass a binary plan on its own", jsonPath)
		}
		planJSON, planText, err := terraformShow(jsonPath)
		if err != nil {
			return nil, err
		}
		tmp, err := os.CreateTemp("", "tfreport-inspect-*.txt")
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmp.Name())
		if _, err := tmp.Write(planText); err != nil {
			return nil, err
		}
		tmp.Close()
		report, err = buildReportFromBytes(cfg, planJSON, tmp.Name(), true, nil)
		if err != nil {
			return nil, err
		}
	}
	if report.Label == "" {
		report.Label = strings.TrimSuffix(filepath.Base(jsonPath), filepath.Ext(jsonPath))
	}
	if !noCache {
		if err := writeCache(key, report); err != nil {
			fmt.Fprintf(stderr, "inspect: cache write skipped: %v\n", err)
		}
	}
	return report, nil
}

// inputKind sniffs a JSON document for the key that tells report from
// plan. Anything that is not a JSON object is "binary".
func inputKind(data []byte) string {
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "binary"
	}
	head := trimmed
	if len(head) > 4096 {
		head = head[:4096]
	}
	switch {
	case bytes.Contains(head, []byte(`"module_groups"`)):
		return "report"
	case bytes.Contains(head, []byte(`"format_version"`)):
		return "plan"
	}
	var probe struct {
		FormatVersion string `json:"format_version"`
		ModuleGroups  any    `json:"module_groups"`
	}
	if json.Unmarshal(data, &probe) == nil {
		if probe.ModuleGroups != nil {
			return "report"
		}
		if probe.FormatVersion != "" {
			return "plan"
		}
	}
	return "binary"
}

func terraformShow(planPath string) ([]byte, []byte, error) {
	tf, err := exec.LookPath("terraform")
	if err != nil {
		return nil, nil, fmt.Errorf("%s looks like a binary plan but terraform is not on PATH", planPath)
	}
	dir := filepath.Dir(planPath)
	run := func(args ...string) ([]byte, error) {
		c := exec.Command(tf, args...)
		c.Dir = dir
		var stderr bytes.Buffer
		c.Stderr = &stderr
		out, err := c.Output()
		if err != nil {
			return nil, fmt.Errorf("terraform %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
		}
		return out, nil
	}
	base := filepath.Base(planPath)
	js, err := run("show", "-json", base)
	if err != nil {
		return nil, nil, err
	}
	txt, err := run("show", "-no-color", base)
	if err != nil {
		return nil, nil, err
	}
	return js, txt, nil
}

func cacheDir() (string, error) {
	if d := os.Getenv("TFREPORT_CACHE_DIR"); d != "" {
		return d, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "tfreport"), nil
}

// cacheKey hashes file identity (path, size, mtime) of every input plus the
// config file and binary version, so an edited plan or config never serves
// a stale report.
func cacheKey(configPath string, paths ...string) (string, error) {
	h := sha256.New()
	fmt.Fprintf(h, "v=%s|changed_only|", version)
	for _, p := range paths {
		if p == "" {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			return "", fmt.Errorf("%s: %w", p, err)
		}
		abs, _ := filepath.Abs(p)
		fmt.Fprintf(h, "%s|%d|%d|", abs, st.Size(), st.ModTime().UnixNano())
	}
	candidates := []string{configPath}
	if configPath == "" {
		if cwd, err := os.Getwd(); err == nil {
			candidates = []string{filepath.Join(cwd, ".tfreport.yml"), filepath.Join(cwd, ".tfreport.yaml")}
		}
	}
	for _, cp := range candidates {
		if st, err := os.Stat(cp); err == nil {
			fmt.Fprintf(h, "cfg=%s|%d|%d|", cp, st.Size(), st.ModTime().UnixNano())
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:24], nil
}

func readCache(key string) (*core.Report, bool) {
	dir, err := cacheDir()
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(dir, key+".json"))
	if err != nil {
		return nil, false
	}
	r, err := core.UnmarshalReport(data)
	if err != nil {
		return nil, false
	}
	return r, true
}

func writeCache(key string, r *core.Report) error {
	dir, err := cacheDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := core.MarshalReport(r)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, key+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, key+".json"))
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// filterResources applies --type / --action, returning a flat list in
// module-group order. "destroy" is accepted as an alias for delete.
func filterResources(r *core.Report, typeSub, action string) []core.ResourceChange {
	var out []core.ResourceChange
	for _, mg := range r.ModuleGroups {
		for _, rc := range mg.Changes {
			if rc.Action == core.ActionNoOp {
				continue
			}
			if typeSub != "" && !strings.Contains(rc.ResourceType, typeSub) {
				continue
			}
			if action != "" && string(rc.Action) != action && !(action == "destroy" && rc.Action == core.ActionDelete) {
				continue
			}
			out = append(out, rc)
		}
	}
	return out
}
