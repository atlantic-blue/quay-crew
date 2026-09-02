package job

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The failing tests become an implementation, written by workers that may read every test and change
// none of them, several at once.
//
// The suite is red when this stage opens, and every requirement on it is a vertical a person accepted.
// So the work divides the way the list does: one worker for each vertical, all at once, each turning
// its own tests green and nothing else. The stage is one thing and the work inside it is many.
//
// The boundary is the whole of this stage. A worker may read the test code as much as it needs to, and
// it may not change a test. The looseness is deliberate and the discipline it comes from is stricter:
// there the implementer never sees the test source at all. A build that cannot read the test cannot
// tell a failing assertion from a broken one, so it guesses, and a guess against a test it cannot read
// is worse than the risk of reading one.
//
// The refusal is a gate rather than a sentence in the brief. The brief says it too, because a session
// that knows the rule does not spend a tool call finding it, but what makes it a boundary is the hook:
// the system sets KREWE_BUILDING on the worker's task and the test gate refuses the write. A rule only
// stated in a prompt is a rule the model weighs against everything else it was told.
//
// It ends by holding the job for a person rather than by calling it done. Four things finish a build:
// acceptance, behaviour tests, unit tests and integration tests. The last three are the machine's and
// they are in the report this stage reads. Acceptance is a person looking at what was built and
// agreeing the value arrived, and nothing here can do that on their behalf.

// TheBuildAsk is the phrase every ask for the build of one vertical carries, and the phrase a double
// answers a report to. It is a constant for the reason the test ask is one: the ask and everything
// that recognises it must not drift apart.
const TheBuildAsk = "make the failing tests for this vertical pass, and change no test"

// BuildMarker is the line every report carries, and it is how anything holding a reply can tell a
// build report from a plan, a reading, a list or a test report.
const BuildMarker = "Passing 1:"

// BuildAttempts is how many workers one vertical may have: the first, and one more after a person has
// said to carry on. The bound the stages around this one already keep, for the same reason. Every ask
// is a task somebody pays for, and a vertical whose worker fails twice is a vertical a person has to
// look at rather than a run to repeat.
const BuildAttempts = 2

// ClaimOnBuild is the piece of work one build worker holds, and it is written by the system rather
// than passed by a caller.
//
// The claim already refuses a second job taking work a first job holds, so two workers cannot build
// one vertical. It is derived rather than declared because a mechanism a caller has to remember is a
// mechanism that is forgotten: the fan out happens inside the system and nobody types it.
//
// It says build where the test stage's claim says requirement, so the worker that wrote a vertical's
// tests and the worker that builds it hold different pieces of work. One claim for both would mean the
// build worker was refused for work the test worker did, and the test stage would then read a build
// report as its own.
func ClaimOnBuild(job string, wanted Requirement) string {
	return TidyClaim(fmt.Sprintf("%s build %d", job, wanted.Number))
}

// BuildingVertical is the title of the worker that builds one vertical. It says which vertical it
// holds, because that title is what a refused second claim names.
func BuildingVertical(wanted Requirement) string {
	title := fmt.Sprintf("build vertical %d: %s", wanted.Number, wanted.Text)
	if len(title) > TitleLimit {
		title = strings.TrimSpace(title[:TitleLimit])
	}
	return title
}

// BuildTheVertical is the brief one build worker is given: its vertical, the tests that fail for it
// now, and the boundary it works under.
//
// It is given one vertical rather than the list, because a worker holding the whole list builds a
// little of each and the fan out buys nothing. It is given the names of the failing tests, because
// those names are what the stage reads its answer against: a worker that does not know which tests it
// owns cannot report on them.
func BuildTheVertical(one *Job, wanted Requirement, failing []string, opened Opened) string {
	said := []string{
		fmt.Sprintf("Vertical %d of the list a person accepted for this job. %s",
			wanted.Number, TheBuildAsk),
	}
	if one.Product != "" {
		said = append(said, ServesAPerson(one.Product))
	}
	said = append(said, fmt.Sprintf("Vertical %d: %s\nShown %d: %s",
		wanted.Number, wanted.Text, wanted.Number, wanted.Shown))
	if len(failing) > 0 {
		said = append(said, fmt.Sprintf("These tests fail now, and they are yours to turn green:\n%s",
			"- "+strings.Join(failing, "\n- ")))
	}
	// Where those tests are. A worker told to read tests it never fetched reads nothing, and from
	// inside the session a test that is absent and a test that says nothing look the same.
	if opened.Branch != "" {
		said = append(said, ContinueOnTheBranch(opened))
	}
	said = append(said, "Read the tests as much as you need to. You may not change one. A build that "+
		"changes the test makes the suite agree with the code, and the suite is the only thing holding "+
		"the requirement, so the system refuses the write rather than trusting this sentence. If you "+
		"believe a test is wrong, say so in your answer, name the file and the assertion, and say what "+
		"it should assert instead. A person decides that.")
	said = append(said, "Build this vertical only. Another worker is building each of the others at "+
		"the same time, and it holds that vertical. The whole suite is red until all of them land, so "+
		"judge yourself on your own tests rather than on the suite.")
	said = append(said, theShapeOfABuildReport(wanted))
	return strings.Join(said, "\n\n")
}

// theShapeOfABuildReport is what the system asks for, in the shape it reads back.
//
// It asks for the run rather than for a description of it, and it asks what changed. A green run on
// its own says nothing: a suite that finds nothing to execute reports success just the same, and a
// test that was already passing before anything was built passes after it too. What the files say is
// the difference between a build and a claim of one.
func theShapeOfABuildReport(wanted Requirement) string {
	return fmt.Sprintf("Run the suite the way the repository runs it, and answer in these lines as "+
		"well as your outcome:\n\n"+
		"Vertical: %d\n"+
		"Ran: how many tests the run executed, as a number\n"+
		"Red: how many tests still fail, as a number\n"+
		"Passing 1: the name of a test of yours that failed before and passes now\n"+
		"Passing 2: the name of the next one\n"+
		"Changed 1: a file you wrote to make them pass\n"+
		"Changed 2: the next one\n"+
		"%s"+
		"Taken: what was running, the command behind it, and what has to be up to get it again\n\n"+
		"A run that executed nothing is a failure of this task rather than a pass. A green run that "+
		"changed no file means the test was already passing, which builds nothing.\n\n%s",
		wanted.Number, theEvidenceLines(wanted.Evidence), ShowItWorking(wanted))
}

// buildLine is the shape a report is read back in: the vertical it was built for, what the run did,
// the tests that pass now and the files that made them pass.
//
// Read off the reply rather than reported, the way a list, a plan and a test report already are. What
// it finds is then what the worker meant to say, rather than a sentence that happened to hold the
// word.
var buildLine = regexp.MustCompile(
	`(?im)^[ \t]*(vertical|ran|red|passing|passes|changed|picture|recording|steps?|taken)[ \t]*(\d*)` +
		`[ \t]*[:.][ \t]*(.+?)[ \t]*$`)

// BuildReport is what one worker answers with: which vertical it built, what its run did, the tests
// that pass now, the files it changed to make them, and the picture of the vertical running.
type BuildReport struct {
	Vertical int
	Ran      int
	Red      int
	Passing  []string
	Changed  []string
	// Picture is the name of a picture or a recording of this vertical running, in the workspace's
	// shared folder, Steps are what a person runs or presses to see it themselves, and Taken is the
	// label saying where any of it came from and what it takes to get it again. They are what a person
	// is shown at the end of the stage, and none of them is any use without the label: evidence nobody
	// can reproduce is worth nothing, and a label with nothing under it is a paragraph.
	//
	// Which of the two arrived says which kind this is, and Kind holds it. A worker answers with the
	// kind its vertical asked for, and the stage refuses a report that answers with another.
	Picture string
	Steps   []string
	Kind    Kind
	Taken   string
}

// Evidence is what this report offers a person, in the shape the acceptance stage reads.
func (r BuildReport) Evidence() Evidence {
	kind := r.Kind
	if kind == "" {
		kind = KindPicture
		if len(r.Steps) > 0 {
			kind = KindSteps
		} else if AFileOfKind(KindRecording, r.Picture) {
			kind = KindRecording
		}
	}
	return Evidence{
		Vertical: r.Vertical, Kind: kind, File: r.Picture, Steps: r.Steps, Taken: r.Taken,
	}
}

// ReadBuildReport is the report a reply carries, and the refusal where it carries none.
//
// The refusals are the whole point of the stage. A run that executed nothing, a run that is still red,
// and a green run that changed nothing all read as success everywhere else in this system, and none of
// them says the vertical was built.
func ReadBuildReport(reply string) (BuildReport, error) {
	report := BuildReport{}
	ran, red, saidRan, saidRed := "", "", false, false
	for _, found := range buildLine.FindAllStringSubmatch(reply, -1) {
		text := TidySentence(found[3])
		if text == "" {
			continue
		}
		switch strings.ToLower(found[1]) {
		case "vertical":
			// Either shape it can arrive in: the number after the word, as the ask asks for it, or the
			// number the vertical itself is written under. The first readable one stands, the way the first
			// line under a number stands in a list.
			if report.Vertical != 0 {
				continue
			}
			if number, err := strconv.Atoi(found[2]); err == nil {
				report.Vertical = number
				continue
			}
			report.Vertical, _ = strconv.Atoi(strings.Fields(text)[0])
		case "ran":
			if !saidRan {
				ran, saidRan = strings.Fields(text)[0], true
			}
		case "red":
			if !saidRed {
				red, saidRed = strings.Fields(text)[0], true
			}
		case "passing", "passes":
			report.Passing = append(report.Passing, text)
		case "picture", "recording":
			// One file stands, and it is the first readable one. A worker that named several showed
			// several things, and the person is here to look at this vertical working rather than to pick
			// which of four files is the one that shows it.
			if report.Picture == "" {
				report.Picture, report.Kind = path.Base(text), KindPicture
				if strings.EqualFold(found[1], "recording") || AFileOfKind(KindRecording, report.Picture) {
					report.Kind = KindRecording
				}
			}
		case "step", "steps":
			// Every step stands, in the order they were written, because the steps are the evidence here
			// the way the file is the evidence above. The number on the line is the step rather than the
			// vertical: a worker is given one vertical and says so on its own line.
			report.Steps = append(report.Steps, text)
			report.Kind = KindSteps
		case "taken":
			if report.Taken == "" {
				report.Taken = text
			}
		default:
			report.Changed = append(report.Changed, text)
		}
	}
	if report.Vertical == 0 {
		return BuildReport{}, fmt.Errorf("this reply does not say which vertical it built: write a "+
			"line %q with the number of the vertical you were given", "Vertical: 2")
	}
	if !saidRan {
		return BuildReport{}, fmt.Errorf("this reply does not say how many tests the run executed: run "+
			"the suite and write a line %q. A run that finds nothing to execute reports success just the "+
			"same, so the count is what says the tests ran at all", "Ran: 14")
	}
	count, err := strconv.Atoi(ran)
	if err != nil {
		return BuildReport{}, fmt.Errorf("%q is not a number of tests: write %q with the count the run "+
			"printed", ran, "Ran: 14")
	}
	report.Ran = count
	if !saidRed {
		return BuildReport{}, fmt.Errorf("this reply does not say how many tests still fail: write a "+
			"line %q with the count the run printed", "Red: 0")
	}
	if report.Red, err = strconv.Atoi(red); err != nil {
		return BuildReport{}, fmt.Errorf("%q is not a number of failing tests: write %q with the count "+
			"the run printed", red, "Red: 0")
	}
	if err := report.green(); err != nil {
		return BuildReport{}, err
	}
	return report, nil
}

// green is what makes a report a report: the run happened, nothing fails, tests that failed before
// pass now, and something was written to make them.
func (r BuildReport) green() error {
	if r.Ran <= 0 {
		return fmt.Errorf("this run executed %d tests, so it proved nothing: a run that finds nothing "+
			"to execute reports success just the same. Check the tests are in a file the runner "+
			"collects, run the suite again, and answer with what it printed", r.Ran)
	}
	if r.Red > 0 {
		return fmt.Errorf("this run still has %d failing tests, so this vertical is not built: name "+
			"what fails and why you could not make it pass, or say which test you believe is wrong",
			r.Red)
	}
	if len(r.Passing) == 0 {
		return fmt.Errorf("this reply names no test that passes now: name each test that failed "+
			"before and passes now, on a %q line", "Passing 1:")
	}
	if len(r.Passing) > r.Ran {
		return fmt.Errorf("this reply names %d passing tests out of %d that ran: say how many tests "+
			"the whole run executed", len(r.Passing), r.Ran)
	}
	if len(r.Changed) == 0 {
		return fmt.Errorf("this reply names no file that changed, so nothing was built: a test that "+
			"passes without anything being written was already passing, and it holds no requirement. "+
			"Name each file you wrote on a %q line", "Changed 1:")
	}
	for _, where := range r.Changed {
		if ATest(where) {
			return fmt.Errorf("%q is a test, and a build may not change one: put the change in the code "+
				"the test is about. If the test itself is wrong, say so and name the assertion", where)
		}
	}
	// Last, because it is the check the machine cannot make for itself. Everything above is the run
	// reporting on the run, and a person cannot read any of it and say whether the value arrived.
	//
	// The kind is not checked here, because this reads a report and the kind belongs to the vertical.
	// What holds a report to the kind its vertical asked for is BuiltGreen, which has the list.
	return r.Evidence().Shows()
}

// aTestFile is the shape of a name that says a file is a test, in the ecosystems this system meets.
// It is the same rule the test gate refuses a write by, and it is here as well for the reason a gate
// and a reading of an answer are different things: the gate stops the write, and this refuses the
// claim that a build happened when what changed was the test.
var aTestFile = regexp.MustCompile(`(?i)(^|/)(test|tests|spec|specs|__tests__|testdata|fixtures|` +
	`features)/|(_test|_spec|\.test|\.spec)\.[a-z]+$|\.feature$|(^|/)(test|spec)_[^/]+$`)

// ATest says whether this path is a test.
func ATest(where string) bool {
	return aTestFile.MatchString(strings.ToLower(strings.TrimSpace(where)))
}

// BuiltGreen holds the stage to the one thing that closes it: every vertical on the accepted list has
// a worker that turned its own failing tests green.
//
// A vertical whose worker died is a vertical with no report, and it is refused by name. That is the
// sad path this has to get right: the other workers finished, most of the suite is green, and reading
// that as the stage being done would ship a job with one vertical never built.
func BuiltGreen(wanted []Requirement, failing map[int][]string,
	reports map[int]BuildReport) error {
	if len(wanted) == 0 {
		return fmt.Errorf("this job's accepted list carries no vertical the system can read, so there " +
			"is nothing to build")
	}
	for _, one := range wanted {
		report, held := reports[one.Number]
		if !held {
			return fmt.Errorf("vertical %d is not built: %q. The worker building it answered nothing the "+
				"system could read, so nothing holds this vertical", one.Number, one.Text)
		}
		if err := report.green(); err != nil {
			return fmt.Errorf("vertical %d, %q: %w", one.Number, one.Text, err)
		}
		// The kind is the vertical's to decide, so this is where a report is held to it: here the list
		// is in hand, and a report on its own says only what a worker chose to send.
		if err := report.Evidence().Holds(one.Evidence); err != nil {
			return fmt.Errorf("vertical %d, %q: %w", one.Number, one.Text, err)
		}
		if missing := notNamed(failing[one.Number], report.Passing); missing != "" {
			return fmt.Errorf("vertical %d, %q: %q failed for this vertical before the build and the "+
				"report does not say it passes now: run it, and name it on a %q line",
				one.Number, one.Text, missing, "Passing 1:")
		}
	}
	return nil
}

// notNamed is the first test that was failing and is not named as passing now, and empty where every
// one of them is.
//
// The link between the two stages is these names. Without it a worker could turn one easy test green,
// name that one, and the stage would close on a vertical whose other tests still fail: the count of
// failures it reports is its own word, and the names are the record the test stage already wrote.
func notNamed(failing, passing []string) string {
	for _, one := range failing {
		found := false
		for _, said := range passing {
			if strings.Contains(strings.ToLower(said), strings.ToLower(one)) {
				found = true
				break
			}
		}
		if !found {
			return one
		}
	}
	return ""
}

// BuiltText is the record the job keeps: every vertical, the run that covered it, the tests that pass
// now and what was written to make them.
//
// Each line is written under the vertical it came from, so a reader of the row can say which vertical
// any one of these files belongs to. That provenance is the claim's rather than the file name's: the
// worker that wrote this file held the claim on that vertical and no other worker could take it.
func BuiltText(wanted []Requirement, reports map[int]BuildReport) string {
	var lines []string
	for _, one := range wanted {
		report, held := reports[one.Number]
		if !held {
			continue
		}
		lines = append(lines, fmt.Sprintf("Vertical %d: %s", one.Number, one.Text),
			fmt.Sprintf("Ran %d: %d", one.Number, report.Ran))
		for _, passing := range report.Passing {
			lines = append(lines, fmt.Sprintf("Passes %d: %s", one.Number, passing))
		}
		for _, changed := range report.Changed {
			lines = append(lines, fmt.Sprintf("Changed %d: %s", one.Number, changed))
		}
		// The evidence and its label travel with the vertical they show, because what a person is shown
		// at the end of the stage is read back off this record rather than out of a worker's session,
		// and by then every one of those sandboxes is gone.
		//
		// Each kind is written under its own word, so the record says which kind a vertical was shown
		// with rather than leaving a reader to work it out from a file extension. Steps are written in
		// full and never as "Step 1:", because the number here is the vertical.
		shown := report.Evidence()
		switch shown.Kind {
		case KindSteps:
			for _, step := range shown.Steps {
				lines = append(lines, fmt.Sprintf("Steps %d: %s", one.Number, step))
			}
		case KindRecording:
			lines = append(lines, fmt.Sprintf("Recording %d: %s", one.Number, shown.File))
		default:
			lines = append(lines, fmt.Sprintf("Picture %d: %s", one.Number, shown.File))
		}
		lines = append(lines, fmt.Sprintf("Taken %d: %s", one.Number, shown.Taken))
	}
	return strings.Join(lines, "\n")
}

// BuiltOn is how many verticals and how many passing tests a kept record carries, for a reader that
// wants the size of it rather than the whole.
func BuiltOn(kept string) (verticals, passing int) {
	seen := map[int]bool{}
	for _, found := range buildLine.FindAllStringSubmatch(kept, -1) {
		number, err := strconv.Atoi(found[2])
		if err != nil {
			continue
		}
		switch strings.ToLower(found[1]) {
		case "vertical":
			seen[number] = true
		case "passes":
			passing++
		}
	}
	return len(seen), passing
}

// Built says whether this job's verticals were built against their failing tests.
func Built(one *Job) bool { return one != nil && one.Build != "" }

// WaitingForItsBuild says whether this job still owes a person something built.
//
// Behind every other gate, because it is the last stage: the reading, the list, the failing tests and
// the plan all stand in front of it. A job that holds a plan and no failing tests is a row written
// before the test stage existed, and it is left on the road it was already on rather than fanned out
// against a suite nobody wrote.
func WaitingForItsBuild(one *Job) bool {
	return Planned(one) && Ideated(one) && Designed(one) && TestsWritten(one) &&
		one.PlanApproved && !Built(one)
}

// BuildWorkers is the jobs a fan out declares: one for each vertical, each holding the claim on its
// own vertical, each under the boundary.
//
// Building is what puts them under it. The system reads that field when it sends the task and tells
// the session's runtime, and the gate then refuses a write to a test in that session and in no other.
//
// They are declared with the settle gate off, and for a different reason from the test stage's
// workers. That gate runs the repository's own suite, and while a fan out is in flight the suite is
// red for every vertical that has not landed yet, so it would refuse the first worker home for work
// the others have not done. What checks a build worker instead is this stage: its own tests, named by
// the stage that wrote them, have to pass.
func BuildWorkers(one *Job, wanted []Requirement, failing map[int][]string,
	opened map[int]Opened) []*Job {
	var workers []*Job
	for _, vertical := range wanted {
		workers = append(workers, &Job{
			ID: newRowID(), Workspace: one.Workspace, Project: one.Project,
			Parent: one.ID, Depth: one.Depth + 1, Version: 1, Phase: PhasePending,
			Title: BuildingVertical(vertical),
			Brief: BuildTheVertical(one, vertical, failing[vertical.Number], opened[vertical.Number]),
			Mode:  one.Mode, Repository: one.Repository, Product: one.Product, Request: one.Request,
			Claim: ClaimOnBuild(one.ID, vertical), Ungated: true, Building: true,
			// The branch the worker that wrote this vertical's tests left them on, so this worker lands
			// its implementation in the same pull request rather than opening a second one.
			Branch:  opened[vertical.Number].Branch,
			TraceID: one.TraceID, ParentSpanID: one.ParentSpanID,
		})
	}
	return workers
}

// BuildingIt is what an operator reads on a job whose workers are running: which verticals are built
// and which are being built now.
func BuildingIt(built, wanted int) string {
	return fmt.Sprintf("%d of %d verticals are built, and the rest are being built now, each in its "+
		"own session, none of them able to change a test", built, wanted)
}

// NotBuilt is the question a person is asked where the workers finished and the verticals are not
// green for the reasons this stage needs.
//
// It asks rather than stopping the job. What went wrong is a vertical nobody could build against its
// tests, a test that may itself be wrong, or a worker that died, and all three are a person's to
// decide: the work of every other worker is already in the repository.
func NotBuilt(why string) string {
	return fmt.Sprintf("The verticals of this job are not built for the reasons the build stage "+
		"needs: %s\n\nSay what to do: answer with what should change, say which test is wrong if one "+
		"is, or stop the job with krewe job stop.", why)
}

// BuiltIt is the record of the stage closing: how many verticals, and how many tests pass now.
func BuiltIt(wanted []Requirement, reports map[int]BuildReport) string {
	passing := 0
	for _, report := range reports {
		passing += len(report.Passing)
	}
	return fmt.Sprintf("%d verticals were built against their failing tests, %d of which pass now, "+
		"each by a worker that holds that vertical and cannot change a test", len(wanted), passing)
}

// buildIt moves a job whose suite is red and whose plan a person approved through the build stage.
//
// The same shape as the stage before it: one reading of the rows says what the verticals are, which of
// them a worker already holds, and what those workers answered. A tick that finds work missing
// declares it, a tick that finds it running leaves the row waiting, and the tick that finds it
// finished reads the reports and holds the job for a person.
//
// Nothing here is dispatched into a session for the job itself. The row waits, takes no room on the
// machine and pays for no container, and every session this stage buys belongs to a worker holding one
// vertical.
func (c *Controller) buildIt(ctx context.Context, one *Job) {
	wanted := RequirementsOf(one)
	failing := FailuresOn(one.Tests)
	if len(wanted) == 0 {
		c.askAboutTheBuild(ctx, one, oneLine(BuiltGreen(wanted, failing, nil).Error()))
		return
	}
	workers, err := c.buildersOn(ctx, one, wanted)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which verticals are being built",
			"job", one.ID, "error", err)
		return
	}
	// What the stage before this one left on a branch for each vertical. It is read off those workers'
	// rows rather than out of the record this job keeps, because the record is the system's rendering
	// of the runs and a second copy of the branch could only disagree with the row it came from.
	opened, err := c.openedFor(ctx, one, wanted)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read the branches this job's failing tests are on",
			"job", one.ID, "error", err)
		return
	}

	built, running, missing := 0, 0, 0
	reports := map[int]BuildReport{}
	var refused []string
	for _, vertical := range wanted {
		theirs := workers[ClaimOnBuild(one.ID, vertical)]
		if live(theirs) {
			running++
			continue
		}
		report, why := BuiltBy(theirs, vertical, failing[vertical.Number])
		// A vertical whose worker answered before a person sent the work back is not built, however
		// well that answer reads. The report was read once already and the picture in it is what they
		// looked at, so reading it again would put the same picture in front of them and call it an
		// answer to what they said was missing.
		if why == "" && SentBackToBuild(one, len(theirs)) {
			why = fmt.Sprintf("vertical %d, %q: a person looked at it and said the value did not arrive",
				vertical.Number, vertical.Text)
			report = BuildReport{}
		}
		if why == "" {
			built++
			reports[vertical.Number] = report
			continue
		}
		// No report, so nothing holds this vertical. It gets one more worker where a person has said to
		// carry on, and otherwise it is what that person is asked about.
		if len(theirs) < BuildAttempts && (len(theirs) == 0 || one.Told != "") {
			missing++
			c.declareTheBuilder(ctx, one, vertical, failing[vertical.Number], opened[vertical.Number])
			continue
		}
		refused = append(refused, why)
	}
	if running+missing > 0 {
		c.hold(ctx, one, BuildingIt(built, len(wanted)))
		return
	}
	if len(refused) == 0 {
		if err := BuiltGreen(wanted, failing, reports); err != nil {
			refused = append(refused, oneLine(err.Error()))
		}
	}
	if len(refused) > 0 {
		c.askAboutTheBuild(ctx, one, strings.Join(refused, "; "))
		return
	}

	question := AcceptWhatWasBuilt(one, wanted, reports)
	record := c.event(ctx, one, EventBuilt, BuiltIt(wanted, reports))
	asked := c.event(ctx, one, EventAsked, question)
	if _, err := c.store.HoldJobForAcceptance(ctx, one.ID, BuiltText(wanted, reports), question,
		record, asked); err != nil {
		if !errors.Is(err, ErrNotPending) {
			c.logger.WarnContext(ctx, "could not hold a built job for a person to accept",
				"job", one.ID, "error", err)
		}
		// Nothing is landed either way. Another controller recorded it, or the write did not apply, and a
		// later tick reads the same rows and does the same arithmetic.
		return
	}
	c.exported(ctx, record, asked)
}

// buildersOn is every worker declared for each of this job's verticals, by the claim it holds, oldest
// first.
//
// Keyed on the claim rather than on the parent, because the claim is what says which vertical a worker
// holds and a parent says only that the worker belongs to this job. A job's test workers are under the
// same parent and answer a different question, and their claims say requirement where these say build,
// so neither stage reads the other's answers.
func (c *Controller) buildersOn(ctx context.Context, one *Job, wanted []Requirement) (
	map[string][]*Job, error) {
	claims := make([]string, 0, len(wanted))
	for _, vertical := range wanted {
		claims = append(claims, ClaimOnBuild(one.ID, vertical))
	}
	held, err := c.store.JobsClaiming(ctx, one.Workspace, claims)
	if err != nil {
		return nil, err
	}
	workers := map[string][]*Job{}
	for _, worker := range held {
		workers[worker.Claim] = append(workers[worker.Claim], worker)
	}
	return workers, nil
}

// openedFor is the branch and the pull request the test stage left for each vertical, by vertical
// number.
//
// It reads the same rows the test stage read, by the claim that stage's workers hold, because that
// claim is what says which requirement a worker wrote for. A vertical whose test worker left nothing
// reads as nothing, and its build worker is then briefed the way every build worker was before a
// requirement had a branch: on a checkout of its own.
func (c *Controller) openedFor(ctx context.Context, one *Job, wanted []Requirement) (
	map[int]Opened, error) {
	workers, err := c.workersOn(ctx, one, wanted)
	if err != nil {
		return nil, err
	}
	opened := map[int]Opened{}
	for _, requirement := range wanted {
		opened[requirement.Number] = OpenedFor(workers[ClaimOnRequirement(one.ID, requirement)])
	}
	return opened, nil
}

// BuiltBy is the report the worker holding one vertical answered with, and the refusal where no worker
// of that vertical answered anything the system can read.
//
// The newest worker is the one read. A vertical whose first worker died has a second, and what the
// stage stands on is the run that happened last.
//
// A worker is read as holding the vertical its claim says it holds, rather than the number it wrote in
// its answer. The two disagreeing is a worker that reported on somebody else's vertical, and it is
// refused: a report filed under the wrong number would leave one vertical covered twice and another
// not at all.
func BuiltBy(workers []*Job, vertical Requirement, failing []string) (BuildReport, string) {
	if len(workers) == 0 {
		return BuildReport{}, fmt.Sprintf("vertical %d, %q: nothing has built it",
			vertical.Number, vertical.Text)
	}
	worker := workers[len(workers)-1]
	if worker.Answer == "" {
		return BuildReport{}, fmt.Sprintf("vertical %d, %q: the worker holding it %s and said nothing, %s",
			vertical.Number, vertical.Text, worker.Phase, oneLine(worker.Reason))
	}
	report, err := ReadBuildReport(worker.Answer)
	if err != nil {
		return BuildReport{}, fmt.Sprintf("vertical %d, %q: %s",
			vertical.Number, vertical.Text, oneLine(err.Error()))
	}
	if report.Vertical != vertical.Number {
		return BuildReport{}, fmt.Sprintf("vertical %d, %q: the worker holding it reported on "+
			"vertical %d instead", vertical.Number, vertical.Text, report.Vertical)
	}
	// Early, and again at the close of the stage. A worker that answered with the wrong kind is asked
	// again while it still has a session, rather than at the end when every other vertical is green.
	if err := report.Evidence().Holds(vertical.Evidence); err != nil {
		return BuildReport{}, fmt.Sprintf("vertical %d, %q: %s",
			vertical.Number, vertical.Text, oneLine(err.Error()))
	}
	if missing := notNamed(failing, report.Passing); missing != "" {
		return BuildReport{}, fmt.Sprintf("vertical %d, %q: %q failed for this vertical before the "+
			"build and the report does not say it passes now", vertical.Number, vertical.Text, missing)
	}
	return report, ""
}

// declareTheBuilder declares the one job that builds one vertical.
//
// A claim refused here is the mechanism working rather than a failure: another controller declared this
// worker a moment ago, and two workers building one vertical is exactly what the claim exists to stop.
// The row waits either way, and the next tick reads the worker that other controller declared.
func (c *Controller) declareTheBuilder(ctx context.Context, one *Job, vertical Requirement,
	failing []string, opened Opened) {
	worker := BuildWorkers(one, []Requirement{vertical}, map[int][]string{vertical.Number: failing},
		map[int]Opened{vertical.Number: opened})[0]
	record := &Event{
		ID: newRowID(), Job: worker.ID, Kind: EventDeclared, Workspace: worker.Workspace,
		Project: worker.Project, Parent: worker.Parent, Depth: worker.Depth,
		Detail: fmt.Sprintf("the build of vertical %d of job %s, %q",
			vertical.Number, one.ID, vertical.Text),
		TraceID: worker.TraceID, OccurredAt: time.Now().UTC(),
	}
	if err := c.store.CreateJob(ctx, worker, record); err != nil {
		var taken *Held
		if errors.As(err, &taken) {
			return
		}
		c.logger.WarnContext(ctx, "could not declare the worker that builds a vertical",
			"job", one.ID, "vertical", vertical.Number, "error", err)
		return
	}
	c.exported(ctx, record)
}

// askAboutTheBuild stops the job for a person, with what is wrong with the build.
//
// It asks rather than failing the job. The other verticals are already built, and what is left is a
// person's to decide: a vertical nobody can build against its tests is often a test that says the
// wrong thing, and that is a change to the test rather than a fault in the run.
func (c *Controller) askAboutTheBuild(ctx context.Context, one *Job, why string) {
	question := NotBuilt(why)
	record := c.event(ctx, one, EventAsked, question)
	if _, err := c.store.AskAboutJobBuild(ctx, one.ID, question, record); err != nil {
		if !errors.Is(err, ErrNotPending) {
			c.logger.WarnContext(ctx, "could not put a build that is not green to a person",
				"job", one.ID, "error", err)
		}
		return
	}
	c.exported(ctx, record)
}

// theEvidenceLines are the lines a report answers with for the kind of evidence its vertical asked
// for. A worker asked for a picture and shown the shape of steps sends a paragraph, and the whole
// point of asking in the shape it is read back in is that it does not have to guess.
func theEvidenceLines(wanted Kind) string {
	switch wanted {
	case KindRecording:
		return "Recording: the name of a recording of this vertical running\n"
	case KindSteps:
		return "Steps 1: the first thing a person runs or presses, and what they should see\n" +
			"Steps 2: the next one\n"
	default:
		return "Picture: the name of a picture of this vertical running\n"
	}
}
