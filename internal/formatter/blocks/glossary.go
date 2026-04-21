package blocks

import (
	"strings"
)

// Glossary emits a blockquote defining tfreport's action + impact
// vocabulary for readers who aren't terraform natives. Opt-in only —
// never included in default templates.
//
// Args:
//
//	include  csv   (default "actions,impacts") — which sections to render
//	level    str   (beginner | intermediate; default beginner)
type Glossary struct{}

func (Glossary) Name() string { return "glossary" }

func (Glossary) Render(_ *BlockContext, args map[string]any) (string, error) {
	include := ArgCSV(args, "include")
	if len(include) == 0 {
		include = []string{"actions", "impacts"}
	}
	level := ArgString(args, "level", "beginner")

	has := func(name string) bool {
		for _, n := range include {
			if n == name {
				return true
			}
		}
		return false
	}

	var b strings.Builder
	b.WriteString("> 💡 **What do these words mean?**\n")

	if has("actions") {
		b.WriteString(">\n")
		b.WriteString("> **Actions** (what terraform will do to each resource)\n")
		if level == "beginner" {
			b.WriteString("> - ✅ **create** — brand-new resources (safe)\n")
			b.WriteString("> - ⚠️ **update** — modifying an existing resource in place (usually safe)\n")
			b.WriteString("> - ❗ **delete** — removing a resource (data loss risk)\n")
			b.WriteString("> - ❗ **replace** — destroy-then-recreate (brief outage on that resource)\n")
			b.WriteString("> - ♻️ **read** — data source refresh (no change to infra)\n")
		} else {
			b.WriteString("> - ✅ `create`, ⚠️ `update`, ❗ `delete`, ❗ `replace` (destroy+create), ♻️ `read`\n")
		}
	}

	if has("impacts") {
		b.WriteString(">\n")
		b.WriteString("> **Impact** (reviewer priority)\n")
		if level == "beginner" {
			b.WriteString("> - 🔴 **critical / high** — destructive (delete/replace) or force-new attribute\n")
			b.WriteString("> - 🟡 **medium** — in-place update\n")
			b.WriteString("> - 🟢 **low** — create or read (additive)\n")
			b.WriteString("> - ⚪ **none** — no-op or cosmetic (e.g. tags)\n")
		} else {
			b.WriteString("> - 🔴 critical/high · 🟡 medium · 🟢 low · ⚪ none\n")
		}
	}

	if has("imports") {
		b.WriteString(">\n")
		b.WriteString("> **Imports** — terraform is attaching existing infra to state. No actual infra change occurs; only state bookkeeping. Combined with `update` means \"attach + then change.\"\n")
	}

	return strings.TrimRight(b.String(), "\n"), nil
}

// Doc describes glossary for cmd/docgen.
func (Glossary) Doc() BlockDoc {
	return BlockDoc{
		Name:    "glossary",
		Summary: "Opt-in blockquote defining tfreport's action + impact vocabulary. Never in default templates.",
		Args: []ArgDoc{
			{Name: "include", Type: "csv", Default: "actions,impacts", Description: "Which sections to render. Any subset of `actions`, `impacts`, `imports`."},
			{Name: "level", Type: "string", Default: "beginner", Description: "`beginner` (verbose explanations) or `intermediate` (compact)."},
		},
	}
}

func init() { defaultRegistry.Register(Glossary{}) }
