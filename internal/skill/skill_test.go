package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write puts a skill directory on disk and returns it. Only the files a case cares about are written,
// so a case about a missing brief is written as a missing brief rather than as a flag.
func write(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
			t.Fatalf("make %s: %v", filepath.Dir(full), err)
		}
		mode := os.FileMode(0o666)
		if strings.HasPrefix(name, "bin/") {
			mode = 0o777
		}
		if err := os.WriteFile(full, []byte(body), mode); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return dir
}

const goodManifest = `name: github
version: 3
summary: Open pull requests and issues, and push branches.
binaries: [git, gh]
secrets:
  GH_TOKEN: a token with repo scope, set with ` + "`quay secret set <workspace> GH_TOKEN`" + `
`

func TestASkillIsLoadedFromItsDirectory(t *testing.T) {
	dir := write(t, map[string]string{
		ManifestFile:      goodManifest,
		BriefFile:         "Open a pull request with gh.\n",
		SetupFile:         "#!/bin/sh\ngh auth setup-git\n",
		"reference/pr.md": "the long version nobody pays for\n",
	})

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Name != "github" {
		t.Errorf("name = %q, want github", loaded.Name)
	}
	if loaded.Version != 3 {
		t.Errorf("version = %d, want 3", loaded.Version)
	}
	if loaded.Summary != "Open pull requests and issues, and push branches." {
		t.Errorf("summary = %q", loaded.Summary)
	}
	if got := strings.Join(loaded.Binaries, ","); got != "git,gh" {
		t.Errorf("binaries = %q, want git,gh", got)
	}
	if len(loaded.Secrets) != 1 || loaded.Secrets[0].Name != "GH_TOKEN" {
		t.Fatalf("secrets = %+v, want one named GH_TOKEN", loaded.Secrets)
	}
	if !strings.Contains(loaded.Secrets[0].Purpose, "repo scope") {
		t.Errorf("secret purpose = %q, want it to say what the token needs", loaded.Secrets[0].Purpose)
	}
	if loaded.Brief != "Open a pull request with gh.\n" {
		t.Errorf("brief = %q", loaded.Brief)
	}
}

// The whole directory travels, because a skill in the store has to be whole: a crew on a pod has no
// host directory to go back to for the files it did not copy.
func TestTheWholeDirectoryTravels(t *testing.T) {
	dir := write(t, map[string]string{
		ManifestFile:      goodManifest,
		BriefFile:         "Open a pull request with gh.\n",
		SetupFile:         "#!/bin/sh\n",
		"reference/pr.md": "detail\n",
	})

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var paths []string
	for _, file := range loaded.Files {
		paths = append(paths, file.Path)
	}
	want := "SKILL.md,bin/setup,reference/pr.md,skill.yaml"
	if got := strings.Join(paths, ","); got != want {
		t.Errorf("files = %q, want %q, sorted so a stored revision does not change on a re-read", got, want)
	}
}

// A setup script that arrives without its executable bit cannot run, and the failure surfaces inside a
// container as a permission error with nothing pointing back here.
func TestASetupScriptStaysExecutable(t *testing.T) {
	dir := write(t, map[string]string{
		ManifestFile: goodManifest,
		BriefFile:    "brief\n",
		SetupFile:    "#!/bin/sh\n",
	})

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, file := range loaded.Files {
		switch file.Path {
		case SetupFile:
			if !file.Executable {
				t.Error("bin/setup came back not executable, so it could not run inside a sandbox")
			}
		case BriefFile:
			if file.Executable {
				t.Error("SKILL.md came back executable, so the bit is not being read per file")
			}
		}
	}
}

// Secrets come from a map, which has no order, so they are sorted. Without this a skill's stored
// revision changes between reads with nobody editing it.
func TestSecretsComeBackInOneOrder(t *testing.T) {
	raw := `name: many
version: 1
summary: A skill with several secrets.
secrets:
  ZED_TOKEN: last
  ALPHA_TOKEN: first
  MIDDLE_TOKEN: middle
`
	first, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var names []string
	for _, secret := range first.Secrets {
		names = append(names, secret.Name)
	}
	if got := strings.Join(names, ","); got != "ALPHA_TOKEN,MIDDLE_TOKEN,ZED_TOKEN" {
		t.Errorf("secrets = %q, want them sorted", got)
	}
}

func TestAMalformedSkillIsRefusedAndSaysWhy(t *testing.T) {
	for _, one := range []struct {
		name  string
		files map[string]string
		says  []string
	}{
		{
			name:  "no manifest at all",
			files: map[string]string{BriefFile: "brief\n"},
			says:  []string{ManifestFile, "nothing saying what it is"},
		},
		{
			name:  "no brief",
			files: map[string]string{ManifestFile: goodManifest},
			says:  []string{"github", BriefFile},
		},
		{
			name:  "an empty brief",
			files: map[string]string{ManifestFile: goodManifest, BriefFile: "   \n\n"},
			says:  []string{"github", "says nothing about how the work is done"},
		},
		{
			name: "a brief longer than a page",
			files: map[string]string{
				ManifestFile: goodManifest,
				BriefFile:    strings.Repeat("a", BriefLimit+1),
			},
			says: []string{"4097", "4096", "read only when they are needed"},
		},
		{
			name: "a field the crew does not know",
			files: map[string]string{
				ManifestFile: goodManifest + "on_failure: retry\n",
				BriefFile:    "brief\n",
			},
			says: []string{"on_failure"},
		},
		{
			name: "no name",
			files: map[string]string{
				ManifestFile: "version: 1\nsummary: No name at all.\n",
				BriefFile:    "brief\n",
			},
			says: []string{"needs a name"},
		},
		{
			name: "a name that is not a usable directory",
			files: map[string]string{
				ManifestFile: "name: GitHub Actions\nversion: 1\nsummary: Upper case and a space.\n",
				BriefFile:    "brief\n",
			},
			says: []string{"GitHub Actions", "directory name inside a sandbox"},
		},
		{
			name: "a name that would escape its directory",
			files: map[string]string{
				ManifestFile: "name: ../escape\nversion: 1\nsummary: Tries to leave.\n",
				BriefFile:    "brief\n",
			},
			says: []string{"../escape"},
		},
		{
			name: "no version",
			files: map[string]string{
				ManifestFile: "name: github\nsummary: No version.\n",
				BriefFile:    "brief\n",
			},
			says: []string{"version of 1 or more", "pin the revision"},
		},
		{
			name: "no summary",
			files: map[string]string{
				ManifestFile: "name: github\nversion: 1\n",
				BriefFile:    "brief\n",
			},
			says: []string{"needs a summary"},
		},
		{
			name: "a summary of several lines",
			files: map[string]string{
				ManifestFile: "name: github\nversion: 1\nsummary: |\n  first line\n  second line\n",
				BriefFile:    "brief\n",
			},
			says: []string{"more than one line"},
		},
		{
			name: "a summary that is really a brief",
			files: map[string]string{
				ManifestFile: "name: github\nversion: 1\nsummary: " + strings.Repeat("a", SummaryLimit+1) + "\n",
				BriefFile:    "brief\n",
			},
			says: []string{"201", "200", "every conversation"},
		},
		{
			name: "a binary that is a command line",
			files: map[string]string{
				ManifestFile: "name: github\nversion: 1\nsummary: Fine.\nbinaries: [\"/usr/bin/gh auth\"]\n",
				BriefFile:    "brief\n",
			},
			says: []string{"/usr/bin/gh auth", "not a path or a command line"},
		},
		{
			name: "a secret that is not an environment variable name",
			files: map[string]string{
				ManifestFile: "name: github\nversion: 1\nsummary: Fine.\nsecrets:\n  gh-token: lowercase\n",
				BriefFile:    "brief\n",
			},
			says: []string{"gh-token", "environment variable"},
		},
		{
			name: "a secret starting with a digit",
			files: map[string]string{
				ManifestFile: "name: github\nversion: 1\nsummary: Fine.\nsecrets:\n  1TOKEN: leading digit\n",
				BriefFile:    "brief\n",
			},
			says: []string{"1TOKEN"},
		},
		{
			name: "a secret with nothing said about it",
			files: map[string]string{
				ManifestFile: "name: github\nversion: 1\nsummary: Fine.\nsecrets:\n  GH_TOKEN: \"\"\n",
				BriefFile:    "brief\n",
			},
			says: []string{"GH_TOKEN", "which credential to go and get"},
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			_, err := Load(write(t, one.files))
			if err == nil {
				t.Fatal("loaded a skill that should have been refused")
			}
			for _, want := range one.says {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal %q does not say %q", err, want)
				}
			}
		})
	}
}

// The index is what every conversation pays for, so what is in it and what is not are both the point.
func TestTheIndexIsALinePerSkillAndNotTheBrief(t *testing.T) {
	skills := []Skill{
		{Name: "git", Summary: "Branch, commit and push.", Brief: "the whole git brief, pages of it"},
		{Name: "github", Summary: "Open pull requests.", Brief: "the whole github brief"},
	}

	index := Index("/home/agent/.claude", skills)

	for _, want := range []string{
		"git: Branch, commit and push.",
		"github: Open pull requests.",
		"/home/agent/.claude/skills/git/SKILL.md",
		"/home/agent/.claude/skills/github/SKILL.md",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("index does not say %q:\n%s", want, index)
		}
	}
	for _, unwanted := range []string{"pages of it", "the whole github brief"} {
		if strings.Contains(index, unwanted) {
			t.Errorf("index carries a brief body (%q), which is the cost this design exists to avoid:\n%s", unwanted, index)
		}
	}
}

// A session with no skills gets no section, rather than a heading with nothing under it, which is a
// thing the model opens and learns nothing from.
func TestNoSkillsIsNoIndex(t *testing.T) {
	if index := Index("/home/agent/.claude", nil); index != "" {
		t.Errorf("index = %q, want empty for a session holding no skills", index)
	}
}

// A skill arriving over the wire goes through the same validator as one read from a directory, because
// the control plane cannot see the operator's machine and a second validator is a second answer.
func TestFilesFromTheWireGoThroughTheSameRefusals(t *testing.T) {
	_, err := FromFiles([]File{
		{Path: ManifestFile, Body: []byte("name: github\nversion: 1\nsummary: Fine.\non_failure: retry\n")},
		{Path: BriefFile, Body: []byte("brief\n")},
	})
	if err == nil {
		t.Fatal("accepted a manifest field the crew does not know")
	}
	if !strings.Contains(err.Error(), "on_failure") {
		t.Errorf("refusal %q does not name the field", err)
	}
}

// A path that climbs out of the skill's directory would have the crew writing wherever a caller asked,
// into a directory it then mounts into a container.
func TestAFileThatLeavesTheSkillsDirectoryIsRefused(t *testing.T) {
	for _, escape := range []string{"../../etc/cron.d/mine", "/etc/passwd", "bin/../../out", ".."} {
		_, err := FromFiles([]File{
			{Path: ManifestFile, Body: []byte(goodManifest)},
			{Path: BriefFile, Body: []byte("brief\n")},
			{Path: escape, Body: []byte("anything")},
		})
		if err == nil {
			t.Errorf("accepted file path %q", escape)
			continue
		}
		if !strings.Contains(err.Error(), escape) {
			t.Errorf("refusal for %q does not name it: %v", escape, err)
		}
	}
}

// Importing the same version twice has to be able to tell the same skill from a different one wearing
// the same number, which is what stops a skill changing under a session that pinned it.
func TestAFingerprintChangesWithTheSkill(t *testing.T) {
	base := []File{
		{Path: ManifestFile, Body: []byte(goodManifest)},
		{Path: BriefFile, Body: []byte("brief\n")},
		{Path: SetupFile, Body: []byte("#!/bin/sh\n"), Executable: true},
	}
	load := func(files []File) Skill {
		t.Helper()
		loaded, err := FromFiles(files)
		if err != nil {
			t.Fatalf("from files: %v", err)
		}
		return loaded
	}

	same := load(base).Fingerprint()
	if again := load(base).Fingerprint(); again != same {
		t.Error("the same skill fingerprinted differently twice")
	}

	otherManifest := "name: github\nversion: 4\nsummary: Open pull requests and issues, and push branches.\n"
	for _, one := range []struct {
		name  string
		files []File
	}{
		{"a changed brief", []File{base[0], {Path: BriefFile, Body: []byte("a different brief\n")}, base[2]}},
		{"a changed manifest", []File{{Path: ManifestFile, Body: []byte(otherManifest)}, base[1], base[2]}},
		{"a lost executable bit", []File{base[0], base[1], {Path: SetupFile, Body: base[2].Body}}},
		{"an added file", append(append([]File{}, base...), File{Path: "reference/pr.md", Body: []byte("more")})},
	} {
		if got := load(one.files).Fingerprint(); got == same {
			t.Errorf("%s did not change the fingerprint", one.name)
		}
	}
}

func TestWhereASkillLives(t *testing.T) {
	if got := Dir("/home/agent/.claude", "github"); got != "/home/agent/.claude/skills/github" {
		t.Errorf("Dir = %q", got)
	}
	if got := BriefPath("/home/agent/.claude", "github"); got != "/home/agent/.claude/skills/github/SKILL.md" {
		t.Errorf("BriefPath = %q", got)
	}
}
