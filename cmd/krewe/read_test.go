package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// krewe read: the one command that gets work out of a session. Attaching was the only road in before
// this, which is a person driving a terminal and does not compose into a script, a flow or a report.

// aSessionThatMadeSomething is a system with one session whose working directory holds a file.
func aSessionThatMadeSomething(t *testing.T, name, body string) (quaycrewv1.ControlPlaneServiceClient, string) {
	t.Helper()
	dir := t.TempDir()
	storage := sandbox.Storage{Dir: dir, Host: "/qdata"}
	client := testClientWith(t, controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "made the change"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(), Storage: storage,
	})
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "exec", "acme/house-bills", "make the listing sort by the clock it shows")

	listed, err := client.ListSessions(context.Background(), &quaycrewv1.ListSessionsRequest{})
	if err != nil || len(listed.GetSessions()) != 1 {
		t.Fatalf("ListSessions: %v, %d sessions", err, len(listed.GetSessions()))
	}
	session := listed.GetSessions()[0]
	work, kept := storage.WorkingDir(sandbox.Config{
		ID: session.GetId(), Workspace: session.GetWorkspace(), Project: session.GetProject(),
	})
	if !kept {
		t.Fatal("the system keeps no working directory for the session it just made")
	}
	if err := os.MkdirAll(work, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, name), []byte(body), 0o666); err != nil {
		t.Fatal(err)
	}
	return client, session.GetId()
}

// The refusal, first. A command that read the wrong session, or every session, would be worse than
// one that refuses.
func TestReadingWithoutASessionSaysHowToTypeIt(t *testing.T) {
	client := testClient(t)
	var out bytes.Buffer

	err := run(context.Background(), client, []string{"read"}, &out, "")
	if err == nil {
		t.Fatal("krewe read with nothing to read answered as though it worked")
	}
	if !strings.Contains(err.Error(), "krewe read <session> [<path>]") {
		t.Fatalf("the refusal is %q, want it to say how to type the command", err)
	}
}

// A path that is not there is said so with the directory, because the operator's next move is to
// look at what is.
func TestReadingSomethingTheSessionDoesNotHoldSaysWhereItLooked(t *testing.T) {
	client, session := aSessionThatMadeSomething(t, "listing.go", "sorted by the clock it shows\n")
	var out bytes.Buffer

	err := run(context.Background(), client, []string{"read", session, "nothing.go"}, &out, "")
	if err == nil {
		t.Fatal("reading a file the session does not hold answered as though it worked")
	}
	if !strings.Contains(err.Error(), "nothing.go") || !strings.Contains(err.Error(), "/qdata/") {
		t.Fatalf("the refusal is %q, want it to name what was asked for and where it looked", err)
	}
}

// A path that climbs out is a name inside the work, never a road out of it. The system reads a
// directory it mounted, so a path that escaped it would read the machine the system runs on.
func TestAPathThatClimbsOutOfTheWorkStaysInsideIt(t *testing.T) {
	client, session := aSessionThatMadeSomething(t, "listing.go", "sorted by the clock it shows\n")
	var out bytes.Buffer

	err := run(context.Background(), client, []string{"read", session, "../../../../../etc/passwd"}, &out, "")
	if err == nil {
		t.Fatalf("a path that climbs out read something: %q", out.String())
	}
	if strings.Contains(out.String(), "root:") {
		t.Fatalf("a path that climbs out read the machine: %q", out.String())
	}
}

// The listing names the directory on the machine, on its own line, because that is the thing an
// operator acts on when the system could not publish the work.
func TestReadingASessionListsWhatItMadeAndNamesTheDirectory(t *testing.T) {
	client, session := aSessionThatMadeSomething(t, "listing.go", "sorted by the clock it shows\n")

	said := mustRun(t, client, "read", session)

	if !strings.HasPrefix(said, "/qdata/workspaces/") {
		t.Fatalf("the listing opens with %q, want the directory on the machine", firstLine(said))
	}
	if !strings.Contains(said, "listing.go") {
		t.Fatalf("the listing does not hold the file the session wrote:\n%s", said)
	}
}

// And a file comes back as its bytes and nothing else, so the command composes: a caller redirects it
// into a file and gets the file back.
func TestReadingAFileOutOfASessionGivesBackItsBytes(t *testing.T) {
	client, session := aSessionThatMadeSomething(t, "listing.go", "sorted by the clock it shows\n")

	said := mustRun(t, client, "read", session, "listing.go")

	if said != "sorted by the clock it shows\n" {
		t.Fatalf("the file reads %q, want exactly what the session wrote", said)
	}
}
