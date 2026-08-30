package flow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// shippedFlows is where the example graphs live, from this package's directory.
const shippedFlows = "../../flows"

// Every graph in flows/ parses.
//
// They are examples, which is exactly why this is here: an example is the file somebody copies, and
// a graph that dies at its first movement teaches the wrong shape to everybody who reads it. The
// directory is read rather than a list written here, so a graph added tomorrow is held to this
// without anybody remembering.
//
// A directory holding no graphs fails, because a walk that finds nothing to check reports success in
// exactly the same way as one that checked everything.
func TestEveryShippedFlowGraphParses(t *testing.T) {
	entries, err := os.ReadDir(shippedFlows)
	if err != nil {
		t.Fatalf("reading %s: %v", shippedFlows, err)
	}
	parsed := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		at := filepath.Join(shippedFlows, entry.Name())
		body, err := os.ReadFile(at)
		if err != nil {
			t.Fatalf("reading %s: %v", at, err)
		}
		graph, err := Parse(body)
		if err != nil {
			t.Errorf("%s does not parse, so importing it would fail: %v", at, err)
			continue
		}
		parsed++
		// A graph is named by its file, so `krewe flow import flows/x.yaml` imports x.
		if want := strings.TrimSuffix(entry.Name(), ".yaml"); graph.Name != want {
			t.Errorf("%s calls itself %q, and a reader looks for the graph by its file name", at, graph.Name)
		}
		if graph.Start == "" {
			t.Errorf("%s has no start node", at)
		}
	}
	// The count is reported so a run that parsed one graph cannot read as a run that parsed them all.
	t.Logf("flows/ holds %d graphs", parsed)
	if parsed == 0 {
		t.Fatalf("%s holds no graphs, and a directory with nothing in it is not a set of examples", shippedFlows)
	}
}
