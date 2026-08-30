package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/cucumber/godog"
)

// What a conversation cost, which the system reads from the transcript the model keeps rather than from
// anything it recorded itself.
type usageWorld struct {
	listed []*quaycrewv1.Session
}

type usageKey struct{}

func usageFrom(ctx context.Context) *usageWorld {
	u, _ := ctx.Value(usageKey{}).(*usageWorld)
	return u
}

func initializeUsageSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, usageKey{}, &usageWorld{}), nil
	})

	// The model writing its own record, which is the only place an interactive conversation is
	// recorded at all.
	sc.Step(`^the model has written (\d+) in, (\d+) out and (\d+) read from cache$`,
		func(ctx context.Context, in, out, cached int) error {
			world := worldFrom(ctx)
			current, err := world.lastTask()
			if err != nil {
				return err
			}
			session, err := world.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: current.sessionID})
			if err != nil {
				return err
			}
			return writeTranscript(world.storage.Dir, session.GetSession().GetWorkspace(),
				session.GetSession().GetModelSessionId(), in, out, cached)
		})

	// Listing the sessions is one step, defined beside the presence scenarios, because there is one
	// listing and it asks each sandbox what is in it the way `quay sessions` does. It leaves the rows
	// here too, so a scenario about what a conversation cost reads the listing an operator reads.

	sc.Step(`^the session reports (\d+) tokens in and (\d+) out$`, func(ctx context.Context, in, out int) error {
		spent, err := onlySpender(ctx)
		if err != nil {
			return err
		}
		if spent.GetInput() != int64(in) || spent.GetOutput() != int64(out) {
			return fmt.Errorf("the session reports %d in and %d out, want %d and %d",
				spent.GetInput(), spent.GetOutput(), in, out)
		}
		return nil
	})

	// The number that would be missing from a report of inbound and outbound alone, and the largest of
	// them by three orders of magnitude on any conversation with context behind it.
	sc.Step(`^the session reports (\d+) read from the cache$`, func(ctx context.Context, cached int) error {
		spent, err := onlySpender(ctx)
		if err != nil {
			return err
		}
		if spent.GetCacheRead() != int64(cached) {
			return fmt.Errorf("the session reports %d read from the cache, want %d",
				spent.GetCacheRead(), cached)
		}
		return nil
	})

	sc.Step(`^the driver reports no cost, rather than a cost of nothing$`, func(ctx context.Context) error {
		world, u := worldFrom(ctx), usageFrom(ctx)
		if len(world.drivers) == 0 {
			return fmt.Errorf("no driver was opened")
		}
		for _, session := range u.listed {
			if session.GetId() != world.drivers[0].GetId() {
				continue
			}
			if session.GetUsage() != nil {
				return fmt.Errorf("a conversation nobody has had reports %+v", session.GetUsage())
			}
			return nil
		}
		return fmt.Errorf("the driver is not in the listing at all")
	})
}

// onlySpender is the one session in the listing that has cost anything.
func onlySpender(ctx context.Context) (*quaycrewv1.Usage, error) {
	u := usageFrom(ctx)
	var found *quaycrewv1.Usage
	for _, session := range u.listed {
		if session.GetUsage() != nil {
			found = session.GetUsage()
		}
	}
	if found == nil {
		return nil, fmt.Errorf("no session in the listing reports a cost")
	}
	return found, nil
}

// writeTranscript writes what the model's command line tool writes as it goes: one record per line,
// with the usage on the assistant's messages.
func writeTranscript(dir, workspace, conversation string, in, out, cached int) error {
	if conversation == "" {
		return fmt.Errorf("the system holds no conversation for this session, so there is nowhere to write")
	}
	at := filepath.Join(dir, "workspaces", workspace, "claude", "projects", "-home-agent-workspace")
	if err := os.MkdirAll(at, 0o777); err != nil {
		return err
	}
	line := fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","usage":`+
		`{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,`+
		`"cache_creation_input_tokens":0}}}`+"\n", in, out, cached)
	return os.WriteFile(filepath.Join(at, conversation+sandbox.ConversationFile), []byte(line), 0o666)
}
