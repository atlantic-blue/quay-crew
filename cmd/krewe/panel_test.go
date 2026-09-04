package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestKreweWithNoArgumentsOpensTheConsoleAndNothingElse.
//
// `krewe` used to open a tmux window with the console in one half and a conversation in the other, so
// a person who typed the name of the tool got a split terminal and a conversation they had not asked
// for. There is one thing left for it to open.
func TestKreweWithNoArgumentsOpensTheConsoleAndNothingElse(t *testing.T) {
	if got := kreweOpens(nil, true); got != theConsole {
		t.Fatalf("krewe with no arguments opens %v, want the console on its own", got)
	}
	if got := kreweOpens([]string{}, true); got != theConsole {
		t.Fatalf("krewe with an empty argument list opens %v, want the console on its own", got)
	}
}

// TestKreweWithNoTerminalStillPrintsLines, so `krewe | grep` keeps working.
func TestKreweWithNoTerminalStillPrintsLines(t *testing.T) {
	if got := kreweOpens(nil, false); got != plainLines {
		t.Fatalf("krewe into a pipe opens %v, want plain lines", got)
	}
}

// TestKreweWithAWordRunsThatCommand, terminal or not: a named command is never the console.
func TestKreweWithAWordRunsThatCommand(t *testing.T) {
	for _, terminal := range []bool{true, false} {
		if got := kreweOpens([]string{"sessions"}, terminal); got != aCommand {
			t.Fatalf("krewe sessions with terminal %v opens %v, want the command", terminal, got)
		}
	}
}

// TestThePanelCommandIsRefused is rule 46 in this repository: when a command is removed, test the way
// off it. `krewe panel` is in somebody's fingers and in these notes, so it has to fail loudly and name
// what to type instead rather than being taken for an unknown word.
//
// The words changed with the panel. A refusal that still said `krewe` opens the system would send a
// person back to the split screen this took away.
func TestThePanelCommandIsRefused(t *testing.T) {
	err := run(context.Background(), testClient(t), []string{"panel"}, io.Discard, "")
	if err == nil {
		t.Fatal("krewe panel was accepted")
	}
	for _, says := range []string{"the panel is gone", "opens the console", "press p", "krewe attach"} {
		if !strings.Contains(err.Error(), says) {
			t.Fatalf("the refusal is %q, and it never says %q", err, says)
		}
	}
	// And it is gone from the usage, or it reads as still being a command.
	if strings.Contains(usage, "panel [<session id>]") {
		t.Fatal("the usage still lists a panel command")
	}
}

// TestEndingAConversationSaysWhenItCouldNotEndIt.
//
// `N` ends the conversation beside the console and reopens the pane. The reopen attaches to the
// conversation rather than starting one, so if the ending failed the same conversation comes back
// with its history and the key reads as doing nothing whatsoever. That failure was discarded: a
// container that is not running, or an image too old to have tmux in it, both ended nothing and said
// nothing.
func TestEndingAConversationSaysWhenItCouldNotEndIt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		run     func(box string, argv ...string) ([]byte, error)
		says    bool
		because string
	}{
		{
			name: "it ended",
			run: func(_ string, _ ...string) ([]byte, error) {
				return nil, nil
			},
			because: "the conversation is gone, so the next open starts one",
		},
		{
			name: "there was nothing to end",
			run: func(_ string, argv ...string) ([]byte, error) {
				if argv[1] == "has-session" {
					return []byte("can't find session: krewe"), fmt.Errorf("exit status 1")
				}
				return []byte("can't find session: krewe"), fmt.Errorf("exit status 1")
			},
			because: "no conversation is the state the next open wants anyway",
		},
		{
			name: "the conversation is still running afterwards",
			run: func(_ string, argv ...string) ([]byte, error) {
				if argv[1] == "has-session" {
					return nil, nil
				}
				return []byte("Error response from daemon: container is not running"), fmt.Errorf("exit status 1")
			},
			says:    true,
			because: "the next open comes back to the conversation that is still there",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := endConversation("quaycrew-c9964dc2", tc.run)
			if said := err != nil; said != tc.says {
				t.Fatalf("ending it reported %v, want a report: %v, because %s", err, tc.says, tc.because)
			}
		})
	}
}

// TestASessionSaysWhenItWasNotToldWhereTheSystemIs.
//
// A session that was never told where the system is falls back to localhost, and localhost inside a
// container is the container. The dial error names an address nobody chose and nothing can be at,
// which reads as the system being down: "dial tcp [::1]:50051: connect: connection refused" from inside
// a sandbox, while the control plane was up the whole time.
func TestASessionSaysWhenItWasNotToldWhereTheSystemIs(t *testing.T) {
	refused := status.Error(codes.Unavailable, `connection error: desc = "transport: Error while `+
		`dialing: dial tcp [::1]:50051: connect: connection refused"`)

	for _, tc := range []struct {
		name      string
		err       error
		told      string
		sandboxed bool
		explains  bool
		because   string
	}{
		{
			name: "refused inside a sandbox that was told nothing", err: refused, sandboxed: true,
			explains: true,
			because:  "nothing can ever answer on localhost in here, and only the operator can fix it",
		},
		{
			name: "refused inside a sandbox that was given an address", err: refused, sandboxed: true,
			told:    "controlplane:50051",
			because: "the system was named, so this is the system being unreachable and the address is worth reading",
		},
		{
			name: "refused on the operator's own machine", err: refused,
			because: "localhost is where their stack runs, so the dial error is the right answer",
		},
		{
			name: "a refusal that is not about reaching anything", sandboxed: true,
			err:     status.Error(codes.NotFound, "session not found"),
			because: "the system answered, so where it lives is not the problem",
		},
		{
			name: "nothing went wrong", sandboxed: true, because: "there is nothing to explain",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := unreachable(tc.err, tc.told, tc.sandboxed)
			if tc.err == nil {
				if got != nil {
					t.Fatalf("a nil error came back as %v", got)
				}
				return
			}
			said := strings.Contains(fmt.Sprint(got), "QC_SANDBOX_CONTROL_PLANE")
			if said != tc.explains {
				t.Fatalf("it explained the setup: %v, want %v, because %s\n%v", said, tc.explains, tc.because, got)
			}
			if !errors.Is(got, tc.err) {
				t.Fatal("the original failure was thrown away rather than wrapped")
			}
		})
	}
}

// Which project the driver opens in, which is what p in the console has to decide before it can put a
// conversation beside it.
//
// It read where you are standing only when you stood in a project, then counted projects across the
// whole system. So `krewe use atlantic-blue` printed "now in atlantic-blue", and asking for a
// conversation refused, because the system held eight projects and it would not choose between them.
// The workspace the operator had just named counted for nothing.
func TestTheDriverOpensWhereYouAreStanding(t *testing.T) {
	for _, one := range []struct {
		name     string
		standing []string
		want     string
		beside   bool
	}{
		{
			name:     "standing in a project opens that project",
			standing: []string{"use", "acme/house-bills"},
			want:     "house-bills",
			beside:   true,
		},
		{
			name:     "standing in a workspace with one project opens that project",
			standing: []string{"use", "solo"},
			want:     "only-one",
			beside:   true,
		},
		{
			name:     "standing in a workspace with two projects refuses and says which to name",
			standing: []string{"use", "acme"},
			beside:   false,
		},
		{
			name:   "standing nowhere with projects in reach refuses and says which to name",
			beside: false,
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			client := testClient(t)
			mustRun(t, client, "workspace", "create", "acme")
			mustRun(t, client, "project", "create", "house-bills")
			mustRun(t, client, "project", "create", "gardening")
			mustRun(t, client, "workspace", "create", "solo")
			mustRun(t, client, "project", "create", "only-one")
			if len(one.standing) > 0 {
				mustRun(t, client, one.standing...)
			} else {
				// Making a project moves you into it, so standing nowhere means forgetting where the
				// setup left us rather than saying so: `krewe use` has no word for nowhere.
				t.Setenv(HomeEnv, t.TempDir())
			}

			project, err := driverProject(context.Background(), client)
			if !one.beside {
				if err == nil {
					t.Fatal("a project was chosen where the operator has to say which")
				}
				// The console holds this on the screen, so it is the whole of what the operator gets.
				// A refusal that does not say what to do next reads as the key being broken.
				if !strings.Contains(err.Error(), "krewe use") && !strings.Contains(err.Error(), "press o") {
					t.Fatalf("the refusal is %q, and it never says what to type or press", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("driverProject: %v", err)
			}
			listed, err := client.ListProjects(context.Background(), &quaycrewv1.ListProjectsRequest{})
			if err != nil {
				t.Fatalf("ListProjects: %v", err)
			}
			for _, held := range listed.GetProjects() {
				if held.GetId() == project {
					if held.GetName() != one.want {
						t.Fatalf("the driver opened in %q, want %q", held.GetName(), one.want)
					}
					return
				}
			}
			t.Fatalf("the driver opened in %q, which is no project this system has", project)
		})
	}
}

// The console hands over the row the cursor is on, and that session is the one the pane opens. It
// used to be dropped, so every open landed on the driver whatever the operator pointed at, and the
// console's own test for it passed because it asserted the call rather than the conversation.
func TestTheConversationBesideTheConsoleIsTheOneTheCursorIsOn(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "exec", "one")
	mustRun(t, client, "exec", "two")

	sessions, err := client.ListSessions(context.Background(), &quaycrewv1.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("listing the sessions: %v", err)
	}
	if len(sessions.GetSessions()) < 2 {
		t.Fatalf("the system holds %d sessions, want the two the execs made", len(sessions.GetSessions()))
	}
	beside := conversationBeside(context.Background(), client)

	for _, session := range sessions.GetSessions() {
		argv, err := beside(session.GetId())
		if err != nil {
			t.Fatalf("opening %s beside the console: %v", session.GetId(), err)
		}
		if line := strings.Join(argv, " "); !strings.HasSuffix(line, "attach "+session.GetId()) {
			t.Fatalf("the cursor is on %s and the pane opens %q", session.GetId(), line)
		}
	}
}

// Nothing pointed at is the driver, which is what opening the system with no argument means. That
// half is deliberate and stays.
func TestNothingUnderTheCursorOpensTheDriver(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")

	argv, err := conversationBeside(context.Background(), client)("")
	if err != nil {
		t.Fatalf("opening the driver beside the console: %v", err)
	}
	driver, err := client.OpenDriver(context.Background(), &quaycrewv1.OpenDriverRequest{
		Project: mustProject(t, client),
	})
	if err != nil {
		t.Fatalf("asking the system for its driver: %v", err)
	}
	if line := strings.Join(argv, " "); !strings.HasSuffix(line, "attach "+driver.GetSession().GetId()) {
		t.Fatalf("with nothing under the cursor the pane opens %q, want the driver", line)
	}
}

// mustProject is the one project the system holds, for a scenario that has just made it.
func mustProject(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) string {
	t.Helper()
	listed, err := client.ListProjects(context.Background(), &quaycrewv1.ListProjectsRequest{})
	if err != nil {
		t.Fatalf("listing the projects: %v", err)
	}
	if len(listed.GetProjects()) != 1 {
		t.Fatalf("the system holds %d projects, want the one this made", len(listed.GetProjects()))
	}
	return listed.GetProjects()[0].GetId()
}
