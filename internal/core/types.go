package core

// Action represents a terraform resource action.
type Action string

const (
	ActionCreate  Action = "create"
	ActionUpdate  Action = "update"
	ActionDelete  Action = "delete"
	ActionReplace Action = "replace" // ["delete","create"] in plan JSON
	ActionRead    Action = "read"
	ActionNoOp    Action = "no-op"
)

// Impact classification for a resource change.
type Impact string

const (
	ImpactCritical Impact = "critical" // replace (destroy + recreate)
	ImpactHigh     Impact = "high"     // delete
	ImpactMedium   Impact = "medium"   // update
	ImpactLow      Impact = "low"      // create, read
	ImpactNone     Impact = "none"     // no-op
)

// ActionEmoji returns the emoji prefix for an action.
func ActionEmoji(a Action) string {
	switch a {
	case ActionCreate:
		return "\u2705" // heavy_check_mark
	case ActionUpdate:
		return "\u26a0\ufe0f" // warning
	case ActionDelete:
		return "\u2757" // exclamation
	case ActionReplace:
		return "\u2757" // exclamation
	case ActionRead:
		return "\u267b\ufe0f" // recycle
	default:
		return ""
	}
}

// ChangedAttribute represents a single attribute diff between before and after.
type ChangedAttribute struct {
	Key         string
	OldValue    any
	NewValue    any
	Computed    bool   // true if value comes from after_unknown
	Description string // from preset or config
}

// ResourceChange represents a single resource change parsed from plan JSON.
type ResourceChange struct {
	Address           string
	ModulePath        string // extracted from address (e.g., "module.virtual_network")
	ResourceType      string
	ResourceName      string
	ProviderName      string
	Action            Action
	Impact            Impact
	IsImport          bool   // true when terraform plan's `importing` field is set on this change
	DisplayLabel      string // pre-computed from Before/After "name" attr; survives JSON round-trip
	ChangedAttributes []ChangedAttribute
	Before            map[string]any
	After             map[string]any
	AfterUnknown      map[string]any
	BeforeSensitive   any
	AfterSensitive    any
}

// ModuleGroup represents grouped changes for a module.
type ModuleGroup struct {
	Name         string
	Path         string
	Changes      []ResourceChange
	ActionCounts map[Action]int
	Description  string // from config overrides or presets
}

// KeyChange is a single plain-English sentence produced by the summarizer,
// tagged with the worst-case impact among the resources it covers. Impact
// enables filtering like `{{ key_changes "impact" "critical,high" }}`.
type KeyChange struct {
	Text   string
	Impact Impact
}

// Report is the intermediate representation — the heart of tfreport.
type Report struct {
	Label          string // subscription/environment label (for multi-report aggregation)
	ModuleGroups   []ModuleGroup
	KeyChanges     []KeyChange // plain-English sentences from summarizer, impact-tagged
	TotalResources int
	ActionCounts   map[Action]int
	MaxImpact      Impact
	ModuleSources  map[string]string // top-level module call name → source URL
	TextPlanBlocks map[string]string // resource address → native terraform text block
	DisplayNames   map[string]string // resource type → human-readable display name

	// Custom is a pass-through bag for user-supplied metadata that
	// accompanies a report from generation time through to template
	// rendering. Populated via `--custom key=value` on the CLI (repeatable).
	// Survives JSON round-trip via MarshalReport / UnmarshalReport.
	// Templates access values via `{{ $r.Custom.<key> }}` (Go's template
	// engine resolves map keys with dot syntax); keys with hyphens or
	// other special characters need `{{ index $r.Custom "some-key" }}`.
	Custom map[string]string
}
