package core

// AttributeResolver resolves impact overrides for specific resource+attribute pairs.
type AttributeResolver func(resourceType, attrName string) (Impact, bool)

// ClassifyImpact sets the Impact field on each ResourceChange based on its Action.
// For update actions with changed attributes, attribute-level overrides are checked:
// if ALL changed attributes resolve to an impact, the highest is used.
// Otherwise, action-level overrides or defaults apply.
func ClassifyImpact(changes []ResourceChange, actionOverrides map[Action]Impact, attrResolver AttributeResolver) {
	for i := range changes {
		// For updates with changed attributes, try attribute-level resolution
		if changes[i].Action == ActionUpdate && attrResolver != nil && len(changes[i].ChangedAttributes) > 0 {
			if impact, ok := resolveAttributeImpact(changes[i], attrResolver); ok {
				changes[i].Impact = impact
				continue
			}
		}

		if actionOverrides != nil {
			if impact, ok := actionOverrides[changes[i].Action]; ok {
				changes[i].Impact = impact
				continue
			}
		}
		changes[i].Impact = defaultImpact(changes[i].Action)
	}
}

// resolveAttributeImpact checks if ALL changed attributes have an impact override.
// If so, returns the highest. If ANY attribute lacks an override, returns false
// to fall back to action-based classification.
func resolveAttributeImpact(rc ResourceChange, resolver AttributeResolver) (Impact, bool) {
	maxImpact := ImpactNone
	for _, attr := range rc.ChangedAttributes {
		impact, ok := resolver(rc.ResourceType, attr.Key)
		if !ok {
			return "", false // not all attributes covered, fall back
		}
		if ImpactSeverity(impact) > ImpactSeverity(maxImpact) {
			maxImpact = impact
		}
	}
	return maxImpact, true
}

// MaxImpactForGroup returns the highest impact level in a module group.
func MaxImpactForGroup(group ModuleGroup) Impact {
	max := ImpactNone
	for _, rc := range group.Changes {
		if ImpactSeverity(rc.Impact) > ImpactSeverity(max) {
			max = rc.Impact
		}
	}
	return max
}

// MaxImpactOverall returns the highest impact level across all groups.
func MaxImpactOverall(groups []ModuleGroup) Impact {
	max := ImpactNone
	for _, g := range groups {
		gi := MaxImpactForGroup(g)
		if ImpactSeverity(gi) > ImpactSeverity(max) {
			max = gi
		}
	}
	return max
}

// ImpactSeverity returns a numeric severity for ordering impacts.
func ImpactSeverity(impact Impact) int {
	switch impact {
	case ImpactCritical:
		return 4
	case ImpactHigh:
		return 3
	case ImpactMedium:
		return 2
	case ImpactLow:
		return 1
	default:
		return 0
	}
}

// ImpactEmoji returns the emoji prefix for an impact level.
func ImpactEmoji(impact Impact) string {
	switch impact {
	case ImpactCritical:
		return "\u2757" // exclamation
	case ImpactHigh:
		return "\u2757"
	case ImpactMedium:
		return "\u26a0\ufe0f" // warning
	case ImpactLow:
		return "\u2705" // check mark
	default:
		return ""
	}
}
