package job

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The accepted requirements become failing tests, written by workers that never see an
// implementation, several at once.
//
// Requirements became code without ever becoming a failing test first. A session that builds and
// then tests writes the test it needs to pass, so the suite agrees with whatever was built: it
// records the implementation rather than the requirement, and it stays green through the change that
// breaks the product.
//
// So the tests come first, and the worker that writes them is not the worker that builds. Isolation
// is the whole of it. A worker that has read the code writes a test that agrees with the code, and
// at this point in the job there is nothing to read: nothing is implemented until the suite is red.
//
// It fans out, one worker for each requirement, and they run at once. A requirement is the unit here,
// the way a vertical is the unit in design, and each worker writes the tests for its own requirement
// and nothing else.
//
// There is no second record of what the requirements are. The list a person accepted in design is
// the list, read off the row, for the reason the stage itself is read off the row: a second copy of a
// fact could only disagree with the first.

// TheTestAsk is the phrase every ask for the tests of one requirement carries, and the phrase a
// double answers a report to. It is a constant for the reason the design ask is one: the ask and
// everything that recognises it must not drift apart.
const TheTestAsk = "write the tests that prove this requirement, and make them fail"

// TestMarker is the line every report carries, and it is how anything holding a reply can tell a
// report from a plan, a reading or a list.
const TestMarker = "Ran:"

// Requirement is one thing the tests must prove: a vertical a person accepted, with what that person
// is shown when it lands.
type Requirement struct {
	Number int
	Text   string
	Shown  string
}

// RequirementsOf is the requirement list, which is the list of verticals a person accepted.
//
// Empty on a job whose list nobody accepted, because an unaccepted list is a proposal: writing tests
// against it would spend a session for every line a person is about to change.
func RequirementsOf(one *Job) []Requirement {
	if !Designed(one) {
		return nil
	}
	var wanted []Requirement
	for _, vertical := range DesignIn(one.Design).Verticals {
		wanted = append(wanted, Requirement{
			Number: vertical.Number, Text: vertical.Text, Shown: vertical.Shown,
		})
	}
	return wanted
}

// ClaimOnRequirement is the piece of work one test worker holds, and it is written by the system
// rather than passed by a caller.
//
// The claim already refuses a second job taking work a first job holds, so two workers cannot write
// the tests for one requirement. It is derived rather than declared because a mechanism a caller has
// to remember is a mechanism that is forgotten: the fan out happens inside the system, nobody types
// it, and the string has to be the same one on the second reading of the same row.
//
// It carries the job the requirement belongs to, so two jobs that accepted a list of the same length
// never collide, and it stays inside the claim's ceiling because a job identifier and a number are
// both short.
func ClaimOnRequirement(job string, wanted Requirement) string {
	return TidyClaim(fmt.Sprintf("%s requirement %d", job, wanted.Number))
}

// TestsFor is the title of the worker that writes the tests for one requirement. It says which
// requirement it holds, because that title is what a refused second claim names.
func TestsFor(wanted Requirement) string {
	title := fmt.Sprintf("tests for requirement %d: %s", wanted.Number, wanted.Text)
	if len(title) > TitleLimit {
		title = strings.TrimSpace(title[:TitleLimit])
	}
	return title
}

// WriteFailingTests is the brief one test worker is given: its requirement, and nothing about how
// anything is built.
//
// It is given one requirement rather than the list, because a worker holding the whole list writes a
// little of each and the fan out buys nothing. It is told not to implement, and told what its answer
// has to carry, in the shape the system reads back.
func WriteFailingTests(one *Job, wanted Requirement) string {
	said := []string{
		fmt.Sprintf("Requirement %d of the list a person accepted for this job. %s",
			wanted.Number, TheTestAsk),
	}
	if one.Product != "" {
		said = append(said, ServesAPerson(one.Product))
	}
	said = append(said, fmt.Sprintf("Requirement %d: %s\nShown %d: %s",
		wanted.Number, wanted.Text, wanted.Number, wanted.Shown))
	said = append(said, "Write the tests and nothing else. Do not implement this requirement, do not "+
		"implement any other, and do not change code that is not a test. A test written by somebody "+
		"who read the implementation agrees with the implementation, which is the failure this stage "+
		"exists to stop.")
	said = append(said, "Write the tests for this requirement only. Another worker is writing the "+
		"tests for each of the others at the same time, and it holds that requirement.")
	said = append(said, theShapeOfATestReport(wanted))
	return strings.Join(said, "\n\n")
}

// theShapeOfATestReport is what the system asks for, in the shape it reads back.
//
// It asks for the run rather than for a description of it. A suite that finds nothing to execute
// reports success just the same, so how many tests ran is the number that says whether anything
// happened at all, and the failures are what say the tests assert something nothing has built yet.
func theShapeOfATestReport(wanted Requirement) string {
	return fmt.Sprintf("Run the suite the way the repository runs it, and answer in these lines as "+
		"well as your outcome:\n\n"+
		"Requirement: %d\n"+
		"Ran: how many tests the run executed, as a number\n"+
		"Failing 1: the name of a test you wrote that fails now\n"+
		"Failing 2: the name of the next one\n\n"+
		"Every test you write must fail, because nothing implements this yet. A run that executed "+
		"nothing is a failure of this task rather than a pass, and a test that passes before anything "+
		"is built asserts nothing.", wanted.Number)
}

// reportLine is the shape a report is read back in: the requirement it was written for, how many
// tests the run executed, and each test that fails now.
//
// Read off the reply rather than reported, the way a list and a plan already are. What it finds is
// then what the worker meant to say, rather than a sentence that happened to hold the word.
var reportLine = regexp.MustCompile(`(?im)^[ \t]*(requirement|ran|failing|fails)[ \t]*(\d*)[ \t]*[:.][ \t]*(.+?)[ \t]*$`)

// TestReport is what one worker answers with: which requirement it wrote for, how many tests its run
// executed, and the tests that fail now.
type TestReport struct {
	Requirement int
	Ran         int
	Failing     []string
}

// ReadTestReport is the report a reply carries, and the refusal where it carries none.
//
// The refusals are the whole point of the stage. A run that executed nothing and a run where
// everything passed both read as success everywhere else in this system, and both mean the tests
// prove nothing about the requirement they were written for.
func ReadTestReport(reply string) (TestReport, error) {
	report := TestReport{}
	ran, said := "", false
	for _, found := range reportLine.FindAllStringSubmatch(reply, -1) {
		text := TidySentence(found[3])
		if text == "" {
			continue
		}
		switch strings.ToLower(found[1]) {
		case "requirement":
			// Either shape it can arrive in: the number after the word, as the ask asks for it, or the
			// number the requirement itself is written under. The first readable one stands, the way the
			// first line under a number stands in a list: a reply naming two requirements wrote for one of
			// them and mentioned the other.
			if report.Requirement != 0 {
				continue
			}
			if number, err := strconv.Atoi(found[2]); err == nil {
				report.Requirement = number
				continue
			}
			report.Requirement, _ = strconv.Atoi(strings.Fields(text)[0])
		case "ran":
			if !said {
				ran, said = strings.Fields(text)[0], true
			}
		default:
			report.Failing = append(report.Failing, text)
		}
	}
	if report.Requirement == 0 {
		return TestReport{}, fmt.Errorf("this reply does not say which requirement it wrote the tests "+
			"for: write a line %q with the number of the requirement you were given", "Requirement: 2")
	}
	if !said {
		return TestReport{}, fmt.Errorf("this reply does not say how many tests the run executed: run "+
			"the suite and write a line %q. A run that finds nothing to execute reports success just "+
			"the same, so the count is what says the tests ran at all", "Ran: 12")
	}
	count, err := strconv.Atoi(ran)
	if err != nil {
		return TestReport{}, fmt.Errorf("%q is not a number of tests: write %q with the count the run "+
			"printed", ran, "Ran: 12")
	}
	report.Ran = count
	if err := report.red(); err != nil {
		return TestReport{}, err
	}
	return report, nil
}

// red is what makes a report a report: the run happened, and it failed.
func (r TestReport) red() error {
	if r.Ran <= 0 {
		return fmt.Errorf("this run executed %d tests, so it proved nothing: a run that finds nothing "+
			"to execute reports success just the same. Check the tests are in a file the runner "+
			"collects, run the suite again, and answer with what it printed", r.Ran)
	}
	if len(r.Failing) == 0 {
		return fmt.Errorf("this run executed %d tests and none of them failed. Nothing implements this "+
			"requirement yet, so a test that passes now asserts nothing: write tests that fail, and "+
			"name each one on a %q line", r.Ran, "Failing 1:")
	}
	if len(r.Failing) > r.Ran {
		return fmt.Errorf("this reply names %d failing tests out of %d that ran: say how many tests the "+
			"whole run executed", len(r.Failing), r.Ran)
	}
	return nil
}

// TestsRed holds the stage to the one thing that closes it: every requirement on the accepted list
// has tests, they ran, and they fail.
//
// A requirement whose worker died is a requirement with no report, and it is refused by name. That is
// the sad path this has to get right: the other workers finished, the suite is red, and reading that
// as the stage being done would leave one requirement with nothing holding it.
func TestsRed(wanted []Requirement, reports map[int]TestReport) error {
	if len(wanted) == 0 {
		return fmt.Errorf("this job's accepted list carries no requirement the system can read, so " +
			"there is nothing to write tests for")
	}
	for _, one := range wanted {
		report, held := reports[one.Number]
		if !held {
			return fmt.Errorf("requirement %d has no failing test: %q. The worker that writes its tests "+
				"answered nothing the system could read, so nothing holds this requirement",
				one.Number, one.Text)
		}
		if err := report.red(); err != nil {
			return fmt.Errorf("requirement %d, %q: %w", one.Number, one.Text, err)
		}
	}
	return nil
}

// TestsText is the record the job keeps: every requirement, the run that covered it, and the tests
// that fail now.
//
// Each failure is written under the requirement it came from, so a reader of the row can say which
// requirement any one of these tests holds. That provenance is the claim's rather than the test
// name's: the worker that wrote this failure held the claim on that requirement and no other worker
// could take it.
func TestsText(wanted []Requirement, reports map[int]TestReport) string {
	var lines []string
	for _, one := range wanted {
		report, held := reports[one.Number]
		if !held {
			continue
		}
		lines = append(lines, fmt.Sprintf("Requirement %d: %s", one.Number, one.Text),
			fmt.Sprintf("Ran %d: %d", one.Number, report.Ran))
		for _, failing := range report.Failing {
			lines = append(lines, fmt.Sprintf("Fails %d: %s", one.Number, failing))
		}
	}
	return strings.Join(lines, "\n")
}

// TestsOn is how many requirements and how many failing tests a kept record carries, for a reader
// that wants the size of it rather than the whole.
func TestsOn(kept string) (requirements, failing int) {
	seen := map[int]bool{}
	for _, found := range reportLine.FindAllStringSubmatch(kept, -1) {
		number, err := strconv.Atoi(found[2])
		if err != nil {
			continue
		}
		switch strings.ToLower(found[1]) {
		case "requirement":
			seen[number] = true
		case "fails":
			failing++
		}
	}
	return len(seen), failing
}

// pastItsTests says whether this job is past the test stage, whether or not it went through it.
//
// A row written before this existed carries a plan and no tests, and a gate that read those as owing
// tests would drag work a person already approved back to the beginning. So a job that holds a plan
// is past this, the way the design gate already treats one.
func pastItsTests(one *Job) bool {
	return one != nil && (one.Tests != "" || one.Plan != "" || one.PlanApproved)
}

// TestsWritten says whether the requirements of this job became failing tests.
func TestsWritten(one *Job) bool { return one != nil && one.Tests != "" }

// WaitingForItsTests says whether this job still owes a red suite.
//
// The same gate the list and the plan are held by, and it stands behind the list: tests written
// before a person accepted what would be built are tests for the wrong things. It stands in front of
// the plan, because the plan is the steps towards making these tests pass.
func WaitingForItsTests(one *Job) bool {
	return Planned(one) && Ideated(one) && Designed(one) && !pastItsTests(one)
}

// WritingTheTests is what an operator reads on a job whose workers are running: which requirements
// have their tests and which are still being written.
func WritingTheTests(written, wanted int) string {
	return fmt.Sprintf("the tests for %d of %d requirements are written, and the rest are being "+
		"written now, each in its own session", written, wanted)
}

// NoRedSuite is the question a person is asked where the workers finished and the suite is not red
// for the reasons this stage needs.
//
// It asks rather than stopping the job. What went wrong is a requirement nobody can write a failing
// test for, or a worker that died, and both of those are a person's to decide: the job has an
// accepted list behind it and the work of every other worker is already in the repository.
func NoRedSuite(why string) string {
	return fmt.Sprintf("The tests for this job's requirements are not red for the reasons the test "+
		"stage needs: %s\n\nNothing is built yet, and nothing will be until every requirement has a "+
		"test that fails. Say what to do: answer with what should change about the list, or stop the "+
		"job with krewe job stop.", why)
}

// TestWorkers is the jobs a fan out declares: one for each requirement, each holding the claim on its
// own requirement, each carrying what the parent job serves.
//
// They are declared with the settle gate off. A gate runs the repository's own suite and reads its
// output, and the work these jobs do is deliberately red, so a gate would refuse every one of them
// for doing exactly what it was asked. What checks them instead is the stage itself: the report is
// read off each answer, and the requirement it was written for has to be the one it holds.
func TestWorkers(one *Job, wanted []Requirement) []*Job {
	var workers []*Job
	for _, requirement := range wanted {
		workers = append(workers, &Job{
			ID: newRowID(), Workspace: one.Workspace, Project: one.Project,
			Parent: one.ID, Depth: one.Depth + 1, Version: 1, Phase: PhasePending,
			Title: TestsFor(requirement), Brief: WriteFailingTests(one, requirement),
			Mode: one.Mode, Repository: one.Repository, Product: one.Product, Request: one.Request,
			Claim: ClaimOnRequirement(one.ID, requirement), Ungated: true,
			TraceID: one.TraceID, ParentSpanID: one.ParentSpanID,
		})
	}
	return workers
}

// ReportFrom is the report the worker holding one requirement answered with, and the refusal where
// no worker of that requirement answered anything the system can read.
//
// The newest worker is the one read. A requirement whose first worker died has a second, and what
// the stage stands on is the run that happened last.
//
// A worker is read as holding the requirement its claim says it holds, rather than the number it
// wrote in its answer. The two disagreeing is a worker that wrote for somebody else's requirement,
// and it is refused: a report filed under the wrong number would leave one requirement covered twice
// and another not at all.
func ReportFrom(workers []*Job, requirement Requirement) (TestReport, string) {
	if len(workers) == 0 {
		return TestReport{}, fmt.Sprintf("requirement %d, %q: nothing has written its tests",
			requirement.Number, requirement.Text)
	}
	worker := workers[len(workers)-1]
	if worker.Answer == "" {
		return TestReport{}, fmt.Sprintf("requirement %d, %q: the worker holding it %s and said nothing, %s",
			requirement.Number, requirement.Text, worker.Phase, oneLine(worker.Reason))
	}
	report, err := ReadTestReport(worker.Answer)
	if err != nil {
		return TestReport{}, fmt.Sprintf("requirement %d, %q: %s",
			requirement.Number, requirement.Text, oneLine(err.Error()))
	}
	if report.Requirement != requirement.Number {
		return TestReport{}, fmt.Sprintf("requirement %d, %q: the worker holding it reported on "+
			"requirement %d instead", requirement.Number, requirement.Text, report.Requirement)
	}
	return report, ""
}

// TestAttempts is how many workers one requirement may have.
//
// The first, and one more after a person has said to carry on. It is the bound the gates around this
// one already keep: a session is asked once more and then the job stops, because every ask is a task
// somebody pays for, and a requirement whose worker dies twice is a requirement a person has to look
// at rather than a run to repeat.
const TestAttempts = 2

// writeTheTests moves a job whose list a person accepted through the test stage.
//
// The fan out and the gathering are one comparison made against one reading of the rows: what the
// requirements are, which of them a worker already holds, and what those workers answered. A tick
// that finds work missing declares it, a tick that finds it running leaves the row waiting, and the
// tick that finds it finished reads the reports and closes the stage.
//
// Nothing here is dispatched into a session for the job itself. The row waits, takes no room on the
// machine and pays for no container, and every session this stage buys belongs to a worker holding
// one requirement.
func (c *Controller) writeTheTests(ctx context.Context, one *Job) {
	wanted := RequirementsOf(one)
	if len(wanted) == 0 {
		c.askAboutTheTests(ctx, one, oneLine(TestsRed(wanted, nil).Error()))
		return
	}
	workers, err := c.workersOn(ctx, one, wanted)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which requirements have their tests being written",
			"job", one.ID, "error", err)
		return
	}

	written, running, missing := 0, 0, 0
	reports := map[int]TestReport{}
	var refused []string
	for _, requirement := range wanted {
		theirs := workers[ClaimOnRequirement(one.ID, requirement)]
		if live(theirs) {
			running++
			continue
		}
		report, why := ReportFrom(theirs, requirement)
		if why == "" {
			written++
			reports[requirement.Number] = report
			continue
		}
		// No report, so nothing holds this requirement. It gets one more worker where a person has said
		// to carry on, and otherwise it is what that person is asked about.
		if len(theirs) < TestAttempts && (len(theirs) == 0 || one.Told != "") {
			missing++
			c.declareTheWorker(ctx, one, requirement)
			continue
		}
		refused = append(refused, why)
	}
	if running+missing > 0 {
		c.hold(ctx, one, WritingTheTests(written, len(wanted)))
		return
	}
	if len(refused) == 0 {
		if err := TestsRed(wanted, reports); err != nil {
			refused = append(refused, oneLine(err.Error()))
		}
	}
	if len(refused) > 0 {
		c.askAboutTheTests(ctx, one, strings.Join(refused, "; "))
		return
	}

	record := c.event(ctx, one, EventTested, WrittenTheTests(wanted, reports))
	if _, err := c.store.RecordJobTests(ctx, one.ID, TestsText(wanted, reports), record); err != nil {
		if !errors.Is(err, ErrNotPending) {
			c.logger.WarnContext(ctx, "could not record the failing tests a job's requirements became",
				"job", one.ID, "error", err)
		}
		// Nothing is landed either way. Another controller recorded it, or the write did not apply, and
		// a later tick reads the same rows and does the same arithmetic.
		return
	}
	c.exported(ctx, record)
}

// workersOn is every worker declared for each of this job's requirements, by the claim it holds,
// oldest first.
//
// Keyed on the claim rather than on the parent, because the claim is what says which requirement a
// worker holds and a parent says only that the worker belongs to this job. It is a list rather than
// one job, because a requirement whose first worker died can have a second, and how many it has is
// the bound on how many more it gets.
func (c *Controller) workersOn(ctx context.Context, one *Job, wanted []Requirement) (
	map[string][]*Job, error) {
	claims := make([]string, 0, len(wanted))
	for _, requirement := range wanted {
		claims = append(claims, ClaimOnRequirement(one.ID, requirement))
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

// live says whether any of these workers is still writing.
func live(workers []*Job) bool {
	for _, worker := range workers {
		if !Terminal(worker.Phase) {
			return true
		}
	}
	return false
}

// declareTheWorker declares the one job that writes the tests for one requirement.
//
// A claim refused here is the mechanism working rather than a failure: another controller declared
// this worker a moment ago, and two workers writing the tests for one requirement is exactly what the
// claim exists to stop. The row waits either way, and the next tick reads the worker that other
// controller declared.
func (c *Controller) declareTheWorker(ctx context.Context, one *Job, requirement Requirement) {
	worker := TestWorkers(one, []Requirement{requirement})[0]
	record := &Event{
		ID: newRowID(), Job: worker.ID, Kind: EventDeclared, Workspace: worker.Workspace,
		Project: worker.Project, Parent: worker.Parent, Depth: worker.Depth,
		Detail: fmt.Sprintf("the tests for requirement %d of job %s, %q",
			requirement.Number, one.ID, requirement.Text),
		TraceID: worker.TraceID, OccurredAt: time.Now().UTC(),
	}
	if err := c.store.CreateJob(ctx, worker, record); err != nil {
		var taken *Held
		if errors.As(err, &taken) {
			return
		}
		c.logger.WarnContext(ctx, "could not declare the worker that writes a requirement's tests",
			"job", one.ID, "requirement", requirement.Number, "error", err)
		return
	}
	c.exported(ctx, record)
}

// askAboutTheTests stops the job for a person, with what is wrong with the suite.
//
// It asks rather than failing the job. The tests for the other requirements are already written, and
// what is left is a person's to decide: a requirement nobody can write a failing test for is usually
// a requirement that says two things, and that is a change to the list rather than a fault in a run.
func (c *Controller) askAboutTheTests(ctx context.Context, one *Job, why string) {
	question := NoRedSuite(why)
	record := c.event(ctx, one, EventAsked, question)
	if _, err := c.store.AskAboutJobTests(ctx, one.ID, question, record); err != nil {
		if !errors.Is(err, ErrNotPending) {
			c.logger.WarnContext(ctx, "could not put a suite that is not red to a person",
				"job", one.ID, "error", err)
		}
		return
	}
	c.exported(ctx, record)
}

// WrittenTheTests is the record of the stage closing: how many requirements, and how many tests fail
// now.
func WrittenTheTests(wanted []Requirement, reports map[int]TestReport) string {
	failing := 0
	for _, report := range reports {
		failing += len(report.Failing)
	}
	return fmt.Sprintf("%d requirements became %d failing tests, each written by a worker that holds "+
		"that requirement and nothing else", len(wanted), failing)
}
