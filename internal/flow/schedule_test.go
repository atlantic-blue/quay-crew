package flow

import (
	"strings"
	"testing"
	"time"
)

func TestAGraphDeclaresHowOftenItRuns(t *testing.T) {
	graph, err := Parse([]byte(`
name: nightly
version: 1
on:
  every: 24h
nodes:
  sweep: { type: dispatch, prompt: "check the overnight builds" }
edges:
  - [sweep, done]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if graph.Every != 24*time.Hour {
		t.Fatalf("the graph runs every %s, want a day", graph.Every)
	}
}

// A graph that says nothing about when it runs is started by a person and by nothing else, which is
// every graph up to now.
func TestAGraphWithNoScheduleRunsOnlyWhenAsked(t *testing.T) {
	graph, err := Parse([]byte(`
name: manual
version: 1
nodes:
  go: { type: dispatch, prompt: "go" }
edges:
  - [go, done]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if graph.Every != 0 {
		t.Fatalf("a graph with no schedule runs every %s, want never on its own", graph.Every)
	}
}

// A schedule fast enough to overlap its own runs is refused rather than accepted and then quietly
// throttled: an automation started every second would spend money as fast as the model can take it.
func TestATooFrequentScheduleIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: frantic
version: 1
on:
  every: 1s
nodes:
  go: { type: dispatch, prompt: "go" }
edges:
  - [go, done]
`))
	if err == nil {
		t.Fatal("a schedule of one second was accepted")
	}
	if !strings.Contains(err.Error(), MinimumEvery.String()) {
		t.Errorf("the refusal says %q, want it to name the shortest schedule allowed", err)
	}
}

func TestAnUnreadableScheduleIsRefused(t *testing.T) {
	_, err := Parse([]byte(`
name: vague
version: 1
on:
  every: "nightly"
nodes:
  go: { type: dispatch, prompt: "go" }
edges:
  - [go, done]
`))
	if err == nil {
		t.Fatal("a schedule of \"nightly\" parsed")
	}
}
