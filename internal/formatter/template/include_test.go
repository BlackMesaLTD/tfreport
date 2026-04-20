package template

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInclude_happyPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "snippet.md")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := MakeIncludeFunc(dir)("snippet.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestInclude_rejectsAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeIncludeFunc(dir)("/etc/passwd")
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("want 'absolute' error, got %v", err)
	}
}

func TestInclude_rejectsParentTraversal(t *testing.T) {
	// Create a nested configDir so we have somewhere to escape from.
	base := t.TempDir()
	cfgDir := filepath.Join(base, "cfg")
	if err := os.Mkdir(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "secret.md")
	if err := os.WriteFile(outside, []byte("leak"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := MakeIncludeFunc(cfgDir)("../secret.md")
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Errorf("want escape error, got %v", err)
	}
}

func TestInclude_rejectsSymlinkEscape(t *testing.T) {
	if _, err := os.Lstat("/tmp"); err != nil {
		t.Skip("platform doesn't support symlinks")
	}
	base := t.TempDir()
	cfgDir := filepath.Join(base, "cfg")
	if err := os.Mkdir(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "secret.md")
	if err := os.WriteFile(outside, []byte("leak"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(cfgDir, "link.md")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	_, err := MakeIncludeFunc(cfgDir)("link.md")
	if err == nil || !strings.Contains(err.Error(), "escape") {
		t.Errorf("want symlink-escape error, got %v", err)
	}
}

func TestInclude_missingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := MakeIncludeFunc(dir)("nope.md")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want 'not found', got %v", err)
	}
}

func TestInclude_noConfigDir(t *testing.T) {
	_, err := MakeIncludeFunc("")("anything.md")
	if err == nil {
		t.Error("want error when no config dir bound")
	}
}
