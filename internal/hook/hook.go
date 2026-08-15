// Package hook reads the hooks an operator has written, from files.
//
// A hook is a constraint a session runs under: what it may not do, checked when it tries, refused
// with a sentence saying what to do instead. The design is in docs/HOOKS.md. Files are the authoring
// and sharing format, the same answer skills already gave, and this package is the part that reads
// them.
//
// A hook is not a skill and not a workflow. It holds no state, it never decides what happens next,
// and it is never given to the model to read. Everything with control flow in it is a workflow, and
// everything the model reads is context or a skill's brief.
package hook

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestFile is what a hook's directory is recognised by.
const ManifestFile = "hook.yaml"

// SummaryLimit is how long a summary may be, in bytes. It is the line a listing shows.
//
// Shorter than it needs to be on purpose. Nothing pays for it per turn, because a hook is not in the
// context, but a summary that runs to a paragraph is a design document arriving in a listing.
const SummaryLimit = 200

// DefaultEntry is the executable a binding runs when it does not name one. Every hook has an entry
// point and almost every hook has exactly one, so the common case says nothing.
const DefaultEntry = "bin/hook"

// Events are the events the model runtime actually raises.
//
// An allow list rather than free text. A name the runtime never raises is a hook that is quietly
// never called, and a hook that is never called looks exactly like a hook that approves of
// everything. Refusing at import is the only moment anybody is looking.
var Events = []string{
	"PreToolUse",
	"PostToolUse",
	"UserPromptSubmit",
	"Stop",
	"SubagentStop",
	"SessionStart",
	"SessionEnd",
	"Notification",
	"PreCompact",
}

// matching are the two events that fire per tool, and so the only two a matcher means anything on.
var matching = map[string]bool{"PreToolUse": true, "PostToolUse": true}

// A Hook is one constraint, as it was written down.
type Hook struct {
	// Name is what it is called, and it is the directory it lives in.
	Name string
	// Version is what this one is. A session is pinned to the version it started with, so editing a
	// hook never changes one already running under it.
	Version int
	// Summary is one line, for a listing.
	Summary string
	// Events are what it fires on, in the order they were written.
	Events []Binding
	// Binaries are the commands it cannot work without, declared so a session missing one is refused
	// with a sentence rather than the hook failing inside a container.
	Binaries []string
	// Secrets are the workspace secrets it needs, by name, with a line saying what each is for. The
	// value never appears here: a value in a hook file is a value in a git repository.
	Secrets map[string]string
	// Dir is where the hook is, as this process sees it. Empty for one that arrived over the wire.
	Dir string
	// Files is every file of the hook's directory, by path relative to it, the manifest among them.
	Files []File
}

// Binding is one event this hook fires on.
type Binding struct {
	// On is the event name, one of Events.
	On string
	// Matcher is which tools this fires for, as the runtime's pattern, for example "Bash" or
	// "Write|Edit". Empty means every tool. Only meaningful on PreToolUse and PostToolUse.
	Matcher string
	// Entry is the executable to run, relative to the hook's own directory.
	Entry string
	// TimeoutSeconds is how long the runtime waits. Zero means the runtime's own default.
	TimeoutSeconds int
}

// File is one file of a hook's directory.
type File struct {
	// Path is relative to the hook's directory, with forward slashes whatever the host uses.
	Path string
	Body []byte
	// Executable says the file has to be runnable. An entry point that arrives without its bit
	// cannot run, and the failure surfaces inside a container with nothing pointing back at the
	// import.
	Executable bool
}

// manifest is hook.yaml as written. It is data: no expressions, no conditionals, nothing that runs
// on the host.
type manifest struct {
	Name     string            `yaml:"name"`
	Version  int               `yaml:"version"`
	Summary  string            `yaml:"summary"`
	Events   []bindingManifest `yaml:"events"`
	Binaries []string          `yaml:"binaries"`
	Secrets  map[string]string `yaml:"secrets"`
}

type bindingManifest struct {
	On             string `yaml:"on"`
	Matcher        string `yaml:"matcher"`
	Entry          string `yaml:"entry"`
	TimeoutSeconds int    `yaml:"timeoutSeconds"`
}

// FromFiles reads a hook out of the files of its directory, which is what an import carries and what
// a store hands back.
func FromFiles(files []File) (Hook, error) {
	byPath := make(map[string]File, len(files))
	for _, file := range files {
		byPath[file.Path] = file
	}

	raw, found := byPath[ManifestFile]
	if !found {
		return Hook{}, fmt.Errorf("hook: no %s, so there is nothing saying what it is", ManifestFile)
	}
	var read manifest
	decoder := yaml.NewDecoder(strings.NewReader(string(raw.Body)))
	// A field the crew does not know is refused by name rather than ignored. Ignored, it looks
	// configured and does nothing, which sends whoever wrote it looking somewhere else entirely.
	decoder.KnownFields(true)
	if err := decoder.Decode(&read); err != nil {
		return Hook{}, fmt.Errorf("hook: %s is not readable: %w", ManifestFile, err)
	}

	loaded := Hook{
		Name:     strings.TrimSpace(read.Name),
		Version:  read.Version,
		Summary:  strings.TrimSpace(read.Summary),
		Binaries: read.Binaries,
		Secrets:  read.Secrets,
	}
	for _, one := range read.Events {
		entry := strings.TrimSpace(one.Entry)
		if entry == "" {
			entry = DefaultEntry
		}
		loaded.Events = append(loaded.Events, Binding{
			On:             strings.TrimSpace(one.On),
			Matcher:        strings.TrimSpace(one.Matcher),
			Entry:          entry,
			TimeoutSeconds: one.TimeoutSeconds,
		})
	}
	loaded.Files = make([]File, len(files))
	copy(loaded.Files, files)
	sort.Slice(loaded.Files, func(i, j int) bool { return loaded.Files[i].Path < loaded.Files[j].Path })
	return loaded, loaded.check(loaded.Name, byPath)
}

// Fingerprint is what this revision is, over every file.
//
// Importing the same version twice is only harmless when it is the same hook. This answers that
// without reading the files back, and it is what makes a refusal possible rather than an overwrite
// of a constraint that sessions are already running under.
func (h Hook) Fingerprint() string {
	sum := sha256.New()
	for _, file := range h.Files {
		fmt.Fprintf(sum, "%s\x00%t\x00%d\x00", file.Path, file.Executable, len(file.Body))
		sum.Write(file.Body)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// SecretNames are the secrets this hook names, sorted, so a refusal and a listing say them in the
// same order every time.
func (h Hook) SecretNames() []string {
	names := make([]string, 0, len(h.Secrets))
	for name := range h.Secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// check is every rule a hook has to satisfy, stated tightly enough that a test can fail one.
func (h Hook) check(directory string, byPath map[string]File) error {
	switch {
	case h.Name == "":
		return fmt.Errorf("hook: %s/%s has no name", directory, ManifestFile)
	case !usableName(h.Name):
		return fmt.Errorf("hook: %q can hold lowercase letters, digits and dashes only, because it is also a directory name inside a sandbox",
			h.Name)
	case h.Version < 1:
		return fmt.Errorf("hook: %s has no version, and a session is pinned to the one it started with",
			directory)
	case h.Summary == "":
		return fmt.Errorf("hook: %s has no summary, which is the line a listing shows",
			directory)
	case strings.Contains(h.Summary, "\n"):
		return fmt.Errorf("hook: %s has a summary of more than one line", directory)
	case len(h.Summary) > SummaryLimit:
		return fmt.Errorf("hook: %s has a summary of %d bytes and the limit is %d",
			directory, len(h.Summary), SummaryLimit)
	case len(h.Events) == 0:
		return fmt.Errorf("hook: %s fires on nothing, so it would be imported, attached, mounted and never called",
			directory)
	}
	for _, binding := range h.Events {
		if err := binding.check(directory, byPath); err != nil {
			return err
		}
	}
	for _, binary := range h.Binaries {
		if !plain(binary) {
			return fmt.Errorf("hook: %s needs a binary called %q, which is not a command name",
				directory, binary)
		}
	}
	for _, name := range h.SecretNames() {
		if !environmentName(name) {
			return fmt.Errorf("hook: %s names a secret %q, which is not an environment variable name",
				directory, name)
		}
		if crewOwnName(name) {
			return fmt.Errorf("hook: %s names the secret %s, and names starting QC_ or CLAUDE_ are the crew's own: a hook cannot ask for the crew's configuration or the model's token",
				directory, name)
		}
		if strings.TrimSpace(h.Secrets[name]) == "" {
			return fmt.Errorf("hook: %s names secret %s with nothing saying what it is; whoever reads a refusal has to know which credential to go and get, and how to set it",
				directory, name)
		}
	}
	for _, file := range h.Files {
		if !insideOwnDirectory(file.Path) {
			return fmt.Errorf("hook: %s carries file %q, which does not stay inside the hook's own directory",
				directory, file.Path)
		}
	}
	return nil
}

// check is every rule one binding has to satisfy.
func (b Binding) check(directory string, byPath map[string]File) error {
	if b.On == "" {
		return fmt.Errorf("hook: %s has a binding that fires on nothing; name one of %s",
			directory, strings.Join(Events, ", "))
	}
	if !known(b.On) {
		return fmt.Errorf("hook: %s fires on %q, which the runtime never raises, so it would never be called; use one of %s",
			directory, b.On, strings.Join(Events, ", "))
	}
	if b.Matcher != "" && !matching[b.On] {
		return fmt.Errorf("hook: %s matches %q on %s, and only PreToolUse and PostToolUse fire per tool, so the matcher would do nothing",
			directory, b.Matcher, b.On)
	}
	if !insideOwnDirectory(b.Entry) {
		return fmt.Errorf("hook: %s runs %q, which does not stay inside the hook's own directory",
			directory, b.Entry)
	}
	entry, carried := byPath[b.Entry]
	if !carried {
		return fmt.Errorf("hook: %s runs %q on %s and does not carry that file",
			directory, b.Entry, b.On)
	}
	if !entry.Executable {
		return fmt.Errorf("hook: %s runs %q and that file is not executable; it would fail inside the container with nothing pointing back here",
			directory, b.Entry)
	}
	if b.TimeoutSeconds < 0 {
		return fmt.Errorf("hook: %s waits %d seconds on %s, which is not a length of time",
			directory, b.TimeoutSeconds, b.On)
	}
	return nil
}

func known(event string) bool {
	for _, one := range Events {
		if one == event {
			return true
		}
	}
	return false
}

var (
	nameShape        = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	commandShape     = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	environmentShape = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

func usableName(name string) bool { return nameShape.MatchString(name) }

func plain(binary string) bool { return commandShape.MatchString(binary) }

func environmentName(name string) bool { return environmentShape.MatchString(name) }

// crewOwnName says the name belongs to the crew rather than to whoever wrote the hook.
func crewOwnName(name string) bool {
	return strings.HasPrefix(name, "QC_") || strings.HasPrefix(name, "CLAUDE_")
}

// insideOwnDirectory says the path stays under the hook's directory: relative, no parent steps, no
// root. A hook is written by somebody and imported by somebody else, so a path that climbs out of
// the directory is a path that writes wherever the crew happens to be standing.
func insideOwnDirectory(relative string) bool {
	if relative == "" || path.IsAbs(relative) || strings.HasPrefix(relative, "/") {
		return false
	}
	if strings.Contains(relative, `\`) {
		return false
	}
	cleaned := path.Clean(relative)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return false
	}
	return cleaned == relative
}
