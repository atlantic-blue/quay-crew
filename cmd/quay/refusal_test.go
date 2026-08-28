package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/workspace"
)

// refused runs one invocation that is expected to fail and hands back the error.
func refused(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, args ...string) error {
	t.Helper()
	var out bytes.Buffer
	err := run(context.Background(), client, args, &out, "")
	if err == nil {
		t.Fatalf("quay %s was accepted: %q", strings.Join(args, " "), out.String())
	}
	return err
}

// A crew with two workspaces, one of which has a project and a session, which is enough shape for
// every level of an address to be got wrong.
func aCrew(t *testing.T) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "itv")
	mustRun(t, client, "project", "create", "fe-player")
	mustRun(t, client, "workspace", "create", "acme")
	return client
}

// The one that started this. `quay use itv/nope` answered "workspace: no workspace with that id or
// name: project \"nope\"", which names the wrong level, blames the one part of the address that was
// right, and sends the operator to check their workspace.
func TestAMissingProjectNamesTheWorkspaceAndSaysWhatItHas(t *testing.T) {
	client := aCrew(t)

	err := refused(t, client, "use", "itv/nope")
	for _, want := range []string{"itv", "no project", "nope", "fe-player"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %s", want, err)
		}
	}
	if strings.Contains(err.Error(), "no workspace with that id or name") {
		t.Errorf("the refusal still blames the workspace: %s", err)
	}
}

func TestAMissingWorkspaceSaysWhatTheCrewHas(t *testing.T) {
	client := aCrew(t)

	err := refused(t, client, "use", "nope")
	for _, want := range []string{"no workspace", "nope", "itv", "acme"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %s", want, err)
		}
	}
}

func TestAMissingSessionSaysWhatTheProjectHas(t *testing.T) {
	client := aCrew(t)
	mustRun(t, client, "task", flagDispatch, "itv/fe-player", "hello")

	err := refused(t, client, "use", "itv/fe-player/ffffffff")
	for _, want := range []string{"no session", "ffffffff"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %s", want, err)
		}
	}
}

// A level with nothing in it is a different sentence, because there is nothing to offer and telling
// somebody what a level has when it has nothing reads as a bug.
func TestAnEmptyLevelSaysHowToMakeOneRatherThanListingNothing(t *testing.T) {
	client := aCrew(t)

	err := refused(t, client, "use", "acme/anything")
	for _, want := range []string{"acme", "no projects yet", "quay project create"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %s", want, err)
		}
	}
}

// Callers that only care whether something was missing keep working, which is what lets the message
// change without every caller changing with it.
func TestEveryLevelIsStillRecognisedAsNotFound(t *testing.T) {
	client := aCrew(t)
	mustRun(t, client, "task", flagDispatch, "itv/fe-player", "hello")

	for _, address := range []string{"nope", "itv/nope", "itv/fe-player/ffffffff"} {
		if err := refused(t, client, "use", address); !errors.Is(err, workspace.ErrNotFound) {
			t.Errorf("%q was not recognised as not found: %v", address, err)
		}
	}
}

// Where you are standing is kept on this machine and the crew's state is not, so a wiped crew, a
// fresh install or a different crew leaves the tool pointing at something gone. Every command that
// defaults to where you are then refused with a sentence about a missing workspace, which reads as
// the crew being broken rather than as you being nowhere.
func TestStandingSomewhereTheCrewNoLongerHasSaysSo(t *testing.T) {
	client := aCrew(t)
	// What a wipe leaves behind: the tool still standing in an address the crew has never heard of.
	if err := moveTo(workspace.Path{Workspace: "ghost", Project: "gone"}); err != nil {
		t.Fatal(err)
	}

	// Every command that defaults to where you are, not just the one that was noticed. A listing and
	// a dispatch resolve the address by different roads, and the first version of this fix only
	// covered one of them, which a mutation caught.
	for _, command := range [][]string{{"sessions"}, {"task", flagDispatch, "hello"}, {"context"}} {
		err := refused(t, client, command...)
		if !strings.Contains(err.Error(), "standing in ghost/gone") {
			t.Errorf("quay %s does not say where you are standing: %s", strings.Join(command, " "), err)
		}
		if !strings.Contains(err.Error(), "quay use") {
			t.Errorf("quay %s does not say how to move: %s", strings.Join(command, " "), err)
		}
	}
}

// An address the operator typed is their mistake to fix and gets the plain refusal, not a sentence
// about where they are standing, which would be wrong and confusing.
func TestATypedAddressIsNotBlamedOnWhereYouAreStanding(t *testing.T) {
	client := aCrew(t)
	mustRun(t, client, "use", "itv/fe-player")

	err := refused(t, client, "sessions", "nope")
	if strings.Contains(err.Error(), "standing in") {
		t.Errorf("a typed address was blamed on the stored context: %s", err)
	}
}
