package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/BlackMesaLTD/tfreport/internal/preserve"
)

// loadPreviousBody reads the file referenced by flagPreviousBodyFile, parses
// it via preserve.ParseRegions, and returns the keyed region map. An empty
// flag value returns (nil, nil) — callers treat that as "no prior body
// supplied" and skip reconciliation.
//
// Malformed prior bodies are handled according to strict:
//   - strict=false (default): emit a ::warning:: to stderr, return nil (no
//     regions), and let rendering continue as if no prior was supplied.
//   - strict=true: return the parse error so the CLI exits 1.
//
// Pass "-" to read from stdin. Stdin is only consumed for this purpose when
// --plan-file / --report-file is set so the plan input path isn't needed.
func loadPreviousBody(path string, strict bool) (map[string]preserve.Region, error) {
	if path == "" {
		return nil, nil
	}

	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("reading previous body from stdin: %w", err)
		}
	} else {
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading previous body file: %w", err)
		}
	}

	regions, parseErr := preserve.ParseRegions(string(data))
	if parseErr != nil {
		if strict {
			return nil, fmt.Errorf("previous body file %q: %w", path, parseErr)
		}
		fmt.Fprintf(os.Stderr, "::warning::tfreport: prior body parse failed (%s): %v — skipping preservation\n", path, parseErr)
		return nil, nil
	}
	return regions, nil
}
