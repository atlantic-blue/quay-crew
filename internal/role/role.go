// Package role reads the roles an operator has written, from files.
//
// A role is a named way of working that a session is given: a brief the model reads, the model it
// runs on, and the material it is allowed to receive. The design is in docs/ROLES.md. Files are the
// authoring and sharing format, the same answer skills and hooks already gave, so a role is code
// somebody can review, version and hand to another crew.
//
// The important part of a role is not the persona. It is the boundary. A role that writes tests must
// not receive the code, and a role that writes code must not receive the test bodies, or the two
// sessions are one conversation wearing two names. So `receives` is a declaration the crew can hold
// a session to, rather than prose in a brief that asks it nicely.
package role

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestFile is what a role's directory is recognised by.
const ManifestFile = "role.yaml"

// BriefFile is how this role works. It is the whole instruction a session running as the role is
// given, so it is read once by that session rather than by every session the crew has.
const BriefFile = "ROLE.md"

// SummaryLimit is how long a summary may be, in bytes. It is the line a listing shows.
const SummaryLimit = 200

// BriefLimit is how long a brief may be, in bytes. Roughly four pages.
//
// Four times a skill's ceiling, and the reason is who pays. A skill's summary reaches every session
// on every conversation, so it is held to a sentence. A role's brief reaches one session, once, and
// that session exists to do this one job, so the detail it needs is the point rather than an
// overhead. The ceiling is still here because a brief nobody can read is a brief nobody follows, and
// because the roles this build ships run to between five and sixteen thousand bytes.
const BriefLimit = 16384

// Material is what a role may declare it receives. An allow list rather than free text.
//
// A name the crew does not assemble is a boundary that quietly means nothing, and a boundary that
// means nothing looks exactly like one that holds. Refusing at import is the only moment somebody is
// looking. These three are what the crew puts in front of a session today.
var Material = []string{
	// Work is the piece of work the role was given. Every role receives it: a task without its work
	// is not a task.
	MaterialWork,
	// Context is what the crew, the workspace and the project know, as the memory files carry it.
	MaterialContext,
	// Skills are the skills the workspace holds, their index and their files.
	MaterialSkills,
}

const (
	MaterialWork    = "work"
	MaterialContext = "context"
	MaterialSkills  = "skills"
)

// Verbs is what a role may declare it calls. An allow list, refused by name at import for the same
// reason Material is: a boundary that quietly means nothing looks exactly like one that holds.
//
// Four, and no more, because a verb nobody uses is a boundary that means nothing. Nothing here
// creates a workspace, a project, a secret, a skill, a hook or a role: a session that could grant
// itself a capability could write itself a way of working nobody approved and then run as it.
var Verbs = []string{
	// VerbWorkCreate declares a piece of work. The parent comes from the credential, never from the
	// caller, which is what keeps the depth count honest.
	VerbWorkCreate,
	// VerbWorkRead reads work and its answer.
	VerbWorkRead,
	// VerbWorkAnswer answers a question a piece of work asked.
	VerbWorkAnswer,
	// VerbWorkStop stops a piece of work.
	VerbWorkStop,
}

const (
	VerbWorkCreate = "work.create"
	VerbWorkRead   = "work.read"
	VerbWorkAnswer = "work.answer"
	VerbWorkStop   = "work.stop"
)

// A Role is one way of working, as it was written down.
type Role struct {
	// Name is what it is called, and it is the directory it lives in.
	Name string
	// Version is what this one is. A session is pinned to the version it started with, so editing a
	// role never changes a session already running as it.
	Version int
	// Summary is one line, for a listing.
	Summary string
	// Model is which model a session running as this role uses, as a tier ("opus") or a full name
	// ("claude-opus-5"). Declared per role because the work differs: naming a team is worth the
	// larger model and writing one file to a specification is not.
	Model string
	// Receives is the material this role is given, sorted, drawn from Material.
	Receives []string
	// May_ is what a session running as this role may call, sorted, drawn from Verbs. Empty is a role
	// that may call nothing, which is what every role written before this existed becomes: default
	// deny, so a boundary is something an author wrote rather than something they forgot.
	May_ []string
	// Brief is how this role works, read from ROLE.md.
	Brief string
	// Dir is where the role is, as this process sees it. Empty for one that arrived over the wire.
	Dir string
}

// File is one file of a role's directory, on its way into the crew.
type File struct {
	// Path is relative to the role's directory, with forward slashes whatever the host uses.
	Path string
	Body []byte
}

// manifest is role.yaml as written. It is data: no expressions, no conditionals, nothing that runs
// on the host.
type manifest struct {
	Name     string   `yaml:"name"`
	Version  int      `yaml:"version"`
	Summary  string   `yaml:"summary"`
	Model    string   `yaml:"model"`
	Receives []string `yaml:"receives"`
	May      []string `yaml:"may"`
}

// One reads a single role from its directory.
func One(dir string) (Role, error) {
	files, err := ReadDir(dir)
	if err != nil {
		return Role{}, err
	}
	loaded, err := FromFiles(files)
	if err != nil {
		return Role{}, fmt.Errorf("%w (in %s)", err, dir)
	}
	loaded.Dir = dir
	// A role read from a directory is named by that directory. It is the one rule that cannot travel
	// over the wire, where there is no directory to disagree with.
	if loaded.Name != filepath.Base(dir) {
		return Role{}, fmt.Errorf("role: %s/%s calls itself %q, and a role is the directory it lives in",
			filepath.Base(dir), ManifestFile, loaded.Name)
	}
	return loaded, nil
}

// FromFiles builds a role from the files of its directory, wherever they came from.
//
// This is the one validator. A client reads a directory and sends the files, because the control
// plane runs in a container and a path on the operator's machine means nothing to it, so everything
// the crew refuses is refused here rather than once per client.
func FromFiles(files []File) (Role, error) {
	byPath := make(map[string][]byte, len(files))
	for _, file := range files {
		byPath[file.Path] = file.Body
	}

	raw, found := byPath[ManifestFile]
	if !found {
		return Role{}, fmt.Errorf("role: no %s, so there is nothing saying what it is", ManifestFile)
	}
	var read manifest
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	// A field the crew does not know is refused by name rather than ignored. Ignored, it looks
	// configured and does nothing, which sends whoever wrote it looking somewhere else entirely.
	decoder.KnownFields(true)
	if err := decoder.Decode(&read); err != nil {
		return Role{}, fmt.Errorf("role: %s is not readable: %w", ManifestFile, err)
	}

	brief, found := byPath[BriefFile]
	if !found {
		return Role{}, fmt.Errorf("role: no %s, which is how the role works", BriefFile)
	}

	loaded := Role{
		Name:     strings.TrimSpace(read.Name),
		Version:  read.Version,
		Summary:  strings.TrimSpace(read.Summary),
		Model:    strings.TrimSpace(read.Model),
		Receives: normalise(read.Receives),
		May_:     normalise(read.May),
		Brief:    strings.TrimRight(string(brief), "\n"),
	}
	return loaded, loaded.check(loaded.Name)
}

// ReadDir reads a role's directory, so what travels to the crew is the role rather than a path on
// somebody's machine.
func ReadDir(dir string) ([]File, error) {
	if _, err := os.Stat(filepath.Join(dir, ManifestFile)); err != nil {
		return nil, fmt.Errorf("role: read %s: %w", filepath.Join(dir, ManifestFile), err)
	}
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
		files = append(files, File{Path: filepath.ToSlash(relative), Body: body})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("role: read %s: %w", dir, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// Fingerprint is what this revision of a role is, so importing the same version twice can tell
// whether it is the same role or a different one wearing the same number.
func (r Role) Fingerprint() string {
	sum := sha256.New()
	fmt.Fprintf(sum, "%s\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00",
		r.Name, r.Version, r.Summary, r.Model, strings.Join(r.Receives, ","),
		strings.Join(r.May_, ","), r.Brief)
	return hex.EncodeToString(sum.Sum(nil))
}

// May says whether a session running as this role may call a verb. A role that declared nothing may
// call nothing.
func (r Role) May(verb string) bool {
	for _, held := range r.May_ {
		if held == verb {
			return true
		}
	}
	return false
}

// Gets says whether this role receives a kind of material.
func (r Role) Gets(material string) bool {
	for _, held := range r.Receives {
		if held == material {
			return true
		}
	}
	return false
}

// normalise sorts and deduplicates what the manifest declared, so what a role receives does not
// depend on the order somebody typed it in.
func normalise(declared []string) []string {
	seen := make(map[string]bool, len(declared))
	out := make([]string, 0, len(declared))
	for _, one := range declared {
		trimmed := strings.TrimSpace(one)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

// check refuses a manifest that cannot mean what it says.
func (r Role) check(directory string) error {
	switch {
	case r.Name == "":
		return fmt.Errorf("role: %s/%s has no name", directory, ManifestFile)
	case !UsableName(r.Name):
		return fmt.Errorf("role: %q can hold lowercase letters, digits and dashes only, because it is also a key in the store and a word in a refusal",
			r.Name)
	case r.Version < 1:
		return fmt.Errorf("role: %s has no version, and a session is pinned to the one it started with",
			directory)
	case r.Summary == "":
		return fmt.Errorf("role: %s has no summary, which is the line a listing shows",
			directory)
	case strings.Contains(r.Summary, "\n"):
		return fmt.Errorf("role: %s has a summary of more than one line; the detail belongs in %s",
			directory, BriefFile)
	case len(r.Summary) > SummaryLimit:
		return fmt.Errorf("role: %s has a summary of %d bytes and the limit is %d; it is a sentence, and the rest goes in %s",
			directory, len(r.Summary), SummaryLimit, BriefFile)
	case r.Model == "":
		return fmt.Errorf("role: %s names no model; say a tier such as opus or a full name such as claude-opus-5, because what a role costs is part of what it is",
			directory)
	case !plain(r.Model):
		return fmt.Errorf("role: %s runs on model %q, which is not a model name",
			directory, r.Model)
	case strings.TrimSpace(r.Brief) == "":
		return fmt.Errorf("role: %s has an empty %s, so a session would be told nothing",
			directory, BriefFile)
	case len(r.Brief) > BriefLimit:
		return fmt.Errorf("role: %s has a brief of %d bytes and the limit is %d; a brief nobody reads to the end is a brief nobody follows",
			directory, len(r.Brief), BriefLimit)
	case len(r.Receives) == 0:
		return fmt.Errorf("role: %s says nothing about what it receives; a role is its boundary, so say at least %s and list what else it may see: %s",
			directory, MaterialWork, strings.Join(Material, ", "))
	}
	for _, material := range r.Receives {
		if !known(material) {
			return fmt.Errorf("role: %s receives %q, which is not material the crew hands out; it is one of: %s",
				directory, material, strings.Join(Material, ", "))
		}
	}
	for _, verb := range r.May_ {
		if !knownVerb(verb) {
			return fmt.Errorf("role: %s may %q, which is not a verb the crew grants; it is one of: %s",
				directory, verb, strings.Join(Verbs, ", "))
		}
	}
	if !r.Gets(MaterialWork) {
		return fmt.Errorf("role: %s does not receive %s, and a session with no work to do is not a task; add %s to what it receives",
			directory, MaterialWork, MaterialWork)
	}
	return nil
}

func knownVerb(verb string) bool {
	for _, one := range Verbs {
		if one == verb {
			return true
		}
	}
	return false
}

func known(material string) bool {
	for _, one := range Material {
		if one == material {
			return true
		}
	}
	return false
}

// UsableName says whether this could be a role name. It is exported because a graph may name a role,
// and a graph parser that carried its own copy of the rule would be a second answer to the same
// question, drifting the moment either changed.
func UsableName(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
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

// plain says whether this is a model name and not a path or a shell fragment.
//
// A name rather than a fixed list of tiers, because a tier the model's own tool stops accepting
// would otherwise fail every task of every role with nothing an operator could configure around it.
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

// All reads every role under a directory, and refuses a directory holding none.
//
// Refusing empty is the point of it. A loader that answers "no roles, no error" is how a check on
// the roles a build ships reports success against a directory that lost them: finding nothing to do
// looks exactly like doing it all correctly. So the caller asking for a set of roles is told when
// there is not one, and the shipped set is held up by a test that reads this directory rather than a
// list somebody typed out and has to remember to extend.
func All(dir string) ([]Role, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("role: read %s: %w", dir, err)
	}
	roles := make([]Role, 0, len(entries))
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
		roles = append(roles, loaded)
	}
	if len(roles) == 0 {
		return nil, fmt.Errorf("role: %s holds no roles, and a directory with nothing in it is not a set of roles", dir)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Name < roles[j].Name })
	return roles, nil
}
