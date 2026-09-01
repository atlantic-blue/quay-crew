package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/quay-krewe/internal/flow"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/cucumber/godog"
)

// The graph the system ships is read off disk here rather than written out in the feature file. A
// scenario carrying its own copy would prove the copy, and the file is what an operator imports.

// shippedGraph reads one of the graphs in flows/, from the features directory the suite runs in.
func shippedGraph(name string) (flow.Graph, string, error) {
	at := filepath.Join("..", "flows", name+".yaml")
	body, err := os.ReadFile(at)
	if err != nil {
		return flow.Graph{}, "", fmt.Errorf("reading %s: %w", at, err)
	}
	graph, err := flow.Parse(body)
	if err != nil {
		return flow.Graph{}, "", fmt.Errorf("%s does not parse, so nobody could import it: %w", at, err)
	}
	return graph, string(body), nil
}

func initializePullRequestReviewSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the system holds the flow graph it ships as "([^"]*)"$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		graph, source, err := shippedGraph(name)
		if err != nil {
			return err
		}
		return w.store.ImportFlowGraph(ctx, graph.Name, graph.Version, source)
	})

	// A model that does the job rather than describing it. Every step of this graph says which file
	// would show it worked, and the system reads the session for that file afterwards, so a double
	// that only talks would stop the run at its first step. The files are taken from the graph, so a
	// step added tomorrow is covered without anybody remembering this.
	sc.Step(`^the model does the work every step of that graph expects$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		graph, _, err := shippedGraph("pull-request-review")
		if err != nil {
			return err
		}
		var wanted []string
		for _, node := range graph.Nodes {
			if node.Expect != nil && node.Expect.File != "" {
				wanted = append(wanted, node.Expect.File)
			}
		}
		if len(wanted) == 0 {
			return fmt.Errorf("the graph claims no files, so this step would write nothing and prove nothing")
		}
		w.runner.onTask = func() {
			for _, cfg := range w.provider.Configurations() {
				dir, kept := w.storage.WorkingDir(cfg)
				if !kept {
					continue
				}
				if err := os.MkdirAll(dir, 0o777); err != nil {
					continue
				}
				for _, name := range wanted {
					_ = os.WriteFile(filepath.Join(dir, name), []byte("written by the step\n"), 0o600)
				}
			}
		}
		return nil
	})

	// The order is the design: a security finding blocks the merge whatever else is true, so it is
	// read first, and what is missing is read last.
	sc.Step(`^the run read the pull request for security, then for features, then for completeness$`,
		func(ctx context.Context) error {
			nodes, err := reviewStepNodes(ctx)
			if err != nil {
				return err
			}
			want := []string{"pick", "security", "features", "completeness", "draft"}
			if strings.Join(nodes, " ") != strings.Join(want, " ") {
				return fmt.Errorf("the run took its steps as %q, want %q",
					strings.Join(nodes, " "), strings.Join(want, " "))
			}
			return nil
		})

	// The operator reads the draft in the question itself. The session that wrote it is put away by
	// the time they are asked, so a question that named a file would name a file nobody can open.
	sc.Step(`^the flow run is asking, and the question carries "([^"]*)"$`,
		func(ctx context.Context, carries string) error {
			w := worldFrom(ctx)
			kept, err := w.store.GetFlowRun(ctx, w.flowRun.ID)
			if err != nil {
				return err
			}
			if kept.Status != flow.StatusAsking {
				return fmt.Errorf("the run reads back as %q on node %q, want it asking the operator",
					kept.Status, kept.Node)
			}
			if !strings.Contains(kept.Question, carries) {
				return fmt.Errorf("the operator is asked %q, and %q is not in it", kept.Question, carries)
			}
			return nil
		})

	sc.Step(`^nothing has been posted$`, func(ctx context.Context) error {
		posted, err := reviewPostingSteps(ctx)
		if err != nil {
			return err
		}
		if len(posted) != 0 {
			return fmt.Errorf("%d posting steps were declared, and the operator never said yes", len(posted))
		}
		return nil
	})

	sc.Step(`^the review was posted, carrying "([^"]*)"$`, func(ctx context.Context, carries string) error {
		posted, err := reviewPostingSteps(ctx)
		if err != nil {
			return err
		}
		if len(posted) != 1 {
			return fmt.Errorf("%d posting steps were declared, want the one the operator agreed to", len(posted))
		}
		if !strings.Contains(posted[0].Brief, carries) {
			return fmt.Errorf("the posting step was asked %q, and %q is not in it", posted[0].Brief, carries)
		}
		return nil
	})
}

// reviewStepNodes is the node each of the run's steps belongs to, oldest first.
func reviewStepNodes(ctx context.Context) ([]string, error) {
	steps, err := runSteps(ctx, worldFrom(ctx))
	if err != nil {
		return nil, err
	}
	nodes := make([]string, 0, len(steps))
	for _, one := range steps {
		nodes = append(nodes, one.Labels["flow.node"])
	}
	return nodes, nil
}

// reviewPostingSteps is every step the run declared for the node that posts, whatever phase it
// reached. The question is whether the system ever asked for one, not whether one is out now.
func reviewPostingSteps(ctx context.Context) ([]*job.Job, error) {
	steps, err := runSteps(ctx, worldFrom(ctx))
	if err != nil {
		return nil, err
	}
	var posting []*job.Job
	for _, one := range steps {
		if one.Labels["flow.node"] == "post" {
			posting = append(posting, one)
		}
	}
	return posting, nil
}
