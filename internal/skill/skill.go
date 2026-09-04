// Package skill reads the skills an operator has written, from files.
//
// A skill is a capability a session can be given: a brief the model reads, the binaries it needs, the
// secrets it names, and its own setup. Files are the authoring and
// sharing format, which is what makes a skill code somebody can review, version and hand to another
// system, and this package is the part that reads them.
//
// A skill reaches a session two ways, and this package serves both. The system's own skills directory is
// read from disk and given to every session. A skill imported into the store arrives as its files over
// the wire, because the control plane runs in a container where a path on the operator's machine means
// nothing, and a system on a pod has no host directory to go back to for whatever it did not copy. One
// set of rules either way: FromFiles is what both end up in.
package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestFile is what a skill's directory is recognised by.
const ManifestFile = "skill.yaml"

// BriefFile is how that kind of work is done here. It is read when the model does that kind of work
// rather than on every conversation, and it is short by construction, with the detail in the other
// files beside it.
const BriefFile = "SKILL.md"

// SetupFile is run inside the sandbox, once, before the first exec. A skill without one is a skill
// that needs nothing done to be usable.
const SetupFile = "bin/setup"

// SummaryLimit is how long a summary may be, in bytes.
//
// The summary is the part every session holding the skill pays for on every conversation, because it is
// the line in the memory file saying the skill exists and when to reach for it. A sentence is enough to
// decide whether to open the brief, and anything longer is a brief that has leaked into the index.
const SummaryLimit = 200

// BriefLimit is how long a brief may be, in bytes. Roughly a thousand tokens, which is about a page.
//
// The ceiling is written down while the directories are still nearly empty, because the alternative has
// already happened to this system, to its context rather than to a skill: four levels rendered to 51,727
// bytes at the workspace and every session paid it before a word was typed. A place that holds prose
// gets filled until it hurts.
//
// The detail goes in the skill's other files, which are mounted and cost nothing until one is opened.
const BriefLimit = 4096

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
	// Brief is how the work is done, read from SKILL.md.
	Brief string
	// Dir is where the skill is, as this process sees it. Empty for a skill that arrived over the wire,
	// which has no directory anywhere until the system writes one.
	Dir string
	// HasSetup says whether there is anything to run inside the sandbox before the first exec.
	HasSetup bool
	// Files is every file of the skill's directory, by path relative to it, the manifest and the brief
	// among them.
	//
	// Carried so a skill can live in the store and be whole wherever the system runs. A skill read from the
	// system's own directory does not need them, because that directory is mounted as it is, so this is
	// filled only when something is going to have to write the skill out again.
	Files []File
}

// File is one file of a skill's directory.
type File struct {
	// Path is relative to the skill's directory, with forward slashes whatever the host uses.
	Path string
	Body []byte
	// Executable says the file has to be runnable. A setup script that arrives without its bit cannot
	// run, and the failure surfaces inside a container with nothing pointing back at the import.
	Executable bool
}

// Held is a skill a session holds and where its brief is inside that sandbox.
//
// The path travels with the skill because the two sources land in different places: the system's own
// skills are mounted from the directory the operator keeps them in, and a workspace's are written out of
// the store. Whoever reads the index does not need to know which.
type Held struct {
	Skill
	// BriefPath is where to read the brief, as the sandbox sees it.
	BriefPath string
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
//
// The whole directory is read, not just the manifest and the brief, so the same skill can be handed to
// the store and be whole there. Reading a few files that the system's own directory would have mounted
// anyway is cheaper than two ways of loading a skill.
func One(dir string) (Skill, error) {
	if _, err := os.Stat(filepath.Join(dir, ManifestFile)); err != nil {
		return Skill{}, fmt.Errorf("skill: read %s: %w", filepath.Join(dir, ManifestFile), err)
	}
	files, err := readFiles(dir)
	if err != nil {
		return Skill{}, err
	}
	loaded, err := FromFiles(files)
	if err != nil {
		return Skill{}, fmt.Errorf("%w (in %s)", err, dir)
	}
	loaded.Dir = dir
	// A skill read from a directory is named by that directory. It is the one rule that cannot travel
	// over the wire, where there is no directory to disagree with.
	if loaded.Name != filepath.Base(dir) {
		return Skill{}, fmt.Errorf("skill: %s/%s calls itself %q, and a skill is the directory it lives in",
			filepath.Base(dir), ManifestFile, loaded.Name)
	}
	return loaded, nil
}

// FromFiles builds a skill from the files of its directory, wherever they came from.
//
// This is the one validator. A client reads a directory and sends the files, because the control plane
// runs in a container and a path on the operator's machine means nothing to it, so everything the system
// refuses is refused here rather than once per client.
func FromFiles(files []File) (Skill, error) {
	byPath := make(map[string][]byte, len(files))
	for _, file := range files {
		byPath[file.Path] = file.Body
	}

	raw, found := byPath[ManifestFile]
	if !found {
		return Skill{}, fmt.Errorf("skill: no %s, so there is nothing saying what it is", ManifestFile)
	}
	var read manifest
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	// A field the system does not know is refused by name rather than ignored. Ignored, it looks
	// configured and does nothing, which sends whoever wrote it looking somewhere else entirely.
	decoder.KnownFields(true)
	if err := decoder.Decode(&read); err != nil {
		return Skill{}, fmt.Errorf("skill: %s is not readable: %w", ManifestFile, err)
	}

	brief, found := byPath[BriefFile]
	if !found {
		return Skill{}, fmt.Errorf("skill: no %s, which is how the work gets done", BriefFile)
	}
	_, hasSetup := byPath[SetupFile]

	loaded := Skill{
		Name:     strings.TrimSpace(read.Name),
		Version:  read.Version,
		Summary:  strings.TrimSpace(read.Summary),
		Binaries: read.Binaries,
		Secrets:  read.Secrets,
		Brief:    strings.TrimRight(string(brief), "\n"),
		HasSetup: hasSetup,
	}
	loaded.Files = make([]File, len(files))
	copy(loaded.Files, files)
	sort.Slice(loaded.Files, func(i, j int) bool { return loaded.Files[i].Path < loaded.Files[j].Path })
	return loaded, loaded.check(loaded.Name)
}

// readFiles reads a skill's whole directory, so what travels is the skill rather than a path on
// somebody's machine.
func readFiles(dir string) ([]File, error) {
	var files []File
	err := filepath.WalkDir(dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(dir, name)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, File{
			Path:       filepath.ToSlash(relative),
			Body:       body,
			Executable: info.Mode().Perm()&0o111 != 0,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("skill: read %s: %w", dir, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// Fingerprint is what this revision of a skill is, so importing the same version twice can tell whether
// it is the same skill or a different one wearing the same number.
//
// Every file counts, including the manifest and the brief, and so do the path and the executable bit: a
// setup script that has lost its bit is a different skill in the only way that matters, which is whether
// it runs.
func (s Skill) Fingerprint() string {
	sum := sha256.New()
	for _, file := range s.Files {
		fmt.Fprintf(sum, "%s\x00%t\x00%d\x00", file.Path, file.Executable, len(file.Body))
		sum.Write(file.Body)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// FingerprintHeld is one string for a whole skill set, so what a sandbox was born holding can be
// compared with what a fresh one would hold. Name, version and each skill's own fingerprint go in,
// so an edited skill changes the answer the same way an attached or detached one does.
func FingerprintHeld(held []Held) string {
	sum := sha256.New()
	for _, one := range held {
		fmt.Fprintf(sum, "%s\x00%d\x00%s\x00", one.Name, one.Version, one.Fingerprint())
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// Index is what a session is told about the skills it holds: one line each, and where to read the rest.
//
// This is why a skill can carry more than a page without every session paying for it. The line says the
// skill exists and when to reach for it; the brief at the path says how, and is opened only when that
// kind of work comes up. A system with ten skills spends ten lines rather than ten pages.
//
// Nothing held is no section at all, rather than a heading with nothing under it, which is a thing the
// model opens and learns nothing from.
func Index(held []Held) string {
	if len(held) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("You hold the skills below. Each one says how that kind of work is done here.\n")
	out.WriteString("Read a skill's brief before doing that kind of work, not before.\n\n")
	for _, one := range held {
		fmt.Fprintf(&out, "- %s: %s\n  %s\n", one.Name, one.Summary, one.BriefPath)
	}
	return strings.TrimRight(out.String(), "\n")
}

// DirIn is where a skill's directory sits under a root, on a host or inside a sandbox.
func DirIn(root, name string) string { return path.Join(root, name) }

// BriefPathIn is where a skill's brief sits under a root, which is what the index points at.
func BriefPathIn(root, name string) string { return path.Join(DirIn(root, name), BriefFile) }

// check refuses a manifest that cannot mean what it says.
//
// Named rather than guessed at every exec: a skill whose name does not match its directory is the
// same skill under two names as soon as anybody attaches it, and a secret name that is not a name is
// something the system would have to quote into an environment.
func (s Skill) check(directory string) error {
	switch {
	case s.Name == "":
		return fmt.Errorf("skill: %s/%s has no name", directory, ManifestFile)
	case !usableName(s.Name):
		return fmt.Errorf("skill: %q can hold lowercase letters, digits and dashes only, because it is also a directory name inside a sandbox",
			s.Name)
	case s.Version < 1:
		return fmt.Errorf("skill: %s has no version, and a session is pinned to the one it started with",
			directory)
	case s.Summary == "":
		return fmt.Errorf("skill: %s has no summary, which is the line telling a session the skill exists and when to use it",
			directory)
	case strings.Contains(s.Summary, "\n"):
		return fmt.Errorf("skill: %s has a summary of more than one line; the detail belongs in %s",
			directory, BriefFile)
	case len(s.Summary) > SummaryLimit:
		return fmt.Errorf("skill: %s has a summary of %d bytes and the limit is %d; every session holding the skill reads it on every conversation, so it is a sentence and the rest goes in %s",
			directory, len(s.Summary), SummaryLimit, BriefFile)
	case strings.TrimSpace(s.Brief) == "":
		return fmt.Errorf("skill: %s has an empty %s, so a session would be told nothing",
			directory, BriefFile)
	case len(s.Brief) > BriefLimit:
		return fmt.Errorf("skill: %s has a brief of %d bytes and the limit is %d; put the reference and the examples in other files in the skill's directory, which are read only when they are needed",
			directory, len(s.Brief), BriefLimit)
	}
	for _, binary := range s.Binaries {
		if !plain(binary) {
			return fmt.Errorf("skill: %s needs a binary called %q, which is not a command name",
				directory, binary)
		}
	}
	for _, name := range s.SecretNames() {
		if !environmentName(name) {
			return fmt.Errorf("skill: %s names a secret %q, which is not an environment variable name",
				directory, name)
		}
		if SystemOwnName(name) {
			return fmt.Errorf("skill: %s names the secret %s, and names starting QC_ or CLAUDE_ are the system's own: a skill cannot ask for the system's configuration or the model's token",
				directory, name)
		}
		if strings.TrimSpace(s.Secrets[name]) == "" {
			return fmt.Errorf("skill: %s names secret %s with nothing saying what it is; whoever reads a refusal has to know which credential to go and get, and how to set it",
				directory, name)
		}
	}
	for _, file := range s.Files {
		if !insideOwnDirectory(file.Path) {
			return fmt.Errorf("skill: %s carries file %q, which does not stay inside the skill's own directory",
				directory, file.Path)
		}
	}
	return nil
}

// usableName refuses a name that could not be a directory or a key.
//
// It becomes a directory inside a sandbox and a key in the store, so it is held to what is safe in both
// rather than to what yaml will carry: the same shape as this repository's own branch and file names.
func usableName(name string) bool {
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

// insideOwnDirectory refuses a path that would climb out of the skill.
//
// Files arrive over the wire from a client and are written into a directory the system then mounts into a
// container, so a path of "../../etc/something" would be the system writing wherever a caller asked.
func insideOwnDirectory(file string) bool {
	switch {
	case file == "", path.IsAbs(file), file == "..",
		strings.HasPrefix(file, "../"), strings.Contains(file, "/../"), strings.HasSuffix(file, "/.."):
		return false
	default:
		return true
	}
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

// SystemOwnName says whether a secret name belongs to the system rather than to any skill: the system's
// own configuration (QC_) and the model's own token and settings (CLAUDE_). A skill declaring one
// is refused at validation, and one stored by a build from before that refusal is filtered at the
// sandbox boundary, so neither road hands a session the system's own names.
func SystemOwnName(name string) bool {
	return strings.HasPrefix(name, "QC_") || strings.HasPrefix(name, "CLAUDE_")
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
