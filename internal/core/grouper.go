package core

import "sort"

// GroupByModule groups a slice of ResourceChange by module path,
// returning a sorted slice of ModuleGroup.
func GroupByModule(changes []ResourceChange) []ModuleGroup {
	groupMap := make(map[string]*ModuleGroup)
	var order []string

	for i := range changes {
		rc := &changes[i]
		path := rc.ModulePath
		if path == "" {
			path = "(root)"
		}

		g, ok := groupMap[path]
		if !ok {
			g = &ModuleGroup{
				Name:         moduleName(path),
				Path:         path,
				ActionCounts: make(map[Action]int),
				Module:       ParseModuleAddress(rc.ModulePath),
			}
			groupMap[path] = g
			order = append(order, path)
		}

		g.Changes = append(g.Changes, *rc)
		g.ActionCounts[rc.Action]++
	}

	// Sort by module path for deterministic output
	sort.Strings(order)

	groups := make([]ModuleGroup, 0, len(order))
	for _, path := range order {
		groups = append(groups, *groupMap[path])
	}

	return groups
}

// moduleName extracts the short name from a module path.
// "module.virtual_network" -> "virtual_network"
// "module.a.module.b" -> "b"
// "(root)" -> "(root)"
func moduleName(path string) string {
	if path == "(root)" || path == "" {
		return "(root)"
	}

	// Split and find the last "module.X" pair
	parts := splitModulePath(path)
	if len(parts) == 0 {
		return path
	}
	return parts[len(parts)-1]
}

// splitModulePath splits "module.a.module.b" into ["a", "b"].
func splitModulePath(path string) []string {
	var names []string
	i := 0
	runes := []rune(path)

	for i < len(runes) {
		// Skip "module."
		if i+7 <= len(runes) && string(runes[i:i+7]) == "module." {
			i += 7
			// Read until next ".module." or end
			start := i
			for i < len(runes) {
				if i+8 <= len(runes) && string(runes[i:i+8]) == ".module." {
					break
				}
				i++
			}
			names = append(names, string(runes[start:i]))
			if i < len(runes) {
				i++ // skip the dot before next "module."
			}
		} else {
			i++
		}
	}

	return names
}

// DisambiguateNames detects module name collisions and prepends parent context
// to make names unique. E.g., two groups named "zscc_lb" from different parents
// become "zscc-azci > zscc_lb" and "zscc-azsvcs > zscc_lb".
func DisambiguateNames(groups []ModuleGroup) {
	// Count name occurrences
	nameCounts := make(map[string]int)
	for _, g := range groups {
		nameCounts[g.Name]++
	}

	// For colliding names, prepend parent context
	for i := range groups {
		if nameCounts[groups[i].Name] <= 1 {
			continue
		}
		parts := splitModulePath(groups[i].Path)
		if len(parts) >= 2 {
			groups[i].Name = parts[len(parts)-2] + " > " + parts[len(parts)-1]
		}
		// If still not unique after one level of parent, keep going
	}

	// Second pass: check if still colliding, add more context
	nameCounts2 := make(map[string]int)
	for _, g := range groups {
		nameCounts2[g.Name]++
	}
	for i := range groups {
		if nameCounts2[groups[i].Name] <= 1 {
			continue
		}
		parts := splitModulePath(groups[i].Path)
		if len(parts) >= 3 {
			groups[i].Name = parts[len(parts)-3] + " > " + parts[len(parts)-2] + " > " + parts[len(parts)-1]
		}
	}
}

// TotalActionCounts aggregates action counts across all module groups.
func TotalActionCounts(groups []ModuleGroup) map[Action]int {
	totals := make(map[Action]int)
	for _, g := range groups {
		for action, count := range g.ActionCounts {
			totals[action] += count
		}
	}
	return totals
}
