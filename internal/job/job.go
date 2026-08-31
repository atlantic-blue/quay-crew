// Package job is the declared unit of intent: a caller writes a job, and the system keeps
// it.
//
// The record is the whole point. A caller declares what it wants, closes its terminal, and the
// intent is still there tomorrow, because it is a row rather than a list held in a process. Nothing
// here dispatches anything: a controller makes reality match the record, and that is its own slice.
//
// Almost every rule below is checked at the moment of the write, because a refusal that arrives
// hours later, inside a run, has nothing to point back at. The one exception is what a role
// receives, which is checked again at the dispatch: a role can be detached, imported again and
// attached again while a job sits pending, so what the system would put in front of a session is only
// settled at the moment it hands it over.
package job

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atlantic-blue/krewe/internal/capacity"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/role"
)

// The phases a job moves through. They are the flow engine's words plus one, so a reader
// learns one vocabulary rather than two.
const (
	// PhasePending is declared and not started. Every job opens here.
	PhasePending = "pending"
	// PhaseWaiting is held back: something it waits for is open, or the workspace is at its limit.
	PhaseWaiting = "waiting"
	// PhaseRunning is a session exists and a task is in flight.
	PhaseRunning = "running"
	// PhaseAsking is a question is on the record and nothing but an answer moves it.
	PhaseAsking = "asking"
	// PhaseDone is it finished and the answer is on the record.
	PhaseDone = "done"
	// PhaseFailed is the model did not finish, or the sandbox could not be made.
	PhaseFailed = "failed"
	// PhaseStopped is it was halted: a person stopped it, or it met a limit, or its claim did not
	// hold. A job that went quiet and a job that was halted must never read the same.
	PhaseStopped = "stopped"
)

// The ceilings a declaration is held to.
const (
	// TitleLimit is how long a title may be. It is the line a listing shows, and it is the ceiling a
	// role's summary already has.
	TitleLimit = role.SummaryLimit
	// BriefLimit is how long a brief may be. A brief nobody reads to the end is a brief nobody
	// follows.
	BriefLimit = role.BriefLimit
	// LabelCount is how many labels a job may carry.
	LabelCount = 16
	// LabelLimit is how long a label key or value may be, which is the ceiling Kubernetes puts on a
	// label value.
	LabelLimit = 63
	// ProductLimit is how long the one sentence may be. It is the title's ceiling, because both are one
	// line a person reads, and a paragraph here is a design document arriving by the back door.
	ProductLimit = TitleLimit
)

// Job is one piece of declared intent and the status a controller keeps on it.
type Job struct {
	ID        string
	Workspace string
	Project   string

	// What the caller declared.
	Title          string
	Brief          string
	Role           string
	RoleVersion    int
	Mode           string
	ExpectFile     string
	ExpectContains string
	After          []string
	Deadline       *time.Time
	BudgetTokens   int64
	Labels         map[string]string
	// Requires is the material this job cannot be done without, drawn from role.Material. It is the
	// other side of a role's boundary: the role says what it receives and this says what the job
	// needs, and where the two disagree the job is refused rather than run without it.
	Requires []string

	// Claim is the piece of work this job is doing: an issue, a branch, or a name two people would
	// both use for the same thing. A second job claiming it is refused while this one holds it. See
	// claim.go for what holding means and when it ends.
	Claim string

	// Repository is the repository this job works in, written owner/name. Naming one says how the job
	// ends: the session pushes and opens a pull request, and the job is not done until its answer names
	// that pull request. Empty claims nothing and is checked as nothing.
	Repository string

	// Product is the one sentence this job serves, in a person's words: what somebody does with what is
	// built, and what they get back. It is stated on the root and every child carries it.
	//
	// It exists because a design document is not the product. A run built one faithfully, every check
	// was green, and the operator opened it two days later and could not use it: the document said the
	// address reads /videos?id=<video id>, and nobody had written the sentence a person would say,
	// which is that you paste a link and get the text back. Nothing measured the one against the other
	// because only one of them existed.
	Product string

	// Plan is what the crew said it would do, one numbered step per line, and PlanApproved says
	// whether a person approved it. A job that states the sentence writes its plan before it does any
	// work, and nothing is built until somebody says yes to these lines. See plan.go.
	//
	// Both are the controller's and the operator's to write, never a caller's: the crew writes the
	// plan and the person approves it, which is the whole shape.
	Plan         string
	PlanApproved bool

	// Steers is how many times the operator had to say something this job should have known, counted
	// on the job the steer landed on and on every job above it. On the job at the top it is the score
	// of the whole tree, which is the number the acceptance job exists to move. See steer.go.
	Steers int

	// What the system assigned, and the caller may not.
	Parent string
	Depth  int
	// Version rises on every write to a declared field, so a status can be told current from stale.
	Version int

	// What the controller writes, and nobody else.
	Phase    string
	Session  string
	Attempts int
	// Answer is what came back, whole. This field is the read path: it is the difference between an
	// answer that lives in a conversation and an answer a caller can read.
	Answer   string
	Reason   string
	Question string
	// Told is the last thing a person told this job, and it is what the system sends the session when
	// the job starts again. It stays on the row after it has been handed over: what somebody decided
	// is part of the record of the job, and a field that empties itself leaves a reader unable to say
	// whether an answer was ever given.
	Told string
	// PullRequest is the address the answer named, read off the answer rather than reported by the
	// model. It is what a reader opens to see the work without opening a sandbox, which is the whole
	// point of the field: a listing that says a job is done and nothing about where the work went is
	// the silence this was built to end.
	PullRequest string
	// SpentTokens is what this job's own session has cost.
	SpentTokens int64
	// Steps are what the session doing this job said it finished, in the order it finished them. They
	// are what a continued job carries on from, and they are read with one job rather than with a
	// listing: a listing of a hundred lists is a listing nobody can read.
	Steps []Step
	// Escalation is what this job does when it goes in circles, as the caller declared it: "ask" to put
	// the question to the operator, or "role:<name>" to hand it to another role. Empty is asking, which
	// is what a job whose author never thought about looping gets. See loop.go.
	Escalation string
	// Attempted is what each attempt at a step produced, with how like the earlier attempts at that
	// step it was. Attempts, above, is how many times a session was started for this job; this is what
	// those attempts said, which is the only thing a loop can be read off.
	Attempted []Attempt
	// LoopedStep is the step this job went in circles on, and zero for a job that never has.
	// EscalatedTo is the route the system took when it did, in the shape it was declared. A job
	// escalates once: the second loop stops it rather than escalating again.
	LoopedStep  int
	EscalatedTo string
	// Resuming is the failure this attempt is continuing past, and empty for a job nobody continued.
	// It is what the job failed with, moved off the reason by the resume, so a job that is going again
	// does not sit pending reading as one the machine is holding back.
	Resuming string
	// ObservedVersion is the Version of the declaration the status describes. A controller that has
	// not caught up leaves this behind, and a reader can then tell a status that is current from one
	// that is stale.
	ObservedVersion int

	// LeaseOwner is the controller holding this job, and LeaseUntil is when its hold runs out.
	// They are the only status fields a reader should ignore: they say who is holding the job, not
	// what came of it. A lease that has run out is the signal that its holder went away.
	LeaseOwner string
	LeaseUntil *time.Time

	// TraceID is the trace this whole tree belongs to, minted at the root and inherited unchanged by
	// every descendant. ParentSpanID is the span the caller was inside when it declared this job,
	// empty for a root nothing was tracing.
	//
	// Both are on the row rather than in a process, which is what makes a trace survive the
	// controller that started the job: the context is in the declaration, the way a wait is a column
	// rather than a timer.
	TraceID      string
	ParentSpanID string

	CreatedAt  time.Time
	UpdatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// Event is one thing that happened to a job. It is written in the same transaction as the
// row it describes, so the record and its history cannot disagree.
type Event struct {
	ID        string
	Kind      string
	Job       string
	Workspace string
	Project   string
	Parent    string
	Depth     int
	// Detail is a short line about what happened. It goes through the system's redactor before it is
	// written.
	Detail string
	// TraceID is the trace this happened in, and the same value the job carries. A reader holding
	// one record reaches the trace without reading the job row first.
	TraceID    string
	OccurredAt time.Time
}

// The kinds of event the system writes. The contract another service may one day depend on is wider:
// the two a lease writes belong to the slice that adds one.
const (
	EventDeclared = "job.declared"
	// EventClaimed and EventReleased are internal, and nothing outside should read them. A dashboard
	// counting job must not break because the system changed how it leases.
	EventClaimed  = "job.claimed"
	EventReleased = "job.released"
	EventStarted  = "job.started"
	// EventHeld is written when the system will not start a job yet: the machine has no room for its
	// sandbox. It is not a movement, because the job stays pending, and it is written once per
	// reason rather than once per tick.
	EventHeld     = "job.held"
	EventAnswered = "job.answered"
	EventFailed   = "job.failed"
	EventStopped  = "job.stopped"
	// EventAsked is written when a job puts a question to a person, and EventTold when that person
	// answers it. Between the two nothing moves the job, which is what makes the question a gate
	// rather than a note.
	EventAsked = "job.asked"
	EventTold  = "job.told"
	// EventStepped is written when the session doing a job says it finished one step. It is not a
	// movement: the job is running before it and running after it, and what it adds is the record a
	// second attempt carries on from.
	EventStepped = "job.stepped"
	// EventLooped is written when a job goes in circles: the same step attempted three times in a way
	// the system cannot tell apart. It is not a phase, because where the job goes next is what the job
	// declared, so the record carries the loop and the row carries the escalation.
	EventLooped = "job.looped"
	// EventResumed is written when a person continues a job that failed, and EventRefused when they
	// end one instead. They are the two answers to a failure, and which of the two was given is the
	// part of the record somebody reads a week later.
	EventResumed = "job.resumed"
	EventRefused = "job.refused"
	// EventUnstuck is written when the system finds it is running nothing while jobs wait for room,
	// and takes a container back to start again. It is not a movement: the job is pending before it
	// and pending after it, and the next tick starts it. It goes on the job the room was freed for,
	// because that is the job that was being denied.
	EventUnstuck = "job.unstuck"
)

// Contract says whether a kind is one another service may depend on.
//
// The split is the useful part. A dashboard counting jobs must never break because the system changed
// how it leases, and a dashboard counting leases has taken a dependency it was told not to take.
func Contract(kind string) bool {
	switch kind {
	case EventClaimed, EventReleased:
		return false
	default:
		return true
	}
}

// Filter narrows a listing. The zero value is every job the system holds.
type Filter struct {
	// Project wins over Workspace when both are set, being the narrower.
	Workspace string
	Project   string
	// Parent narrows to the children of one job. Root narrows to jobs with no parent,
	// which cannot be said with Parent alone because empty means "do not narrow".
	Parent string
	Root   bool
	Phase  string
	// LabelKey and LabelValue narrow to jobs carrying one label. A key with no value matches any
	// value, which is how a caller finds everything it labelled at all.
	LabelKey   string
	LabelValue string
}

// Declaration is what a caller writes. Everything else on a job is the system's to assign.
//
// ID and Parent are here so they can be refused. A field that is quietly ignored is worse than one
// that does not exist: the caller believes it took effect.
type Declaration struct {
	Workspace      string
	Project        string
	Title          string
	Brief          string
	Role           string
	Mode           string
	ExpectFile     string
	ExpectContains string
	After          []string
	Deadline       *time.Time
	BudgetTokens   int64
	Labels         map[string]string
	Requires       []string
	Repository     string
	Product        string
	Claim          string
	// Escalation is what this job does when it goes in circles: "ask", or "role:<name>". Empty is
	// asking, and every word is refused at the write, where the person who typed it is looking.
	Escalation string
	ID         string
	Parent     string
}

// Tidied is the declaration as it is stored: the space around the lines it will be read as comes off.
func (d Declaration) Tidied() Declaration {
	d.Title = strings.TrimSpace(d.Title)
	d.Brief = strings.TrimSpace(d.Brief)
	d.Role = strings.TrimSpace(d.Role)
	d.Mode = strings.TrimSpace(d.Mode)
	d.ExpectFile = strings.TrimSpace(d.ExpectFile)
	d.Product = TidySentence(d.Product)
	d.Escalation = strings.ToLower(strings.TrimSpace(d.Escalation))
	d.Requires = TidyRequires(d.Requires)
	d.Repository = TidyRepository(d.Repository)
	d.Claim = TidyClaim(d.Claim)
	return d
}

// Validate refuses a declaration that could not run, with a sentence saying what to do instead.
//
// Everything here is decidable from the declaration alone. Whether the workspace, the project, the
// role and the job it waits for exist is the control plane's question, because only the store can
// answer it.
func (d Declaration) Validate() error {
	tidy := d.Tidied()
	switch {
	case tidy.ID != "":
		return fmt.Errorf("job carries an identifier of its own, and the system assigns the identifier: "+
			"declare it without one and read the identifier back from the answer (you sent %q)", tidy.ID)
	case tidy.Parent != "":
		return fmt.Errorf("job carries a parent, and the parent comes from the credential the caller "+
			"presented rather than from the request: declare it without one (you sent %q)", tidy.Parent)
	case tidy.Title == "":
		return fmt.Errorf("job needs a title, which is the one line a listing shows: say what this job is, in a few words")
	case len(tidy.Title) > TitleLimit:
		return fmt.Errorf("the title is %d bytes and the ceiling is %d, because it is the line a listing shows: "+
			"put the detail in the brief", len(tidy.Title), TitleLimit)
	case tidy.Brief == "":
		return fmt.Errorf("job needs a brief, which is what the session is asked to do: say what has to happen")
	case len(tidy.Brief) > BriefLimit:
		return fmt.Errorf("the brief is %d bytes and the ceiling is %d, because a brief nobody reads to the end "+
			"is a brief nobody follows: split it into more than one job", len(tidy.Brief), BriefLimit)
	case len(tidy.Product) > ProductLimit:
		return fmt.Errorf("the sentence is %d bytes and the ceiling is %d, because it is one sentence a person "+
			"would say: write what somebody does and what they get back, and put the rest in the brief",
			len(tidy.Product), ProductLimit)
	case tidy.BudgetTokens < 0:
		return fmt.Errorf("the budget is %d tokens and a budget cannot be below zero: leave it at zero to draw "+
			"from the parent, or give a number of tokens", tidy.BudgetTokens)
	}
	if err := tidy.validateMode(); err != nil {
		return err
	}
	if err := usableExpectFile(tidy.ExpectFile); err != nil {
		return err
	}
	if err := usableRepository(tidy.Repository); err != nil {
		return err
	}
	if err := tidy.validateModeReachesTheRepository(); err != nil {
		return err
	}
	if err := usableClaim(tidy.Claim); err != nil {
		return err
	}
	if _, err := ReadRoute(tidy.Escalation); err != nil {
		return err
	}
	if err := tidy.validateRequires(); err != nil {
		return err
	}
	return tidy.validateLabels()
}

// validateMode holds a mode to the words the model runtime knows, and offers those words back.
func (d Declaration) validateMode() error {
	if d.Mode == "" {
		return nil
	}
	if _, known := model.PermissionModeNamed(d.Mode); !known {
		return fmt.Errorf("job runs in mode %q, which is not a mode; use %s",
			d.Mode, strings.Join(model.PermissionModesOffered(), ", "))
	}
	return nil
}

// validateModeReachesTheRepository holds the mode against the repository, after each has been held to
// its own shape, so a job that got the address wrong is told about the address.
//
// A job that names no mode is admitted here and held again at the control plane. What an unnamed mode
// runs in is the system's own configuration, which a declaration does not hold: refusing it here
// would refuse every job on a crew that already runs its work in the mode that can push.
func (d Declaration) validateModeReachesTheRepository() error {
	if d.Mode == "" {
		return nil
	}
	return UsableModeFor(d.Repository, d.NamedMode())
}

func (d Declaration) validateLabels() error {
	if len(d.Labels) > LabelCount {
		return fmt.Errorf("job carries %d labels and the ceiling is %d: labels are for finding job later, "+
			"so keep the few that a search would use", len(d.Labels), LabelCount)
	}
	for key, value := range d.Labels {
		if key == "" {
			return fmt.Errorf("a label has no key, and a label nobody can name is a label nobody can search for")
		}
		if len(key) > LabelLimit {
			return fmt.Errorf("the label key %q is %d characters and the ceiling is %d: shorten it",
				key, len(key), LabelLimit)
		}
		if len(value) > LabelLimit {
			return fmt.Errorf("the value of label %q is %d characters and the ceiling is %d: shorten it, "+
				"and put anything longer in the brief", key, len(value), LabelLimit)
		}
	}
	return nil
}

// NamedMode returns the mode a declaration names, in the runtime's own spelling. Empty leaves a session in
// the mode it is born in.
func (d Declaration) NamedMode() string {
	if d.Mode == "" {
		return ""
	}
	named, _ := model.PermissionModeNamed(strings.TrimSpace(d.Mode))
	return named
}

// usableExpectFile refuses a path that would be checked somewhere other than the job's own room.
//
// The path is read inside the session's working directory, so an absolute one, or one that climbs
// out of it, asks about a file the job never touched.
func usableExpectFile(path string) error {
	if path == "" {
		return nil
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("job expects the file %q, and the path is read inside the session's working "+
			"directory; write it relative, as package.json", path)
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return fmt.Errorf("job expects the file %q, which climbs out of the session's working directory; "+
				"name a path inside it", path)
		}
	}
	return nil
}

// Terminal says whether nothing moves a job out of this phase.
func Terminal(phase string) bool {
	switch phase {
	case PhaseDone, PhaseFailed, PhaseStopped:
		return true
	default:
		return false
	}
}

// Phases are every phase a job can be in, in the order it moves through them.
func Phases() []string {
	return []string{PhasePending, PhaseWaiting, PhaseRunning, PhaseAsking, PhaseDone, PhaseFailed, PhaseStopped}
}

// LivePhases are the phases a job has not ended in. It is Phases without the terminal ones, in one
// place so a store reading "the jobs that are still going" cannot spell that set its own way.
func LivePhases() []string {
	live := make([]string, 0, len(Phases()))
	for _, phase := range Phases() {
		if !Terminal(phase) {
			live = append(live, phase)
		}
	}
	return live
}

// KnownPhase says whether a word is a phase, which is what a listing filter is held to.
func KnownPhase(phase string) bool {
	for _, known := range Phases() {
		if phase == known {
			return true
		}
	}
	return false
}

// Cycle walks what a job would wait for and reports the pair that closes a loop.
//
// A caller cannot reach this today: the system assigns every identifier and `after` must name job
// that already exists, so a declaration cannot be waited on by anything yet. It is the guard for the
// first thing that rewrites `after`, and a loop of jobs waiting on one another would otherwise sit
// pending forever with nothing saying why.
func Cycle(id string, after []string, dependsOn func(string) []string) (from, to string, found bool) {
	seen := map[string]bool{}
	var walk func(at string) (string, string, bool)
	walk = func(at string) (string, string, bool) {
		for _, next := range dependsOn(at) {
			if next == id {
				return at, id, true
			}
			if seen[next] {
				continue
			}
			seen[next] = true
			if from, to, found := walk(next); found {
				return from, to, true
			}
		}
		return "", "", false
	}

	for _, next := range after {
		if next == id {
			return id, id, true
		}
		if seen[next] {
			continue
		}
		seen[next] = true
		if from, to, found := walk(next); found {
			return from, to, true
		}
	}
	return "", "", false
}

// Limits is what a workspace lets its sessions declare.
//
// The workspace carries the ceiling and a role carries the grant. A role alone would give the same
// power in every workspace it reaches, including the ones nobody thought about; a workspace alone
// would give no review, because a limit is not a file anybody reads in a pull request.
type Limits struct {
	Workspace string
	// MaxDepth is how deep the tree of jobs may go. Zero, the default, means no session in this
	// workspace may declare a job at all.
	MaxDepth int
	// MaxRunning is how many jobs may run at once here. Zero is unset.
	MaxRunning int
	// BudgetTokens is what a tree may spend when its root declares none. Zero is unset.
	BudgetTokens int64
	// LeaseSeconds is how long a controller holds a job here. Zero takes the system's own
	// measured default.
	LeaseSeconds int
	// ReclaimSeconds is how long a settled session here keeps its container before the system takes it
	// back. Zero is unset, and it ships unset: see Reclaim.
	ReclaimSeconds int
	// ArchiveSeconds is how long a reclaimed session here waits before the system files it away. Zero
	// is unset, and it ships unset for the same reason.
	ArchiveSeconds int
	// RequestMemoryBytes and RequestProcessor are what one sandbox in this workspace asks the machine
	// for. The system adds up what it has placed and admits a job only where its runtime still has that
	// much unallocated, so a workspace whose jobs run heavier says so here rather than being counted
	// the same as every other. Zero on either takes the system's own measured request.
	RequestMemoryBytes int64
	RequestProcessor   int
}

// Request is what one sandbox in this workspace asks for, or the system's own where it says nothing.
func (l Limits) Request(standard capacity.Request) capacity.Request {
	return capacity.Request{Memory: l.RequestMemoryBytes, Processor: l.RequestProcessor}.Or(standard)
}

// Lease is how long a hold lasts in this workspace, or the default where the workspace says nothing.
func (l Limits) Lease(standard time.Duration) time.Duration {
	if l.LeaseSeconds > 0 {
		return time.Duration(l.LeaseSeconds) * time.Second
	}
	return standard
}

// Reclaim is how long a settled session here keeps its container. Zero means unset, and unset means
// the controller reclaims nothing in this workspace, however long it runs.
//
// There is no default beside it, unlike Lease. A lease is a property of the loop and the loop was
// measured; a reclaim time is a property of how an operator uses a conversation, and nothing has
// measured that. Three runs would set it and none of them has happened, so the system refuses a number
// it was never given rather than choosing one. Section 11 of docs/ORCHESTRATION.md names the runs.
func (l Limits) Reclaim() time.Duration { return seconds(l.ReclaimSeconds) }

// Archive is how long a reclaimed session here waits before it is filed away. Zero means unset, and
// unset means the controller archives nothing in this workspace.
func (l Limits) Archive() time.Duration { return seconds(l.ArchiveSeconds) }

// seconds reads a configured number of seconds as a length of time, and reads anything at or below
// zero as unset. Negative is unset rather than an error here: the refusal belongs where the number is
// written, and a limit that arrives negative anyway must not turn into a time in the past that
// reclaims everything at once.
func seconds(count int) time.Duration {
	if count <= 0 {
		return 0
	}
	return time.Duration(count) * time.Second
}

// TidyRequires puts what a caller required into one order, with the blanks and the repeats gone, so
// what a job requires does not depend on the order somebody typed it in. It is the rule a
// role already applies to what it receives.
func TidyRequires(required []string) []string {
	seen := make(map[string]bool, len(required))
	tidy := make([]string, 0, len(required))
	for _, one := range required {
		trimmed := strings.TrimSpace(one)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		tidy = append(tidy, trimmed)
	}
	if len(tidy) == 0 {
		return nil
	}
	sort.Strings(tidy)
	return tidy
}

// validateRequires holds what a caller required to the words the system hands out, and offers those
// words back. A word nobody assembles is a boundary that quietly means nothing, and a boundary that
// means nothing looks exactly like one that holds.
func (d Declaration) validateRequires() error {
	for _, material := range d.Requires {
		if !handedOut(material) {
			return fmt.Errorf("job requires %q, which is not material the system hands out; it is one of: %s",
				material, strings.Join(role.Material, ", "))
		}
	}
	return nil
}

func handedOut(material string) bool {
	for _, one := range role.Material {
		if one == material {
			return true
		}
	}
	return false
}

// Receiver is a role, as much of one as this rule needs. It is an interface so the rule lives in this
// package, which holds no role, and is applied wherever one is read.
type Receiver interface {
	// Gets says whether the role receives a kind of material.
	Gets(material string) bool
}

// Unreceived is the first material a job requires that its role does not receive, and
// empty where the boundary holds.
//
// A job that requires nothing, and a job that runs as no role, are both empty: this changes nothing
// for either.
func Unreceived(required []string, held Receiver) string {
	if held == nil {
		return ""
	}
	for _, material := range TidyRequires(required) {
		if !held.Gets(material) {
			return material
		}
	}
	return ""
}

// RefusedMaterial is what the system says to a job whose role cannot be given the material it requires.
// It names the role, the material the role does not receive, and the two ways out, because a refusal
// a caller cannot act on is a refusal that sends them looking.
func RefusedMaterial(named, material string) string {
	return fmt.Sprintf("this job requires %s and the %s role does not receive it, so the session would be "+
		"asked to do the work without it. Add %s to what the %s role receives and import it again, or "+
		"declare the job without %s.", material, named, material, named, material)
}

// TidySentence is the one sentence as it is stored: the space around it comes off, and any run of
// space inside it becomes one space.
//
// A person pastes this out of a document, so it arrives wrapped over two lines often enough to
// matter. It is one line everywhere it is read, in a listing, on `krewe job show`, and in front of a
// session, and a line break in the middle of it breaks all three.
func TidySentence(sentence string) string {
	return strings.Join(strings.Fields(sentence), " ")
}
