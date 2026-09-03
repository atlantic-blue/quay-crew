package features_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/contextspend"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/cucumber/godog"
)

// Where a session's context went, read back through the control plane the way the console and the
// command line read it.
//
// The conversation is written the way the model's own command line tool writes it, because that file
// is the only record of what a session read: the interesting conversations never pass through the
// control plane at all.
type contextSpendWorld struct {
	// records are the conversation so far. They are kept because a scenario adds to a conversation one
	// step at a time and the file is rewritten whole each time.
	records []string
	// carried is what the model says the last answer carried, and zero until a scenario says.
	carried int64
	// session is whose conversation this is.
	session string
}

type contextSpendKey struct{}

func contextSpendFrom(ctx context.Context) *contextSpendWorld {
	s, _ := ctx.Value(contextSpendKey{}).(*contextSpendWorld)
	return s
}

func initializeContextSpendSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, contextSpendKey{}, &contextSpendWorld{}), nil
	})

	sc.Step(`^the session read (\d+) characters of a file$`,
		func(ctx context.Context, characters int) error {
			return conversationGrew(ctx, sessionOfLastTask, readAFile(characters))
		})

	sc.Step(`^the session ran a command that printed (\d+) characters$`,
		func(ctx context.Context, characters int) error {
			return conversationGrew(ctx, sessionOfLastTask, ranACommand(characters))
		})

	sc.Step(`^the session wrote (\d+) characters of its own$`,
		func(ctx context.Context, characters int) error {
			return conversationGrew(ctx, sessionOfLastTask, wroteItsOwnWords(characters))
		})

	sc.Step(`^the model says it carries (\d+) tokens of context$`,
		func(ctx context.Context, tokens int) error {
			contextSpendFrom(ctx).carried = int64(tokens)
			return conversationGrew(ctx, sessionOfLastTask)
		})

	sc.Step(`^the session reports no breakdown of its context$`, func(ctx context.Context) error {
		session, err := onlyListedSession(ctx)
		if err != nil {
			return err
		}
		if where := session.GetContextSpend(); where != nil {
			return fmt.Errorf("a conversation nobody has spoken in reports %+v, and filling nothing "+
				"is not the same as filling it with nothing", where)
		}
		return nil
	})

	sc.Step(`^the session reports (\d+) characters read from files$`,
		theSessionReports(contextspend.Reads))
	sc.Step(`^the session reports (\d+) characters of tool output$`,
		theSessionReports(contextspend.Tools))
	sc.Step(`^the session reports (\d+) characters of its own turns$`,
		theSessionReports(contextspend.Turns))

	sc.Step(`^the session reports nothing read from files$`, func(ctx context.Context) error {
		spent, err := listedSpend(ctx)
		if err != nil {
			return err
		}
		if spent.Reads != 0 {
			return fmt.Errorf("the session reports %d characters read from files, and it opened none",
				spent.Reads)
		}
		return nil
	})

	sc.Step(`^the listing says its context went on "([^"]*)"$`, func(ctx context.Context, want string) error {
		session, err := onlyListedSession(ctx)
		if err != nil {
			return err
		}
		cell := display.Spend(session).Cell()
		if !strings.HasPrefix(cell, want) {
			return fmt.Errorf("the listing cell reads %q, want it to name %q", cell, want)
		}
		return nil
	})

	// The whole point of the four numbers, and the one thing a reader will assume without checking.
	sc.Step(`^the four parts of the breakdown add up to its total$`, func(ctx context.Context) error {
		spent, err := listedSpend(ctx)
		if err != nil {
			return err
		}
		var summed int64
		for _, category := range contextspend.Categories() {
			summed += spent.Of(category)
		}
		if summed != spent.Total() {
			return fmt.Errorf("the parts add up to %d and the total says %d", summed, spent.Total())
		}
		if spent.Total() == 0 {
			return fmt.Errorf("nothing was measured, so the sum proves nothing")
		}
		return nil
	})

	sc.Step(`^the breakdown accounts for (\d+) per cent of what the model counted$`,
		func(ctx context.Context, want int) error {
			session, err := onlyListedSession(ctx)
			if err != nil {
				return err
			}
			check := display.Spend(session).Against(session.GetContextWindow().GetUsed())
			if !check.Known() {
				return fmt.Errorf("the check cannot be made: the breakdown is %+v and the model counted %d",
					display.Spend(session), session.GetContextWindow().GetUsed())
			}
			if got := check.Share(); got != int64(want) {
				return fmt.Errorf("the breakdown accounts for %d per cent, want %d", got, want)
			}
			return nil
		})

	sc.Step(`^what it does not account for is named$`, func(ctx context.Context) error {
		session, err := onlyListedSession(ctx)
		if err != nil {
			return err
		}
		line := display.Spend(session).Against(session.GetContextWindow().GetUsed()).Line()
		for _, want := range []string{"system prompt", "tool definitions"} {
			if !strings.Contains(line, want) {
				return fmt.Errorf("the check says %q, and does not name the %s", line, want)
			}
		}
		return nil
	})
}

// theSessionReports is the assertion on one category, written once because all three read the same.
func theSessionReports(category contextspend.Category) func(context.Context, int) error {
	return func(ctx context.Context, want int) error {
		spent, err := listedSpend(ctx)
		if err != nil {
			return err
		}
		if got := spent.Of(category); got != int64(want) {
			return fmt.Errorf("the session reports %d characters of %s, want %d (the whole breakdown is %+v)",
				got, category, want, spent)
		}
		return nil
	}
}

// listedSpend is the breakdown of the one session in the listing that has one.
func listedSpend(ctx context.Context) (contextspend.Spend, error) {
	session, err := onlyListedSession(ctx)
	if err != nil {
		return contextspend.Spend{}, err
	}
	return display.Spend(session), nil
}

// onlyListedSession is the session the scenario wrote a conversation for, found in the listing an
// operator reads.
func onlyListedSession(ctx context.Context) (*quaycrewv1.Session, error) {
	wanted := contextSpendFrom(ctx).session
	listed := usageFrom(ctx).listed
	if len(listed) == 0 {
		return nil, fmt.Errorf("the listing is empty, so there is no session to read")
	}
	for _, session := range listed {
		if wanted == "" || session.GetId() == wanted {
			return session, nil
		}
	}
	return nil, fmt.Errorf("the session this scenario wrote a conversation for is not in the listing")
}

// sessionOfLastTask is how a scenario reaches the session it dispatched a task to.
func sessionOfLastTask(ctx context.Context) (string, error) {
	current, err := worldFrom(ctx).lastTask()
	if err != nil {
		return "", err
	}
	return current.sessionID, nil
}

// conversationGrew adds records to the session's conversation and writes the whole file again.
//
// Whole rather than appended, because the answer carrying the model's own count has to stay last: the
// window is what the last answer carried, and a record after it would leave the count on whichever
// record happened to be at the end.
func conversationGrew(ctx context.Context, whose func(context.Context) (string, error), added ...string) error {
	scenario := contextSpendFrom(ctx)
	id, err := whose(ctx)
	if err != nil {
		return err
	}
	scenario.session, scenario.records = id, append(scenario.records, added...)

	world := worldFrom(ctx)
	session, err := world.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: id})
	if err != nil {
		return err
	}
	lines := scenario.records
	if scenario.carried > 0 {
		lines = append(lines, answered(scenario.carried))
	}
	return writeConversation(world.storage.Dir, session.GetSession().GetWorkspace(),
		session.GetSession().GetModelSessionId(), lines)
}

// The records a conversation is made of. A file read is a call and what came back, because the result
// carries no tool name at all and the call is the only place the name is written down.

func readAFile(characters int) string {
	return called("read-1", "Read", map[string]any{"file_path": "/repo/controller.go"}) + "\n" +
		toolReturned("read-1", strings.Repeat("a", characters))
}

func ranACommand(characters int) string {
	return called("run-1", contextspend.Shell, map[string]any{"command": "go test ./..."}) + "\n" +
		toolReturned("run-1", strings.Repeat("b", characters))
}

func wroteItsOwnWords(characters int) string {
	return conversationRecord("assistant", map[string]any{
		"role":    "assistant",
		"content": []any{map[string]any{"type": "text", "text": strings.Repeat("c", characters)}},
	})
}

func called(id, tool string, input map[string]any) string {
	return conversationRecord("assistant", map[string]any{
		"role": "assistant",
		"content": []any{map[string]any{
			"type": "tool_use", "id": id, "name": tool, "input": input,
		}},
	})
}

func toolReturned(id, text string) string {
	return conversationRecord("user", map[string]any{
		"role": "user",
		"content": []any{map[string]any{
			"type": "tool_result", "tool_use_id": id, "content": text,
		}},
	})
}

// answered is the model saying what its last answer carried, which is the count the breakdown is
// held against.
func answered(carried int64) string {
	return conversationRecord("assistant", map[string]any{
		"role":    "assistant",
		"content": []any{map[string]any{"type": "text", "text": "done"}},
		"usage": map[string]any{
			"input_tokens": 0, "output_tokens": 100,
			"cache_read_input_tokens": carried, "cache_creation_input_tokens": 0,
		},
	})
}

func conversationRecord(kind string, message map[string]any) string {
	line, err := json.Marshal(map[string]any{
		"type": kind, "isSidechain": false, "message": message,
	})
	if err != nil {
		panic(err)
	}
	return string(line)
}

// writeConversation puts the transcript where the system reads it, under the conversation store this
// session's workspace mounts.
func writeConversation(dir, workspace, conversation string, lines []string) error {
	if conversation == "" {
		return fmt.Errorf("the system holds no conversation for this session, so there is nowhere to write")
	}
	at := filepath.Join(dir, "workspaces", workspace, "claude", "projects", "-home-agent-workspace")
	if err := os.MkdirAll(at, 0o777); err != nil {
		return err
	}
	body := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(filepath.Join(at, conversation+sandbox.ConversationFile), []byte(body), 0o666)
}
