// Package flow is the state machine that runs automations across sessions.
//
// Inside a session the model decides what happens next, and it is better at that than any diagram.
// Across sessions the operator wants the opposite: a decision written down where it can be read,
// tested and stopped before it spends anything. A graph is a deliberate restriction for legibility,
// not a programming language: the console can draw it, and the operator can see which node a run is
// sitting on while it moves.
//
// The substrate is Postgres, decided 9 August 2026: a run and its transitions are rows written in
// one transaction, so reconstructable is a guarantee rather than a sentence. The reducer here is
// pure, and everything that touches Docker, Postgres or the model lives beside it in the engine.
package flow

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/role"
	"gopkg.in/yaml.v3"
)

// DoneNode is the implicit end of every graph. An edge may lead to it without anybody declaring it,
// because every graph ends and making each author write the same node out teaches nothing.
const DoneNode = "done"

// Node types. Dispatch sends a task, choice branches on state without a side effect, wait puts the
// run down until its time comes, ask puts a question to a person, and done ends the run.
//
// NodeTrigger is the entry node of a graph that reacts. A run of such a graph begins because
// something happened rather than because a person or a schedule started it, and what the trigger
// carried is the run's opening state. It does nothing itself: by the time a run exists the trigger
// has already arrived, so the run settles through it onto the first node that does work.
const (
	NodeDispatch = "dispatch"
	NodeChoice   = "choice"
	NodeWait     = "wait"
	NodeAsk      = "ask"
	NodeTrigger  = "trigger"
	NodeDone     = "done"
)

// DefaultTransitions is how many movements a run may take when its graph declares no cap.
//
// A number rather than no limit, because an automation dispatches tasks with nobody watching and a
// cycling edge is a spend loop. Generous enough that no reasonable graph meets it by accident, and
// small enough that a runaway is a bill somebody shrugs at rather than one that ruins the week.
const DefaultTransitions = 100

// MinimumEvery is the shortest schedule a graph may declare.
//
// A number rather than a warning, because a schedule fast enough to overlap its own runs spends
// money as fast as the model can take it. Fifteen minutes is far below anything a real automation
// wants and far above the range where a graph is racing itself.
const MinimumEvery = 15 * time.Minute

// Limits are what a run may spend before it is stopped.
type Limits struct {
	// Transitions is how many movements a run may take. Always set: a graph that declares none
	// gets DefaultTransitions.
	Transitions int
	// Tokens is what the run's own conversation may cost, counting everything the model was sent
	// and sent back. Zero means no ceiling, because what is reasonable differs per automation and
	// a made up number would either stop real work or protect nothing.
	Tokens int64
}

// Graph is one automation as authored: named, versioned, and static enough to answer what it will
// do without running it.
type Graph struct {
	Name    string
	Version int
	// Every is how often the system starts a run of this graph on its own. Zero means never: the
	// graph runs when a person asks for it, which is every graph until one says otherwise.
	Every time.Duration
	// Mode is what the run's turns may do, as the model spells it. Every graph declares one, and a
	// graph that does not is refused at import.
	//
	// It belongs to the graph rather than to the operator starting the run, for the same reason the
	// schedule does: what an automation is allowed to do is versioned and reviewable beside what it
	// does. There is nowhere else to put it either, because the run's session does not exist until
	// the run starts, so `krewe mode` has nothing to point at.
	Mode string
	// Product is the one sentence a run of this graph serves, in a person's words: what somebody
	// does with what the run builds, and what they get back. It reaches the job carrying the run,
	// and every step under it carries the same one.
	//
	// A graph that stops at its first usable path declares one, because the question put to the
	// operator there names the sentence, and a question with no sentence in it is a question about
	// the code.
	Product string
	Limits  Limits
	Nodes   map[string]Node
	Edges   []Edge
	// Start is the one node no edge points into, derived rather than declared so the file cannot
	// say one thing and the shape another.
	Start string
}

// Expect is what a dispatch node declares will show its task did the job.
//
// The system checks it. That is the whole point of it: a model asked to read a file that is not there
// answers plausibly instead of stopping, so the reply is not evidence. Whichever of these is declared
// is checked; declaring neither is refused at import.
type Expect struct {
	// File is a path that must exist in the run's session after the task, relative to its working
	// directory. It is the strong one: nothing the model says can satisfy it.
	File string
	// Contains is a string the reply must carry. It is weaker, because it is still the model's own
	// prose, and it is here because some job has no file to point at.
	Contains string
}

// Declared is the claim in words, for a reader of a run that stopped over it.
//
// It is here because a run stopping used to record only what was found, and what was found reads as
// an opinion until the claim it answers is beside it: "pr-state.md is not in the session that did the
// work" leaves a reader unable to say whether the graph wanted that file or wanted something else
// entirely. The two are separate lines on the run for the same reason.
func (e Expect) Declared() string {
	var said []string
	if e.File != "" {
		said = append(said, fmt.Sprintf("the file %s", e.File))
	}
	if e.Contains != "" {
		said = append(said, fmt.Sprintf("a reply carrying %q", e.Contains))
	}
	return strings.Join(said, " and ")
}

// Node is one step.
type Node struct {
	Type string
	// Prompt is what a dispatch node says to the run's session, with {{key}} rendered from the
	// run's state.
	Prompt string
	// Role is the name of the role a dispatch runs as, empty for a step that runs in the run's own
	// session. A step that names a role runs in a session of its own, so the job is done by
	// somebody who has read only what the role declares.
	Role string
	// Expect is what shows this dispatch worked, or nil where the graph claims nothing.
	Expect *Expect
	// On is a choice node's condition: every named state key must equal its value for the choice
	// to answer true. Equality only, deliberately: accepting arbitrary expressions means owning a
	// language and a sandbox.
	On map[string]string
	// Text is what an ask node puts to the operator, with {{key}} rendered from the run's state so
	// a question can say what it is asking about.
	Text string
	// For is how long a wait node waits. It becomes a due time on the run, read by a poller, so a
	// waiting run costs nothing and survives the system being restarted underneath it.
	For time.Duration
	// Usable says this dispatch is the first thing a person can open. A run stops there once, shows
	// the address the step replied with beside the sentence the run serves, and asks whether it is
	// what they wanted.
	//
	// It is the moment where an answer of no is cheap. A run built a design document faithfully,
	// every check was green, and the operator opened it two days later and could not use it: the
	// same answer at the end cost the whole run, and here it costs one step.
	Usable bool
}

// Edge joins two nodes. When is the label a choice's answer must match, empty on the single edge
// out of a dispatch.
type Edge struct {
	From, To, When string
}

// graphFile is the YAML as authored.
type graphFile struct {
	Name    string `yaml:"name"`
	Version int    `yaml:"version"`
	Mode    string `yaml:"mode"`
	Product string `yaml:"product"`
	Limits  struct {
		Transitions *int  `yaml:"transitions"`
		Tokens      int64 `yaml:"tokens"`
	} `yaml:"limits"`
	On struct {
		Every string `yaml:"every"`
	} `yaml:"on"`
	Nodes map[string]struct {
		Type   string            `yaml:"type"`
		Prompt string            `yaml:"prompt"`
		Role   string            `yaml:"role"`
		On     map[string]string `yaml:"on"`
		For    string            `yaml:"for"`
		Text   string            `yaml:"text"`
		Usable bool              `yaml:"usable"`
		Expect *struct {
			File     string `yaml:"file"`
			Contains string `yaml:"contains"`
		} `yaml:"expect"`
	} `yaml:"nodes"`
	Edges [][]string `yaml:"edges"`
}

// Parse reads a graph and refuses one a run could fall off. Every refusal here is one that would
// otherwise surface in the middle of a run, hours later, with nothing pointing back at the file.
func Parse(source []byte) (Graph, error) {
	var file graphFile
	if err := yaml.Unmarshal(source, &file); err != nil {
		return Graph{}, fmt.Errorf("flow: the graph does not read as yaml: %w", err)
	}
	if strings.TrimSpace(file.Name) == "" {
		return Graph{}, fmt.Errorf("flow: a graph needs a name")
	}
	if file.Version < 1 {
		return Graph{}, fmt.Errorf("flow: graph %s needs a version of 1 or more, so a run can be pinned to the one it started with", file.Name)
	}
	if len(file.Nodes) == 0 {
		return Graph{}, fmt.Errorf("flow: graph %s has no nodes", file.Name)
	}

	limits := Limits{Transitions: DefaultTransitions, Tokens: file.Limits.Tokens}
	if file.Limits.Transitions != nil {
		if *file.Limits.Transitions < 1 {
			return Graph{}, fmt.Errorf("flow: graph %s allows %d transitions, and a run of it could never take its first step; leave it out for the default of %d", file.Name, *file.Limits.Transitions, DefaultTransitions)
		}
		limits.Transitions = *file.Limits.Transitions
	}
	if limits.Tokens < 0 {
		return Graph{}, fmt.Errorf("flow: graph %s allows %d tokens; leave it out for no ceiling", file.Name, limits.Tokens)
	}

	var every time.Duration
	if declared := strings.TrimSpace(file.On.Every); declared != "" {
		parsed, err := time.ParseDuration(declared)
		if err != nil {
			return Graph{}, fmt.Errorf("flow: graph %s runs every %q, which is not a length of time; say 30m or 6h or 24h", file.Name, declared)
		}
		if parsed < MinimumEvery {
			return Graph{}, fmt.Errorf("flow: graph %s runs every %s, and the shortest schedule allowed is %s: an automation started faster than it finishes spends money as fast as the model can take it", file.Name, parsed, MinimumEvery)
		}
		every = parsed
	}

	// Refused here rather than at the first dispatch, which is the moment a run has already been
	// made, has a session of its own, and is about to spend money to find out the word was wrong.
	//
	// A graph that says nothing is refused for the same reason it is refused at all. A run works with
	// nobody there, so a step that stops to ask for approval waits on a person who will never answer:
	// it does not fail, it sits, and the bill is what the model spent finding that out. The mode a
	// session is born in is the system's choice about the sessions a person types into, and it is not an
	// answer to what an automation may do unwatched, so declaring one is the author's to do.
	offered := model.PermissionModesOffered()
	declared := strings.TrimSpace(file.Mode)
	if declared == "" {
		return Graph{}, fmt.Errorf("flow: graph %s does not say what its runs may do, and a run works with nobody there to approve a step; add a line `mode: %s` beside the name, where the modes are %s",
			file.Name, offered[len(offered)-1], strings.Join(offered, ", "))
	}
	mode, known := model.PermissionModeNamed(declared)
	if !known {
		return Graph{}, fmt.Errorf("flow: graph %s runs in mode %q, which is not a mode; use %s",
			file.Name, declared, strings.Join(offered, ", "))
	}

	sentence := job.TidySentence(file.Product)
	if len(sentence) > job.ProductLimit {
		return Graph{}, fmt.Errorf("flow: graph %s says a person gets a sentence of %d bytes and the ceiling is %d: write what somebody does and what they get back, and put the rest in the prompts",
			file.Name, len(sentence), job.ProductLimit)
	}

	graph := Graph{Name: file.Name, Version: file.Version, Every: every, Mode: mode, Product: sentence, Limits: limits, Nodes: map[string]Node{}}
	for name, node := range file.Nodes {
		var waitFor time.Duration
		if name == DoneNode {
			return Graph{}, fmt.Errorf("flow: graph %s declares a node called %s, which is the implicit end of every graph", file.Name, DoneNode)
		}
		switch node.Type {
		case NodeDispatch:
			if strings.TrimSpace(node.Prompt) == "" {
				return Graph{}, fmt.Errorf("flow: dispatch node %s has no prompt", name)
			}
		case NodeChoice:
			if len(node.On) == 0 {
				return Graph{}, fmt.Errorf("flow: choice node %s has no condition", name)
			}
			if err := usableOutcome(name, node.On); err != nil {
				return Graph{}, err
			}
		case NodeWait:
			if strings.TrimSpace(node.For) == "" {
				return Graph{}, fmt.Errorf("flow: wait node %s has no `for`, so a run would sit on it forever; say how long, as 30s or 10m or 2h", name)
			}
			waits, err := time.ParseDuration(node.For)
			if err != nil {
				return Graph{}, fmt.Errorf("flow: wait node %s waits %q, which is not a length of time; say 30s or 10m or 2h", name, node.For)
			}
			if waits <= 0 {
				return Graph{}, fmt.Errorf("flow: wait node %s waits %s, which is not a wait at all", name, waits)
			}
			waitFor = waits
		case NodeAsk:
			if strings.TrimSpace(node.Text) == "" {
				return Graph{}, fmt.Errorf("flow: ask node %s has no `text`, so the operator would be shown an empty question", name)
			}
		case NodeTrigger:
			// Nothing to declare. What a trigger carries is decided by whoever raises one, and the
			// row it is raised on names the graph, so the node says only that this graph reacts.
		default:
			return Graph{}, fmt.Errorf("flow: node %s has type %q; this graph engine knows %s, %s, %s, %s, %s and the implicit %s", name, node.Type, NodeDispatch, NodeChoice, NodeWait, NodeAsk, NodeTrigger, DoneNode)
		}

		// A role belongs to a dispatch, because a role is who does the work and nothing else in a graph
		// does any. Refused here rather than ignored: a role silently dropped from a choice reads as
		// a boundary that is in force and is not.
		roleName := strings.TrimSpace(node.Role)
		if roleName != "" {
			if node.Type != NodeDispatch {
				return Graph{}, fmt.Errorf("flow: %s node %s names the role %s, and only a %s does the work, so the role would do nothing",
					node.Type, name, roleName, NodeDispatch)
			}
			if !role.UsableName(roleName) {
				return Graph{}, fmt.Errorf("flow: dispatch node %s names the role %q, which is not a role name; a role is lowercase letters, digits and dashes",
					name, roleName)
			}
		}
		var expect *Expect
		if node.Expect != nil {
			if node.Type != NodeDispatch {
				return Graph{}, fmt.Errorf("flow: %s node %s says what proves it worked, and only a %s does the work", node.Type, name, NodeDispatch)
			}
			path, carries := strings.TrimSpace(node.Expect.File), node.Expect.Contains
			if path == "" && carries == "" {
				return Graph{}, fmt.Errorf("flow: dispatch node %s expects nothing; say `file:` for a path the task must leave behind, or `contains:` for something the reply must carry", name)
			}
			if err := usableExpectFile(name, path); err != nil {
				return Graph{}, err
			}
			expect = &Expect{File: path, Contains: carries}
		}
		// The first thing a person can open is something a step builds, and only a dispatch builds
		// anything. Refused rather than ignored, for the reason a dropped role is refused: a graph
		// that reads as stopping for a person and does not is worse than one that never claimed to.
		if node.Usable && node.Type != NodeDispatch {
			return Graph{}, fmt.Errorf("flow: %s node %s says it is the first thing a person can open, and only a %s builds anything, so the run would never stop there",
				node.Type, name, NodeDispatch)
		}
		graph.Nodes[name] = Node{Type: node.Type, Prompt: node.Prompt, Role: roleName, On: node.On,
			For: waitFor, Text: node.Text, Usable: node.Usable, Expect: expect}
	}

	pointedAt := map[string]bool{}
	for _, edge := range file.Edges {
		if len(edge) != 2 && len(edge) != 3 {
			return Graph{}, fmt.Errorf("flow: graph %s has an edge with %d parts, want [from, to] or [from, to, when]", file.Name, len(edge))
		}
		from, to := edge[0], edge[1]
		when := ""
		if len(edge) == 3 {
			when = edge[2]
		}
		if _, declared := graph.Nodes[from]; !declared {
			return Graph{}, fmt.Errorf("flow: an edge leaves %q, which is not a node of graph %s", from, file.Name)
		}
		if _, declared := graph.Nodes[to]; !declared && to != DoneNode {
			return Graph{}, fmt.Errorf("flow: an edge leads to %q, which is not a node of graph %s", to, file.Name)
		}
		graph.Edges = append(graph.Edges, Edge{From: from, To: to, When: when})
		pointedAt[to] = true
	}

	var starts []string
	for name := range graph.Nodes {
		if !pointedAt[name] {
			starts = append(starts, name)
		}
	}
	switch len(starts) {
	case 1:
		graph.Start = starts[0]
	case 0:
		return Graph{}, fmt.Errorf("flow: graph %s has no start: every node has an edge into it, so a run would have nowhere to begin", file.Name)
	default:
		return Graph{}, fmt.Errorf("flow: graph %s has %d nodes nothing points into (%s), so which one starts is ambiguous", file.Name, len(starts), strings.Join(starts, ", "))
	}

	if err := usableTrigger(graph); err != nil {
		return Graph{}, err
	}
	if err := theFirstUsablePath(graph); err != nil {
		return Graph{}, err
	}
	for name, node := range graph.Nodes {
		if err := usableEdges(graph, name, node); err != nil {
			return Graph{}, err
		}
	}
	return graph, nil
}

// StartsOnTrigger says whether a run of this graph begins because something happened. It is the
// question the system asks of a graph a trigger names, so a trigger cannot start an automation whose
// author never said it reacts.
func (g Graph) StartsOnTrigger() bool {
	return g.Nodes[g.Start].Type == NodeTrigger
}

// usableTrigger refuses a graph whose trigger node is not where a run of it begins.
//
// A trigger node in the middle would be a node a run walks onto after the thing that triggered it
// already happened, which is a graph that reads as reacting and does not. A second one is the same
// mistake twice: a graph has one way in, because a trigger row names the graph rather than a node.
func usableTrigger(graph Graph) error {
	var triggers []string
	for name, node := range graph.Nodes {
		if node.Type == NodeTrigger {
			triggers = append(triggers, name)
		}
	}
	sort.Strings(triggers)
	if len(triggers) > 1 {
		return fmt.Errorf("flow: graph %s has %d trigger nodes (%s), and a run begins at one node; keep the one a trigger arrives at",
			graph.Name, len(triggers), strings.Join(triggers, ", "))
	}
	if len(triggers) == 1 && triggers[0] != graph.Start {
		return fmt.Errorf("flow: graph %s begins at %s, and its trigger node %s has an edge into it; a trigger node is where a run begins, because the trigger arrived before the run existed",
			graph.Name, graph.Start, triggers[0])
	}
	return nil
}

// theFirstUsablePath refuses a graph whose stop for a person could not be put.
//
// Two nodes marked usable is the same mistake as two starts: a run stops once, at the first thing a
// person can open, and a second stop puts a question the operator has already answered. Which of
// the two comes first depends on the path a run takes, so the file says which one it is rather than
// leaving it to the run.
//
// A graph with no sentence is refused because the question is the sentence. Without it the operator
// is shown an address and asked whether it is right, which is the question that was never worth
// asking: right against what.
func theFirstUsablePath(graph Graph) error {
	var usable []string
	for name, node := range graph.Nodes {
		if node.Usable {
			usable = append(usable, name)
		}
	}
	sort.Strings(usable)
	if len(usable) > 1 {
		return fmt.Errorf("flow: graph %s marks %d nodes as the first thing a person can open (%s), and a run stops once; keep the earliest one",
			graph.Name, len(usable), strings.Join(usable, ", "))
	}
	if len(usable) == 1 && graph.Product == "" {
		return fmt.Errorf("flow: graph %s stops at %s for a person to use what it built, and says nothing about what that person gets, so the question would name an address and nothing to measure it against; add a line `product: <what somebody does and what they get back>` beside the name",
			graph.Name, usable[0])
	}
	return nil
}

// usableEdges refuses a node whose outgoing edges cannot be followed: a dispatch with anything but
// one unlabeled edge, or a choice missing one of its two answers.
func usableEdges(graph Graph, name string, node Node) error {
	var out []Edge
	for _, edge := range graph.Edges {
		if edge.From == name {
			out = append(out, edge)
		}
	}
	switch node.Type {
	case NodeDispatch, NodeWait, NodeAsk, NodeTrigger:
		if len(out) != 1 || out[0].When != "" {
			return fmt.Errorf("flow: %s node %s needs exactly one unlabeled edge out, and has %d", node.Type, name, len(out))
		}
	case NodeChoice:
		answers := map[string]bool{}
		for _, edge := range out {
			answers[edge.When] = true
		}
		if !answers["true"] || !answers["false"] {
			return fmt.Errorf("flow: choice node %s needs an edge for %q and an edge for %q", name, "true", "false")
		}
	}
	return nil
}

// usableExpectFile refuses a path that would be checked somewhere other than the run's own room.
//
// The path is read inside the session's working directory, so an absolute one or one that climbs out
// of it would be asking about a file the run never touched, and a graph is written by whoever may
// import one rather than by whoever runs the system.
func usableExpectFile(node, path string) error {
	if path == "" {
		return nil
	}
	if strings.HasPrefix(path, "/") {
		return fmt.Errorf("flow: dispatch node %s expects %q, and the path is read inside the session's working directory; write it relative, as package.json", node, path)
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return fmt.Errorf("flow: dispatch node %s expects %q, which climbs out of the session's working directory", node, path)
		}
	}
	return nil
}

// OutcomeKey is where the word a step ended on sits in a run's state, which is what a choice node
// branches on. The prose the step wrote sits beside it under result.reply, and the two are separate
// keys because two readings of one sentence give two answers.
const OutcomeKey = "result.outcome"

// usableOutcome refuses a choice that waits for an outcome the system never hands out.
//
// Held at import, while the author is looking, for the reason every other rule here is: a condition
// that can never be true is a graph that silently takes the other edge, hours later, with nothing
// pointing back at the file. The words are the job package's, so a graph and a session are offered
// one set rather than two.
func usableOutcome(name string, on map[string]string) error {
	want, branches := on[OutcomeKey]
	if !branches || job.KnownOutcome(want) {
		return nil
	}
	return fmt.Errorf("flow: choice node %s waits for the outcome %q, and a job ends on one of %s; "+
		"a condition nothing can meet takes the other edge every time", name, want,
		strings.Join(job.Outcomes(), ", "))
}
