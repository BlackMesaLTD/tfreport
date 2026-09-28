package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BlackMesaLTD/tfreport/internal/core"
)

// ---------------------------------------------------------------------------
// painter — minimal ANSI colouring, off when not a terminal
// ---------------------------------------------------------------------------

type painter struct{ on bool }

func newPainter(on bool) painter { return painter{on: on} }

func (p painter) wrap(code, s string) string {
	if !p.on {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}
func (p painter) bold(s string) string   { return p.wrap("1", s) }
func (p painter) dim(s string) string    { return p.wrap("2", s) }
func (p painter) red(s string) string    { return p.wrap("31", s) }
func (p painter) green(s string) string  { return p.wrap("32", s) }
func (p painter) yellow(s string) string { return p.wrap("33", s) }
func (p painter) cyan(s string) string   { return p.wrap("36", s) }

// diff colours a diff-converted block (column-0 symbols) line by line.
func (p painter) diff(block string) string {
	if !p.on {
		return block
	}
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "+"):
			lines[i] = p.green(l)
		case strings.HasPrefix(l, "-"):
			lines[i] = p.red(l)
		case strings.HasPrefix(l, "!"):
			lines[i] = p.yellow(l)
		case strings.HasPrefix(strings.TrimSpace(l), "#"):
			lines[i] = p.dim(l)
		}
	}
	return strings.Join(lines, "\n")
}

func (p painter) action(a core.Action) string {
	s := string(a)
	switch a {
	case core.ActionCreate:
		return p.green(s)
	case core.ActionDelete, core.ActionReplace:
		return p.red(s)
	case core.ActionUpdate:
		return p.yellow(s)
	default:
		return p.dim(s)
	}
}

// ---------------------------------------------------------------------------
// breakdown
// ---------------------------------------------------------------------------

type countRow struct {
	Count  int    `json:"count"`
	Action string `json:"action,omitempty"`
	Type   string `json:"type,omitempty"`
	Module string `json:"module,omitempty"`
	Key    string `json:"key,omitempty"`
}

type replacementRow struct {
	Address  string   `json:"address"`
	ForcedBy []string `json:"forced_by"`
}

type breakdownData struct {
	Label        string                `json:"label"`
	Total        int                   `json:"total"`
	ActionCounts map[string]int        `json:"action_counts"`
	MaxImpact    string                `json:"max_impact"`
	ByTypeAction []countRow            `json:"by_type_action"`
	UpdateAttrs  map[string][]countRow `json:"update_attrs_by_type"`
	Replacements []replacementRow      `json:"replacements"`
	Creates      []countRow            `json:"creates"`
	Destroys     []string              `json:"destroys"`
	Cosmetic     int                   `json:"cosmetic_only_updates"`
}

func breakdownView(r *core.Report, rcs []core.ResourceChange) breakdownData {
	d := breakdownData{Label: r.Label, ActionCounts: map[string]int{}, UpdateAttrs: map[string][]countRow{}, MaxImpact: string(r.MaxImpact)}
	typeAction := map[[2]string]int{}
	attrsByType := map[string]map[string]int{}
	creates := map[[2]string]int{}
	for _, rc := range rcs {
		d.Total++
		d.ActionCounts[string(rc.Action)]++
		typeAction[[2]string{rc.ResourceType, string(rc.Action)}]++
		switch rc.Action {
		case core.ActionUpdate:
			if attrsByType[rc.ResourceType] == nil {
				attrsByType[rc.ResourceType] = map[string]int{}
			}
			seen := map[string]bool{}
			allCosmetic := len(rc.ChangedAttributes) > 0
			for _, a := range rc.ChangedAttributes {
				k := core.SummaryKey(a.Key)
				if !seen[k] {
					seen[k] = true
					attrsByType[rc.ResourceType][k]++
				}
				if !core.IsCosmeticKey(a.Key) {
					allCosmetic = false
				}
			}
			if allCosmetic {
				d.Cosmetic++
			}
		case core.ActionReplace:
			d.Replacements = append(d.Replacements, replacementRow{Address: rc.Address, ForcedBy: rc.ReplacePaths})
		case core.ActionCreate:
			creates[[2]string{rc.ResourceType, core.TopLevelModuleName(rc.ModulePath)}]++
		case core.ActionDelete:
			d.Destroys = append(d.Destroys, rc.Address)
		}
	}
	for k, c := range typeAction {
		d.ByTypeAction = append(d.ByTypeAction, countRow{Count: c, Type: k[0], Action: k[1]})
	}
	sortRows(d.ByTypeAction)
	for t, m := range attrsByType {
		var rows []countRow
		for k, c := range m {
			rows = append(rows, countRow{Count: c, Key: k})
		}
		sortRows(rows)
		d.UpdateAttrs[t] = rows
	}
	for k, c := range creates {
		d.Creates = append(d.Creates, countRow{Count: c, Type: k[0], Module: k[1]})
	}
	sortRows(d.Creates)
	sort.Strings(d.Destroys)
	return d
}

func sortRows(rows []countRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Type+rows[i].Action+rows[i].Key+rows[i].Module < rows[j].Type+rows[j].Action+rows[j].Key+rows[j].Module
	})
}

func renderBreakdown(d breakdownData, p painter) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n", p.bold(fmt.Sprintf("%d resources with a planned action", d.Total)), p.dim(actionSummary(d.ActionCounts)))
	if d.Cosmetic > 0 {
		fmt.Fprintf(&b, "%s\n", p.dim(fmt.Sprintf("%d update(s) are cosmetic only (string/list form, case, order) — no functional change", d.Cosmetic)))
	}
	fmt.Fprintf(&b, "\n%s\n", p.cyan("== by resource type and action =="))
	for _, row := range d.ByTypeAction {
		fmt.Fprintf(&b, "  %5d  %-8s %s\n", row.Count, p.action(core.Action(row.Action)), row.Type)
	}
	if len(d.UpdateAttrs) > 0 {
		fmt.Fprintf(&b, "\n%s\n", p.cyan("== attributes driving updates, by resource type =="))
		types := make([]string, 0, len(d.UpdateAttrs))
		for t := range d.UpdateAttrs {
			types = append(types, t)
		}
		sort.Strings(types)
		for _, t := range types {
			fmt.Fprintf(&b, "\n  %s\n", p.bold(t))
			for i, row := range d.UpdateAttrs[t] {
				if i >= 12 {
					fmt.Fprintf(&b, "    %s\n", p.dim(fmt.Sprintf("… %d more", len(d.UpdateAttrs[t])-12)))
					break
				}
				note := ""
				if core.IsCosmeticKey(row.Key) {
					note = p.dim("  (functionally identical)")
				}
				fmt.Fprintf(&b, "    %5d  %s%s\n", row.Count, row.Key, note)
			}
		}
	}
	fmt.Fprintf(&b, "\n%s\n", p.cyan(fmt.Sprintf("== REPLACEMENTS: %d ==", len(d.Replacements))))
	for _, rep := range d.Replacements {
		fmt.Fprintf(&b, "  %s\n", p.red(rep.Address))
		if len(rep.ForcedBy) > 0 {
			fmt.Fprintf(&b, "    forced by: %s\n", strings.Join(rep.ForcedBy, ", "))
		} else {
			fmt.Fprintf(&b, "    forced by: %s\n", p.dim("(terraform did not name an attribute)"))
		}
	}
	fmt.Fprintf(&b, "\n%s\n", p.cyan("== CREATES =="))
	for _, row := range d.Creates {
		fmt.Fprintf(&b, "  %5d  %-45s in %s\n", row.Count, row.Type, row.Module)
	}
	fmt.Fprintf(&b, "\n%s\n", p.cyan(fmt.Sprintf("== DESTROYS: %d ==", len(d.Destroys))))
	for _, a := range d.Destroys {
		fmt.Fprintf(&b, "  %s\n", p.red(a))
	}
	return b.String()
}

func actionSummary(counts map[string]int) string {
	order := []string{"create", "update", "replace", "delete", "read"}
	var parts []string
	for _, a := range order {
		if c := counts[a]; c > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c, a))
		}
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// ---------------------------------------------------------------------------
// attr
// ---------------------------------------------------------------------------

type attrHit struct {
	Address string   `json:"address"`
	Action  string   `json:"action"`
	Impact  string   `json:"impact"`
	Keys    []string `json:"keys"`
	Notes   []string `json:"notes,omitempty"`
}

type attrData struct {
	Attr    string     `json:"attr"`
	Hits    []attrHit  `json:"hits"`
	ByType  []countRow `json:"by_type_action"`
	Samples int        `json:"-"`
}

// attrMatches reports whether a changed-attribute key belongs to `attr`:
// exact key, base attribute of an expanded key, or dotted prefix.
func attrMatches(key, attr string) bool {
	return key == attr || core.BaseAttributeKey(key) == attr || strings.HasPrefix(key, attr+".") || strings.HasPrefix(key, attr+"[")
}

func attrView(rcs []core.ResourceChange, attr string, samples int) attrData {
	d := attrData{Attr: attr, Samples: samples}
	byType := map[[2]string]int{}
	for _, rc := range rcs {
		var keys, notes []string
		for _, a := range rc.ChangedAttributes {
			if attrMatches(a.Key, attr) {
				keys = append(keys, a.Key)
				if a.Description != "" {
					notes = append(notes, a.Key+" — "+a.Description)
				}
			}
		}
		if len(keys) == 0 {
			continue
		}
		d.Hits = append(d.Hits, attrHit{Address: rc.Address, Action: string(rc.Action), Impact: string(rc.Impact), Keys: keys, Notes: notes})
		byType[[2]string{rc.ResourceType, string(rc.Action)}]++
	}
	for k, c := range byType {
		d.ByType = append(d.ByType, countRow{Count: c, Type: k[0], Action: k[1]})
	}
	sortRows(d.ByType)
	return d
}

func renderAttr(d attrData, p painter) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", p.bold(fmt.Sprintf("%d resources touch %q", len(d.Hits), d.Attr)))
	for i, h := range d.Hits {
		if i >= d.Samples {
			fmt.Fprintf(&b, "  %s\n\n", p.dim(fmt.Sprintf("… %d more (raise --samples)", len(d.Hits)-d.Samples)))
			break
		}
		fmt.Fprintf(&b, "  %-8s %s\n", p.action(core.Action(h.Action)), h.Address)
		for _, k := range h.Keys {
			fmt.Fprintf(&b, "           %s\n", k)
		}
		for _, n := range h.Notes {
			fmt.Fprintf(&b, "           %s\n", p.dim(n))
		}
		b.WriteString("\n")
	}
	for _, row := range d.ByType {
		fmt.Fprintf(&b, "  %5d  %-8s %s\n", row.Count, p.action(core.Action(row.Action)), row.Type)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// show
// ---------------------------------------------------------------------------

type showData struct {
	Query     string   `json:"query"`
	Address   string   `json:"address,omitempty"`
	Action    string   `json:"action,omitempty"`
	Impact    string   `json:"impact,omitempty"`
	Keys      []string `json:"keys,omitempty"`
	RuleDiff  string   `json:"rule_diff,omitempty"`
	RawBlock  string   `json:"raw_block,omitempty"`
	Truncated bool     `json:"raw_truncated,omitempty"`
	Others    []string `json:"other_matches,omitempty"`
}

func showView(r *core.Report, rcs []core.ResourceChange, query string, context, maxLines int) showData {
	d := showData{Query: query}
	var matches []core.ResourceChange
	for _, rc := range rcs {
		if strings.Contains(rc.Address, query) {
			matches = append(matches, rc)
		}
	}
	if len(matches) == 0 {
		return d
	}
	rc := matches[0]
	d.Address, d.Action, d.Impact = rc.Address, string(rc.Action), string(rc.Impact)
	for _, a := range rc.ChangedAttributes {
		k := a.Key
		if a.Description != "" {
			k += " — " + a.Description
		}
		d.Keys = append(d.Keys, k)
	}
	for _, m := range matches[1:] {
		d.Others = append(d.Others, m.Address)
	}
	if rd, ok := r.RuleDiffs[rc.Address]; ok {
		d.RuleDiff = core.TextToDiff(rd)
	}
	if raw, ok := r.TextPlanBlocks[rc.Address]; ok {
		if context >= 0 {
			raw = core.CollapseUnchanged(raw, context)
		}
		lines := strings.Split(core.TextToDiff(raw), "\n")
		if maxLines > 0 && len(lines) > maxLines {
			lines = lines[:maxLines]
			d.Truncated = true
		}
		d.RawBlock = strings.Join(lines, "\n")
	}
	return d
}

func renderShow(d showData, p painter) string {
	if d.Address == "" {
		return fmt.Sprintf("no resource address contains %q\n", d.Query)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s  impact %s\n", p.bold(d.Address), p.action(core.Action(d.Action)), d.Impact)
	for _, k := range d.Keys {
		fmt.Fprintf(&b, "  %s\n", k)
	}
	if d.RuleDiff != "" {
		fmt.Fprintf(&b, "\n%s\n%s\n", p.cyan("-- per-rule diff --"), p.diff(d.RuleDiff))
	}
	if d.RawBlock != "" {
		fmt.Fprintf(&b, "\n%s\n%s\n", p.cyan("-- terraform --"), p.diff(d.RawBlock))
		if d.Truncated {
			fmt.Fprintf(&b, "%s\n", p.dim("  … (truncated, raise --lines)"))
		}
	} else if d.RuleDiff == "" {
		fmt.Fprintf(&b, "%s\n", p.dim("  (no text plan supplied — pass plan.txt or a binary plan for the terraform block)"))
	}
	if len(d.Others) > 0 {
		fmt.Fprintf(&b, "\n%s\n", p.dim(fmt.Sprintf("%d other address(es) also match:", len(d.Others))))
		for _, o := range d.Others {
			fmt.Fprintf(&b, "  %s\n", p.dim(o))
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// rules
// ---------------------------------------------------------------------------

type ruleRow struct {
	Address     string         `json:"address"`
	Set         string         `json:"set"`
	Added       []string       `json:"added,omitempty"`
	Removed     []string       `json:"removed,omitempty"`
	Changed     []string       `json:"changed,omitempty"`
	Cosmetic    map[string]int `json:"cosmetic,omitempty"`
	Reordered   bool           `json:"reordered_only,omitempty"`
	NetLoss     int            `json:"net_loss"`
	HasRuleDiff bool           `json:"has_rule_diff"`
}

type rulesData struct {
	Rows     []ruleRow `json:"rows"`
	NetLoss  int       `json:"resources_net_losing"`
	Cosmetic int       `json:"resources_cosmetic_only"`
	Samples  int       `json:"-"`
}

func rulesView(r *core.Report, rcs []core.ResourceChange, samples int) rulesData {
	d := rulesData{Samples: samples}
	for _, rc := range rcs {
		if rc.Action != core.ActionUpdate {
			continue
		}
		row := ruleRow{Address: rc.Address, Cosmetic: map[string]int{}}
		_, row.HasRuleDiff = r.RuleDiffs[rc.Address]
		touched := false
		allCosmetic := true
		for _, a := range rc.ChangedAttributes {
			k := a.Key
			open := strings.IndexByte(k, '[')
			if open < 0 {
				if strings.HasSuffix(k, "."+core.NestedOrder) {
					touched, row.Reordered, row.Set = true, true, core.BaseAttributeKey(k)
				} else {
					allCosmetic = false
				}
				continue
			}
			touched = true
			row.Set = k[:open]
			close := strings.Index(k, "].")
			if close < 0 {
				continue
			}
			id := k[open+1 : close]
			rest := k[close+2:]
			switch {
			case rest == core.NestedAdded:
				row.Added = append(row.Added, id)
				allCosmetic = false
			case rest == core.NestedRemoved:
				row.Removed = append(row.Removed, id)
				allCosmetic = false
			case core.IsCosmeticKey(k):
				row.Cosmetic[core.CosmeticVerdict(k)]++
			default:
				row.Changed = append(row.Changed, id+"."+rest)
				allCosmetic = false
			}
		}
		if !touched {
			continue
		}
		row.NetLoss = len(row.Removed) - len(row.Added)
		if row.NetLoss > 0 {
			d.NetLoss++
		}
		if allCosmetic {
			d.Cosmetic++
		}
		d.Rows = append(d.Rows, row)
	}
	sort.SliceStable(d.Rows, func(i, j int) bool {
		if d.Rows[i].NetLoss != d.Rows[j].NetLoss {
			return d.Rows[i].NetLoss > d.Rows[j].NetLoss
		}
		return d.Rows[i].Address < d.Rows[j].Address
	})
	return d
}

func renderRules(d rulesData, p painter) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n\n", p.bold(fmt.Sprintf("%d resources with rule-set changes", len(d.Rows))),
		p.dim(fmt.Sprintf("%d net-losing rules, %d cosmetic only", d.NetLoss, d.Cosmetic)))
	for i, row := range d.Rows {
		if i >= d.Samples {
			fmt.Fprintf(&b, "  %s\n", p.dim(fmt.Sprintf("… %d more (raise --samples)", len(d.Rows)-d.Samples)))
			break
		}
		fmt.Fprintf(&b, "  %s%-5s %s%-5s %s\n", p.green("+"), fmt.Sprint(len(row.Added)), p.red("-"), fmt.Sprint(len(row.Removed)), row.Address)
		if len(row.Removed) > 0 {
			fmt.Fprintf(&b, "        %s %s\n", p.red("removed:"), strings.Join(row.Removed, ", "))
		}
		if len(row.Added) > 0 {
			fmt.Fprintf(&b, "        %s   %s\n", p.green("added:"), strings.Join(row.Added, ", "))
		}
		if len(row.Changed) > 0 {
			fmt.Fprintf(&b, "        %s %s\n", p.yellow("changed:"), strings.Join(row.Changed, ", "))
		}
		if len(row.Cosmetic) > 0 {
			verdicts := make([]string, 0, len(row.Cosmetic))
			for v, c := range row.Cosmetic {
				verdicts = append(verdicts, fmt.Sprintf("%d %s", c, v))
			}
			sort.Strings(verdicts)
			fmt.Fprintf(&b, "        %s\n", p.dim("cosmetic: "+strings.Join(verdicts, ", ")+" (functionally identical)"))
		}
		if row.Reordered {
			fmt.Fprintf(&b, "        %s\n", p.dim("reordered only"))
		}
	}
	return b.String()
}
