package flow

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// Run statuses. Failed is a run the graph could not carry any further, which is different from a
// task that failed: a graph that branches on result.failed handles the second without ever being
// the first.
const (
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
	// StatusWaiting is a run sitting on a wait node until its time comes. It is a live run that
	// costs nothing: the due time is a column a poller reads, not a timer somebody is holding, so
	// a system restarted underneath a waiting run still resumes it.
	StatusWaiting = "waiting"
	// StatusAsking is a run waiting on a person. Nothing but an answer moves it: no timer, no
	// poller, because an automation nobody answered must never carry on by itself.
	StatusAsking = "asking"
	// StatusStopped is a run brought to a halt rather than one that finished: it hit a limit, or
	// somebody stopped it. A run that went quiet and a run that was halted must never read the
	// same, which is why this is its own word and why it carries a reason.
	StatusStopped = "stopped"
)

// Event kinds. Started begins a run; a finished task is the result of the one dispatch the run was
// waiting on.
const (
	EventStarted      = "started"
	EventTaskFinished = "task.finished"
	// EventDue is a wait whose time has come, delivered by the poller.
	EventDue = "due"
	// EventAnswered is the operator answering a question the run put to them.
	EventAnswered = "answered"
)

// Command kinds. Dispatch sends a task to the run's own session; archive puts that session away when
// the run ends, because a finished run must not leave a container behind.
const (
	CommandDispatch = "dispatch"
	CommandArchive  = "archive"
)

// Run is one instance of a graph: where it is, what it knows, and which graph version it is pinned
// to, so editing a file cannot change an automation that is halfway through.
type Run struct {
	ID           string
	Workspace    string
	Project      string
	GraphName    string
	GraphVersion int
	// Node is where the run sits. Empty before the started event places it.
	Node   string
	Status string
	// State is the run's small memory: what tasks answered, what the trigger carried, what a
	// prompt template reads.
	State map[string]string
	// Attempts counts dispatches per node, which is one third of the idempotency key run, node and
	// attempt.
	Attempts map[string]int
	// Transitions is how many movements the run has taken, counted against the graph's cap so a
	// cycling graph terminates on its own.
	Transitions int
	// Spent is what the run's own conversation has cost in tokens, read from the model's transcript
	// before each dispatch and checked against the graph's ceiling.
	Spent int64
	// Reason says why a stopped run stopped. Empty on a run that is running or that finished.
	Reason string
	// Question is what an asking run is waiting to be told, rendered from its state. Empty on a run
	// that is not asking anything.
	Question string
	// DueAt is when a waiting run should be looked at again, as the store holds it. The poller
	// reads it; the reducer never does, because a pure function has no clock.
	DueAt *time.Time
	// DueIn is how long the wait the run just reached lasts. The engine turns it into a due time
	// on the row; the reducer stays pure and never reads a clock.
	DueIn time.Duration
}

// The state a run keeps about the product, which is the one sentence it serves.
//
// Both are ordinary state keys, so a prompt renders the sentence with {{product}} and a choice can
// read either one without the graph needing an expression language.
const (
	// stateProduct is the sentence the run serves now. It opens as the graph's own and is replaced by
	// an answer at the first usable path, which is the whole point: the answer of no is a new
	// sentence, and the run carries on from it.
	stateProduct = "product"
	// stateProductAsked is "true" once the run has stopped for a person to use what it built. A run
	// stops once. A graph that loops back over the same step must not ask a question the operator
	// already answered.
	stateProductAsked = "product.asked"
)

// theAnswerThatCarriesOn is the answer at the first usable path that means the product is right.
// Anything else is read as the sentence the operator wanted instead.
const theAnswerThatCarriesOn = "yes"

// ErrNotASentence is what an answer at the first usable path could not be read as. It is the
// operator's typing rather than a fault in the system, so the run stays where it is and the caller
// is told what to type instead.
var ErrNotASentence = errors.New("flow: this answer is not a sentence a run can serve")

// askingWhetherItIsTheProduct is the one question a run puts at the first thing a person can open.
//
// It names the address and the sentence, so the answer is about the product rather than about the
// code. A person who is shown a change and asked whether it is correct answers about the code,
// because that is the only thing in front of them.
func askingWhetherItIsTheProduct(sentence, address string) string {
	return fmt.Sprintf("This is the first thing a person can open, and it is here: %s\n\n"+
		"It was built to serve one sentence, which is what a person does and what they get back: %s\n\n"+
		"Open it and use it. Does it do what that sentence says?\n\n"+
		"Answer %s and the run carries on. Answer with the sentence you wanted instead, and the run "+
		"replaces this one with yours and carries on from that. Answer about the product, not about "+
		"the code.", address, sentence, theAnswerThatCarriesOn)
}

// Event is one thing that happened to a run.
type Event struct {
	Kind string
	// Node is the node the event answers, so a late duplicate for a node the run already left is
	// refused rather than moving it twice.
	Node   string
	Reply  string
	Failed bool
	// Answer is what the operator said to an ask node.
	Answer string
	// Unmet says what the node claimed would prove the task worked, and did not. Empty when the node
	// claimed nothing or the claim held. The engine fills it in, because checking it touches the
	// world; what to do about it is here, where it can be read.
	Unmet string
}

// Command is one thing the engine must do on the run's behalf. The reducer never does it: it
// touches no Docker, no Postgres and no model, which is what keeps it a table test.
type Command struct {
	Kind    string
	Node    string
	Attempt int
	Prompt  string
	// Role is who does this dispatch, empty for the run's own session. A dispatch that names one
	// runs in a session of that role rather than in the run's, which is what makes the boundary
	// possible: a conversation that never received the material cannot have read it.
	Role string
}

// Advance is the whole of the flow logic: a pure function from a run and an event to the next run
// and the commands to carry out.
func Advance(graph Graph, run Run, event Event) (Run, []Command, error) {
	// A waiting run is live: it moves when its time comes and on nothing else.
	if run.Status == StatusWaiting {
		if event.Kind != EventDue {
			return run, nil, fmt.Errorf("flow: run %s is waiting on %s, so it moves when its time comes and not on a %s", run.ID, run.Node, event.Kind)
		}
	} else if run.Status == StatusAsking {
		// Only a person moves this one. A timer must never answer a question, or an automation
		// nobody replied to would carry on and do the thing it was asking permission for.
		if event.Kind != EventAnswered {
			return run, nil, fmt.Errorf("flow: run %s is waiting to be told %q, so only an answer moves it, not a %s", run.ID, run.Question, event.Kind)
		}
	} else if run.Status != StatusRunning {
		return run, nil, fmt.Errorf("flow: run %s is %s, and a run that ended does not move", run.ID, run.Status)
	}
	// The brakes are checked before the movement rather than after it, so the dispatch that would
	// cross a line is never made and never paid for.
	if stopped, halted := brake(graph, run); halted {
		return stopped, nil, nil
	}
	run.Transitions++
	if run.State == nil {
		run.State = map[string]string{}
	}
	if run.Attempts == nil {
		run.Attempts = map[string]int{}
	}

	switch event.Kind {
	case EventAnswered:
		if event.Node != run.Node || run.Status != StatusAsking {
			return run, nil, fmt.Errorf("flow: an answer arrived for node %s while run %s sits %s on %s; two answers to one question must not move a run twice", event.Node, run.ID, run.Status, run.Node)
		}
		// Under one name, so an ordinary choice reads a person's decision and the graph needs no
		// expression language to branch on it.
		run.State["answer"] = event.Answer
		// At the first usable path the question was about the product, so anything but yes is the
		// sentence the operator wanted instead. The run carries on either way: an answer of no there
		// costs the one step that was built, and the same answer at the end costs the whole run.
		if graph.Nodes[run.Node].Usable {
			instead, err := theSentenceInstead(event.Answer)
			if err != nil {
				return run, nil, err
			}
			if instead != "" {
				run.State[stateProduct] = instead
			}
		}
		next, err := follow(graph, run.Node, "")
		if err != nil {
			return run, nil, err
		}
		run.Status, run.Question = StatusRunning, ""
		return settle(graph, run, next)
	case EventDue:
		if event.Node != run.Node || run.Status != StatusWaiting {
			return run, nil, fmt.Errorf("flow: a wait came due for node %s while run %s sits %s on %s; a poller that fired twice must not move a run twice", event.Node, run.ID, run.Status, run.Node)
		}
		next, err := follow(graph, run.Node, "")
		if err != nil {
			return run, nil, err
		}
		run.Status, run.DueIn = StatusRunning, 0
		return settle(graph, run, next)
	case EventStarted:
		if run.Node != "" {
			return run, nil, fmt.Errorf("flow: run %s already started, at node %s", run.ID, run.Node)
		}
		return settle(graph, run, graph.Start)
	case EventTaskFinished:
		if event.Node != run.Node {
			return run, nil, fmt.Errorf("flow: a result arrived for node %s while run %s sits on %s; a late duplicate must not move a run twice", event.Node, run.ID, run.Node)
		}
		node, known := graph.Nodes[run.Node]
		if !known || node.Type != NodeDispatch {
			return run, nil, fmt.Errorf("flow: run %s sits on %s, which is not a dispatch, so no task result belongs to it", run.ID, run.Node)
		}
		run.State["result.reply"] = event.Reply
		// True when the model errored and true when the step did not do what the node said would show
		// it worked, because both are the step failing to do its job and the field is called failed. It
		// used to carry only the first, so a run halted over an unmet claim read `result.failed false`
		// next to the sentence saying it stopped, and the two contradicted each other on the same
		// screen. Nothing branches differently for it: an unmet claim stops the run before any edge is
		// followed, so no choice node can ever see this case.
		run.State["result.failed"] = fmt.Sprintf("%t", event.Failed || event.Unmet != "")
		// A step that did not do what the graph said would show it worked stops the run, rather than
		// carrying on down the edge a reply happens to be sitting on. There is no recovery the system
		// could pick: it knows the job did not happen and it does not know why. A run that walks its
		// success path through job that never happened is worse than one that halts, because the
		// summary it ends with is the model's plausible account of it.
		if event.Unmet != "" {
			// The claim and the finding are two keys, not one. `result.expected` held the finding, so
			// reading a stopped run gave the same sentence twice and never said what the graph wanted.
			if expect := node.Expect; expect != nil {
				run.State["result.expected"] = expect.Declared()
			}
			run.State["result.unmet"] = event.Unmet
			run.Status = StatusStopped
			run.Reason = fmt.Sprintf("stopped at %s, which did not do what the graph says proves it worked: %s",
				run.Node, event.Unmet)
			return run, nil, nil
		}
		if putDown := theFirstUsableStop(graph, &run, event.Reply); putDown {
			return run, nil, nil
		}
		next, err := follow(graph, run.Node, "")
		if err != nil {
			return run, nil, err
		}
		return settle(graph, run, next)
	default:
		return run, nil, fmt.Errorf("flow: run %s was handed an event of kind %q", run.ID, event.Kind)
	}
}

// theFirstUsableStop puts the run down at the first thing a person can open, and says whether it
// put it down.
//
// This is the moment the whole feature exists for. A run that builds something a person can open
// used to build all of it, pass every check, and be opened two days later by an operator who could
// not use it. The stop costs one step. The same answer at the end costs the run.
//
// It stops once. A graph that loops back over the step must not put a question the operator has
// already answered, so the run remembers that it asked rather than counting attempts: an answer of
// no is meant to send the work round again, and the second time round the sentence is the new one.
//
// A step that replied with no address stops the run instead. A question naming an address a person
// cannot open is a question about nothing, and the run walking on would be a run whose one gate
// passed by being empty.
func theFirstUsableStop(graph Graph, run *Run, reply string) bool {
	node, known := graph.Nodes[run.Node]
	if !known || !node.Usable || run.State[stateProductAsked] == "true" {
		return false
	}
	address := strings.TrimSpace(reply)
	if address == "" {
		run.Status = StatusStopped
		run.Reason = fmt.Sprintf("stopped at %s, which is the first thing a person can open and replied "+
			"with no address, so the operator would be asked about something they cannot open; have the "+
			"step reply with the address", run.Node)
		return true
	}
	sentence := run.State[stateProduct]
	if sentence == "" {
		sentence = graph.Product
	}
	run.State[stateProduct], run.State[stateProductAsked] = sentence, "true"
	run.Status, run.Question = StatusAsking, askingWhetherItIsTheProduct(sentence, address)
	return true
}

// theSentenceInstead is what an answer at the first usable path replaces the sentence with, and the
// refusal where it could not be read as one. Empty means the answer was yes and nothing changes.
//
// An empty answer is refused rather than read as a yes. Silence taking the product further is the
// failure this whole gate exists to stop, and it is the same rule an ask node already keeps: nothing
// but an answer moves a run that is asking.
func theSentenceInstead(answer string) (string, error) {
	tidy := job.TidySentence(answer)
	if strings.EqualFold(tidy, theAnswerThatCarriesOn) {
		return "", nil
	}
	switch {
	case tidy == "":
		return "", fmt.Errorf("%w: the answer is empty, and the run is waiting to be told whether what it "+
			"built is the product: answer %s if it does what the sentence says, or answer with the sentence "+
			"you wanted instead", ErrNotASentence, theAnswerThatCarriesOn)
	case len(tidy) > job.ProductLimit:
		return "", fmt.Errorf("%w: this answer is %d bytes and the sentence it replaces may be %d: say what "+
			"somebody does and what they get back, and leave the rest to the steps that build it",
			ErrNotASentence, len(tidy), job.ProductLimit)
	}
	return tidy, nil
}

// brake stops a run that has reached a limit, saying which one in words the operator can act on.
//
// Both limits are the same idea: an automation runs with nobody watching, so it has to be able to
// stop itself. The transition cap catches a graph that cycles; the token ceiling catches a graph
// that does not cycle but whose tasks are expensive.
func brake(graph Graph, run Run) (Run, bool) {
	if run.Transitions >= graph.Limits.Transitions {
		run.Status = StatusStopped
		run.Reason = fmt.Sprintf("stopped after %d transitions, which is the cap graph %s declares; raise limits.transitions if the automation genuinely needs more steps",
			run.Transitions, graph.Name)
		return run, true
	}
	if graph.Limits.Tokens > 0 && run.Spent >= graph.Limits.Tokens {
		run.Status = StatusStopped
		run.Reason = fmt.Sprintf("stopped having spent %d tokens against the ceiling of %d that graph %s declares; raise limits.tokens if this is the cost of the job",
			run.Spent, graph.Limits.Tokens, graph.Name)
		return run, true
	}
	return run, false
}

// settle places the run on a node and keeps moving while the node is pure, so a chain of choices
// costs one advance. It stops on a dispatch, whose command it returns, or on done. The loop is
// bounded by the graph's own size: a pure cycle would otherwise spin forever, and a graph that
// loops without a dispatch in the loop is a graph that computes nothing.
func settle(graph Graph, run Run, at string) (Run, []Command, error) {
	for range len(graph.Nodes) + 1 {
		if at == DoneNode {
			run.Node = DoneNode
			run.Status = StatusDone
			return run, []Command{{Kind: CommandArchive}}, nil
		}
		node, known := graph.Nodes[at]
		if !known {
			return run, nil, fmt.Errorf("flow: run %s was routed to %q, which is not a node", run.ID, at)
		}
		run.Node = at
		switch node.Type {
		case NodeDispatch:
			run.Attempts[at]++
			return run, []Command{{
				Kind:    CommandDispatch,
				Node:    at,
				Attempt: run.Attempts[at],
				Prompt:  render(node.Prompt, run.State),
				Role:    node.Role,
			}}, nil
		case NodeWait:
			// The run is put down here rather than pushed on: it says how long to leave it, asks
			// for nothing, and the engine writes a due time the poller will read.
			run.Status, run.DueIn = StatusWaiting, node.For
			return run, nil, nil
		case NodeAsk:
			// Put down the way a wait is, but woken by a person rather than by the clock.
			run.Status, run.Question = StatusAsking, render(node.Text, run.State)
			return run, nil, nil
		case NodeTrigger:
			// Pure, and the run walks straight through it. The trigger arrived before the run
			// existed and its payload is already the run's opening state, so there is nothing left
			// here to wait for. It is the entry marker, not a step.
			next, err := follow(graph, at, "")
			if err != nil {
				return run, nil, err
			}
			at = next
		case NodeChoice:
			answer := "true"
			for key, want := range node.On {
				if run.State[key] != want {
					answer = "false"
				}
			}
			next, err := follow(graph, at, answer)
			if err != nil {
				return run, nil, err
			}
			at = next
		default:
			return run, nil, fmt.Errorf("flow: node %s has type %q, which the reducer cannot advance", at, node.Type)
		}
	}
	return run, nil, fmt.Errorf("flow: run %s cycled through %d pure nodes without reaching a dispatch or the end", run.ID, len(graph.Nodes)+1)
}

// follow finds the one edge out of a node with the given label.
func follow(graph Graph, from, when string) (string, error) {
	for _, edge := range graph.Edges {
		if edge.From == from && edge.When == when {
			return edge.To, nil
		}
	}
	return "", fmt.Errorf("flow: node %s has no edge labeled %q", from, when)
}

// render fills {{key}} from the run's state. A key the state does not hold stays as typed, so a
// misspelling is visible in the session's own transcript rather than silently blank.
func render(prompt string, state map[string]string) string {
	out := prompt
	for key, value := range state {
		out = strings.ReplaceAll(out, "{{"+key+"}}", value)
	}
	return out
}
