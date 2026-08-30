// Package changelog assembles a release section out of the fragments in changelog.d.
//
// Every change used to write its entry at the top of CHANGELOG.md. Two changes made at the same time
// then wrote the same lines of the same file, so they collided by construction, and the resolution
// was always the same and always mechanical: keep both entries. A crew that runs work in parallel
// pays that on every batch rather than occasionally.
//
// A fragment is one small file per change, named after the issue it closes. Two changes write two
// different files and never touch each other, so nothing has to be resolved. The release is where
// they are assembled into one dated section and the files go away.
package changelog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Dir is where the fragments live, from the root of the repository.
const Dir = "changelog.d"

// named is the shape of a fragment's file name: the issue number it closes, then words joined with
// hyphens. The number is what orders the release, and a fragment nobody can trace back to an issue is
// an entry whose reason has already been lost, so a name that is not this shape is refused rather
// than guessed at.
var named = regexp.MustCompile(`^([0-9]+)-[a-z0-9]+(-[a-z0-9]+)*\.md$`)

// Fragment is one change's entry, waiting for a release.
type Fragment struct {
	// File is the name in changelog.d, and Number is the issue it closes, read off the front of it.
	File   string
	Number int
	// Body is the markdown the entry is written in, as the author wrote it.
	Body string
}

// Collect reads every fragment in a directory, newest first.
//
// It refuses rather than skips. A fragment with a name it cannot read is work somebody did that
// would silently miss the release, which is worse than a command that stops and says which file.
func Collect(dir string) ([]Fragment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}

	fragments := make([]Fragment, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		// The README says what the convention is, and anything that is not markdown is not an entry.
		if entry.IsDir() || name == "README.md" || filepath.Ext(name) != ".md" {
			continue
		}
		match := named.FindStringSubmatch(name)
		if match == nil {
			return nil, fmt.Errorf("%s is not a fragment name: call it <issue>-<words-joined-with-hyphens>.md, for example 480-changelog-fragments.md", filepath.Join(dir, name))
		}
		number, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("%s does not start with an issue number: %w", filepath.Join(dir, name), err)
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", filepath.Join(dir, name), err)
		}
		text := strings.TrimSpace(strings.ReplaceAll(string(body), "\r\n", "\n"))
		if text == "" {
			return nil, fmt.Errorf("%s is empty, so the release would carry a bullet that says nothing", filepath.Join(dir, name))
		}
		fragments = append(fragments, Fragment{File: name, Number: number, Body: text})
	}

	// Highest issue number first, which is the changelog's own order: newest at the top. Two
	// fragments closing one issue keep the order of their names, so a release does not shuffle
	// between runs.
	sort.Slice(fragments, func(i, j int) bool {
		if fragments[i].Number != fragments[j].Number {
			return fragments[i].Number > fragments[j].Number
		}
		return fragments[i].File < fragments[j].File
	})
	return fragments, nil
}

// Render writes the fragments as one dated section, in the shape CHANGELOG.md already uses: a bullet
// per change, with everything under it indented to sit inside that bullet.
func Render(date string, fragments []Fragment) string {
	var out strings.Builder
	fmt.Fprintf(&out, "## %s\n\n", date)
	for at, fragment := range fragments {
		if at > 0 {
			out.WriteString("\n")
		}
		for line, text := range strings.Split(fragment.Body, "\n") {
			switch {
			case line == 0:
				fmt.Fprintf(&out, "- %s\n", text)
			case strings.TrimSpace(text) == "":
				out.WriteString("\n")
			default:
				fmt.Fprintf(&out, "  %s\n", text)
			}
		}
	}
	return out.String()
}

// Assemble is the release: every fragment in the directory, as one section dated today.
//
// A directory with nothing in it is refused. An assembled release that says nothing looks exactly
// like a release that assembled correctly, and the point of a release note is that somebody reads it.
func Assemble(dir, date string) (string, error) {
	fragments, err := Collect(dir)
	if err != nil {
		return "", err
	}
	if len(fragments) == 0 {
		return "", fmt.Errorf("%s holds no fragments, so there is nothing to assemble", dir)
	}
	return Render(date, fragments), nil
}
