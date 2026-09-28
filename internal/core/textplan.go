package core

import (
	"regexp"
	"strconv"
	"strings"
)

// markerRe matches terraform text plan marker lines like:
//
//	# module.vnet.azurerm_virtual_network.main will be updated in-place
//	# azurerm_resource_group.rg will be created
//	# module.nsg["app"].azurerm_network_security_group.nsg must be replaced
//
// It captures the resource address (group 1).
var markerRe = regexp.MustCompile(`(?m)^\s*#\s+(\S+)\s+(?:will be|must be|has been)`)

// ParseTextPlan splits terraform text plan output into per-resource blocks
// keyed by resource address. Each block runs from its marker line
// (e.g. "  # module.x.type.name will be updated in-place") to just before
// the next marker line or end of text.
//
// Data source reads (lines containing "<= data") are included — the address
// is extracted the same way. Addresses may contain brackets, for example
// module.nsg["app"].azurerm_network_security_group.nsg.
//
// Returns an empty map for empty input or when no markers are found.
func ParseTextPlan(text string) map[string]string {
	if text == "" {
		return map[string]string{}
	}

	// Find all marker positions.
	matches := markerRe.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return map[string]string{}
	}

	// Also extract the sub-match (address) for each hit.
	submatches := markerRe.FindAllStringSubmatch(text, -1)

	blocks := make(map[string]string, len(matches))
	for i, sm := range submatches {
		address := sm[1]
		start := matches[i][0]
		var end int
		if i+1 < len(matches) {
			end = matches[i+1][0]
		} else {
			end = len(text)
		}
		block := text[start:end]
		// Trim trailing whitespace/newlines from the block.
		block = strings.TrimRight(block, "\n\r ")
		blocks[address] = block
	}

	return blocks
}

// AggregateTextBlocks groups per-resource text blocks into per-module text.
// For each ModuleGroup, it finds all blocks whose address starts with the
// group's Path and concatenates them (separated by a blank line).
// Returns a map from module group Path to the concatenated text.
func AggregateTextBlocks(blocks map[string]string, groups []ModuleGroup) map[string]string {
	result := make(map[string]string, len(groups))

	for _, g := range groups {
		var parts []string
		// Collect blocks belonging to this module group.
		// A resource belongs to a group if its address starts with the group path.
		// For root modules (path "(root)" or ""), match resources without a "module." prefix.
		for addr, block := range blocks {
			if belongsToGroup(addr, g.Path) {
				parts = append(parts, block)
			}
		}
		if len(parts) > 0 {
			result[g.Path] = strings.Join(parts, "\n\n")
		}
	}

	return result
}

// diffSymbolRe matches a leading terraform change symbol (+, -, ~) before the
// first '=' on a line. It captures: (1) leading whitespace, (2) the symbol.
var diffSymbolRe = regexp.MustCompile(`^(\s*)([+~-])(\s)`)

// TextToDiff converts terraform text plan output into markdown diff format.
// It moves +/-/~ symbols to column 0 so ```diff syntax highlighting works:
//   - '+' stays '+' (green — additions)
//   - '-' stays '-' (red — deletions)
//   - '~' becomes '!' (yellow/modified in some renderers)
//
// Lines without terraform symbols are passed through with a leading space
// (neutral context line in diff format).
func TextToDiff(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))

	for _, line := range lines {
		// Only transform symbols that appear before '=' (actual change markers,
		// not values that happen to contain +/-/~).
		eqPos := strings.Index(line, "=")
		prefix := line
		if eqPos >= 0 {
			prefix = line[:eqPos]
		}

		if m := diffSymbolRe.FindStringSubmatch(prefix); m != nil {
			symbol := m[2]
			switch symbol {
			case "~":
				symbol = "!"
			}
			// Move symbol to column 0, keep indentation after it
			rest := line[len(m[0]):]
			out = append(out, symbol+m[1]+m[3]+rest)
		} else {
			out = append(out, " "+line)
		}
	}

	return strings.Join(out, "\n")
}

// belongsToGroup checks whether a resource address belongs to a module group path.
func belongsToGroup(addr, groupPath string) bool {
	if groupPath == "(root)" || groupPath == "" {
		// Root group: address does not start with "module."
		return !strings.HasPrefix(addr, "module.")
	}
	// The address should start with the group path followed by a dot.
	// e.g., path "module.vnet" matches "module.vnet.azurerm_virtual_network.main"
	return strings.HasPrefix(addr, groupPath+".")
}

// changeLineRe matches terraform change-marker lines that TextToDiff cannot
// classify through diffSymbolRe alone: the replace header (`-/+ resource`)
// and data-source reads (`<= data`). Both must count as "changed" for
// CollapseUnchanged so they are never folded into a hidden run.
var changeLineRe = regexp.MustCompile(`^\s*(?:-/\+|<=)\s`)

// hiddenRunRe matches a marker line previously emitted by CollapseUnchanged.
// Re-collapsing already-collapsed text must not nest markers.
var hiddenRunRe = regexp.MustCompile(`^\s*# \.\.\. \(\d+ unchanged lines hidden\)$`)

// IsChangeLine reports whether a terraform text-plan line carries a change
// symbol (+, -, ~, -/+ or <=) in its key position — the same test TextToDiff
// uses to decide which lines get a column-0 diff symbol. Lines that fail the
// test are unchanged context: attribute echoes, closing braces, terraform's
// own "# (N unchanged attributes hidden)" notes, and blank lines.
func IsChangeLine(line string) bool {
	if changeLineRe.MatchString(line) {
		return true
	}
	prefix := line
	if eqPos := strings.Index(line, "="); eqPos >= 0 {
		prefix = line[:eqPos]
	}
	return diffSymbolRe.MatchString(prefix)
}

// CollapseUnchanged rewrites a terraform text-plan block so that long runs of
// unchanged context lines are replaced by a single
//
//	# ... (N unchanged lines hidden)
//
// marker, keeping `keep` context lines on either side of every changed line
// (the same idea as unified-diff context). Resource marker lines
// ("# <address> will be updated in-place") are always kept so a block never
// loses its identity. A run is only collapsed when doing so hides at least
// two lines AND the marker is shorter than the bytes it replaces, so the
// result is never larger than the input. keep < 0 disables collapsing and
// returns text as-is.
//
// The function operates on raw terraform text (before TextToDiff) so the
// marker inherits the indentation of the first hidden line and renders as a
// neutral context line under every code_format.
func CollapseUnchanged(text string, keep int) string {
	if keep < 0 || text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	run := make([]string, 0, 16)

	flush := func() {
		hidden := len(run) - 2*keep
		if hidden >= 2 {
			marker := hiddenMarker(run[keep], hidden)
			// Only fold when the marker is genuinely smaller than what it
			// replaces — a run of short lines can be cheaper left alone.
			saved := 0
			for _, l := range run[keep : keep+hidden] {
				saved += len(l) + 1
			}
			if saved > len(marker)+1 {
				out = append(out, run[:keep]...)
				out = append(out, marker)
				out = append(out, run[len(run)-keep:]...)
				run = run[:0]
				return
			}
		}
		out = append(out, run...)
		run = run[:0]
	}

	for _, line := range lines {
		if IsChangeLine(line) || markerRe.MatchString(line) || hiddenRunRe.MatchString(line) {
			flush()
			out = append(out, line)
			continue
		}
		run = append(run, line)
	}
	flush()
	return strings.Join(out, "\n")
}

// hiddenMarker builds the collapse marker line, indented like the first
// line it replaces so it sits at the same nesting level in the block.
func hiddenMarker(firstHidden string, hidden int) string {
	indent := firstHidden[:len(firstHidden)-len(strings.TrimLeft(firstHidden, " \t"))]
	if strings.TrimSpace(firstHidden) == "" {
		indent = "        "
	}
	return indent + "# ... (" + strconv.Itoa(hidden) + " unchanged lines hidden)"
}
