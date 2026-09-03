package features_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The README is the front door, and a front door that promises what the system cannot do is worse than
// no front door: a reader takes it at its word, types the command, and concludes the product is
// broken. Its list of what works predated a day of merged work once, so these hold it to the three
// things it can be wrong about. What commands exist, what make targets exist, and what documents
// exist are all questions with an answer somewhere else in this repository.
//
// It is held to a shape as well as to its claims. It ran to 253 lines of feature list, principles,
// stack, roadmap and prior art, all of it already written down in features/, and none of it read. So the sections are an exact list rather than a minimum, and there is a line limit
// underneath that.
//
// What none of this checks is whether a sentence is true. A bullet claiming a capability the system
// does not have, in words that name no command, passes every case here. The scenarios in this
// directory are what say whether a capability is real; this says the front door points at them.

// theFrontDoor is the file under test, a directory up from here.
const theFrontDoor = "../README.md"

// theMakefile is the other file the front door makes claims about.
const theFrontDoorsMakefile = "../Makefile"

// theFrontDoorsSections is every section the front door carries, in order. It answers what the system
// is before the first one, then those, and stops.
//
// The list is exact rather than a minimum, because a limit on length alone is satisfied by a shorter
// version of the same sprawl: every section that made the old one unreadable was added one at a
// time, each of them defensible on its own.
//
// "The words" is first. The confusion it answers cost real time twice, and a reader hits it before
// the first command rather than after, so it sits above the quick start. It is a list somebody scans
// for one entry, not prose read top to bottom, which is why it can be long and the sections below it
// still cannot.
var theFrontDoorsSections = []string{"The words", "Quick start", "Where to read next", "License"}

// theFewestWordsThatSayWhatItIs. The lead is the answer to "what is this", and it sits before any
// heading, so an empty lead is a front door that opens on an install command.
const theFewestWordsThatSayWhatItIs = 40

// theLongestFrontDoorWorthReading, in lines. The one this replaced was 253, and the reason it was
// rewritten is that nobody reached the bottom of it.
//
// It moved from 80 to 120 once, for the resources: one short paragraph each, and the reason for a
// ceiling at all is unchanged. What that ceiling buys is the room for the vocabulary and nothing
// else, so the next section that wants space has to take it from a section already here.
const theLongestFrontDoorWorthReading = 120

// codeIn returns everything the front door marks as something to type: every inline code span, and
// every line inside a fenced block. Prose is left out on purpose, because "krewe is on your path" is
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
			// A trailing shell comment is prose, so "make tool # over whatever krewe your shell runs"
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

// kreweCommands is every command the real tool lists, read from the build in this checkout rather
// than from a copy of the list. A test that held the front door to a remembered list would go stale
// in exactly the way the front door did.
func kreweCommands() (map[string]bool, error) {
	built := filepath.Join(os.TempDir(), "krewe-frontdoor-test")
	build := exec.Command("go", "build", "-o", built, "../cmd/krewe")
	if out, err := build.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("building the tool: %w\n%s", err, out)
	}
	defer func() { _ = os.Remove(built) }()

	out, err := exec.Command(built, "help").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("krewe help: %w\n%s", err, out)
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
		return nil, fmt.Errorf("krewe help listed no commands at all, so this would pass on anything:\n%s", out)
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

// TestTheFrontDoorHoldsThreeThingsAndNoOthers.
//
// What the system is, how to start it, and where to read next. The forty item feature list, the
// principles, the stack, the roadmap and the prior art all say something already said in
// features/, and every one of them made the file longer than the reader it was written for.
func TestTheFrontDoorHoldsThreeThingsAndNoOthers(t *testing.T) {
	for _, wrong := range theShapeOf(frontDoor(t)) {
		t.Error(wrong)
	}
}

// TestTheFrontDoorIsShortEnoughToRead. The section list above stops a new section going in; this
// stops one that is already there growing until nobody reaches the bottom.
func TestTheFrontDoorIsShortEnoughToRead(t *testing.T) {
	if held := linesIn(frontDoor(t)); held > theLongestFrontDoorWorthReading {
		t.Errorf("the front door is %d lines, and nobody reads more than %d of them",
			held, theLongestFrontDoorWorthReading)
	}
}

// TestEveryCommandTheFrontDoorNamesExists, checked against the tool this checkout builds.
//
// This is the one that catches the front door going stale, from either end: a command named before
// it is built, and a command renamed underneath a README nobody reread.
func TestEveryCommandTheFrontDoorNamesExists(t *testing.T) {
	commands, err := kreweCommands()
	if err != nil {
		t.Fatalf("asking the tool what it can do: %v", err)
	}

	named := namedAfter("krewe", codeIn(frontDoor(t)))
	if len(named) == 0 {
		t.Fatal("the front door names no krewe command at all, so this proved nothing")
	}
	t.Logf("the front door names %d commands: %s", len(named), strings.Join(named, " "))

	for _, one := range named {
		if !commands[one] {
			t.Errorf("the front door tells a reader to run `krewe %s`, and the tool has no such command", one)
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
	for _, retired := range []string{"make config", "make sandbox-image", "make up"} {
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

// headingsIn returns the front door's sections in the order it holds them. A heading inside a fenced
// block is something to type rather than a section.
func headingsIn(text string) []string {
	var headings []string
	fenced := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if !fenced && strings.HasPrefix(line, "## ") {
			headings = append(headings, strings.TrimSpace(strings.TrimPrefix(line, "## ")))
		}
	}
	return headings
}

// leadIn returns the prose between the title and the first section, which is where the front door
// answers what the system is.
func leadIn(text string) string {
	var lead []string
	for n, line := range strings.Split(text, "\n") {
		if n == 0 && strings.HasPrefix(line, "# ") {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
		lead = append(lead, line)
	}
	return strings.TrimSpace(strings.Join(lead, "\n"))
}

// linesIn counts the front door as a reader scrolls it, so a file that ends in a newline is not one
// line longer than it looks.
func linesIn(text string) int {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}

// theShapeOf returns everything wrong with the front door's three parts, or nothing. Each of these
// fails on an empty file, which is the point: a rule that only forbids sections is satisfied by a
// file with nothing in it.
func theShapeOf(text string) []string {
	var wrong []string

	if !strings.HasPrefix(text, "# ") {
		wrong = append(wrong, "the front door opens with no title, so a reader does not know what they opened")
	}
	if words := len(strings.Fields(leadIn(text))); words < theFewestWordsThatSayWhatItIs {
		wrong = append(wrong, fmt.Sprintf("the front door says what the system is in %d words, and %d is "+
			"the fewest that says anything at all", words, theFewestWordsThatSayWhatItIs))
	}
	if held := headingsIn(text); !slices.Equal(held, theFrontDoorsSections) {
		wrong = append(wrong, fmt.Sprintf("the front door holds the sections %v, and it holds %v and "+
			"nothing else: what the system is, the words for what it holds, how to start it, and where to read next. Everything a "+
			"new section would say belongs in features/", held, theFrontDoorsSections))
	}
	return wrong
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

// theResourcesTheSystemKeeps is every resource the front door has to define, in the order it defines
// them. Each one is a heading in bold at the start of its paragraph.
//
// It is a list here rather than something read out of the code, and that is the weaker choice made
// deliberately: nothing in the system enumerates its own resources. The console's views and the
// store's interface are both a different shape from this, so anything derived would be a different
// list dressed up as this one. What this holds is that the front door and this list agree, and a
// ninth resource lands here in the same change that adds it.
//
// Exact rather than a minimum, for the same reason the section list is: a front door that defines
// the eight and then eight more is the sprawl this file exists to stop.
var theResourcesTheSystemKeeps = []string{
	"Workspaces", "Projects", "Sessions", "Tasks",
	"Skills", "Hooks", "Secrets", "Context",
}

// theVocabulary is the section of the front door that defines them.
const theVocabulary = "## The words"

// theWordsFor returns everything wrong with the front door's vocabulary, or nothing.
//
// The confusion this answers cost real time twice, which is why it is checked rather than left to
// whoever edits the README next. A reader who cannot tell a job from a task cannot use either.
func theWordsFor(frontDoor string) []string {
	said, err := sectionOf(frontDoor, theVocabulary)
	if err != nil {
		return []string{fmt.Sprintf("%v, so the front door defines none of the words it uses", err)}
	}

	// A definition opens its own paragraph in bold. Anywhere in the section is too weak: the word
	// "Sessions" appears in the paragraph about projects, and a substring check would take that for
	// a definition of a session.
	defined := map[string]bool{}
	var inOrder []string
	for _, block := range strings.Split(strings.TrimSpace(said), "\n\n") {
		found := regexp.MustCompile(`^\*\*([A-Za-z]+)\.\*\*`).FindStringSubmatch(strings.TrimSpace(block))
		if found == nil {
			continue
		}
		defined[found[1]] = true
		inOrder = append(inOrder, found[1])
	}

	var wrong []string
	for _, resource := range theResourcesTheSystemKeeps {
		if !defined[resource] {
			wrong = append(wrong, fmt.Sprintf("no paragraph opens by defining %s, so a reader meets the "+
				"word for the first time in a command", resource))
		}
	}
	if len(wrong) == 0 && !slices.Equal(inOrder, theResourcesTheSystemKeeps) {
		wrong = append(wrong, fmt.Sprintf("the front door defines %v, and it defines %v: outer first, "+
			"then what runs, then what shapes it", inOrder, theResourcesTheSystemKeeps))
	}

	// The borrowed word, with the place it is not borrowed. A session that a reader takes for a Pod is
	// a session they expect to be replaceable.
	if !strings.Contains(said, "not a Pod") {
		wrong = append(wrong, "it never says a session is not a Pod, so the obvious question about the "+
			"vocabulary is left for the reader to ask somewhere else")
	}
	return wrong
}

// TestTheFrontDoorDefinesEveryResourceTheSystemKeeps.
//
// The vocabulary cost real time twice before it was written down. A reader who does not know what a
// session is cannot tell it from the task they send to it.
func TestTheFrontDoorDefinesEveryResourceTheSystemKeeps(t *testing.T) {
	for _, wrong := range theWordsFor(frontDoor(t)) {
		t.Error(wrong)
	}
}
