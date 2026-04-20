package template

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MakeIncludeFunc returns an {{ include "path" }} implementation sandboxed to
// configDir. Rejects absolute paths, parent-directory traversal, and symlink
// escapes.
//
// Security posture: the config directory is treated as the trust boundary.
// Any resolved path — after symlink evaluation — must remain inside that
// directory (or equal it). Symlinks pointing outside are refused even when
// the literal relative path looks innocuous.
func MakeIncludeFunc(configDir string) func(string) (string, error) {
	return func(relPath string) (string, error) {
		if configDir == "" {
			return "", fmt.Errorf("include: no config directory (pass --config to enable includes)")
		}
		if filepath.IsAbs(relPath) {
			return "", fmt.Errorf("include: absolute paths not permitted: %q", relPath)
		}
		if strings.Contains(relPath, "\x00") {
			return "", fmt.Errorf("include: null byte in path")
		}

		absDir, err := filepath.Abs(configDir)
		if err != nil {
			return "", fmt.Errorf("include: resolving config dir: %w", err)
		}
		// Resolve any symlinks in the config dir itself so comparisons
		// below are apples-to-apples.
		if resolved, err := filepath.EvalSymlinks(absDir); err == nil {
			absDir = resolved
		}

		joined := filepath.Join(absDir, filepath.Clean(relPath))
		if !inside(joined, absDir) {
			return "", fmt.Errorf("include: path escapes config directory: %q", relPath)
		}

		// Resolve symlinks and re-check. If the file doesn't exist, return a
		// friendly error rather than the EvalSymlinks native error.
		resolved, err := filepath.EvalSymlinks(joined)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("include: file not found: %q", relPath)
			}
			return "", fmt.Errorf("include: %w", err)
		}
		if !inside(resolved, absDir) {
			return "", fmt.Errorf("include: symlink target escapes config directory: %q", relPath)
		}

		data, err := os.ReadFile(resolved)
		if err != nil {
			return "", fmt.Errorf("include: %w", err)
		}
		return string(data), nil
	}
}

// inside reports whether path is configDir or a descendant of it.
func inside(path, dir string) bool {
	sep := string(os.PathSeparator)
	if path == dir {
		return true
	}
	if !strings.HasSuffix(dir, sep) {
		dir += sep
	}
	return strings.HasPrefix(path, dir)
}
