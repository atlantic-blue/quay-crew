package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// FileSystem is everything the analyser asks of the machine it runs on. It is an interface so the
// rules for what gets sent to the model can be tested without a directory tree behind them.
type FileSystem interface {
	// List names the entries of a directory. An unreadable directory is an error, not a panic.
	List(dir string) ([]string, error)
	// Read reads a whole file.
	Read(file string) ([]byte, error)
	// Write replaces a file, creating it when it is not there.
	Write(file string, body []byte) error
}

// Skill is one capability a session holds, as its own SKILL.md describes it.
type Skill struct {
	Name        string
	Description string
}

// CollectSkills gathers the skills under each directory, one level deep, from
// <dir>/<slug>/SKILL.md.
//
// A directory may hold one *, which stands for one level of subdirectories, so a path through orgs
// and a star reaches every org's skills without naming the orgs. A skill with no description is left
// out: the description is the whole of what the model is given, so an entry without one is a name
// with nothing to decide on.
func CollectSkills(dirs []string, fs FileSystem) []Skill {
	found := map[string]Skill{}

	for _, dir := range expandStars(dirs, fs) {
		entries, err := fs.List(dir)
		if err != nil {
			continue
		}
		for _, slug := range entries {
			body, err := fs.Read(dir + "/" + slug + "/SKILL.md")
			if err != nil {
				continue
			}
			fields := Frontmatter(string(body))
			name := fields["name"]
			if name == "" {
				name = slug
			}
			if fields["description"] == "" {
				continue
			}
			if _, already := found[name]; !already {
				found[name] = Skill{Name: name, Description: fields["description"]}
			}
		}
	}

	skills := make([]Skill, 0, len(found))
	for _, one := range found {
		skills = append(skills, one)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills
}

// expandStars turns one * in a path into the subdirectories it stands for.
func expandStars(dirs []string, fs FileSystem) []string {
	expanded := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		star := strings.Index(dir, "*")
		if star == -1 {
			expanded = append(expanded, dir)
			continue
		}
		head := strings.TrimSuffix(dir[:star], "/")
		tail := strings.TrimPrefix(dir[star+1:], "/")
		children, err := fs.List(head)
		if err != nil {
			continue
		}
		for _, child := range children {
			at := head + "/" + child
			if tail != "" {
				at += "/" + tail
			}
			expanded = append(expanded, at)
		}
	}
	return expanded
}

var frontmatterField = regexp.MustCompile(`^([A-Za-z][\w-]*):\s?(.*)$`)

// Frontmatter reads the leading YAML-ish block of a markdown file as flat key to value pairs. A
// wrapped value continues the key above it, which is how a long description is written.
func Frontmatter(text string) map[string]string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return map[string]string{}
	}

	fields := map[string]string{}
	key := ""
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		if match := frontmatterField.FindStringSubmatch(line); match != nil {
			key = match[1]
			fields[key] = strings.TrimSpace(match[2])
			continue
		}
		if key != "" && strings.HasPrefix(line, " ") && strings.TrimSpace(line) != "" {
			fields[key] = strings.TrimSpace(fields[key] + " " + strings.TrimSpace(line))
		}
	}
	return fields
}

var ruleHeadline = regexp.MustCompile(`(?ms)^(\d+)\.\s+\*\*(.+?)\*\*`)

var whitespace = regexp.MustCompile(`\s+`)

// RuleIndex is one line per numbered rule in a rules file, taken from its bold headline. The index
// is derived from the file every run, so a rule that changes wording cannot go stale here.
func RuleIndex(markdown string) []string {
	matches := ruleHeadline.FindAllStringSubmatch(markdown, -1)
	headlines := make([]string, 0, len(matches))
	for _, match := range matches {
		headline := strings.TrimSpace(whitespace.ReplaceAllString(match[2], " "))
		headlines = append(headlines, match[1]+". "+headline)
	}
	return headlines
}

// Clip cuts a document to the ceiling and says so, rather than sending a novel to the model.
//
// The count is characters rather than bytes, so a cut never lands in the middle of one and hands the
// model a broken character where a word was.
func Clip(text string, max int) string {
	if max <= 0 || utf8.RuneCountInString(text) <= max {
		return text
	}
	cut := []rune(text)[:max]
	return string(cut) + "\n[cut at " + strconv.Itoa(max) + " characters]"
}
