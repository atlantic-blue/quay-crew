// Package work is the declared unit of intent: a caller writes a piece of work, and the crew keeps
// it.
//
// The record is the whole point. A caller declares what it wants, closes its terminal, and the
// intent is still there tomorrow, because it is a row rather than a list held in a process. Nothing
// here dispatches anything: a controller makes reality match the record, and that is its own slice.
//
// Almost every rule below is checked at the moment of the write, because a refusal that arrives
// hours later, inside a run, has nothing to point back at. The one exception is what a role
// receives, which is checked again at the dispatch: a role can be detached, imported again and
// attached again while work sits pending, so what the crew would put in front of a session is only
// settled at the moment it hands it over.
package work

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/role"
)

// The phases a piece of work moves through. They are the flow engine's words plus one, so a reader
// learns one vocabulary rather than two.
const (
	// PhasePending is declared and not started. Every piece of work opens here.
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
	// hold. Work that went quiet and work that was halted must never read the same.
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
	// LabelCount is how many labels a piece of work may carry.
	LabelCount = 16
	// LabelLimit is how long a label key or value may be, which is the ceiling Kubernetes puts on a
	// label value.
	LabelLimit = 63
)

// Work is one piece of declared intent and the status a controller keeps on it.
type Work struct {
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
	// Hands is the material this work cannot be done without, drawn from role.Material. It is the
	// other side of a role's boundary: the role says what it receives and this says what the work
	// needs, and where the two disagree the work is refused rather than run without it.
	Hands []string

	// What the crew assigned, and the caller may not.
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
	// SpentTokens is what this work's own session has cost.
	SpentTokens int64
	// ObservedVersion is the Version of the declaration the status describes. A controller that has
	// not caught up leaves this behind, and a reader can then tell a status that is current from one
	// that is stale.
	ObservedVersion int

	// LeaseOwner is the controller holding this work, and LeaseUntil is when its hold runs out.
	// They are the only status fields a reader should ignore: they say who is holding the work, not
	// what came of it. A lease that has run out is the signal that its holder went away.
	LeaseOwner string
	LeaseUntil *time.Time

	// TraceID is the trace this whole tree belongs to, minted at the root and inherited unchanged by
	// every descendant. ParentSpanID is the span the caller was inside when it declared this work,
	// empty for a root nothing was tracing.
	//
	// Both are on the row rather than in a process, which is what makes a trace survive the
	// controller that started the work: the context is in the declaration, the way a wait is a column
	// rather than a timer.
	TraceID      string
	ParentSpanID string

	CreatedAt  time.Time
	UpdatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// Event is one thing that happened to a piece of work. It is written in the same transaction as the
// row it describes, so the record and its history cannot disagree.
type Event struct {
	ID        string
	Kind      string
	Work      string
	Workspace string
	Project   string
	Parent    string
	Depth     int
	// Detail is a short line about what happened. It goes through the crew's redactor before it is
	// written.
	Detail string
	// TraceID is the trace this happened in, and the same value the work carries. A reader holding
	// one record reaches the trace without reading the work row first.
	TraceID    string
	OccurredAt time.Time
}

// The kinds of event the crew writes. The contract another service may one day depend on is wider:
// the two a lease writes belong to the slice that adds one.
const (
	EventDeclared = "work.declared"
	// EventClaimed and EventReleased are internal, and nothing outside should read them. A dashboard
	// counting work must not break because the crew changed how it leases.
	EventClaimed  = "work.claimed"
	EventReleased = "work.released"
	EventStarted  = "work.started"
	EventAnswered = "work.answered"
	EventFailed   = "work.failed"
	EventStopped  = "work.stopped"
	// EventAsked is written when a piece of work puts a question to a person. Nothing writes it yet:
	// asking is the slice that gives a session a credential to ask with. It is named here because the
	// kinds are a contract another service may switch on, and a consumer written against a list that
	// grows later has to be changed twice.
	EventAsked = "work.asked"
)

// Contract says whether a kind is one another service may depend on.
//
// The split is the useful part. A dashboard counting work must never break because the crew changed
// how it leases, and a dashboard counting leases has taken a dependency it was told not to take.
func Contract(kind string) bool {
	switch kind {
	case EventClaimed, EventReleased:
		return false
	default:
		return true
	}
}

// Filter narrows a listing. The zero value is every piece of work the crew holds.
type Filter struct {
	// Project wins over Workspace when both are set, being the narrower.
	Workspace string
	Project   string
	// Parent narrows to the children of one piece of work. Root narrows to work with no parent,
	// which cannot be said with Parent alone because empty means "do not narrow".
	Parent string
	Root   bool
	Phase  string
	// LabelKey and LabelValue narrow to work carrying one label. A key with no value matches any
	// value, which is how a caller finds everything it labelled at all.
	LabelKey   string
	LabelValue string
}

// Declaration is what a caller writes. Everything else on a piece of work is the crew's to assign.
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
	Hands          []string
	ID             string
	Parent         string
}

// Tidied is the declaration as it is stored: the space around the lines it will be read as comes off.
func (d Declaration) Tidied() Declaration {
	d.Title = strings.TrimSpace(d.Title)
	d.Brief = strings.TrimSpace(d.Brief)
	d.Role = strings.TrimSpace(d.Role)
	d.Mode = strings.TrimSpace(d.Mode)
	d.ExpectFile = strings.TrimSpace(d.ExpectFile)
	d.Hands = TidyHands(d.Hands)
	return d
}

// Validate refuses a declaration that could not run, with a sentence saying what to do instead.
//
// Everything here is decidable from the declaration alone. Whether the workspace, the project, the
// role and the work it waits for exist is the control plane's question, because only the store can
// answer it.
func (d Declaration) Validate() error {
	tidy := d.Tidied()
	switch {
	case tidy.ID != "":
		return fmt.Errorf("work carries an identifier of its own, and the crew assigns the identifier: "+
			"declare it without one and read the identifier back from the answer (you sent %q)", tidy.ID)
	case tidy.Parent != "":
		return fmt.Errorf("work carries a parent, and the parent comes from the credential the caller "+
			"presented rather than from the request: declare it without one (you sent %q)", tidy.Parent)
	case tidy.Title == "":
		return fmt.Errorf("work needs a title, which is the one line a listing shows: say what this work is, in a few words")
	case len(tidy.Title) > TitleLimit:
		return fmt.Errorf("the title is %d bytes and the ceiling is %d, because it is the line a listing shows: "+
			"put the detail in the brief", len(tidy.Title), TitleLimit)
	case tidy.Brief == "":
		return fmt.Errorf("work needs a brief, which is what the session is asked to do: say what has to happen")
	case len(tidy.Brief) > BriefLimit:
		return fmt.Errorf("the brief is %d bytes and the ceiling is %d, because a brief nobody reads to the end "+
			"is a brief nobody follows: split it into more than one piece of work", len(tidy.Brief), BriefLimit)
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
	if err := tidy.validateHands(); err != nil {
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
		return fmt.Errorf("work runs in mode %q, which is not a mode; use %s",
			d.Mode, strings.Join(model.PermissionModesOffered(), ", "))
	}
	return nil
}

func (d Declaration) validateLabels() error {
	if len(d.Labels) > LabelCount {
		return fmt.Errorf("work carries %d labels and the ceiling is %d: labels are for finding work later, "+
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

// usableExpectFile refuses a path that would be checked somewhere other than the work's own room.
//
// The path is read inside the session's working directory, so an absolute one, or one that climbs
// out of it, asks about a file the work never touched.
func usableExpectFile(path string) error {
	if path == "" {
		return nil
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("work expects the file %q, and the path is read inside the session's working "+
			"directory; write it relative, as package.json", path)
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return fmt.Errorf("work expects the file %q, which climbs out of the session's working directory; "+
				"name a path inside it", path)
		}
	}
	return nil
}

// Terminal says whether nothing moves work out of this phase.
func Terminal(phase string) bool {
	switch phase {
	case PhaseDone, PhaseFailed, PhaseStopped:
		return true
	default:
		return false
	}
}

// Phases are every phase a piece of work can be in, in the order it moves through them.
func Phases() []string {
	return []string{PhasePending, PhaseWaiting, PhaseRunning, PhaseAsking, PhaseDone, PhaseFailed, PhaseStopped}
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

// Cycle walks what a piece of work would wait for and reports the pair that closes a loop.
//
// A caller cannot reach this today: the crew assigns every identifier and `after` must name work
// that already exists, so a declaration cannot be waited on by anything yet. It is the guard for the
// first thing that rewrites `after`, and a loop of work waiting on itself would otherwise sit
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
	// MaxDepth is how deep the tree of work may go. Zero, the default, means no session in this
	// workspace may declare work at all.
	MaxDepth int
	// MaxRunning is how many pieces of work may run at once here. Zero is unset.
	MaxRunning int
	// BudgetTokens is what a tree may spend when its root declares none. Zero is unset.
	BudgetTokens int64
	// LeaseSeconds is how long a controller holds a piece of work here. Zero takes the crew's own
	// measured default.
	LeaseSeconds int
}

// Lease is how long a hold lasts in this workspace, or the default where the workspace says nothing.
func (l Limits) Lease(standard time.Duration) time.Duration {
	if l.LeaseSeconds > 0 {
		return time.Duration(l.LeaseSeconds) * time.Second
	}
	return standard
}

// TidyHands puts what a caller handed into one order, with the blanks and the repeats gone, so what
// a piece of work hands does not depend on the order somebody typed it in. It is the rule a role
// already applies to what it receives.
func TidyHands(handed []string) []string {
	seen := make(map[string]bool, len(handed))
	tidy := make([]string, 0, len(handed))
	for _, one := range handed {
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

// validateHands holds what a caller handed to the words the crew hands out, and offers those words
// back. A word nobody assembles is a boundary that quietly means nothing, and a boundary that means
// nothing looks exactly like one that holds.
func (d Declaration) validateHands() error {
	for _, material := range d.Hands {
		if !handable(material) {
			return fmt.Errorf("work is handed %q, which is not material the crew hands out; it is one of: %s",
				material, strings.Join(role.Material, ", "))
		}
	}
	return nil
}

func handable(material string) bool {
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

// Unreceived is the first material a piece of work hands that its role does not receive, and empty
// where the boundary holds.
//
// Work that hands nothing, and work that runs as no role, are both empty: this changes nothing for
// either.
func Unreceived(handed []string, held Receiver) string {
	if held == nil {
		return ""
	}
	for _, material := range TidyHands(handed) {
		if !held.Gets(material) {
			return material
		}
	}
	return ""
}

// RefusedMaterial is what the crew says to work whose role cannot be given the material it hands. It
// names the role, the material the role does not receive, and the two ways out, because a refusal a
// caller cannot act on is a refusal that sends them looking.
func RefusedMaterial(named, material string) string {
	return fmt.Sprintf("this work is handed %s and the %s role does not receive it, so the session would be "+
		"asked to do the work without it. Add %s to what the %s role receives and import it again, or "+
		"declare the work without %s.", material, named, material, named, material)
}
