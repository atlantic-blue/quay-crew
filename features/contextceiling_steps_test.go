package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/cucumber/godog"
)

// A session that has filled its context window hands the rest of its job over, driven from all three
// sides: the operator sets the ceiling, the session writes what it leaves behind over the credential
// the system minted for its job, and the controller moves the job to a fresh conversation.
//
// The assertions go past the phase. What decides whether this saved anything is what the fresh
// session is handed, so these read the task it was given rather than the fact that it started.

func initializeContextCeilingSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, ceilingKey{}, &ceilingWorld{}), nil
	})

	sc.Step(`^the operator sets the workspace's context ceiling to (\d+) per cent$`,
		func(ctx context.Context, share int) error {
			w := worldFrom(ctx)
			held, err := w.client.GetWorkspaceLimits(ctx, &quaycrewv1.GetWorkspaceLimitsRequest{
				Workspace: w.workspaceID,
			})
			if err != nil {
				return err
			}
			asked := held.GetLimits()
			asked.ContextCeilingPercent = int32(share)
			_, w.lastErr = w.client.SetWorkspaceLimits(ctx,
				&quaycrewv1.SetWorkspaceLimitsRequest{Limits: asked})
			return nil
		})

	sc.Step(`^the operator reads the workspace's limits$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		held, err := w.client.GetWorkspaceLimits(ctx, &quaycrewv1.GetWorkspaceLimitsRequest{
			Workspace: w.workspaceID,
		})
		if err != nil {
			return err
		}
		ceilingFrom(ctx).limits = held.GetLimits()
		return nil
	})

	sc.Step(`^the ceiling reads (\d+) per cent$`, func(ctx context.Context, want int) error {
		return theCeilingReads(ctx, want)
	})

	// Where the number came from, printed beside it. This one is not measured and reads exactly like
	// the ones that are, and a reader who takes it for a measurement will not go and take one.
	sc.Step(`^the ceiling reads (\d+) per cent, and says it comes from a standard rather than a measurement$`,
		func(ctx context.Context, want int) error {
			if err := theCeilingReads(ctx, want); err != nil {
				return err
			}
			if set := ceilingFrom(ctx).limits.GetContextCeilingPercent(); set != 0 {
				return fmt.Errorf("the workspace carries a ceiling of %d on its row, so this is not the "+
					"system's own", set)
			}
			return nil
		})

	sc.Step(`^the control plane refuses it, saying a ceiling is a share of the window$`,
		func(ctx context.Context) error {
			return theRefusalSays("share of the model's context window")(ctx)
		})

	sc.Step(`^the control plane refuses it, saying a handoff says what is left$`,
		func(ctx context.Context) error {
			return theRefusalSays("what is left")(ctx)
		})

	// The system cannot work out how big a model's context window is. A session in the workspace
	// writes down what the runtime told it, in the conversation directory the system mounts.
	sc.Step(`^the model runtime told the workspace its context window holds (\d+)$`,
		func(ctx context.Context, size int) error {
			w := worldFrom(ctx)
			at := filepath.Join(w.storage.Dir, "workspaces", w.workspaceID, "claude")
			if err := os.MkdirAll(at, 0o777); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(at, sandbox.ContextWindowFile),
				[]byte(strconv.Itoa(size)+"\n"), 0o666)
		})

	// What the last answer carried, out of the transcript the model keeps. That is the context rather
	// than what the conversation cost: cost only grows, and the window empties again when the model
	// compacts.
	sc.Step(`^that session has carried (\d+) tokens of context$`, func(ctx context.Context, carried int) error {
		session, err := theSessionDoingTheJob(ctx)
		if err != nil {
			return err
		}
		return theModelWrote(ctx, session, carried)
	})

	sc.Step(`^the session was asked for the pull request rather than for a handoff$`,
		func(ctx context.Context) error {
			asked, err := taskAsking(ctx, 1)
			if err != nil {
				return err
			}
			if job.AskingForAHandoff(asked) {
				return fmt.Errorf("the session was made to hand over:\n%s", asked)
			}
			if !strings.Contains(asked, "pull request") {
				return fmt.Errorf("the session was asked %q, want the ask it would have got before the "+
					"ceiling existed", asked)
			}
			return nil
		})

	// The branch is named because a fresh session starts in an empty working directory. Nothing clones
	// a repository once per workspace yet, so a handoff that names no branch hands over nothing but
	// prose.
	sc.Step(`^the session was asked to hand over, and told to push its branch first$`,
		func(ctx context.Context) error {
			asked, err := taskAsking(ctx, 1)
			if err != nil {
				return err
			}
			if !job.AskingForAHandoff(asked) {
				return fmt.Errorf("the session over the ceiling was asked %q, want the handoff", asked)
			}
			for _, want := range []string{"70 per cent", "krewe job handoff", "push", "name the branch"} {
				if !strings.Contains(asked, want) {
					return fmt.Errorf("the ask does not say %q:\n%s", want, asked)
				}
			}
			return nil
		})

	sc.Step(`^the session running that job hands over nothing$`, func(ctx context.Context) error {
		return handOver(ctx, "  ", "")
	})

	sc.Step(`^the session running that job hands over "([^"]*)" having tried "([^"]*)"$`,
		func(ctx context.Context, left, tried string) error {
			if err := handOver(ctx, left, tried); err != nil {
				return err
			}
			// Returned here, unlike the refusal above, which keeps it to read. A handoff the system
			// would not write is this scenario failing, and a step that swallowed it would have the
			// failure show up three steps later as a job that never moved.
			return worldFrom(ctx).lastErr
		})

	sc.Step(`^the job is stopped, and the reason says nothing was handed over$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseStopped {
			return fmt.Errorf("the job is %q saying %q, want stopped", one.GetPhase(), one.GetReason())
		}
		for _, want := range []string{"context ceiling", "nothing for a fresh session to start from"} {
			if !strings.Contains(one.GetReason(), want) {
				return fmt.Errorf("the reason says %q, want it to say %q", one.GetReason(), want)
			}
		}
		return nil
	})

	// A different conversation. The point of handing over is a window that is empty, and the same
	// conversation would be the same full one.
	sc.Step(`^the rest of the job went to a session the first one was not in$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		before := ceilingFrom(ctx).session
		if before == "" {
			return fmt.Errorf("this scenario never recorded which session started the job")
		}
		if one.GetSession() == "" {
			return fmt.Errorf("the job is in no session at all")
		}
		if one.GetSession() == before {
			return fmt.Errorf("the rest of the job went back into %s, the conversation that was full; phase=%s reason=%q handoffs=%d",
				before, one.GetPhase(), one.GetReason(), len(one.GetHandoffs()))
		}
		return nil
	})

	sc.Step(`^that session was told what is left, what was tried, what is finished, and the brief$`,
		func(ctx context.Context) error {
			carried, err := taskAsking(ctx, 2)
			if err != nil {
				return err
			}
			for _, want := range []string{
				"the query still reads the old one",
				"539-feat-index",
				"which deadlocks",
				"read the issue",
				"make the listing sort by the clock it shows",
				"clone atlantic-blue/quay-crew",
			} {
				if !strings.Contains(carried, want) {
					return fmt.Errorf("the fresh session is not told %q:\n%s", want, carried)
				}
			}
			return nil
		})

	sc.Step(`^it is the same job, with the step the first session finished$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseRunning {
			return fmt.Errorf("the job is %q saying %q, want running in the fresh session",
				one.GetPhase(), one.GetReason())
		}
		if len(one.GetSteps()) != 1 {
			return fmt.Errorf("the job says it finished %d steps, want the one the first session recorded",
				len(one.GetSteps()))
		}
		if len(one.GetHandoffs()) != 1 {
			return fmt.Errorf("the job carries %d handoffs, want the one that was written",
				len(one.GetHandoffs()))
		}
		return nil
	})

	// The column the operator reads. A share on its own left them holding the workspace's ceiling in
	// their head, so the word beside it says what the system is about to do.
	sc.Step(`^the row for that session reads "([^"]*)"$`, func(ctx context.Context, want string) error {
		session, err := theSessionDoingTheJob(ctx)
		if err != nil {
			return err
		}
		for _, listed := range usageFrom(ctx).listed {
			if listed.GetId() != session.GetId() {
				continue
			}
			if got := display.ContextLabel(listed); got != want {
				return fmt.Errorf("the row reads %q, want %q (used %d of %d against a ceiling of %d)",
					got, want, listed.GetContextWindow().GetUsed(), listed.GetContextWindow().GetSize(),
					listed.GetContextWindow().GetCeiling())
			}
			return nil
		}
		return fmt.Errorf("the session doing the job is not in the listing")
	})
}

// ceilingWorld is what this feature has to carry between steps: the limits last read, and the
// conversation the job started in, which is what "a fresh session" is measured against.
type ceilingWorld struct {
	limits  *quaycrewv1.WorkspaceLimits
	session string
}

type ceilingKey struct{}

func ceilingFrom(ctx context.Context) *ceilingWorld {
	c, _ := ctx.Value(ceilingKey{}).(*ceilingWorld)
	return c
}

func theCeilingReads(ctx context.Context, want int) error {
	held := ceilingFrom(ctx).limits
	if held == nil {
		return fmt.Errorf("nothing read the workspace's limits")
	}
	read := int(held.GetContextCeilingPercent())
	if read == 0 {
		read = job.DefaultContextCeiling
	}
	if read != want {
		return fmt.Errorf("the ceiling reads %d per cent, want %d", read, want)
	}
	return nil
}

// handOver is the session writing what it leaves behind, over the credential the system minted for
// its job, which is what krewe job handoff does inside the container.
func handOver(ctx context.Context, left, tried string) error {
	w := worldFrom(ctx)
	one, err := readJob(ctx, 0)
	if err != nil {
		return err
	}
	session, err := theSessionRunning(ctx, one.GetId())
	if err != nil {
		return err
	}
	_, w.lastErr = session.RecordJobHandoff(ctx, &quaycrewv1.RecordJobHandoffRequest{
		Left: left, Tried: tried,
	})
	return nil
}

// theSessionDoingTheJob is the conversation the scenario's job is in, remembered the first time it is
// asked for so a later step can say whether the job moved out of it.
func theSessionDoingTheJob(ctx context.Context) (*quaycrewv1.Session, error) {
	one, err := readJob(ctx, 0)
	if err != nil {
		return nil, err
	}
	if one.GetSession() == "" {
		return nil, fmt.Errorf("the job is in no session yet")
	}
	held := ceilingFrom(ctx)
	if held.session == "" {
		held.session = one.GetSession()
	}
	found, err := worldFrom(ctx).client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: held.session})
	if err != nil {
		return nil, err
	}
	return found.GetSession(), nil
}

// theModelWrote puts a transcript where the system reads one for this session, which is the only
// record of what the last answer carried.
//
// Where that is depends on the session. A session running as a role keeps its conversation in its own
// directory rather than in the workspace's shared store, so writing to the shared one would leave the
// system reading nothing and every scenario here passing for the wrong reason.
func theModelWrote(ctx context.Context, session *quaycrewv1.Session, carried int) error {
	w := worldFrom(ctx)
	store := filepath.Join(w.storage.Dir, "workspaces", session.GetWorkspace(), "claude")
	if session.GetRole() != "" {
		store = filepath.Join(w.storage.Dir, "workspaces", session.GetWorkspace(),
			"projects", session.GetProject(), "sessions", session.GetId(), "claude")
	}
	at := filepath.Join(store, "projects", "-home-agent-workspace")
	if err := os.MkdirAll(at, 0o777); err != nil {
		return err
	}
	if session.GetModelSessionId() == "" {
		return fmt.Errorf("the session holds no conversation, so there is nowhere to write a transcript")
	}
	line := fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","usage":`+
		`{"input_tokens":0,"output_tokens":400,"cache_read_input_tokens":%d,`+
		`"cache_creation_input_tokens":0}}}`+"\n", carried)
	return os.WriteFile(filepath.Join(at, session.GetModelSessionId()+sandbox.ConversationFile),
		[]byte(line), 0o666)
}
