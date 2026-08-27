package features_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The README is the front door, and a front door that promises what the crew cannot do is worse than
// no front door: a reader takes it at its word, types the command, and concludes the product is
// broken. Its list of what works predated a day of merged work once, so these hold it to the three
// things it can be wrong about. What commands exist, what make targets exist, and what documents
// exist are all questions with an answer somewhere else in this repository.
//
// What none of this checks is whether a sentence is true. A bullet claiming a capability the crew
// does not have, in words that name no command, passes every case here. The scenarios in this
// directory are what say whether a capability is real; this says the front door points at them.

// theFrontDoor is the file under test, a directory up from here.
const theFrontDoor = "../README.md"

// theMakefile is the other file the front door makes claims about.
const theFrontDoorsMakefile = "../Makefile"

// codeIn returns everything the front door marks as something to type: every inline code span, and
// every line inside a fenced block. Prose is left out on purpose, because "quay is on your path" is
// not a claim that `is` is a command.
func codeIn(text string) []string {
	var typed []string

	fenced := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			// A trailing shell comment is prose, so "make tool # over whatever quay your shell runs"
			// would otherwise claim a command called `your`. Everything from an unquoted hash on is
			// the author talking to the reader rather than something to type.
			typed = append(typed, strings.TrimSpace(withoutComment(line)))
			continue
		}
		// An inline span may be wrapped over two lines, so the newline is a space by then.
		for _, span := range regexp.MustCompile("`([^`]*)`").FindAllStringSubmatch(line, -1) {
			typed = append(typed, strings.Join(strings.Fields(span[1]), " "))
		}
	}
	return typed
}

// namedAfter returns the word following each occurrence of a tool's name in something to type, which
// is the command or the target being claimed. A span that is only the tool's name names nothing.
func namedAfter(tool string, typed []string) []string {
	seen := map[string]bool{}
	var named []string
	for _, one := range typed {
		fields := strings.Fields(one)
		for n, field := range fields {
			if field != tool || n+1 >= len(fields) {
				continue
			}
			word := fields[n+1]
			// A placeholder is the reader's to fill in, and a flag is not a command.
			if strings.HasPrefix(word, "<") || strings.HasPrefix(word, "-") || strings.HasPrefix(word, "$") {
				continue
			}
			if !seen[word] {
				seen[word] = true
				named = append(named, word)
			}
		}
	}
	sort.Strings(named)
	return named
}

// quayCommands is every command the real tool lists, read from the build in this checkout rather
// than from a copy of the list. A test that held the front door to a remembered list would go stale
// in exactly the way the front door did.
func quayCommands() (map[string]bool, error) {
	built := filepath.Join(os.TempDir(), "quay-frontdoor-test")
	build := exec.Command("go", "build", "-o", built, "../cmd/quay")
	if out, err := build.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("building the tool: %w\n%s", err, out)
	}
	defer func() { _ = os.Remove(built) }()

	out, err := exec.Command(built, "help").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("quay help: %w\n%s", err, out)
	}

	// The command column starts each entry at a fixed indent, and a continuation line is indented
	// further, so the first word of a two space indented line is a command and nothing else is.
	commands := map[string]bool{}
	listing := false
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "commands:") {
			listing = true
			continue
		}
		if !listing || !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
			continue
		}
		if fields := strings.Fields(line); len(fields) > 0 {
			commands[fields[0]] = true
		}
	}
	if len(commands) == 0 {
		return nil, fmt.Errorf("quay help listed no commands at all, so this would pass on anything:\n%s", out)
	}
	return commands, nil
}

// makeTargets is every target the real Makefile declares, read the way make reads it.
func makeTargets() (map[string]bool, error) {
	body, err := os.ReadFile(theFrontDoorsMakefile)
	if err != nil {
		return nil, err
	}
	header := regexp.MustCompile(`(?m)^([a-z][a-z0-9-]*):`)
	targets := map[string]bool{}
	for _, found := range header.FindAllStringSubmatch(string(body), -1) {
		targets[found[1]] = true
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("the Makefile declares no targets at all, so this would pass on anything")
	}
	return targets, nil
}

// linkedFiles returns every relative link the front door makes, which is every document it sends a
// reader to.
func linkedFiles(text string) []string {
	seen := map[string]bool{}
	var links []string
	for _, found := range regexp.MustCompile(`\]\(([^)]+)\)`).FindAllStringSubmatch(text, -1) {
		target := found[1]
		if strings.HasPrefix(target, "http") || strings.HasPrefix(target, "#") {
			continue
		}
		if at := strings.Index(target, "#"); at >= 0 {
			target = target[:at]
		}
		if target != "" && !seen[target] {
			seen[target] = true
			links = append(links, target)
		}
	}
	sort.Strings(links)
	return links
}

func frontDoor(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(theFrontDoor)
	if err != nil {
		t.Fatalf("reading the front door: %v", err)
	}
	return string(body)
}

// TestEveryCommandTheFrontDoorNamesExists, checked against the tool this checkout builds.
//
// This is the one that catches the front door going stale, from either end: a command named before
// it is built, and a command renamed underneath a README nobody reread.
func TestEveryCommandTheFrontDoorNamesExists(t *testing.T) {
	commands, err := quayCommands()
	if err != nil {
		t.Fatalf("asking the tool what it can do: %v", err)
	}

	named := namedAfter("quay", codeIn(frontDoor(t)))
	if len(named) == 0 {
		t.Fatal("the front door names no quay command at all, so this proved nothing")
	}
	t.Logf("the front door names %d commands: %s", len(named), strings.Join(named, " "))

	for _, one := range named {
		if !commands[one] {
			t.Errorf("the front door tells a reader to run `quay %s`, and the tool has no such command", one)
		}
	}
}

// TestEveryTargetTheFrontDoorNamesExists. The quick start is the first thing anybody types, and a
// target that has been renamed underneath it fails on the first line.
func TestEveryTargetTheFrontDoorNamesExists(t *testing.T) {
	targets, err := makeTargets()
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}

	named := namedAfter("make", codeIn(frontDoor(t)))
	if len(named) == 0 {
		t.Fatal("the front door names no make target at all, so this proved nothing")
	}
	t.Logf("the front door names %d targets: %s", len(named), strings.Join(named, " "))

	for _, one := range named {
		if !targets[one] {
			t.Errorf("the front door tells a reader to run `make %s`, and the Makefile has no such target", one)
		}
	}
}

// TestTheQuickStartIsOneCommand. The four command first run is what the front door said for months
// after it stopped being true, so the shape is pinned rather than left to prose.
func TestTheQuickStartIsOneCommand(t *testing.T) {
	quickStart, err := sectionOf(frontDoor(t), "## Quick start")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(quickStart, "make install") {
		t.Fatalf("the quick start does not name make install:\n%s", quickStart)
	}
	// The three that used to come before it. Naming them in a quick start is naming a first run that
	// takes four commands again.
	for _, retired := range []string{"make config", "make sandbox-image", "make up\n"} {
		if strings.Contains(quickStart, retired) {
			t.Errorf("the quick start still tells a reader to run %q, so a first run reads as more "+
				"than one command", strings.TrimSpace(retired))
		}
	}
}

// TestEveryDocumentTheFrontDoorPointsAtExists. The front door points at the documents rather than
// repeating them, which only works while they are there.
func TestEveryDocumentTheFrontDoorPointsAtExists(t *testing.T) {
	links := linkedFiles(frontDoor(t))
	if len(links) == 0 {
		t.Fatal("the front door links to nothing, so this proved nothing")
	}
	t.Logf("the front door points at %d files", len(links))

	for _, link := range links {
		if _, err := os.Stat(filepath.Join("..", link)); err != nil {
			t.Errorf("the front door points a reader at %s, which is not there: %v", link, err)
		}
	}
}

// TestTheFrontDoorCarriesThePictureOfAPieceOfWork.
//
// Work as a declared resource is the shape of the product, and it is the thing a paragraph explains
// worst. The diagram is required to exist, to be a flowchart, and to have every label quoted, which
// is what stops a parenthesis or a slash breaking the parse where nobody renders it.
func TestTheFrontDoorCarriesThePictureOfAPieceOfWork(t *testing.T) {
	diagram, err := diagramIn(frontDoor(t))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(diagram, "flowchart") {
		t.Errorf("the diagram is not a flowchart:\n%s", diagram)
	}
	for _, through := range []string{"controller", "lease", "role", "session"} {
		if !strings.Contains(strings.ToLower(diagram), through) {
			t.Errorf("the diagram never shows the %s, so it does not follow a piece of work through:\n%s",
				through, diagram)
		}
	}

	// A label with an unquoted parenthesis, slash or colon does not parse, and nothing on this
	// machine renders one, so the quoting is checked rather than assumed.
	for _, label := range regexp.MustCompile(`[\[{(]+([^"\[\]{}()]*[(/:][^"\[\]{}()]*)[\]})]+`).FindAllStringSubmatch(diagram, -1) {
		t.Errorf("the diagram has an unquoted label %q, which does not parse", strings.TrimSpace(label[1]))
	}
}

// TestTheFrontDoorUsesNoMarkdownConstructThatCannotBeReused.
//
// A blockquote renders as a bar down the left of every line, and it wraps the text so a reader
// cannot copy it out. A table is the other one: it reads as a grid and reuses as nothing. The guard
// is over the whole file rather than over the lines that were changed, because the next one would go
// in somewhere else.
func TestTheFrontDoorUsesNoMarkdownConstructThatCannotBeReused(t *testing.T) {
	for _, one := range unreusableMarkdownIn(frontDoor(t)) {
		t.Error(one)
	}
}

// withoutComment cuts a line at the shell comment on it, which is the author explaining rather than
// something the reader types.
func withoutComment(line string) string {
	for n, char := range line {
		if char != '#' {
			continue
		}
		if n == 0 || line[n-1] == ' ' || line[n-1] == '\t' {
			return line[:n]
		}
	}
	return line
}

// sectionOf returns one heading's section, so a rule about the quick start is not accidentally
// satisfied by a line somewhere else in the file.
func sectionOf(text, heading string) (string, error) {
	at := strings.Index(text, heading)
	if at < 0 {
		return "", fmt.Errorf("the front door has no %q section at all", heading)
	}
	section := text[at:]
	if end := strings.Index(section[len(heading):], "\n## "); end >= 0 {
		section = section[:end+len(heading)]
	}
	return section, nil
}

// diagramIn returns the first mermaid block, which is the picture of a piece of work.
func diagramIn(text string) (string, error) {
	const opener = "```mermaid"
	opened := strings.Index(text, opener)
	if opened < 0 {
		return "", fmt.Errorf("the front door carries no diagram, so the shape of the product is prose only")
	}
	closed := strings.Index(text[opened+len(opener):], "```")
	if closed < 0 {
		return "", fmt.Errorf("the front door's diagram is never closed")
	}
	return text[opened : opened+len(opener)+closed], nil
}

// unreusableMarkdownIn returns every line carrying a construct a reader cannot copy back out. A
// blockquote renders as a bar down the left of every line and wraps the text in markers. A table
// reads as a grid and reuses as nothing. A dash used as punctuation is neither, and is house style.
//
// The sweep is over the whole file rather than over the lines that changed, because the next one
// would go in somewhere else.
func unreusableMarkdownIn(text string) []string {
	var found []string
	fenced := false
	for n, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		switch {
		case strings.HasPrefix(line, ">"):
			found = append(found, fmt.Sprintf("README.md:%d is a blockquote: %q", n+1, line))
		case strings.HasPrefix(strings.TrimSpace(line), "|"):
			found = append(found, fmt.Sprintf("README.md:%d is a table row: %q", n+1, line))
		case strings.ContainsAny(line, "—–"):
			found = append(found, fmt.Sprintf("README.md:%d uses a dash as punctuation: %q", n+1, line))
		}
	}
	return found
}
