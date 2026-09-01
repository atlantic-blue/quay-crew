package skill_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/skill"
)

// files is a whole skill on its way to the system, the way a client sends one after reading a directory.
func files(manifest, brief string) []skill.File {
	return []skill.File{
		{Path: skill.ManifestFile, Body: []byte(manifest)},
		{Path: skill.BriefFile, Body: []byte(brief)},
		{Path: skill.SetupFile, Body: []byte("#!/bin/sh\n"), Executable: true},
		{Path: "reference/long.md", Body: []byte("the version nobody pays for until they open it\n")},
	}
}

const wireManifest = `name: github
version: 3
summary: Open pull requests and issues, and push branches.
binaries: [git, gh]
secrets:
  GH_TOKEN: a token with repo scope
`

// A skill that arrived over the wire has no directory, so the one rule that cannot travel is the name
// matching one. Everything else is held to the same standard as a skill read from disk.
func TestASkillFromTheWireIsWhatItsFilesSay(t *testing.T) {
	loaded, err := skill.FromFiles(files(wireManifest, "Open one with the gh tool.\n"))
	if err != nil {
		t.Fatalf("FromFiles: %v", err)
	}
	if loaded.Name != "github" || loaded.Version != 3 {
		t.Fatalf("read %s version %d", loaded.Name, loaded.Version)
	}
	if loaded.Dir != "" {
		t.Errorf("Dir is %q, and a skill from the wire is in no directory yet", loaded.Dir)
	}
	if !loaded.HasSetup {
		t.Error("it has a bin/setup and says it does not")
	}
	if len(loaded.Files) != 4 {
		t.Fatalf("carried %d files, want the 4 that were sent", len(loaded.Files))
	}
	// Sorted, so a stored revision does not change between reads with nobody editing it.
	var paths []string
	for _, file := range loaded.Files {
		paths = append(paths, file.Path)
	}
	if got := strings.Join(paths, ","); got != "SKILL.md,bin/setup,reference/long.md,skill.yaml" {
		t.Errorf("files came back as %q, want them sorted", got)
	}
	for _, file := range loaded.Files {
		if file.Path == skill.SetupFile && !file.Executable {
			t.Error("bin/setup lost its executable bit, so it could not run inside a sandbox")
		}
		if file.Path == skill.BriefFile && file.Executable {
			t.Error("SKILL.md came back executable, so the bit is not carried per file")
		}
	}
}

func TestASkillFromTheWireIsRefusedAndSaysWhy(t *testing.T) {
	for _, one := range []struct {
		name  string
		files []skill.File
		says  []string
	}{
		{
			name:  "no manifest",
			files: []skill.File{{Path: skill.BriefFile, Body: []byte("brief\n")}},
			says:  []string{skill.ManifestFile, "nothing saying what it is"},
		},
		{
			name:  "no brief",
			files: []skill.File{{Path: skill.ManifestFile, Body: []byte(wireManifest)}},
			says:  []string{skill.BriefFile, "how the work gets done"},
		},
		{
			name:  "a field the system does not know",
			files: files(wireManifest+"on_failure: retry\n", "brief\n"),
			says:  []string{"on_failure"},
		},
		{
			name:  "a name that could not be a directory",
			files: files("name: GitHub Actions\nversion: 1\nsummary: Upper case and a space.\n", "brief\n"),
			says:  []string{"GitHub Actions", "directory name inside a sandbox"},
		},
		{
			name:  "a name that would climb out of its directory",
			files: files("name: ../escape\nversion: 1\nsummary: Tries to leave.\n", "brief\n"),
			says:  []string{"../escape"},
		},
		{
			name:  "a summary that is really a brief",
			files: files("name: github\nversion: 1\nsummary: "+strings.Repeat("a", skill.SummaryLimit+1)+"\n", "brief\n"),
			says:  []string{"201", "200", "every conversation"},
		},
		{
			name:  "a brief longer than a page",
			files: files(wireManifest, strings.Repeat("a", skill.BriefLimit+1)),
			says:  []string{"4097", "4096", "read only when they are needed"},
		},
		{
			name:  "a secret with nothing said about it",
			files: files("name: github\nversion: 1\nsummary: Fine.\nsecrets:\n  GH_TOKEN: \"\"\n", "brief\n"),
			says:  []string{"GH_TOKEN", "which credential to go and get"},
		},
		{
			name: "a file that leaves the skill's directory",
			files: append(files(wireManifest, "brief\n"),
				skill.File{Path: "../../etc/cron.d/mine", Body: []byte("anything")}),
			says: []string{"../../etc/cron.d/mine", "does not stay inside"},
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			_, err := skill.FromFiles(one.files)
			if err == nil {
				t.Fatal("it was accepted, and a capability that half works is worse than one that is absent")
			}
			for _, want := range one.says {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal %q does not say %q", err, want)
				}
			}
		})
	}
}

// Importing the same version twice has to tell the same skill from a different one wearing the same
// number, which is what stops a skill changing under a session that pinned it.
func TestAFingerprintChangesWithTheSkill(t *testing.T) {
	load := func(given []skill.File) skill.Skill {
		t.Helper()
		loaded, err := skill.FromFiles(given)
		if err != nil {
			t.Fatalf("FromFiles: %v", err)
		}
		return loaded
	}

	base := files(wireManifest, "the brief\n")
	same := load(base).Fingerprint()
	if again := load(files(wireManifest, "the brief\n")).Fingerprint(); again != same {
		t.Error("the same skill fingerprinted differently twice")
	}

	changed := []struct {
		name  string
		files []skill.File
	}{
		{"a changed brief", files(wireManifest, "a different brief\n")},
		{"a changed manifest", files("name: github\nversion: 4\nsummary: Open pull requests.\n", "the brief\n")},
		{"a lost executable bit", []skill.File{base[0], base[1], {Path: skill.SetupFile, Body: base[2].Body}, base[3]}},
		{"an added file", append(append([]skill.File{}, base...), skill.File{Path: "extra.md", Body: []byte("more")})},
	}
	for _, one := range changed {
		if got := load(one.files).Fingerprint(); got == same {
			t.Errorf("%s did not change the fingerprint", one.name)
		}
	}
}

// The index is what every conversation pays for, so what is in it and what is not are both the point.
func TestTheIndexIsALinePerSkillAndNotTheBrief(t *testing.T) {
	held := []skill.Held{
		{
			Skill:     skill.Skill{Name: "git", Summary: "Branch, commit and push.", Brief: "pages of git detail"},
			BriefPath: "/home/agent/skills/git/SKILL.md",
		},
		{
			Skill:     skill.Skill{Name: "github", Summary: "Open pull requests.", Brief: "pages of github detail"},
			BriefPath: "/home/agent/.claude/skills/github/SKILL.md",
		},
	}

	index := skill.Index(held)

	for _, want := range []string{
		"git: Branch, commit and push.",
		"github: Open pull requests.",
		"/home/agent/skills/git/SKILL.md",
		// The two sources land in different places, and the index is what makes that invisible.
		"/home/agent/.claude/skills/github/SKILL.md",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("the index does not say %q:\n%s", want, index)
		}
	}
	for _, unwanted := range []string{"pages of git detail", "pages of github detail"} {
		if strings.Contains(index, unwanted) {
			t.Errorf("the index carries a brief body (%q), which is the cost it exists to avoid:\n%s", unwanted, index)
		}
	}
}

// A session holding nothing gets no section, rather than a heading with nothing under it, which is a
// thing the model opens and learns nothing from.
func TestNoSkillsIsNoIndex(t *testing.T) {
	if index := skill.Index(nil); index != "" {
		t.Errorf("the index is %q, want empty for a session holding no skills", index)
	}
}

func TestWhereASkillLivesUnderARoot(t *testing.T) {
	if got := skill.DirIn("/home/agent/skills", "github"); got != "/home/agent/skills/github" {
		t.Errorf("DirIn = %q", got)
	}
	if got := skill.BriefPathIn("/home/agent/skills", "github"); got != "/home/agent/skills/github/SKILL.md" {
		t.Errorf("BriefPathIn = %q", got)
	}
}
