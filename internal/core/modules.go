package core

import "strings"

// Module is the structured form of a terraform module address. It retires
// the historical ambiguity between ModuleGroup.Name (leaf segment, possibly
// with an instance bracket) and ModuleGroup.Path (full address) by giving
// each concept its own accessor.
//
// The terraform address "module.platform.module.vnet" parses to a Module
// with Segments [{Name:"platform"}, {Name:"vnet"}]. The address
// `module.dns.module.zone["privatelink.adf.azure.com"]` parses to
// [{Name:"dns"}, {Name:"zone", Instance:`"privatelink.adf.azure.com"`}].
//
// A root-module resource has an empty Address and zero Segments.
type Module struct {
	// Address is the canonical terraform module address, exactly as it
	// appears in plan JSON's module_address field. Empty for root.
	Address string
	// Segments are the parsed module-call names from outermost to innermost.
	// Zero length means root module.
	Segments []ModuleSegment
}

// ModuleSegment is one module-call step inside a Module address. Instance
// carries the for_each key or count index expression — the raw bytes between
// `[` and `]`, including surrounding quotes for string keys. Empty string
// means the module call uses neither for_each nor count.
type ModuleSegment struct {
	Name     string
	Instance string
}

// ParseModuleAddress decodes a terraform module address into a Module.
// Accepts the empty string and the sentinel "(root)" as root-module inputs.
// For malformed input (does not begin with "module."), returns a Module
// with the raw Address and nil Segments — callers can detect this with
// `m.Address != "" && m.IsRoot()`.
func ParseModuleAddress(addr string) Module {
	m := Module{Address: addr}
	if addr == "" || addr == "(root)" {
		return m
	}

	i := 0
	for i < len(addr) {
		if !strings.HasPrefix(addr[i:], "module.") {
			// Malformed — stop parsing, keep what we have so callers can
			// see the raw Address without a spurious ModuleSegment.
			break
		}
		i += len("module.")

		nameStart := i
		for i < len(addr) && addr[i] != '.' && addr[i] != '[' {
			i++
		}
		name := addr[nameStart:i]

		var instance string
		if i < len(addr) && addr[i] == '[' {
			depth := 1
			i++ // past the opening '['
			instStart := i
			inQuote := false
			for i < len(addr) && depth > 0 {
				c := addr[i]
				switch {
				case c == '"' && (i == 0 || addr[i-1] != '\\'):
					inQuote = !inQuote
				case !inQuote && c == '[':
					depth++
				case !inQuote && c == ']':
					depth--
					if depth == 0 {
						instance = addr[instStart:i]
						i++ // past the ']'
					}
				}
				if depth > 0 {
					i++
				}
			}
		}

		m.Segments = append(m.Segments, ModuleSegment{Name: name, Instance: instance})

		// Skip the dot between segments.
		if i < len(addr) && addr[i] == '.' {
			i++
		}
	}

	return m
}

// IsRoot reports whether this is the terraform root module (zero segments).
func (m Module) IsRoot() bool { return len(m.Segments) == 0 }

// Depth returns the number of module-call segments. Zero means root.
func (m Module) Depth() int { return len(m.Segments) }

// First returns the outermost module-call segment (the one directly
// declared in the root module). Returns the zero value for root.
// This is the segment whose source url lives in Report.ModuleSources.
func (m Module) First() ModuleSegment {
	if len(m.Segments) == 0 {
		return ModuleSegment{}
	}
	return m.Segments[0]
}

// Last returns the innermost module-call segment — the one that directly
// owns the resource. Returns the zero value for root.
func (m Module) Last() ModuleSegment {
	if len(m.Segments) == 0 {
		return ModuleSegment{}
	}
	return m.Segments[len(m.Segments)-1]
}

// Segment returns the segment at index i. Returns (zero, false) if i is out
// of range. Index 0 is the outermost segment.
func (m Module) Segment(i int) (ModuleSegment, bool) {
	if i < 0 || i >= len(m.Segments) {
		return ModuleSegment{}, false
	}
	return m.Segments[i], true
}

// Path returns the canonical address string — equivalent to m.Address.
// Provided so callers can treat Module as self-describing without reaching
// into the struct field.
func (m Module) Path() string { return m.Address }

// String implements fmt.Stringer.
func (m Module) String() string {
	if m.Address == "" {
		return "(root)"
	}
	return m.Address
}

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
