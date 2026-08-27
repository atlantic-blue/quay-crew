package work_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/work"
)

// A declaration that would run.
func declared() work.Declaration {
	return work.Declaration{
		Workspace: "workspace-1", Project: "project-1",
		Title: "read the electricity bill", Brief: "open the bill and say when it is due",
	}
}

func TestADeclarationThatIsCompleteIsAccepted(t *testing.T) {
	if err := declared().Validate(); err != nil {
		t.Fatalf("a complete declaration was refused: %v", err)
	}
}

// The identifier is the crew's to mint. One a caller chooses is one a caller can collide.
func TestAnIdentifierTheCallerChoseIsRefused(t *testing.T) {
	d := declared()
	d.ID = "0123456789abcdef01234567"

	err := d.Validate()
	if err == nil {
		t.Fatal("a caller set the identifier and was allowed to")
	}
	if !strings.Contains(err.Error(), "assigns the identifier") {
		t.Fatalf("the refusal says %q, want it to say the crew assigns the identifier", err)
	}
}

// The parent is read from the credential. A parent in the request is refused rather than ignored,
// because the depth limit only bounds anything while the caller cannot lie about its parent.
func TestAParentInTheRequestIsRefused(t *testing.T) {
	d := declared()
	d.Parent = "0123456789abcdef01234567"

	err := d.Validate()
	if err == nil {
		t.Fatal("a caller set the parent and was allowed to")
	}
	if !strings.Contains(err.Error(), "credential") {
		t.Fatalf("the refusal says %q, want it to say the parent comes from the credential", err)
	}
}

func TestWorkWithNoTitleIsRefused(t *testing.T) {
	for _, title := range []string{"", "   ", "\n\t "} {
		d := declared()
		d.Title = title

		err := d.Validate()
		if err == nil {
			t.Errorf("a title of %q was accepted", title)
			continue
		}
		if !strings.Contains(err.Error(), "title") {
			t.Errorf("the refusal says %q, want it to say a title is needed", err)
		}
	}
}

// The ceiling is named because the caller cannot count the bytes of their own sentence.
func TestATitleOverTheCeilingIsRefusedAndSaysHowLongItIs(t *testing.T) {
	d := declared()
	d.Title = strings.Repeat("t", work.TitleLimit+1)

	err := d.Validate()
	if err == nil {
		t.Fatal("a title of 201 bytes was accepted")
	}
	if !strings.Contains(err.Error(), "201") || !strings.Contains(err.Error(), "200") {
		t.Fatalf("the refusal says %q, want it to say the length and the ceiling", err)
	}
}

// A title exactly at the ceiling is fine. An off by one here refuses work nobody should be refused.
func TestATitleAtTheCeilingIsAccepted(t *testing.T) {
	d := declared()
	d.Title = strings.Repeat("t", work.TitleLimit)

	if err := d.Validate(); err != nil {
		t.Fatalf("a title of exactly %d bytes was refused: %v", work.TitleLimit, err)
	}
}

func TestWorkWithNoBriefIsRefused(t *testing.T) {
	d := declared()
	d.Brief = "  "

	err := d.Validate()
	if err == nil {
		t.Fatal("work with no brief was accepted")
	}
	if !strings.Contains(err.Error(), "brief") {
		t.Fatalf("the refusal says %q, want it to say a brief is needed", err)
	}
}

func TestABriefOverTheCeilingIsRefused(t *testing.T) {
	d := declared()
	d.Brief = strings.Repeat("b", work.BriefLimit+1)

	err := d.Validate()
	if err == nil {
		t.Fatal("a brief of 16385 bytes was accepted")
	}
	if !strings.Contains(err.Error(), "16385") || !strings.Contains(err.Error(), "16384") {
		t.Fatalf("the refusal says %q, want it to say the length and the ceiling", err)
	}
}

func TestABriefAtTheCeilingIsAccepted(t *testing.T) {
	d := declared()
	d.Brief = strings.Repeat("b", work.BriefLimit)

	if err := d.Validate(); err != nil {
		t.Fatalf("a brief of exactly %d bytes was refused: %v", work.BriefLimit, err)
	}
}

// A word that is not a mode has to come back with the words that are, because they are not
// guessable from the protocol's own spellings.
func TestAModeThatIsNotAModeIsRefusedAndTheModesAreListed(t *testing.T) {
	d := declared()
	d.Mode = "yolo"

	err := d.Validate()
	if err == nil {
		t.Fatal("a mode that is not a mode was accepted")
	}
	for _, mode := range []string{"plan", "edits", "dangerous"} {
		if !strings.Contains(err.Error(), mode) {
			t.Errorf("the refusal says %q, want it to offer %q", err, mode)
		}
	}
}

func TestEveryModeTheCrewKnowsIsAccepted(t *testing.T) {
	for _, mode := range []string{"plan", "edits", "dangerous"} {
		d := declared()
		d.Mode = mode
		if err := d.Validate(); err != nil {
			t.Errorf("mode %q was refused: %v", mode, err)
		}
	}
}

// The path is read inside the session's working directory, so a path that points anywhere else is
// asking about a file the work never touched.
func TestAnExpectedFileOutsideTheWorkingDirectoryIsRefused(t *testing.T) {
	for _, path := range []string{"/etc/passwd", "../secrets.txt", "notes/../../secrets.txt"} {
		d := declared()
		d.ExpectFile = path

		err := d.Validate()
		if err == nil {
			t.Errorf("the path %q was accepted", path)
			continue
		}
		if !strings.Contains(err.Error(), path) {
			t.Errorf("the refusal says %q, want it to name the path", err)
		}
	}
}

func TestAnExpectedFileInsideTheWorkingDirectoryIsAccepted(t *testing.T) {
	for _, path := range []string{"package.json", "notes/bill.md", "./bill.md"} {
		d := declared()
		d.ExpectFile = path
		if err := d.Validate(); err != nil {
			t.Errorf("the path %q was refused: %v", path, err)
		}
	}
}

func TestABudgetBelowZeroIsRefused(t *testing.T) {
	d := declared()
	d.BudgetTokens = -1

	err := d.Validate()
	if err == nil {
		t.Fatal("a budget of -1 tokens was accepted")
	}
	if !strings.Contains(err.Error(), "budget") {
		t.Fatalf("the refusal says %q, want it to say what is wrong with the budget", err)
	}
}

func TestABudgetOfZeroIsAccepted(t *testing.T) {
	d := declared()
	d.BudgetTokens = 0

	if err := d.Validate(); err != nil {
		t.Fatalf("a budget of zero was refused, and zero is how a caller says it draws from its parent: %v", err)
	}
}

func TestMoreLabelsThanTheCeilingAreRefused(t *testing.T) {
	d := declared()
	d.Labels = map[string]string{}
	for i := 0; i <= work.LabelCount; i++ {
		d.Labels[strings.Repeat("k", i+1)] = "value"
	}

	err := d.Validate()
	if err == nil {
		t.Fatal("17 labels were accepted")
	}
	if !strings.Contains(err.Error(), "17") || !strings.Contains(err.Error(), "16") {
		t.Fatalf("the refusal says %q, want it to say how many there are and the ceiling", err)
	}
}

func TestALabelLongerThanTheCeilingIsRefused(t *testing.T) {
	long := strings.Repeat("v", work.LabelLimit+1)
	for _, labels := range []map[string]string{
		{"owner": long},
		{long: "julian"},
	} {
		d := declared()
		d.Labels = labels

		err := d.Validate()
		if err == nil {
			t.Errorf("a label of %d characters was accepted", work.LabelLimit+1)
			continue
		}
		if !strings.Contains(err.Error(), "64") || !strings.Contains(err.Error(), "63") {
			t.Errorf("the refusal says %q, want it to say the length and the ceiling", err)
		}
	}
}

func TestALabelAtTheCeilingIsAccepted(t *testing.T) {
	d := declared()
	d.Labels = map[string]string{"owner": strings.Repeat("v", work.LabelLimit)}

	if err := d.Validate(); err != nil {
		t.Fatalf("a label of exactly %d characters was refused: %v", work.LabelLimit, err)
	}
}

// A piece of work waiting on itself would never start, and nothing else would tell anybody why.
func TestWorkThatWaitsForItselfIsRefused(t *testing.T) {
	from, to, found := work.Cycle("a", []string{"a"}, func(string) []string { return nil })

	if !found {
		t.Fatal("work waiting on itself was not found to be a cycle")
	}
	if from != "a" || to != "a" {
		t.Fatalf("the cycle is named %s and %s, want a and a", from, to)
	}
}

// The check reaches through the chain, because a cycle two steps away is still a cycle.
func TestACycleThroughTheChainIsFoundAndBothEndsAreNamed(t *testing.T) {
	dependencies := map[string][]string{
		"b": {"c"},
		"c": {"a"},
	}
	from, to, found := work.Cycle("a", []string{"b"}, func(id string) []string { return dependencies[id] })

	if !found {
		t.Fatal("a cycle through the chain was not found")
	}
	if from != "c" || to != "a" {
		t.Fatalf("the cycle is named %s and %s, want the two identifiers that close it, c and a", from, to)
	}
}

func TestAChainThatEndsIsNotACycle(t *testing.T) {
	dependencies := map[string][]string{
		"b": {"c"},
		"c": {},
	}
	if _, _, found := work.Cycle("a", []string{"b"}, func(id string) []string { return dependencies[id] }); found {
		t.Fatal("a chain that ends was called a cycle")
	}
}

// A diamond is not a cycle, and a check that does not remember where it has been calls it one.
func TestWorkReachedTwiceByDifferentPathsIsNotACycle(t *testing.T) {
	dependencies := map[string][]string{
		"b": {"d"},
		"c": {"d"},
		"d": {},
	}
	if _, _, found := work.Cycle("a", []string{"b", "c"}, func(id string) []string { return dependencies[id] }); found {
		t.Fatal("work reached by two paths was called a cycle")
	}
}

func TestTheTerminalPhasesAreTheThreeThatEnd(t *testing.T) {
	for _, phase := range []string{work.PhaseDone, work.PhaseFailed, work.PhaseStopped} {
		if !work.Terminal(phase) {
			t.Errorf("%q is not terminal, and nothing moves work out of it", phase)
		}
	}
	for _, phase := range []string{work.PhasePending, work.PhaseWaiting, work.PhaseRunning, work.PhaseAsking} {
		if work.Terminal(phase) {
			t.Errorf("%q is terminal, and work in it still has somewhere to go", phase)
		}
	}
}

func TestAPhaseThatIsNotAPhaseIsNotKnown(t *testing.T) {
	for _, phase := range []string{"", "idle", "finished", "PENDING"} {
		if work.KnownPhase(phase) {
			t.Errorf("%q was taken for a phase", phase)
		}
	}
	for _, phase := range []string{work.PhasePending, work.PhaseWaiting, work.PhaseRunning,
		work.PhaseAsking, work.PhaseDone, work.PhaseFailed, work.PhaseStopped} {
		if !work.KnownPhase(phase) {
			t.Errorf("%q is a phase and was not known", phase)
		}
	}
}

// The leading and trailing space comes off, so a title that is only space is refused rather than
// stored, and a listing does not print a ragged column.
func TestTheSpaceAroundATitleAndABriefComesOff(t *testing.T) {
	d := declared()
	d.Title, d.Brief = "  read the bill  ", "\n open it \n"

	tidied := d.Tidied()

	if tidied.Title != "read the bill" {
		t.Fatalf("the title is %q, want the space taken off", tidied.Title)
	}
	if tidied.Brief != "open it" {
		t.Fatalf("the brief is %q, want the space taken off", tidied.Brief)
	}
}

// What a piece of work hands is held to the words the crew hands out, and the refusal offers those
// words back. A word nobody assembles is a boundary that quietly means nothing.
func TestWorkHandedSomethingTheCrewDoesNotHandOutIsRefused(t *testing.T) {
	d := declared()
	d.Hands = []string{"the codebase"}

	err := d.Validate()

	if err == nil {
		t.Fatal("work handed material the crew does not hand out was accepted")
	}
	for _, want := range []string{"the codebase", "work", "context", "skills"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal says %q, want it to name %q", err, want)
		}
	}
}

func TestWorkHandedMaterialTheCrewHandsOutIsAccepted(t *testing.T) {
	d := declared()
	d.Hands = []string{"context", "skills", "work"}

	if err := d.Validate(); err != nil {
		t.Fatalf("work handed material the crew does hand out was refused: %v", err)
	}
}

// One order and no repeats, so what a piece of work hands does not depend on the order somebody
// typed it in, and two declarations that say the same thing are the same row.
func TestWhatIsHandedIsSortedAndDeduplicated(t *testing.T) {
	d := declared()
	d.Hands = []string{" skills ", "context", "skills", ""}

	tidy := d.Tidied()

	if len(tidy.Hands) != 2 || tidy.Hands[0] != "context" || tidy.Hands[1] != "skills" {
		t.Fatalf("the work hands %v, want context and skills once each", tidy.Hands)
	}
}

func TestWorkThatHandsNothingHandsNothing(t *testing.T) {
	if tidy := declared().Tidied(); tidy.Hands != nil {
		t.Fatalf("work that handed nothing hands %v", tidy.Hands)
	}
}

// receives is a role, as much of one as the rule needs.
type receives []string

func (r receives) Gets(material string) bool {
	for _, held := range r {
		if held == material {
			return true
		}
	}
	return false
}

func TestTheFirstMaterialARoleDoesNotReceiveIsNamed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		handed []string
		role   work.Receiver
		want   string
	}{
		{"the role receives everything handed", []string{"work", "context"}, receives{"work", "context"}, ""},
		{"the role receives none of it", []string{"context"}, receives{"work"}, "context"},
		{"the role receives some of it", []string{"context", "skills"}, receives{"work", "context"}, "skills"},
		{"nothing was handed", nil, receives{"work"}, ""},
		{"a role that receives nothing at all", []string{"work"}, receives{}, "work"},
		{"no role to hold it against", []string{"context"}, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := work.Unreceived(tc.handed, tc.role); got != tc.want {
				t.Fatalf("the material the role does not receive is %q, want %q", got, tc.want)
			}
		})
	}
}

// A refusal a caller cannot act on is a refusal that sends them looking, so it names the role, the
// material and both ways out.
func TestTheRefusalNamesTheRoleTheMaterialAndWhatToChange(t *testing.T) {
	said := work.RefusedMaterial("test-writer", "context")

	for _, want := range []string{"test-writer", "context", "import it again", "declare the work without"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the refusal says %q, want it to name %q", said, want)
		}
	}
}
