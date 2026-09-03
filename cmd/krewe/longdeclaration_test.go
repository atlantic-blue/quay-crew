package main

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"google.golang.org/protobuf/encoding/prototext"
)

// A declaration at whatever length the person writing it needs.
//
// A title of 201 bytes is refused today, and the person loses the words with the job. The same
// happens to the one sentence, to a label key and to a label value. Nothing about a job of 201 bytes
// cannot run: the number is a guide to what a reader takes in, and a guide that refuses is a cap.
//
// So the declaration is made, the identifier is printed, and one line for each long field says which
// field is long, how many bytes it is and what the guide is. The lines come off the control plane, so
// a surface that is not this tool reads the same words.
//
// These go through the tool, because the store never refused any of this. What refuses it stands
// between the two, and a case written against the store would pass today while a person still could
// not declare the job.

// The lengths this file declares with. They are different from each other on purpose: two fields
// measured at the same number would let one warning line stand in for another, and the test would
// pass on a system that warned about the title twice and about the sentence never.
const (
	aLongTitle       = job.TitleLimit + 11
	aLongSentence    = job.ProductLimit + 31
	aLongLabelKey    = job.LabelLimit + 7
	aLongLabelValue  = job.LabelLimit + 17
	aShortLabelValue = "house"
)

// ofExactly is prose of exactly this many bytes, in the words a person declaring this job would
// write. Exact, because the number is what a warning has to carry, and words rather than one letter
// repeated, because the same text is read back word for word further down.
func ofExactly(size int, words string) string {
	var built strings.Builder
	for built.Len() < size {
		built.WriteString(words)
		built.WriteString(" ")
	}
	text := built.String()[:size]
	if strings.HasSuffix(text, " ") {
		text = text[:size-1] + "."
	}
	return text
}

// aKeyOfExactly is a label key of exactly this many bytes. A key carries no spaces, so it is one
// hyphenated word rather than prose.
func aKeyOfExactly(size int) string {
	var built strings.Builder
	for built.Len() < size {
		built.WriteString("the-team-that-owns-the-electricity-bill-")
	}
	key := built.String()[:size]
	if strings.HasSuffix(key, "-") {
		key = key[:size-1] + "s"
	}
	return key
}

// theseWords are the four long fields one declaration carries, so every test in this file measures
// the same text and a reader can hold one set of numbers in their head.
func theseWords() (title, sentence, labelKey, labelValue string) {
	return ofExactly(aLongTitle, "read every electricity bill this house has had since the meter was changed"),
		ofExactly(aLongSentence, "a person pastes the account number and gets back what each month cost, "+
			"in the order the bills arrived"),
		aKeyOfExactly(aLongLabelKey),
		ofExactly(aLongLabelValue, "the whole street, every flat, and the meter cupboard on the ground floor")
}

// declaring runs one declaration and hands back what it said and what it refused, because whether it
// was refused at all is the first thing every test here asks.
func declaring(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(context.Background(), client, append([]string{"job", "create"}, args...), &out, "")
	return out.String(), err
}

// theProjectHere is the project the tool declares in, for the one case that reaches the control
// plane itself rather than through the tool.
func theProjectHere(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) string {
	t.Helper()
	listed, err := client.ListProjects(context.Background(), &quaycrewv1.ListProjectsRequest{})
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(listed.GetProjects()) != 1 {
		t.Fatalf("this system holds %d projects, and this test stands in one", len(listed.GetProjects()))
	}
	return listed.GetProjects()[0].GetId()
}

// theIdentifierIn is the job the declaration printed, which is the second word of the first line.
func theIdentifierIn(t *testing.T, said string) string {
	t.Helper()
	fields := strings.Fields(said)
	if len(fields) < 2 || fields[0] != "declared" {
		t.Fatalf("krewe job create said %q, want it to open by naming the job it declared", said)
	}
	return fields[1]
}

// theLineAbout is the one line of an output that measures a field: it names the field, says how many
// bytes it is, and says what the guide is. Two of anything is a failure as much as none is, because
// the requirement is one line for each long field.
func theLineAbout(t *testing.T, said, field string, size, guide int) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(said, "\n") {
		if strings.Contains(line, field) &&
			strings.Contains(line, strconv.Itoa(size)) &&
			strings.Contains(line, strconv.Itoa(guide)) {
			found = append(found, line)
		}
	}
	if len(found) == 0 {
		t.Fatalf("krewe job create says:\n%s\nwant one line naming %s, its %d bytes and the guide of %d",
			said, field, size, guide)
	}
	if len(found) > 1 {
		t.Fatalf("krewe job create says %d lines about %s and the requirement is one:\n%s",
			len(found), field, strings.Join(found, "\n"))
	}
	return found[0]
}

// The title. A job whose title runs to 211 bytes is a job, and the words are the person's.
func TestALongTitleIsDeclaredRatherThanRefused(t *testing.T) {
	client := aSystemToJobIn(t)
	title, _, _, _ := theseWords()

	said, err := declaring(t, client, "--title", title, "--brief", "open every bill and say what each month cost")

	if err != nil {
		t.Fatalf("a title of %d bytes was refused: %v", len(title), err)
	}
	shown := mustRun(t, client, "job", "show", theIdentifierIn(t, said))
	if !strings.Contains(shown, title) {
		t.Fatalf("krewe job show says:\n%s\nwant the title back word for word, all %d bytes of it", shown, len(title))
	}
}

// And it says the title is long, in one line carrying the two numbers a person needs to decide
// whether to leave it or say it shorter next time.
func TestALongTitleIsWarnedAboutRatherThanRefused(t *testing.T) {
	client := aSystemToJobIn(t)
	title, _, _, _ := theseWords()

	said, err := declaring(t, client, "--title", title, "--brief", "open every bill and say what each month cost")

	if err != nil {
		t.Fatalf("a title of %d bytes was refused: %v", len(title), err)
	}
	theLineAbout(t, said, "title", len(title), job.TitleLimit)
}

// The one sentence. It is the line the whole job is read against, and a person who needs 231 bytes to
// say what somebody gets back keeps all of them.
func TestALongSentenceIsDeclaredRatherThanRefused(t *testing.T) {
	client := aSystemToJobIn(t)
	_, sentence, _, _ := theseWords()

	said, err := declaring(t, client, "--title", "read the electricity bills",
		"--brief", "open every bill and say what each month cost", "--product", sentence)

	if err != nil {
		t.Fatalf("a sentence of %d bytes was refused: %v", len(sentence), err)
	}
	shown := mustRun(t, client, "job", "show", theIdentifierIn(t, said))
	if !strings.Contains(shown, sentence) {
		t.Fatalf("krewe job show says:\n%s\nwant the sentence back word for word, all %d bytes of it",
			shown, len(sentence))
	}
}

func TestALongSentenceIsWarnedAboutRatherThanRefused(t *testing.T) {
	client := aSystemToJobIn(t)
	_, sentence, _, _ := theseWords()

	said, err := declaring(t, client, "--title", "read the electricity bills",
		"--brief", "open every bill and say what each month cost", "--product", sentence)

	if err != nil {
		t.Fatalf("a sentence of %d bytes was refused: %v", len(sentence), err)
	}
	theLineAbout(t, said, "sentence", len(sentence), job.ProductLimit)
}

// A label key of 70 characters. The job is created and the label is on it, key and value both.
func TestALongLabelKeyIsDeclaredRatherThanRefused(t *testing.T) {
	client := aSystemToJobIn(t)
	_, _, labelKey, _ := theseWords()

	said, err := declaring(t, client, "--title", "read the electricity bills",
		"--brief", "open every bill and say what each month cost", "--label", labelKey+"="+aShortLabelValue)

	if err != nil {
		t.Fatalf("a label key of %d characters was refused: %v", len(labelKey), err)
	}
	shown := mustRun(t, client, "job", "show", theIdentifierIn(t, said))
	if !strings.Contains(shown, "label "+labelKey+"="+aShortLabelValue) {
		t.Fatalf("krewe job show says:\n%s\nwant the label back with its key whole, all %d characters of it",
			shown, len(labelKey))
	}
}

func TestALongLabelKeyIsWarnedAboutRatherThanRefused(t *testing.T) {
	client := aSystemToJobIn(t)
	_, _, labelKey, _ := theseWords()

	said, err := declaring(t, client, "--title", "read the electricity bills",
		"--brief", "open every bill and say what each month cost", "--label", labelKey+"="+aShortLabelValue)

	if err != nil {
		t.Fatalf("a label key of %d characters was refused: %v", len(labelKey), err)
	}
	theLineAbout(t, said, labelKey, len(labelKey), job.LabelLimit)
}

// A label value of 80 characters, under a key a person can read.
func TestALongLabelValueIsDeclaredRatherThanRefused(t *testing.T) {
	client := aSystemToJobIn(t)
	_, _, _, labelValue := theseWords()

	said, err := declaring(t, client, "--title", "read the electricity bills",
		"--brief", "open every bill and say what each month cost", "--label", "owner="+labelValue)

	if err != nil {
		t.Fatalf("a label value of %d characters was refused: %v", len(labelValue), err)
	}
	shown := mustRun(t, client, "job", "show", theIdentifierIn(t, said))
	if !strings.Contains(shown, "label owner="+labelValue) {
		t.Fatalf("krewe job show says:\n%s\nwant the value back word for word, all %d characters of it",
			shown, len(labelValue))
	}
}

func TestALongLabelValueIsWarnedAboutRatherThanRefused(t *testing.T) {
	client := aSystemToJobIn(t)
	_, _, _, labelValue := theseWords()

	said, err := declaring(t, client, "--title", "read the electricity bills",
		"--brief", "open every bill and say what each month cost", "--label", "owner="+labelValue)

	if err != nil {
		t.Fatalf("a label value of %d characters was refused: %v", len(labelValue), err)
	}
	theLineAbout(t, said, "owner", len(labelValue), job.LabelLimit)
}

// All four at once, which is the requirement in one reading: the job is created, the identifier is
// printed, and each long field gets its own line.
func TestFourLongFieldsAreDeclaredAndEachGetsItsOwnWarningLine(t *testing.T) {
	client := aSystemToJobIn(t)
	title, sentence, labelKey, labelValue := theseWords()

	said, err := declaring(t, client, "--title", title,
		"--brief", "open every bill and say what each month cost", "--product", sentence,
		"--label", labelKey+"="+aShortLabelValue, "--label", "owner="+labelValue)

	if err != nil {
		t.Fatalf("a declaration with four long fields was refused: %v", err)
	}
	id := theIdentifierIn(t, said)
	lines := []string{
		theLineAbout(t, said, "title", len(title), job.TitleLimit),
		theLineAbout(t, said, "sentence", len(sentence), job.ProductLimit),
		theLineAbout(t, said, labelKey, len(labelKey), job.LabelLimit),
		theLineAbout(t, said, "owner", len(labelValue), job.LabelLimit),
	}
	seen := map[string]bool{}
	for _, line := range lines {
		if seen[line] {
			t.Fatalf("two fields are measured by one line, and the requirement is a line each:\n%s", line)
		}
		seen[line] = true
	}
	if shown := mustRun(t, client, "job", "show", id); !strings.Contains(shown, job.PhasePending) {
		t.Fatalf("krewe job show %s says:\n%s\nwant a job that was created", id, shown)
	}
}

// The job the identifier names holds all four fields whole, so the warning is a note beside the work
// rather than a note about work that was cut.
func TestTheDeclaredJobKeepsEveryLongFieldWordForWord(t *testing.T) {
	client := aSystemToJobIn(t)
	title, sentence, labelKey, labelValue := theseWords()

	said, err := declaring(t, client, "--title", title,
		"--brief", "open every bill and say what each month cost", "--product", sentence,
		"--label", labelKey+"="+aShortLabelValue, "--label", "owner="+labelValue)

	if err != nil {
		t.Fatalf("a declaration with four long fields was refused: %v", err)
	}
	shown := mustRun(t, client, "job", "show", theIdentifierIn(t, said))
	for _, want := range []string{title, sentence,
		"label " + labelKey + "=" + aShortLabelValue, "label owner=" + labelValue} {
		if !strings.Contains(shown, want) {
			t.Errorf("krewe job show says:\n%s\nwant it to carry %q whole", shown, want)
		}
	}
}

// One line for each long field, and nothing about the fields inside their guides. A person who reads
// a measurement on every job stops reading it, and the line that matters goes with it.
func TestOnlyTheLongFieldIsWarnedAbout(t *testing.T) {
	client := aSystemToJobIn(t)
	title, _, _, _ := theseWords()
	sentence := "a person pastes the account number and gets back what each month cost"

	said, err := declaring(t, client, "--title", title,
		"--brief", "open every bill and say what each month cost", "--product", sentence,
		"--label", "owner="+aShortLabelValue)

	if err != nil {
		t.Fatalf("a title of %d bytes was refused: %v", len(title), err)
	}
	measured := theLineAbout(t, said, "title", len(title), job.TitleLimit)
	for _, line := range strings.Split(said, "\n") {
		if line == measured || strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(line, strconv.Itoa(job.LabelLimit)) ||
			strings.Contains(line, strconv.Itoa(len(sentence))) {
			t.Errorf("krewe job create measures a field that is inside its guide: %q", line)
		}
	}
}

// The lines come off the control plane rather than out of this tool, so the console, the gateway and
// anything else declaring a job reads the same words.
//
// The answer is searched rather than read off a named field, because naming one would be this test
// agreeing with an implementation nobody has written. What the requirement says is that the lines the
// tool prints are the lines the control plane returned.
func TestTheControlPlaneReturnsTheWarningLinesTheToolPrints(t *testing.T) {
	client := aSystemToJobIn(t)
	title, sentence, labelKey, labelValue := theseWords()

	said, err := declaring(t, client, "--title", title,
		"--brief", "open every bill and say what each month cost", "--product", sentence,
		"--label", labelKey+"="+aShortLabelValue, "--label", "owner="+labelValue)
	if err != nil {
		t.Fatalf("a declaration with four long fields was refused: %v", err)
	}
	lines := []string{
		theLineAbout(t, said, "title", len(title), job.TitleLimit),
		theLineAbout(t, said, "sentence", len(sentence), job.ProductLimit),
		theLineAbout(t, said, labelKey, len(labelKey), job.LabelLimit),
		theLineAbout(t, said, "owner", len(labelValue), job.LabelLimit),
	}

	answered, err := client.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: theProjectHere(t, client),
		Title:   title, Brief: "open every bill and say what each month cost", Product: sentence,
		Labels: map[string]string{labelKey: aShortLabelValue, "owner": labelValue},
	})
	if err != nil {
		t.Fatalf("the control plane refused a declaration with four long fields: %v", err)
	}
	answer := prototext.Format(answered)
	for _, line := range lines {
		if !strings.Contains(answer, strings.TrimSpace(line)) {
			t.Errorf("the control plane answers:\n%s\nwant it to carry the line the tool printed: %q",
				answer, strings.TrimSpace(line))
		}
	}
}
