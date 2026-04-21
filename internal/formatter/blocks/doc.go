package blocks

// BlockDoc is the structured metadata every Block exposes via Doc(). It
// feeds cmd/docgen which renders the block-reference section of the user
// docs from the registry itself. Keeping the source of truth co-located
// with each block means `columns` sets, argument defaults, and example
// renderings cannot drift away from the implementation.
type BlockDoc struct {
	// Name is the registry name (must match Block.Name()).
	Name string

	// Summary is one sentence describing what the block renders. Keep it
	// short — docgen puts it on the first line below the heading.
	Summary string

	// Args is the ordered list of named arguments the block accepts.
	// Positional k,v args in templates. Empty when the block takes no
	// parameters.
	Args []ArgDoc

	// Columns is populated only for blocks that accept a `columns` csv
	// arg. Lists every valid column ID plus its heading and short
	// description. Empty means "not a column-capable block".
	Columns []ColumnDoc

	// Examples is optional. Each entry pairs a one-line template snippet
	// with the rendered output. docgen renders under a "**Example:**"
	// heading.
	Examples []ExampleDoc
}

// ArgDoc describes one named argument accepted by a block.
type ArgDoc struct {
	Name        string // positional key as used in templates
	Type        string // "string" | "int" | "bool" | "csv" | "*core.Report"
	Default     string // human-readable default ("—" when required)
	Description string // one sentence; shown in the args table
}

// ColumnDoc describes one valid `columns` csv value for blocks that
// support pluggable columns (modules_table, and, post-Phase-3, the
// broader table family).
type ColumnDoc struct {
	ID          string // csv identifier (e.g. "module_type")
	Heading     string // rendered markdown table heading
	Description string // one sentence; shown in the columns table
}

// ExampleDoc pairs a template snippet with its rendered output. Both are
// shown verbatim inside fenced code blocks by docgen.
type ExampleDoc struct {
	Template string // e.g. `{{ modules_table "columns" "module,resources" }}`
	Rendered string // expected output; keep compact (one table, not a full report)
}
