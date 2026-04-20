package core

import (
	"fmt"
	"sort"
	"strings"
)

// changeGroup is an intermediate grouping for the summarizer.
type changeGroup struct {
	action       Action
	resourceType string
	attrSet      string // sorted, comma-separated attribute keys
	resources    []string
	maxImpact    Impact // worst case among the resources in this group
}

// Summarize generates plain-English sentences describing the changes in a
// module group. Each returned KeyChange carries the worst-case impact among
// the resources it covers, enabling downstream filtering.
func Summarize(changes []ResourceChange, displayNames map[string]string) []KeyChange {
	if len(changes) == 0 {
		return nil
	}

	groups := groupChanges(changes)
	merged := mergeGroups(groups)

	var out []KeyChange
	for _, mg := range merged {
		sentence := generateSentence(mg, displayNames)
		if sentence != "" {
			out = append(out, KeyChange{Text: sentence, Impact: mg.maxImpact})
		}
	}
	return out
}

func groupChanges(changes []ResourceChange) []changeGroup {
	type groupKey struct {
		action       Action
		resourceType string
		attrSet      string
	}

	groupMap := make(map[groupKey]*changeGroup)
	var order []groupKey

	for _, rc := range changes {
		if rc.Action == ActionNoOp {
			continue
		}

		attrKeys := ChangedAttributeKeys(rc.ChangedAttributes)
		sort.Strings(attrKeys)
		attrSet := strings.Join(attrKeys, ",")

		key := groupKey{
			action:       rc.Action,
			resourceType: rc.ResourceType,
			attrSet:      attrSet,
		}

		g, ok := groupMap[key]
		if !ok {
			g = &changeGroup{
				action:       rc.Action,
				resourceType: rc.ResourceType,
				attrSet:      attrSet,
			}
			groupMap[key] = g
			order = append(order, key)
		}

		g.resources = append(g.resources, ResourceDisplayLabel(rc))
		if ImpactSeverity(rc.Impact) > ImpactSeverity(g.maxImpact) {
			g.maxImpact = rc.Impact
		}
	}

	groups := make([]changeGroup, 0, len(order))
	for _, k := range order {
		groups = append(groups, *groupMap[k])
	}
	return groups
}

// mergedGroup combines multiple changeGroups that share the same action and attribute set.
type mergedGroup struct {
	action        Action
	attrSet       string
	resourceTypes []string // distinct types
	resources     []string // all resource names
	count         int
	maxImpact     Impact
}

func mergeGroups(groups []changeGroup) []mergedGroup {
	type mergeKey struct {
		action  Action
		attrSet string
	}

	mergeMap := make(map[mergeKey]*mergedGroup)
	var order []mergeKey

	for _, g := range groups {
		key := mergeKey{action: g.action, attrSet: g.attrSet}

		mg, ok := mergeMap[key]
		if !ok {
			mg = &mergedGroup{
				action:  g.action,
				attrSet: g.attrSet,
			}
			mergeMap[key] = mg
			order = append(order, key)
		}

		mg.resourceTypes = appendUnique(mg.resourceTypes, g.resourceType)
		mg.resources = append(mg.resources, g.resources...)
		mg.count += len(g.resources)
		if ImpactSeverity(g.maxImpact) > ImpactSeverity(mg.maxImpact) {
			mg.maxImpact = g.maxImpact
		}
	}

	merged := make([]mergedGroup, 0, len(order))
	for _, k := range order {
		merged = append(merged, *mergeMap[k])
	}
	return merged
}

func generateSentence(mg mergedGroup, displayNames map[string]string) string {
	emoji := ActionEmoji(mg.action)
	count := mg.count

	// Build resource type display string
	typeNames := make([]string, len(mg.resourceTypes))
	for i, rt := range mg.resourceTypes {
		typeNames[i] = displayName(rt, displayNames)
	}

	switch mg.action {
	case ActionCreate:
		if count == 1 {
			return fmt.Sprintf("%s New %s: %s", emoji, typeNames[0], mg.resources[0])
		}
		return fmt.Sprintf("%s %d new %s", emoji, count, pluralizeTypes(typeNames, count))

	case ActionDelete:
		if count == 1 {
			return fmt.Sprintf("%s Removing %s: %s", emoji, typeNames[0], mg.resources[0])
		}
		return fmt.Sprintf("%s Removing %d %s", emoji, count, pluralizeTypes(typeNames, count))

	case ActionReplace:
		if count == 1 {
			return fmt.Sprintf("%s Replacing %s: %s (destroy + recreate)", emoji, typeNames[0], mg.resources[0])
		}
		return fmt.Sprintf("%s Replacing %d %s (destroy + recreate)", emoji, count, pluralizeTypes(typeNames, count))

	case ActionUpdate:
		attrs := parseAttrSet(mg.attrSet)
		if len(attrs) == 0 {
			return fmt.Sprintf("%s %d %s updated", emoji, count, pluralizeTypes(typeNames, count))
		}
		if len(attrs) == 1 {
			return fmt.Sprintf("%s %s updates across %d %s", emoji, capitalizeFirst(attrs[0]), count, pluralizeTypes(typeNames, count))
		}
		return fmt.Sprintf("%s %d %s updated: %s", emoji, count, pluralizeTypes(typeNames, count), strings.Join(attrs, ", "))

	case ActionRead:
		return fmt.Sprintf("%s Reading %d %s", emoji, count, pluralizeTypes(typeNames, count))

	default:
		return ""
	}
}

func displayName(resourceType string, displayNames map[string]string) string {
	if name, ok := displayNames[resourceType]; ok {
		return name
	}
	// Fallback: strip provider prefix (azurerm_, aws_, google_)
	parts := strings.SplitN(resourceType, "_", 2)
	if len(parts) == 2 {
		return strings.ReplaceAll(parts[1], "_", " ")
	}
	return resourceType
}

func pluralizeTypes(typeNames []string, count int) string {
	if len(typeNames) == 1 {
		name := typeNames[0]
		if count > 1 {
			return pluralize(name)
		}
		return name
	}

	// Multiple types: "3 subnets and 1 route table"
	// For simplicity, join with " and "
	return strings.Join(typeNames, " and ")
}

func pluralize(s string) string {
	if strings.HasSuffix(s, "s") || strings.HasSuffix(s, "x") || strings.HasSuffix(s, "ch") || strings.HasSuffix(s, "sh") {
		return s + "es"
	}
	if strings.HasSuffix(s, "y") && len(s) >= 2 && !isVowel(s[len(s)-2]) {
		return s[:len(s)-1] + "ies"
	}
	return s + "s"
}

func isVowel(b byte) bool {
	return b == 'a' || b == 'e' || b == 'i' || b == 'o' || b == 'u'
}

func parseAttrSet(attrSet string) []string {
	if attrSet == "" {
		return nil
	}
	return strings.Split(attrSet, ",")
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ResourceDisplayLabel extracts a meaningful label for a resource change.
// Prefers the pre-computed DisplayLabel (survives JSON round-trip), then
// falls back to the "name" attribute from plan data, then ResourceName.
func ResourceDisplayLabel(rc ResourceChange) string {
	if rc.DisplayLabel != "" {
		return rc.DisplayLabel
	}
	// Try the actual resource name from plan data
	if rc.After != nil {
		if name, ok := rc.After["name"]; ok {
			if s, ok := name.(string); ok && s != "" {
				return s
			}
		}
	}
	// Try before map for deletes
	if rc.Before != nil {
		if name, ok := rc.Before["name"]; ok {
			if s, ok := name.(string); ok && s != "" {
				return s
			}
		}
	}
	return rc.ResourceName
}

func appendUnique(slice []string, item string) []string {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	return append(slice, item)
}
