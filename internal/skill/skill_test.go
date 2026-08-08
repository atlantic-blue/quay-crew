package skill_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/skill"
)

// write puts a skill on disk the way an operator would, and returns the directory holding it.
func write(t *testing.T, dir, name, manifest, brief string) string {
	t.Helper()
	at := filepath.Join(dir, name)
	if err := os.MkdirAll(at, 0o777); err != nil {
		t.Fatalf("make the skill directory: %v", err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(at, skill.ManifestFile), []byte(manifest), 0o666); err != nil {
			t.Fatalf("write the manifest: %v", err)
		}
	}
	if brief != "" {
		if err := os.WriteFile(filepath.Join(at, skill.BriefFile), []byte(brief), 0o666); err != nil {
			t.Fatalf("write the brief: %v", err)
		}
	}
	return at
}

const gitManifest = `name: git
version: 3
summary: Branch, stage and commit, the way this crew does it.
binaries: [git]
secrets:
  GH_TOKEN: a token that can read the repository
`

// TestASkillIsWhatItsFilesSay.
func TestASkillIsWhatItsFilesSay(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "git", gitManifest, "# git\n\nBranch first. Stage named files.\n")

	skills, err := skill.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("read %d skills, want 1", len(skills))
	}
	got := skills[0]
	if got.Name != "git" || got.Version != 3 {
		t.Fatalf("read %s version %d", got.Name, got.Version)
	}
	if !strings.Contains(got.Brief, "Branch first") {
		t.Fatalf("the brief is %q", got.Brief)
	}
	if len(got.Binaries) != 1 || got.Binaries[0] != "git" {
		t.Fatalf("it needs %v", got.Binaries)
	}
	if names := got.SecretNames(); len(names) != 1 || names[0] != "GH_TOKEN" {
		t.Fatalf("it names %v", names)
	}
	// The value is never in the file. What is written down is what the secret is for.
	if strings.Contains(gitManifest, "ghp_") {
		t.Fatal("the manifest carries a value, and a value in a skill file is a value in a repository")
	}
	if got.HasSetup {
		t.Fatal("it reports a setup script it does not have")
	}
}

// TestASkillWithSomethingToDoBeforeTheFirstTurn.
func TestASkillWithSomethingToDoBeforeTheFirstTurn(t *testing.T) {
	dir := t.TempDir()
	at := write(t, dir, "git", gitManifest, "# git\n\nBranch first.\n")
	if err := os.MkdirAll(filepath.Join(at, "bin"), 0o777); err != nil {
		t.Fatalf("make bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(at, skill.SetupFile), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write the setup: %v", err)
	}

	skills, err := skill.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !skills[0].HasSetup {
		t.Fatal("the setup script is there and the skill does not know about it")
	}
}

// TestSomethingThatIsNotASkillIsPassedOver, so notes and a README can sit beside them.
func TestSomethingThatIsNotASkillIsPassedOver(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "git", gitManifest, "# git\n\nBranch first.\n")
	write(t, dir, "notes", "", "")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("these are skills\n"), 0o666); err != nil {
		t.Fatalf("write the readme: %v", err)
	}

	skills, err := skill.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("read %d skills, want only the one with a manifest", len(skills))
	}
}

// TestASkillNobodyCanUseIsRefusedRatherThanSkipped.
//
// A skill the operator wrote and got wrong has to say so. Skipped, it is simply absent later, and the
// session behaves as though the capability was never asked for.
func TestASkillNobodyCanUseIsRefusedRatherThanSkipped(t *testing.T) {
	for _, tc := range []struct {
		name     string
		manifest string
		brief    string
		says     string
		because  string
	}{
		{
			name:     "a name that is not its directory",
			manifest: "name: github\nversion: 1\n",
			brief:    "# github\n",
			says:     "a skill is the directory it lives in",
			because:  "otherwise it is the same skill under two names the moment anybody attaches it",
		},
		{
			name:     "no version",
			manifest: "name: git\n",
			brief:    "# git\n",
			says:     "no version",
			because:  "a session is pinned to the version it started with",
		},
		{
			name:     "an empty brief",
			manifest: "name: git\nversion: 1\n",
			brief:    "\n",
			says:     "would be told nothing",
			because:  "the brief is the whole of what the model is given",
		},
		{
			name:     "a binary that is a shell fragment",
			manifest: "name: git\nversion: 1\nbinaries: [\"git; rm -rf /\"]\n",
			brief:    "# git\n",
			says:     "not a command name",
			because:  "the crew looks for these inside a sandbox and must not be handed something to run",
		},
		{
			name:     "a secret that is not a name",
			manifest: "name: git\nversion: 1\nsecrets:\n  \"GH TOKEN=x\": nonsense\n",
			brief:    "# git\n",
			says:     "not an environment variable name",
			because:  "the crew puts these in an environment and cannot quote its way out of that",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "git", tc.manifest, tc.brief)
			_, err := skill.Load(dir)
			if err == nil {
				t.Fatalf("it was accepted, and %s", tc.because)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("the refusal is %q, want it to say %q", err, tc.says)
			}
		})
	}
}

// TestNoSkillsAtAllIsNotAFailure. A crew with no skills directory is every crew today.
func TestNoSkillsAtAllIsNotAFailure(t *testing.T) {
	for _, dir := range []string{"", filepath.Join(t.TempDir(), "nothing-here")} {
		skills, err := skill.Load(dir)
		if err != nil {
			t.Fatalf("Load(%q): %v", dir, err)
		}
		if len(skills) != 0 {
			t.Fatalf("Load(%q) read %d skills", dir, len(skills))
		}
	}
}
