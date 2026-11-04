package fixtures

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Manifest lists the planted findings of one fixture repo, as the e2e story needs them.
type Manifest struct {
	Repo     string    `yaml:"repo"`
	Findings []Finding `yaml:"findings"`
}

// Finding is one planted issue: where the defect is in its branch and what proves it. Lines is the 1-based
// inclusive range [first, last]; Contains is a substring the range must hold. Repro and Test are set for
// tier E1 only: Repro is a path relative to the repository root, and Test names the test in it.
type Finding struct {
	ID       string `yaml:"id"`
	Finder   string `yaml:"finder"`
	Tier     string `yaml:"tier"`
	Branch   string `yaml:"branch"`
	File     string `yaml:"file"`
	Lines    []int  `yaml:"lines"`
	Contains string `yaml:"contains"`
	Repro    string `yaml:"repro"`
	Test     string `yaml:"test"`
}

// validTier reports whether tier is one the story may name (TECH_SPEC section 6, D57).
func validTier(tier string) bool {
	switch tier {
	case "E1", "E2", "E3", "E4":
		return true
	}
	return false
}

// LoadManifest reads and validates a manifest. Every problem is reported at once, each naming its field path.
func LoadManifest(path string) (Manifest, error) {
	raw, err := readFileErr(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest %s: %w", path, err)
	}
	if err := m.validate(); err != nil {
		return Manifest{}, fmt.Errorf("manifest %s: %w", path, err)
	}
	return m, nil
}

// validate checks every finding and that no id is used twice.
func (m Manifest) validate() error {
	var errs []error
	seen := map[string]bool{}
	for i, f := range m.Findings {
		errs = append(errs, checkFinding(i, f)...)
		if seen[f.ID] {
			errs = append(errs, fmt.Errorf("findings[%d].id: %q appears twice", i, f.ID))
		}
		seen[f.ID] = true
	}
	return errors.Join(errs...)
}

// checkFinding reports every problem with one finding, each naming its field path.
func checkFinding(i int, f Finding) []error {
	prefix := fmt.Sprintf("findings[%d] (%s)", i, f.ID)
	var errs []error
	for _, field := range []struct{ name, value string }{
		{"id", f.ID}, {"finder", f.Finder}, {"branch", f.Branch}, {"file", f.File}, {"contains", f.Contains},
	} {
		if field.value == "" {
			errs = append(errs, fmt.Errorf("%s.%s: is empty", prefix, field.name))
		}
	}
	if !validTier(f.Tier) {
		errs = append(errs, fmt.Errorf("%s.tier: %q is not E1 to E4", prefix, f.Tier))
	}
	if !validLines(f.Lines) {
		errs = append(errs, fmt.Errorf("%s.lines: %v is not a 1-based range [first, last]", prefix, f.Lines))
	}
	if f.Tier == "E1" && (f.Repro == "" || f.Test == "") {
		errs = append(errs, fmt.Errorf("%s: an E1 finding needs repro and test", prefix))
	}
	return errs
}

// validLines reports whether lines is a 1-based inclusive range [first, last].
func validLines(lines []int) bool {
	return len(lines) == 2 && lines[0] >= 1 && lines[0] <= lines[1]
}
