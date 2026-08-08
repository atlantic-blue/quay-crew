// Package skill reads the skills an operator has written, from files.
//
// A skill is a capability a session can be given: a brief the model reads, the binaries it needs, the
// secrets it names, and its own setup. The design is in docs/SKILLS.md. Files are the authoring and
// sharing format, which is what makes a skill code somebody can review, version and hand to another
// crew, and this package is the part that reads them.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestFile is what a skill's directory is recognised by.
const ManifestFile = "skill.yaml"

// BriefFile is the part the model reads on every turn, so it is short by construction and the detail
// lives in the other files beside it.
const BriefFile = "SKILL.md"

// SetupFile is run inside the sandbox, once, before the first turn. A skill without one is a skill
// that needs nothing done to be usable.
const SetupFile = "bin/setup"

// A Skill is one capability, as it was written down.
type Skill struct {
	// Name is what it is called, and it is the directory it lives in.
	Name string
	// Version is what this one is. It exists so a session can be pinned to the version it started
	// with, rather than changing under itself when the skill is edited.
	Version int
	// Summary is one line, for a listing.
	Summary string
	// Binaries are the commands it cannot work without. Declared so a session missing one is refused
	// with a sentence rather than discovering it halfway through.
	Binaries []string
	// Secrets are the workspace secrets it needs, by name, with a line saying what each is for. The
	// value never appears here: a value in a skill file is a value in a git repository.
	Secrets map[string]string
	// Brief is what the model is told, read from SKILL.md.
	Brief string
	// Dir is where the skill is, as this process sees it.
	Dir string
	// HasSetup says whether there is anything to run inside the sandbox before the first turn.
	HasSetup bool
}

// manifest is skill.yaml as written. It is data: no expressions, no conditionals, nothing that runs
// on the host.
type manifest struct {
	Name     string            `yaml:"name"`
	Version  int               `yaml:"version"`
	Summary  string            `yaml:"summary"`
	Binaries []string          `yaml:"binaries"`
	Secrets  map[string]string `yaml:"secrets"`
}

// Load reads every skill in a directory, sorted by name so the order is the same everywhere.
//
// A directory with no manifest in it is not a skill and is passed over, so notes and a README can sit
// beside them. A directory with a manifest that does not make sense is an error rather than a skip: a
// skill the operator wrote and got wrong should say so, or it is simply missing later with no reason
// given.
func Load(dir string) ([]Skill, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("skill: read %s: %w", dir, err)
	}

	skills := make([]Skill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		at := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(filepath.Join(at, ManifestFile)); err != nil {
			continue
		}
		loaded, err := One(at)
		if err != nil {
			return nil, err
		}
		skills = append(skills, loaded)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

// One reads a single skill from its directory.
func One(dir string) (Skill, error) {
	body, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return Skill{}, fmt.Errorf("skill: read %s: %w", filepath.Join(dir, ManifestFile), err)
	}
	var read manifest
	if err := yaml.Unmarshal(body, &read); err != nil {
		return Skill{}, fmt.Errorf("skill: %s is not readable: %w", filepath.Join(dir, ManifestFile), err)
	}

	brief, err := os.ReadFile(filepath.Join(dir, BriefFile))
	if err != nil {
		return Skill{}, fmt.Errorf("skill: %s has no %s, which is the part the model reads",
			filepath.Base(dir), BriefFile)
	}

	_, setupErr := os.Stat(filepath.Join(dir, SetupFile))
	loaded := Skill{
		Name:     read.Name,
		Version:  read.Version,
		Summary:  strings.TrimSpace(read.Summary),
		Binaries: read.Binaries,
		Secrets:  read.Secrets,
		Brief:    strings.TrimRight(string(brief), "\n"),
		Dir:      dir,
		HasSetup: setupErr == nil,
	}
	return loaded, loaded.check(filepath.Base(dir))
}

// check refuses a manifest that cannot mean what it says.
//
// Named rather than guessed at every turn: a skill whose name does not match its directory is the
// same skill under two names as soon as anybody attaches it, and a secret name that is not a name is
// something the crew would have to quote into an environment.
func (s Skill) check(directory string) error {
	switch {
	case s.Name == "":
		return fmt.Errorf("skill: %s/%s has no name", directory, ManifestFile)
	case s.Name != directory:
		return fmt.Errorf("skill: %s/%s calls itself %q, and a skill is the directory it lives in",
			directory, ManifestFile, s.Name)
	case s.Version < 1:
		return fmt.Errorf("skill: %s has no version, and a session is pinned to the one it started with",
			directory)
	case strings.TrimSpace(s.Brief) == "":
		return fmt.Errorf("skill: %s has an empty %s, so a session would be told nothing",
			directory, BriefFile)
	}
	for _, binary := range s.Binaries {
		if !plain(binary) {
			return fmt.Errorf("skill: %s needs a binary called %q, which is not a command name",
				directory, binary)
		}
	}
	for name := range s.Secrets {
		if !environmentName(name) {
			return fmt.Errorf("skill: %s names a secret %q, which is not an environment variable name",
				directory, name)
		}
	}
	return nil
}

// SecretNames are the secrets a skill needs, sorted, so what a sandbox is given does not depend on
// map order.
func (s Skill) SecretNames() []string {
	names := make([]string, 0, len(s.Secrets))
	for name := range s.Secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// plain says whether this is a command name and not a path or a shell fragment.
func plain(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// environmentName says whether this is something that can be an environment variable.
func environmentName(value string) bool {
	if value == "" || (value[0] >= '0' && value[0] <= '9') {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
		default:
			return false
		}
	}
	return true
}
