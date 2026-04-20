package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load reads a config file from the given path. If path is empty, it searches
// for .tfreport.yml or .tfreport.yaml in the current directory. Returns the
// parsed config plus the directory of the resolved config file (or the CWD
// when no config file was found). The directory is used as the sandbox root
// for `{{ include }}` and for resolving template_file paths.
func Load(path string) (Config, string, error) {
	if path == "" {
		path = findConfig()
	}

	if path == "" {
		cwd, _ := os.Getwd()
		return Default(), cwd, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, "", fmt.Errorf("reading config %s: %w", path, err)
	}

	cfg, err := Parse(data)
	if err != nil {
		return Config{}, "", err
	}

	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return Config{}, "", fmt.Errorf("resolving config directory: %w", err)
	}
	return cfg, dir, nil
}

// Parse parses YAML bytes into a Config, merging with defaults and running
// validation.
func Parse(data []byte) (Config, error) {
	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing config YAML: %w", err)
	}
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate enforces cross-field constraints on the parsed config.
func validate(cfg Config) error {
	for name, tc := range cfg.Output.Targets {
		if tc.Template != "" && tc.TemplateFile != "" {
			return fmt.Errorf("output.targets.%s: template and template_file are mutually exclusive", name)
		}
		inlineSet := tc.Template != "" || tc.TemplateFile != ""
		if inlineSet && !tc.Sections.IsZero() {
			return fmt.Errorf("output.targets.%s: template/template_file and sections are mutually exclusive (sections only apply to the default template)", name)
		}
		if len(tc.Sections.Show) > 0 && len(tc.Sections.Hide) > 0 {
			return fmt.Errorf("output.targets.%s: sections.show and sections.hide are mutually exclusive", name)
		}
	}
	return nil
}

// findConfig looks for .tfreport.yml in the current directory.
func findConfig() string {
	candidates := []string{
		".tfreport.yml",
		".tfreport.yaml",
	}

	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	for _, name := range candidates {
		path := filepath.Join(cwd, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}
