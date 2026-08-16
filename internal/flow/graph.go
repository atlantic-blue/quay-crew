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
	"strings"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/model"
	"gopkg.in/yaml.v3"
)

// DoneNode is the implicit end of every graph. An edge may lead to it without anybody declaring it,
// because every graph ends and making each author write the same node out teaches nothing.
const DoneNode = "done"

// Node types. Slice one ships the three that need no external event source: dispatch sends a turn
// to the run's own thread, choice branches on state without a side effect, and done ends the run.
// wait and ask arrive with their delivery mechanisms.
const (
	NodeDispatch = "dispatch"
	NodeChoice   = "choice"
	NodeWait     = "wait"
	NodeAsk      = "ask"
	NodeDone     = "done"
)

// DefaultTransitions is how many movements a run may take when its graph declares no cap.
//
// A number rather than no limit, because an automation dispatches turns with nobody watching and a
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
	// every thread is born in.
	//
	// It belongs to the graph rather than to the operator starting the run, for the same reason the
	// schedule does: what an automation is allowed to do is versioned and reviewable beside what it
	// does. There is nowhere else to put it either, because the run's thread does not exist until
	// the run starts, so `quay mode` has nothing to point at.
	Mode   string
	Limits Limits
	Nodes  map[string]Node
	Edges  []Edge
	// Start is the one node no edge points into, derived rather than declared so the file cannot
	// say one thing and the shape another.
	Start string
}

// Node is one step.
type Node struct {
	Type string
	// Prompt is what a dispatch node says to the run's thread, with {{key}} rendered from the
	// run's state.
	Prompt string
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
		On     map[string]string `yaml:"on"`
		For    string            `yaml:"for"`
		Text   string            `yaml:"text"`
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
	// made, has a thread of its own, and is about to spend money to find out the word was wrong.
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
		default:
			return Graph{}, fmt.Errorf("flow: node %s has type %q; this graph engine knows %s, %s, %s, %s and the implicit %s", name, node.Type, NodeDispatch, NodeChoice, NodeWait, NodeAsk, DoneNode)
		}
		graph.Nodes[name] = Node{Type: node.Type, Prompt: node.Prompt, On: node.On, For: waitFor, Text: node.Text}
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

	for name, node := range graph.Nodes {
		if err := usableEdges(graph, name, node); err != nil {
			return Graph{}, err
		}
	}
	return graph, nil
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
	case NodeDispatch, NodeWait, NodeAsk:
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
