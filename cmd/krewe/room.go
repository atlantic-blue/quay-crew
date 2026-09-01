package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/room"
)

// runRoom says how much memory there is, and it answers two different questions depending on where
// it is run.
//
// Inside a sandbox it reads the machine's own accounting and says what that session actually has.
// That is the question a session about to run a gate is asking, it talks to nothing, and it is the
// answer this command has always given.
//
// Off a machine that keeps no such accounting, which is every Mac, it used to fail outright:
// "room: this reads a linux sandbox's own memory accounting, and there is none here". So the
// operator most likely to need the number was the one who could not have it. Now it asks the system,
// which reads the daemon on its own timer and knows what the whole machine holds. See issue 405.
func runRoom(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, out io.Writer) error {
	return roomOf(ctx, client, os.DirFS("/"), out)
}

// roomOf is runRoom over a given machine, so a test can be a machine that keeps no accounting
// without being run on one.
func roomOf(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, root fs.FS, out io.Writer) error {
	reading, err := room.Read(root)
	if err == nil {
		fmt.Fprint(out, room.Say(reading))
		return nil
	}

	// No accounting here. The system reads the machine from the other side, so ask it.
	if client == nil {
		return err
	}
	answer, systemErr := client.GetHeadroom(ctx, &quaycrewv1.GetHeadroomRequest{})
	if systemErr != nil {
		// Both roads are shut, and the refusal names both rather than the last one tried. An
		// operator who is told only about the system goes looking for a system fault, and the first
		// reason is the one that says which machine they are standing on.
		return fmt.Errorf("%w\n\nthe system could not be asked either: %v", err, systemErr)
	}
	fmt.Fprint(out, saySystemRoom(answer))
	return nil
}

// saySystemRoom writes what the system read of the machine it runs on.
//
// It states where every figure came from, because the two halves answer different questions. The
// daemon's own cap is what says whether another sandbox will start. The machine underneath it is
// what killed eighteen sandboxes on 27 August 2026 while the daemon sat at less than half its cap.
func saySystemRoom(answer *quaycrewv1.GetHeadroomResponse) string {
	if answer.GetTakenAt() == nil {
		out := "there is no memory accounting here, and the system has not read its machine yet.\n"
		if answer.GetFailed() != "" {
			out += "\n  " + answer.GetFailed() + "\n"
		}
		return out
	}

	out := fmt.Sprintf("there is no memory accounting here, so this is what the system reads of its "+
		"own machine.\n\nthe machine is %s: %s of %s is held.\n\n",
		answer.GetState(), answer.GetUsed(), answer.GetLimit())
	out += fmt.Sprintf("  every container holds  %10s\n", answer.GetUsed())
	out += fmt.Sprintf("  the daemon may hold    %10s   the limit that binds\n", answer.GetLimit())
	out += fmt.Sprintf("  so there is room for   %10s\n", answer.GetFree())

	where := answer.GetMachineName()
	if where == "" {
		where = "the machine it runs on"
	}
	out += fmt.Sprintf("\n%s, which is a different question:\n", where)
	out += fmt.Sprintf("  it has                 %10s\n", answer.GetMachineTotal())
	out += fmt.Sprintf("  free right now         %10s\n", answer.GetMachineAvailable())
	out += fmt.Sprintf("  swap in use            %10s   of %s\n", answer.GetSwapUsed(), answer.GetSwapTotal())

	if len(answer.GetSandboxes()) == 0 {
		out += "\nNo session is holding a sandbox.\n"
	} else {
		out += fmt.Sprintf("\n%d sandboxes, largest first:\n", len(answer.GetSandboxes()))
		for _, box := range answer.GetSandboxes() {
			idle := box.GetIdle()
			if idle == "" {
				idle = "unknown"
			}
			status := box.GetStatus()
			if status == "" {
				status = "stray, no session"
			}
			out += fmt.Sprintf("  %s  %10s  %8s  idle %-6s %s\n",
				box.GetSession(), box.GetHeld(), box.GetProcessor(), idle, status)
		}
	}

	if answer.GetFailed() != "" {
		out += "\nSome of this could not be read: " + answer.GetFailed() + "\n"
	}
	out += "\nStop a session with krewe stop, or read them largest first with the room view in the console.\n"
	return out
}
