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

// krewe where: a name becomes a directory.
//
// The failure it answers: somebody had a screenshot to put in front of a running session, and finding
// where to put it meant reading three directories named in hex and then inspecting a container that
// happened to be up. With every container down there was nothing on the machine to read at all.

// aSystemOnDisk stands up a system whose data directory is a real one, and hands back the client, the
// store and that directory. The store comes back because the hard case is a session that never ran,
// and the only way to make one is to put the row in without an exec touching it.
func aSystemOnDisk(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	held := store.NewMemory()
	client := testClientWith(t, controlplane.Config{
		Store: held, Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Storage: sandbox.Storage{Dir: dir, Host: dir},
	})
	return client, held, dir
}

// A workspace nobody has worked in yet is the first case, because the shared folder is made when a
// sandbox starts and a workspace with no sessions has never had one. Before this the answer was that
// there was no directory, which is true and useless to somebody holding a file.
func TestWhereOnAWorkspaceWithNoSessionsNamesAFolderThatExists(t *testing.T) {
	client, _, _ := aSystemOnDisk(t)
	mustRun(t, client, "workspace", "create", "acme")

	said := mustRun(t, client, "where", "acme")

	path := firstLineOf(said)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("krewe where printed %q, which is not on disk: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("krewe where printed %q, which is not a directory", path)
	}
	if !strings.Contains(said, sandbox.SharedPath) {
		t.Fatalf("the answer does not say where a session sees it:\n%s", said)
	}
}

// The same for a session the system has made and nothing has run in. A job that is about to be
// dispatched is exactly when somebody wants to leave it a file to read.
func TestWhereOnASessionThatHasNeverRunNamesADirectoryThatExists(t *testing.T) {
	client, held, _ := aSystemOnDisk(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")
	handle := aSessionNothingHasRunIn(t, held, "house-bills")

	said := mustRun(t, client, "where", "acme/house-bills/"+handle)

	path := firstLineOf(said)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("krewe where printed %q, which is not on disk: %v", path, err)
	}
	if !strings.Contains(said, sandbox.WorkingPath) {
		t.Fatalf("the answer does not say where the session sees it:\n%s", said)
	}
}

// The proof that matters: the directory the command names is the one a sandbox binds, read from the
// same call the container runtime is given. Asserting the path was assembled correctly would pass
// against a layout nothing mounts.
func TestWhereNamesTheDirectoryASandboxActuallyMounts(t *testing.T) {
	dir := t.TempDir()
	storage := sandbox.Storage{Dir: dir, Host: dir}
	held := store.NewMemory()
	client := testClientWith(t, controlplane.Config{
		Store: held, Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(), Storage: storage,
	})
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "exec", "acme/house-bills", "sort the listing by the clock it shows")
	session := theOnlySession(t, client)

	shared := firstLineOf(mustRun(t, client, "where", "acme"))
	working := firstLineOf(mustRun(t, client, "where", "acme/house-bills/"+session.GetHandle()))

	mounts, err := storage.Prepare(sandbox.Config{
		ID: session.GetId(), Workspace: session.GetWorkspace(), Project: session.GetProject(),
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	bound := make(map[string]string, len(mounts))
	for _, mount := range mounts {
		bound[mount.Source] = mount.Target
	}
	if bound[shared] != sandbox.SharedPath {
		t.Fatalf("krewe where says the shared folder is %q, and a sandbox binds %v", shared, bound)
	}
	if bound[working] != sandbox.WorkingPath {
		t.Fatalf("krewe where says the working directory is %q, and a sandbox binds %v", working, bound)
	}
}

// A file put in by hand is in the session's directory, which is the whole sentence this serves. The
// path is followed rather than rebuilt, so a command that printed a plausible path fails here.
func TestAFilePutInTheDirectoryIsInTheSessionsOwnWork(t *testing.T) {
	client, held, _ := aSystemOnDisk(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")
	handle := aSessionNothingHasRunIn(t, held, "house-bills")

	path := firstLineOf(mustRun(t, client, "where", "acme/house-bills/"+handle))
	if err := os.WriteFile(filepath.Join(path, "screenshot.png"), []byte("a picture"), 0o666); err != nil {
		t.Fatalf("put a file in %q: %v", path, err)
	}

	// Read back through the command that reads a session's work, which finds its own directory rather
	// than being told this one.
	said := mustRun(t, client, "read", handle, "screenshot.png")
	if said != "a picture" {
		t.Fatalf("the session's work reads %q, want the file that was put in by hand", said)
	}
}

// An address that is not there says what is, because the next move is to type one of them.
func TestWhereOnAnAddressThatDoesNotExistSaysWhatThereIs(t *testing.T) {
	client, _, _ := aSystemOnDisk(t)
	mustRun(t, client, "workspace", "create", "acme")
	var out bytes.Buffer

	err := run(context.Background(), client, []string{"where", "nowhere"}, &out, "")

	if err == nil {
		t.Fatalf("an address that does not exist answered with %q", out.String())
	}
	if !strings.Contains(err.Error(), "acme") {
		t.Fatalf("the refusal is %q, want it to name the workspace there is", err)
	}
}

// A session in the wrong project is refused, at the command and again at the call behind it. A path
// assembled from what was typed would name a directory nothing mounts, and somebody would leave a
// file in it and wait for a session that never sees it.
func TestWhereRefusesASessionThatIsNotInThatProject(t *testing.T) {
	client, held, _ := aSystemOnDisk(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "project", "create", "acme/holidays")
	handle := aSessionNothingHasRunIn(t, held, "house-bills")
	var out bytes.Buffer

	err := run(context.Background(), client, []string{"where", "acme/holidays/" + handle}, &out, "")
	if err == nil {
		t.Fatalf("a session in another project answered with %q", out.String())
	}

	// And the call itself, which another client reaches without the tool's own resolution in front of
	// it. Two guards because they fail differently: the tool cannot resolve the address at all, and the
	// system refuses to answer for a session that is not where the caller says it is.
	ctx := context.Background()
	projects, err := held.ListProjects(ctx, "")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	for _, project := range projects {
		if project.GetName() != "holidays" {
			continue
		}
		_, err := client.LocateDirectory(ctx, &quaycrewv1.LocateDirectoryRequest{
			Workspace: project.GetWorkspace(), Project: project.GetId(), Session: handle,
		})
		if err == nil {
			t.Fatal("the call answered for a session that is in another project")
		}
	}
}

// The system's own directory is where this system keeps its credentials, so the one word that would
// name it is refused rather than answered.
func TestWhereRefusesTheSystemsOwnDirectory(t *testing.T) {
	client, _, _ := aSystemOnDisk(t)
	mustRun(t, client, "workspace", "create", "acme")
	var out bytes.Buffer

	err := run(context.Background(), client, []string{"where", "system"}, &out, "")

	if err == nil {
		t.Fatalf("krewe where system answered with %q", out.String())
	}
	if !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("the refusal is %q, want it to say plainly what is in there", err)
	}
	if strings.Contains(out.String(), "token") || strings.Contains(err.Error(), ".token") {
		t.Fatalf("the refusal names a credential file: %q %v", out.String(), err)
	}
}

// The path is on its own line with nothing beside it, because cd "$(krewe where acme)" is the shape
// this gets typed in. Anything sharing that line breaks every use of it.
func TestThePathIsAloneOnTheFirstLine(t *testing.T) {
	client, _, dir := aSystemOnDisk(t)
	mustRun(t, client, "workspace", "create", "acme")

	said := mustRun(t, client, "where", "acme")

	first := firstLineOf(said)
	if strings.ContainsAny(first, " \t") {
		t.Fatalf("the first line is %q, want the path and nothing else", first)
	}
	if !strings.HasPrefix(first, dir) {
		t.Fatalf("the first line is %q, want a path under the data directory %q", first, dir)
	}
	if len(strings.Split(strings.TrimSpace(said), "\n")) != 2 {
		t.Fatalf("the answer is %d lines, want the path and one sentence:\n%s",
			len(strings.Split(strings.TrimSpace(said), "\n")), said)
	}
}

// The path is the machine's, not the control plane's own view of it. Those differ the moment the
// control plane runs in a container, and the one that is any use to a person is the machine's.
func TestWhereAnswersWithTheMachinesPathNotItsOwn(t *testing.T) {
	held := store.NewMemory()
	client := testClientWith(t, controlplane.Config{
		Store: held, Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Storage: sandbox.Storage{Dir: t.TempDir(), Host: "/var/lib/krewe"},
	})
	mustRun(t, client, "workspace", "create", "acme")

	said := mustRun(t, client, "where", "acme")

	if !strings.HasPrefix(firstLineOf(said), "/var/lib/krewe/workspaces/") {
		t.Fatalf("krewe where printed %q, want the path as the machine running the sandboxes sees it",
			firstLineOf(said))
	}
}

// With no address it answers for where you are standing, the way every other command that takes one
// does. Standing in a project is standing in its workspace, and that is the shared folder.
func TestWhereWithNoAddressAnswersForWhereYouAreStanding(t *testing.T) {
	client, _, _ := aSystemOnDisk(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")

	said := mustRun(t, client, "where")

	if !strings.Contains(said, "shared folder of acme") {
		t.Fatalf("standing in acme/house-bills, krewe where says:\n%s", said)
	}
}

// A system that keeps nothing on disk says so, rather than printing a path into a container that is
// about to be thrown away.
func TestWhereOnASystemThatKeepsNothingSaysSo(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "acme")
	var out bytes.Buffer

	err := run(context.Background(), client, []string{"where", "acme"}, &out, "")

	if err == nil {
		t.Fatalf("a system with no data directory printed %q", out.String())
	}
	if !strings.Contains(err.Error(), "container") {
		t.Fatalf("the refusal is %q, want it to say where the state actually is", err)
	}
}

// The three listings each say how to get from a row to a directory, because a listing is where
// somebody looks when they are holding a file and do not know where to put it.
func TestTheListingsSayHowToReachADirectory(t *testing.T) {
	client, _, _ := aSystemOnDisk(t)
	mustRun(t, client, "workspace", "create", "acme")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "exec", "acme/house-bills", "sort the listing")

	for _, listing := range [][]string{
		{"workspace", "list"}, {"project", "list"}, {"sessions"},
	} {
		said := mustRun(t, client, listing...)
		if !strings.Contains(said, "krewe where") {
			t.Fatalf("krewe %s does not say how to reach a directory:\n%s",
				strings.Join(listing, " "), said)
		}
	}
}

// aSessionNothingHasRunIn puts a session row into the named project without an exec, which is the only
// way to have a session whose directory has never been made. The project is named rather than taken
// first from the listing, because a test about the wrong project needs to know which one it is in.
func aSessionNothingHasRunIn(t *testing.T, held store.Store, project string) string {
	t.Helper()
	ctx := context.Background()
	projects, err := held.ListProjects(ctx, "")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	for _, one := range projects {
		if one.GetName() != project {
			continue
		}
		session, _, err := held.FindOrCreateSession(ctx, one.GetId(), store.NewID(), store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		return session.GetHandle()
	}
	t.Fatalf("no project called %q, there are %d", project, len(projects))
	return ""
}

// theOnlySession is the session a system with one of them holds.
func theOnlySession(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) *quaycrewv1.Session {
	t.Helper()
	listed, err := client.ListSessions(context.Background(), &quaycrewv1.ListSessionsRequest{})
	if err != nil || len(listed.GetSessions()) != 1 {
		t.Fatalf("ListSessions: %v, %d sessions", err, len(listed.GetSessions()))
	}
	return listed.GetSessions()[0]
}

// firstLineOf is the path an answer opens with.
func firstLineOf(said string) string {
	line, _, _ := strings.Cut(said, "\n")
	return strings.TrimSpace(line)
}
