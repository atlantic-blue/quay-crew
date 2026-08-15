package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// fakeFS is a filesystem in a map, so what the model is given can be tested without a directory tree.
type fakeFS struct {
	files   map[string]string
	written map[string]string
}

func newFakeFS(files map[string]string) *fakeFS {
	return &fakeFS{files: files, written: map[string]string{}}
}

func (f *fakeFS) List(dir string) ([]string, error) {
	dir = strings.TrimSuffix(dir, "/")
	seen := map[string]bool{}
	var names []string
	for path := range f.files {
		rest, under := strings.CutPrefix(path, dir+"/")
		if !under {
			continue
		}
		name, _, _ := strings.Cut(rest, "/")
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if names == nil {
		return nil, os.ErrNotExist
	}
	return names, nil
}

func (f *fakeFS) Read(file string) ([]byte, error) {
	body, found := f.files[file]
	if !found {
		return nil, os.ErrNotExist
	}
	return []byte(body), nil
}

func (f *fakeFS) Write(file string, body []byte) error {
	f.written[file] = string(body)
	return nil
}

func skillFile(name, description string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\nthe brief\n", name, description)
}

func TestSkillsAreCollectedOneLevelDeepAndSortedByName(t *testing.T) {
	fs := newFakeFS(map[string]string{
		"/skills/git/SKILL.md": skillFile("git", "how work is done in a repository"),
		"/skills/aws/SKILL.md": skillFile("aws", "read cloud state"),
	})

	skills := CollectSkills([]string{"/skills"}, fs)

	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2: %v", len(skills), skills)
	}
	if skills[0].Name != "aws" || skills[1].Name != "git" {
		t.Errorf("got %v, want them sorted by name", skills)
	}
	if skills[1].Description != "how work is done in a repository" {
		t.Errorf("description: got %q", skills[1].Description)
	}
}

// The description is the whole of what the model is given, so an entry without one is a name with
// nothing to decide on.
func TestASkillWithNoDescriptionIsLeftOut(t *testing.T) {
	fs := newFakeFS(map[string]string{
		"/skills/git/SKILL.md":     "---\nname: git\n---\n",
		"/skills/aws/SKILL.md":     skillFile("aws", "read cloud state"),
		"/skills/notes/README.md":  "not a skill",
		"/skills/empty/SKILL.md":   "",
		"/skills/nofront/SKILL.md": "no frontmatter here",
	})

	skills := CollectSkills([]string{"/skills"}, fs)

	if len(skills) != 1 || skills[0].Name != "aws" {
		t.Errorf("got %v, want aws alone", skills)
	}
}

func TestAStarInAPathStandsForOneLevelOfSubdirectories(t *testing.T) {
	fs := newFakeFS(map[string]string{
		"/orgs/one/skills/git/SKILL.md": skillFile("git", "repositories"),
		"/orgs/two/skills/aws/SKILL.md": skillFile("aws", "cloud"),
	})

	skills := CollectSkills([]string{"/orgs/*/skills"}, fs)

	if len(skills) != 2 {
		t.Fatalf("got %d skills, want both orgs: %v", len(skills), skills)
	}
}

// The first directory listed wins, so a personal skill shadows the crew's one of the same name
// rather than appearing twice.
func TestTheFirstDirectoryWinsWhenTwoHoldTheSameName(t *testing.T) {
	fs := newFakeFS(map[string]string{
		"/mine/git/SKILL.md": skillFile("git", "mine"),
		"/crew/git/SKILL.md": skillFile("git", "the crew's"),
		"/crew/aws/SKILL.md": skillFile("aws", "cloud"),
	})

	skills := CollectSkills([]string{"/mine", "/crew"}, fs)

	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2: %v", len(skills), skills)
	}
	for _, skill := range skills {
		if skill.Name == "git" && skill.Description != "mine" {
			t.Errorf("git: got %q, want the first directory to win", skill.Description)
		}
	}
}

func TestADirectoryThatIsNotThereIsPassedOver(t *testing.T) {
	fs := newFakeFS(map[string]string{"/skills/git/SKILL.md": skillFile("git", "repositories")})

	skills := CollectSkills([]string{"/nowhere", "/skills", "/nowhere/*/skills"}, fs)

	if len(skills) != 1 {
		t.Errorf("got %v, want the one directory that exists to still be read", skills)
	}
}

func TestFrontmatterReadsFlatFieldsAndContinuesAWrappedValue(t *testing.T) {
	fields := Frontmatter(strings.Join([]string{
		"---",
		"name: git",
		"description: how work is done here,",
		"  and it wraps onto a second line",
		"---",
		"body: not a field",
	}, "\n"))

	if fields["name"] != "git" {
		t.Errorf("name: got %q", fields["name"])
	}
	want := "how work is done here, and it wraps onto a second line"
	if fields["description"] != want {
		t.Errorf("description: got %q, want %q", fields["description"], want)
	}
	if _, found := fields["body"]; found {
		t.Error("a line after the closing marker was read as a field")
	}
}

func TestAFileWithNoFrontmatterHasNoFields(t *testing.T) {
	if fields := Frontmatter("# just a heading\n"); len(fields) != 0 {
		t.Errorf("got %v, want nothing", fields)
	}
}

func TestTheRuleIndexIsOneLinePerNumberedHeadline(t *testing.T) {
	rules := RuleIndex(strings.Join([]string{
		"# Working rules",
		"",
		"1. **Never commit without permission.**",
		"   Stop and ask first.",
		"",
		"10. **Never force-push without",
		"   explicit permission.**",
		"",
		"Some prose that is not a rule.",
	}, "\n"))

	if len(rules) != 2 {
		t.Fatalf("got %d rules, want 2: %v", len(rules), rules)
	}
	if rules[0] != "1. Never commit without permission." {
		t.Errorf("got %q", rules[0])
	}
	// A headline that wraps comes back as one line, because the index is one line per rule.
	if rules[1] != "10. Never force-push without explicit permission." {
		t.Errorf("got %q", rules[1])
	}
}

func TestClipCutsToTheCeilingAndSaysSo(t *testing.T) {
	clipped := Clip(strings.Repeat("a", 100), 10)

	if !strings.HasPrefix(clipped, strings.Repeat("a", 10)) {
		t.Errorf("got %q, want the first ten characters", clipped)
	}
	if !strings.Contains(clipped, "[cut at 10 characters]") {
		t.Errorf("got %q, want it to say it was cut", clipped)
	}
}

func TestClipLeavesTextInsideTheCeilingAlone(t *testing.T) {
	if got := Clip("short", 10); got != "short" {
		t.Errorf("got %q, want it untouched", got)
	}
}

// The count is characters rather than bytes, so a cut never lands in the middle of one and hands the
// model a broken character where a word was.
func TestClipNeverCutsAcrossACharacter(t *testing.T) {
	clipped := Clip(strings.Repeat("é", 100), 10)

	if !strings.HasPrefix(clipped, strings.Repeat("é", 10)) {
		t.Errorf("got %q, want ten whole characters", clipped)
	}
	if strings.ContainsRune(clipped, '�') {
		t.Errorf("got %q, which holds a broken character", clipped)
	}
}
