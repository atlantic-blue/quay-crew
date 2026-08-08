// Package skill is what a capability is: a brief the model reads, the binaries it needs, the secrets
// it names, and its own setup.
//
// A skill is authored as a directory in a git repository, which is what makes it reviewable, diffable
// and versioned, and it is read from there into the crew. This package is that reading and the
// refusing that goes with it: what a well formed skill is, and what a malformed one is told. Nothing
// here reaches a store, a sandbox or a network.
//
// The manifest is data and never a program. No expression language, no conditionals, no field that
// runs on the host, because accepting an expression means owning a language and a sandbox to evaluate
// it in. The only executable part of a skill is its setup script, and that runs inside the sandbox as
// the sandbox user, which is the boundary that was already there.
//
// A skill also has no control flow. It never decides what happens next, it does not run on a
// schedule, and it holds no state between turns. Anything with control flow in it is a workflow,
// which is a different entity with its own design.
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

// The files a skill directory is made of. Everything else in the directory is detail: reference the
// model opens when it needs it, and which costs nothing until it does.
const (
	// ManifestFile says what the skill is, what it needs, and what it is called.
	ManifestFile = "skill.yaml"
	// BriefFile is how this kind of work is done here, in prose. Read when the model does that kind
	// of work rather than on every conversation.
	BriefFile = "SKILL.md"
	// SetupFile makes the capability real inside a sandbox, run once at creation. Optional: a skill
	// that is only a brief needs nothing to happen before it can be used.
	SetupFile = "bin/setup"
)

// SummaryLimit is how long a summary may be, in bytes.
//
// The summary is the part every session holding the skill pays for on every conversation, because it
// is the line in the memory file that says the skill exists and when to reach for it. A sentence is
// enough to decide whether to open the brief, and anything longer is a brief that has leaked into the
// index.
const SummaryLimit = 200

// BriefLimit is how long a brief may be, in bytes. Roughly a thousand tokens, which is about a page.
//
// The limit is here because the alternative has already happened to this crew, to its context rather
// than to a skill: four levels rendered to 51,727 bytes at the workspace, all of it sitting at the
// level that reaches everything, and every session paid it before a word was typed. A place that
// holds prose gets filled until it hurts, so the ceiling is written down while the directory is still
// empty.
//
// Detail goes in the skill's other files, which are mounted and cost nothing until something opens
// one. The rule of thumb behind the number: if a brief is long enough that a person would skim it,
// the model is reading it no more carefully than they would.
const BriefLimit = 4096

// Skill is one capability, loaded from its directory.
type Skill struct {
	// Name is what the skill is called. It is also a directory name inside a sandbox and a key in the
	// store, which is why it is narrow.
	Name string
	// Version is what this revision of the skill is. A session pins it, so editing a skill in its
	// repository cannot change a session that is running.
	Version int
	// Summary says when to use this skill, in a sentence. It is the line that reaches every
	// conversation.
	Summary string
	// Binaries are the commands the skill needs present in the sandbox image, declared so a session
	// missing one is refused with a sentence rather than discovering it halfway through a piece of
	// work.
	Binaries []string
	// Secrets are the secrets the skill names, never their values. The crew binds them from its own
	// sealed store at sandbox creation.
	Secrets []Secret
	// Brief is the body of BriefFile.
	Brief string
	// Files is every file in the skill's directory, by path relative to it, including the manifest and
	// the brief. This is what gets carried into the store and mounted into a sandbox, so a skill on a
	// pod with no host directory is still whole.
	Files []File
}

// Secret is a secret a skill names and never carries. A value written into a skill file is a value in
// a git repository, so only the name and what it is for live here.
type Secret struct {
	// Name is the environment variable the sandbox will hold the value in.
	Name string
	// Purpose is what the secret is and how to set it, said to whoever has to go and get one.
	Purpose string
}

// File is one file of a skill's directory.
type File struct {
	// Path is relative to the skill's directory, with forward slashes whatever the host uses.
	Path string
	// Body is the contents.
	Body []byte
	// Executable says whether the file needs to be runnable. Setup scripts do, and nothing else is
	// assumed to.
	Executable bool
}

// manifest is skill.yaml as written. It is a separate type from Skill so the wire format and the
// thing the crew holds can differ, and so an unknown field can be refused by name.
type manifest struct {
	Name     string            `yaml:"name"`
	Version  int               `yaml:"version"`
	Summary  string            `yaml:"summary"`
	Binaries []string          `yaml:"binaries"`
	Secrets  map[string]string `yaml:"secrets"`
}

// Load reads a skill from its directory.
//
// It refuses rather than repairs. A skill is a capability a session will be handed, and a capability
// that half works is worse than one that is absent, because the model improvises around the gap
// instead of saying it cannot do the thing.
func Load(dir string) (Skill, error) {
	files, err := readFiles(dir)
	if err != nil {
		return Skill{}, err
	}
	loaded, err := FromFiles(files)
	if err != nil {
		// The directory is worth naming here and nowhere deeper: FromFiles is also what the control
		// plane runs on files that arrived over the wire, where there is no directory to name.
		return Skill{}, fmt.Errorf("%w (in %s)", err, dir)
	}
	return loaded, nil
}

// FromFiles builds a skill from the files of its directory, wherever they came from.
//
// This is the one validator. A client reads a directory and sends the files, because the control plane
// runs in a container and a path on the operator's machine means nothing to it, and everything the
// crew refuses is refused here rather than once per client.
func FromFiles(files []File) (Skill, error) {
	byPath := make(map[string][]byte, len(files))
	for _, file := range files {
		byPath[file.Path] = file.Body
	}

	raw, found := byPath[ManifestFile]
	if !found {
		return Skill{}, fmt.Errorf("skill: no %s, so there is nothing saying what it is", ManifestFile)
	}
	loaded, err := Parse(raw)
	if err != nil {
		return Skill{}, err
	}

	brief, found := byPath[BriefFile]
	if !found {
		return Skill{}, fmt.Errorf("skill: %s has no %s, so it declares a capability and says nothing about how to use it",
			loaded.Name, BriefFile)
	}
	loaded.Brief = string(brief)
	if err := usableBrief(loaded.Name, loaded.Brief); err != nil {
		return Skill{}, err
	}

	loaded.Files = make([]File, len(files))
	copy(loaded.Files, files)
	sort.Slice(loaded.Files, func(i, j int) bool { return loaded.Files[i].Path < loaded.Files[j].Path })
	for _, file := range loaded.Files {
		if err := usableFilePath(loaded.Name, file.Path); err != nil {
			return Skill{}, err
		}
	}
	return loaded, nil
}

// Fingerprint is what this revision of a skill is, so importing the same version twice can tell
// whether it is the same skill or a different one wearing the same number.
//
// It covers every file, including the manifest and the brief, which is the whole skill. The path, the
// executable bit and the body all count: a setup script that loses its bit is a different skill in the
// only way that matters, which is whether it runs.
func (s Skill) Fingerprint() string {
	sum := sha256.New()
	for _, file := range s.Files {
		fmt.Fprintf(sum, "%s\x00%t\x00%d\x00", file.Path, file.Executable, len(file.Body))
		sum.Write(file.Body)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// usableFilePath refuses a path that would write outside the skill's own directory.
//
// Files arrive over the wire from a client, and they are written into a directory that is mounted into
// a container. A path of "../../etc/something" would be the crew writing wherever a caller asked.
func usableFilePath(name, file string) error {
	switch {
	case file == "":
		return fmt.Errorf("skill: %s carries a file with no path", name)
	case path.IsAbs(file), strings.HasPrefix(file, "../"), file == "..",
		strings.Contains(file, "/../"), strings.HasSuffix(file, "/.."):
		return fmt.Errorf("skill: %s carries file %q, which does not stay inside the skill's own directory", name, file)
	}
	return nil
}

// Parse reads a manifest and refuses one that is not usable, without touching a filesystem.
func Parse(raw []byte) (Skill, error) {
	var written manifest
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	// A field the crew does not know is refused rather than ignored. A silently ignored field looks
	// configured and does nothing, which sends whoever wrote it looking for the problem somewhere
	// else entirely.
	decoder.KnownFields(true)
	if err := decoder.Decode(&written); err != nil {
		return Skill{}, fmt.Errorf("skill: %s is not readable: %w", ManifestFile, err)
	}

	loaded := Skill{
		Name:     strings.TrimSpace(written.Name),
		Version:  written.Version,
		Summary:  strings.TrimSpace(written.Summary),
		Binaries: written.Binaries,
	}
	if err := usableName(loaded.Name); err != nil {
		return Skill{}, err
	}
	if written.Version < 1 {
		return Skill{}, fmt.Errorf("skill: %s needs a version of 1 or more, so a session can pin the revision it holds", loaded.Name)
	}
	if err := usableSummary(loaded.Name, loaded.Summary); err != nil {
		return Skill{}, err
	}
	for _, binary := range loaded.Binaries {
		if err := usableBinary(loaded.Name, binary); err != nil {
			return Skill{}, err
		}
	}

	// Sorted, because a map has no order and a skill that renders differently on each read is a skill
	// whose stored revision changes without anybody editing it.
	names := make([]string, 0, len(written.Secrets))
	for name := range written.Secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		purpose := strings.TrimSpace(written.Secrets[name])
		if err := usableSecret(loaded.Name, name, purpose); err != nil {
			return Skill{}, err
		}
		loaded.Secrets = append(loaded.Secrets, Secret{Name: name, Purpose: purpose})
	}
	return loaded, nil
}

// readFiles reads a skill's whole directory, so what the crew holds is the skill rather than a
// reference to a directory on somebody's machine.
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

// Dir is where a skill's directory appears under a root. The root is the caller's business: a sandbox
// mount inside a container, or the data directory on the host.
func Dir(root, name string) string { return path.Join(root, "skills", name) }

// BriefPath is where a skill's brief appears under a root, which is what the index points whoever is
// reading it at.
func BriefPath(root, name string) string { return path.Join(Dir(root, name), BriefFile) }

// Index is what a session is told about the skills it holds: one line each, and where to read the
// rest.
//
// This is the whole reason a skill can carry more than a page without every session paying for it.
// The line says the skill exists and when to reach for it; the brief at the path says how, and is
// opened only when the model is doing that kind of work. A crew with ten skills attached spends ten
// lines, not ten pages.
//
// An empty result is the right answer for a session with no skills. It becomes no section in the
// memory file rather than a heading with nothing under it, which is a thing the model reads and
// learns nothing from.
func Index(root string, skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("You hold the skills below. Each one says how that kind of work is done here.\n")
	out.WriteString("Read a skill's brief before doing that kind of work, not before.\n\n")
	for _, held := range skills {
		fmt.Fprintf(&out, "- %s: %s\n  %s\n", held.Name, held.Summary, BriefPath(root, held.Name))
	}
	return strings.TrimRight(out.String(), "\n")
}

// usableName refuses a name that could not be a directory or a key.
//
// A skill's name becomes a directory inside a sandbox and a key in the store, so it is held to what
// is safe in both rather than to what yaml will carry. Lowercase, digits and dashes: the same shape
// the repository's own branch and file names take.
func usableName(name string) error {
	if name == "" {
		return fmt.Errorf("skill: a skill needs a name, which is what it is attached and referred to by")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return fmt.Errorf("skill: name %q can hold lowercase letters, digits and dashes only, because it is also a directory name inside a sandbox", name)
		}
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return fmt.Errorf("skill: name %q cannot start or end with a dash", name)
	}
	return nil
}

// usableSummary refuses a summary that would not work as the one line every conversation carries.
func usableSummary(name, summary string) error {
	switch {
	case summary == "":
		return fmt.Errorf("skill: %s needs a summary, which is the line telling a session the skill exists and when to use it", name)
	case strings.Contains(summary, "\n"):
		return fmt.Errorf("skill: %s has a summary of more than one line; the detail belongs in %s", name, BriefFile)
	case len(summary) > SummaryLimit:
		return fmt.Errorf("skill: %s has a summary of %d bytes and the limit is %d; every session holding the skill reads it on every conversation, so it is a sentence and the rest goes in %s",
			name, len(summary), SummaryLimit, BriefFile)
	}
	return nil
}

// usableBrief refuses a brief that is absent or long enough to be a manual.
func usableBrief(name, brief string) error {
	switch {
	case strings.TrimSpace(brief) == "":
		return fmt.Errorf("skill: %s has an empty %s, so it says nothing about how the work is done", name, BriefFile)
	case len(brief) > BriefLimit:
		return fmt.Errorf("skill: %s has a brief of %d bytes and the limit is %d; put the reference and the examples in other files in the skill's directory, which are read only when they are needed",
			name, len(brief), BriefLimit)
	}
	return nil
}

// usableBinary refuses anything that is not a plain command name.
//
// A path or a shell fragment here would be the crew checking for one thing and the sandbox running
// another, which is the gap a declaration exists to close.
func usableBinary(name, binary string) error {
	if binary == "" {
		return fmt.Errorf("skill: %s declares an empty binary", name)
	}
	if strings.ContainsAny(binary, "/\\ \t\n") {
		return fmt.Errorf("skill: %s declares binary %q; it is a command name that has to be on the path inside the sandbox, not a path or a command line", name, binary)
	}
	return nil
}

// usableSecret refuses a secret name that is not an environment variable name, and one with nothing
// said about it.
//
// The purpose is required because whoever reads the refusal has to go and find a credential, and
// "GH_TOKEN is not set" tells them nothing about which token, with what on it, or how to set it here.
func usableSecret(skillName, name, purpose string) error {
	if name == "" {
		return fmt.Errorf("skill: %s names an empty secret", skillName)
	}
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return fmt.Errorf("skill: %s names secret %q; a secret reaches a sandbox as an environment variable, so it is uppercase letters, digits and underscores and does not start with a digit", skillName, name)
		}
	}
	if purpose == "" {
		return fmt.Errorf("skill: %s names secret %s with nothing saying what it is; whoever reads a refusal has to know which credential to go and get, and how to set it", skillName, name)
	}
	return nil
}
