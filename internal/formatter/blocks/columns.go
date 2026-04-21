package blocks

import (
	"fmt"
	"sort"
	"strings"
)

// columns.go is the shared scaffolding for blocks that accept a `columns`
// csv arg (modules_table, summary_table, and the Phase-3 table family).
//
// Each block keeps its own column registry (the render-function signatures
// differ by scope — ModuleGroup vs ResourceChange vs *Report vs tally row),
// but the parsing, validation, header emission, and truncation-marker
// conventions are identical and live here.

// validateColumns checks every requested column ID is in the valid set.
// Returns a typed error naming the offending ID and listing the valid
// alternatives (mirrors modules_table's existing error grammar).
func validateColumns(blockName string, requested []string, valid map[string]struct{}) error {
	if len(valid) == 0 {
		return nil
	}
	for _, id := range requested {
		if _, ok := valid[id]; !ok {
			ids := make([]string, 0, len(valid))
			for k := range valid {
				ids = append(ids, k)
			}
			sort.Strings(ids)
			return fmt.Errorf("%s: unknown column %q (valid: %s)",
				blockName, id, strings.Join(ids, ", "))
		}
	}
	return nil
}

// writeColumnHeader writes the `| H1 | H2 |\n|---|---|\n` markdown table
// header. headings must be in the same order the caller will emit cells.
func writeColumnHeader(b *strings.Builder, headings []string) {
	b.WriteString("|")
	for _, h := range headings {
		fmt.Fprintf(b, " %s |", h)
	}
	b.WriteString("\n|")
	for range headings {
		b.WriteString("---|")
	}
	b.WriteString("\n")
}

// writeTruncationRow writes a single-cell spanning '... N more ...' row so
// every block's truncation marker looks the same.
func writeTruncationRow(b *strings.Builder, cols int, moreCount int, noun string) {
	if moreCount <= 0 || cols <= 0 {
		return
	}
	b.WriteString("|")
	fmt.Fprintf(b, " … %d more %s |", moreCount, noun)
	for i := 1; i < cols; i++ {
		b.WriteString(" |")
	}
	b.WriteString("\n")
}

// defaultCols returns the caller's default column list when `requested` is
// empty; otherwise echoes `requested` back. Centralized so every block's
// default-column handling reads identically.
func defaultCols(requested, def []string) []string {
	if len(requested) > 0 {
		return requested
	}
	return def
}
