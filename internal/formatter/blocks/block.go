// Package blocks provides reusable, target-aware output sections for tfreport
// formatters. Each Block renders a single conceptual section (title, plan
// counts, summary table, text plan, etc.) and adapts its grammar to the
// rendering target (markdown, github-pr-body, github-pr-comment,
// github-step-summary).
//
// Blocks are the composition primitives behind the template engine: every
// {{ .Title }} property and every {{ summary_table ... }} function call in a
// user template resolves to exactly one Block.Render invocation.
package blocks

import (
	"fmt"
	"sync"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// BlockContext carries the shared state needed to render any block. One
// context is built per formatter invocation and threaded through every block
// call in the resulting template.
type BlockContext struct {
	// Target is the output target name ("markdown", "github-pr-body",
	// "github-pr-comment", "github-step-summary"). Blocks use this to pick
	// grammar (e.g. whether to wrap sections in <details><blockquote>).
	Target string

	// Report is the single-report being rendered. Nil when rendering in
	// multi-report mode; use Reports instead.
	Report *core.Report

	// Reports is the set of reports being aggregated. Populated in
	// multi-report mode (pr-body / pr-comment with --report-file x N). Nil
	// or len<=1 means single-report mode and blocks should use Report.
	Reports []*core.Report

	// Output mirrors cfg.Output for knobs that blocks consult directly
	// (max_resources_in_summary, group_submodules, submodule_depth,
	// code_format).
	Output OutputOptions

	// DisplayNames maps resource_type -> human-readable name; populated from
	// presets + config.
	DisplayNames map[string]string

	// ForceNewResolver returns (true, true) when the preset marks an
	// attribute as force-new; used only by instance_detail / text_plan to
	// annotate impact in the changed-resources table.
	ForceNewResolver func(resourceType, attrName string) (bool, bool)

	// DescriptionResolver returns a human-readable description for an
	// attribute; populated from presets.
	DescriptionResolver func(resourceType, attrName string) string

	// NoteResolver returns a config-provided note for an attribute (shown
	// inline in the changed-resources impact column).
	NoteResolver func(resourceType, attrName string) string

	// ModuleTypeDescriptions maps module-type name -> description; from
	// cfg.ModuleDescriptionsFile.
	ModuleTypeDescriptions map[string]string

	// TextBudget tracks remaining bytes for native text-plan blocks. Shared
	// (pointer) so multiple text_plan calls in a single template draw from
	// the same pool. nil means unlimited (blocks should guard).
	TextBudget *TextPlanBudget

	// ConfigDir is the directory of the resolved .tfreport.yml. Used by
	// {{ include }} to sandbox relative paths.
	ConfigDir string
}

// OutputOptions mirrors the subset of config.OutputConfig that blocks consult
// directly. Kept local to avoid a cyclic dependency with internal/config.
type OutputOptions struct {
	MaxResourcesInSummary int
	GroupSubmodules       bool
	SubmoduleDepth        int
	StepSummaryMaxKB      int
	CodeFormat            string

	// ChangedAttrsDisplay picks how the per-resource "Changed" cell renders
	// for create and delete actions (update/replace always render the
	// keys-list). Valid values: "dash" (default), "wordy" (new/removed),
	// "count" (N attrs), "list" (legacy full keys-list). Empty string is
	// treated as "dash". Blocks validate and per-block args can override.
	ChangedAttrsDisplay string
}

// TextPlanBudget is a mutable byte-budget shared across all text_plan block
// calls in a render pass. Consumed top-to-bottom as blocks render.
type TextPlanBudget struct {
	Remaining int
}

// Block is the contract every section implementation satisfies. Render must
// be pure aside from mutating ctx.TextBudget when relevant; concurrent render
// is not required (templates are rendered serially). Doc returns the
// structured metadata consumed by cmd/docgen to generate the user-facing
// block reference.
type Block interface {
	Name() string
	Render(ctx *BlockContext, args map[string]any) (string, error)
	Doc() BlockDoc
}

// Registry is a name-indexed set of Blocks. Zero value is not usable; call
// NewRegistry or Default.
type Registry struct {
	mu     sync.RWMutex
	blocks map[string]Block
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{blocks: make(map[string]Block)}
}

// Register adds a block; a second call with the same Name overwrites.
func (r *Registry) Register(b Block) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blocks[b.Name()] = b
}

// Get looks up a block by name.
func (r *Registry) Get(name string) (Block, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.blocks[name]
	return b, ok
}

// Names returns all registered block names (order unspecified).
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.blocks))
	for n := range r.blocks {
		names = append(names, n)
	}
	return names
}

// Render is a convenience for callers that want "look up by name, render, or
// fail with a clear error." Used by the template engine when resolving
// parameterized block calls.
func (r *Registry) Render(name string, ctx *BlockContext, args map[string]any) (string, error) {
	b, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("block %q: not registered", name)
	}
	return b.Render(ctx, args)
}

// Default returns a Registry populated with every built-in block. The
// built-ins are registered via init() functions in their respective files.
func Default() *Registry {
	return defaultRegistry
}

// defaultRegistry is populated by init() in each block file.
var defaultRegistry = NewRegistry()
