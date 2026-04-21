package core

import "strings"

// TopLevelModuleName extracts the first module-call name from a resource's
// module path. Returns "" for root-module resources or non-module paths.
//
// Examples:
//
//	"module.vnet"                     → "vnet"
//	"module.vnet.module.subnet"       → "vnet"
//	"module.nsg[\"app\"]"             → "nsg"
//	"(root)" or ""                    → ""
func TopLevelModuleName(path string) string {
	if path == "" || path == "(root)" {
		return ""
	}
	if !strings.HasPrefix(path, "module.") {
		return ""
	}
	rest := path[len("module."):]
	bracket := strings.Index(rest, "[")
	dot := strings.Index(rest, ".")
	switch {
	case bracket >= 0 && (dot < 0 || bracket < dot):
		return rest[:bracket]
	case dot >= 0:
		return rest[:dot]
	default:
		return rest
	}
}

// ResolveModuleType resolves a top-level module call name to its source's
// extracted module type. Falls back through: extracted type → top-level
// call name → the supplied fallback (usually the module-group name).
func ResolveModuleType(topLevel string, sources map[string]string, fallback string) string {
	if topLevel == "" {
		return fallback
	}
	source, ok := sources[topLevel]
	if !ok {
		return topLevel
	}
	if mt := ExtractModuleType(source); mt != "" {
		return mt
	}
	return topLevel
}

// ModuleTypeForGroup returns the module type for a module group by walking
// up to the group's top-level module call, looking up its source URL in
// r.ModuleSources, and extracting the type. Falls back to the group's own
// name when no source is known.
func ModuleTypeForGroup(mg ModuleGroup, r *Report) string {
	if r == nil {
		return mg.Name
	}
	topLevel := TopLevelModuleName(mg.Path)
	return ResolveModuleType(topLevel, r.ModuleSources, mg.Name)
}
