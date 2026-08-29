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

	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/role"
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
	// Every is how often the crew starts a run of this graph on its own. Zero means never: the
	// graph runs when a person asks for it, which is every graph until one says otherwise.
	Every time.Duration
	// Mode is what the run's turns may do, as the model spells it. Empty leaves the run in the mode
	// every session is born in.
	//
	// It belongs to the graph rather than to the operator starting the run, for the same reason the
	// schedule does: what an automation is allowed to do is versioned and reviewable beside what it
	// does. There is nowhere else to put it either, because the run's session does not exist until
	// the run starts, so `quay mode` has nothing to point at.
	Mode   string
	Limits Limits
	Nodes  map[string]Node
	Edges  []Edge
	// Start is the one node no edge points into, derived rather than declared so the file cannot
	// say one thing and the shape another.
	Start string
}

// Expect is what a dispatch node declares will show its task did the job.
//
// The crew checks it. That is the whole point of it: a task that could not do the work is not a
// failed task, so `result.failed` says only that the model did not error, and a model asked to read
// a file that is not there answers plausibly instead of stopping. Whichever of these is declared is
// checked; declaring neither is refused at import.
type Expect struct {
	// File is a path that must exist in the run's session after the task, relative to its working
	// directory. It is the strong one: nothing the model says can satisfy it.
	File string
	// Contains is a string the reply must carry. It is weaker, because it is still the model's own
	// prose, and it is here because some job has no file to point at.
	Contains string
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
	// waiting run costs nothing and survives the crew being restarted underneath it.
	For time.Duration
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
	mode := ""
	if declared := strings.TrimSpace(file.Mode); declared != "" {
		named, known := model.PermissionModeNamed(declared)
		if !known {
			return Graph{}, fmt.Errorf("flow: graph %s runs in mode %q, which is not a mode; use %s",
				file.Name, declared, strings.Join(model.PermissionModesOffered(), ", "))
		}
		mode = named
	}

	graph := Graph{Name: file.Name, Version: file.Version, Every: every, Mode: mode, Limits: limits, Nodes: map[string]Node{}}
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
		graph.Nodes[name] = Node{Type: node.Type, Prompt: node.Prompt, Role: roleName, On: node.On,
			For: waitFor, Text: node.Text, Expect: expect}
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
	for name, node := range graph.Nodes {
		if err := usableEdges(graph, name, node); err != nil {
			return Graph{}, err
		}
	}
	return graph, nil
}

// StartsOnTrigger says whether a run of this graph begins because something happened. It is the
// question the crew asks of a graph a trigger names, so a trigger cannot start an automation whose
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
// import one rather than by whoever runs the crew.
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
