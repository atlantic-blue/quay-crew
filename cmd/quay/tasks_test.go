package main

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

// Reading a session's history must not depend on the session still being live.
//
// A flow run archives its own session when it ends, so the first command anybody investigating that
// run types is this one, against a session that has just been put away. Every command that reads a
// session resolved it through the live listing alone, so that command was refused and the tasks, the
// only record of what the run actually did, were reachable through the database and nothing else.
func TestAnArchivedSessionsHistoryIsStillReadable(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "dispatch", "read the package file")

	session := onlySession(t, client)
	if _, err := client.ArchiveSession(context.Background(), &quaycrewv1.ArchiveSessionRequest{
		Id: session.GetId(),
	}); err != nil {
		t.Fatalf("archive the session: %v", err)
	}

	read, err := runQuay(t, client, "tasks", session.GetId()[:8])
	if err != nil {
		t.Fatalf("quay tasks on an archived session: %v", err)
	}
	if !strings.Contains(read, "read the package file") {
		t.Fatalf("the history says %q, want the task that was asked", read)
	}
}

// The handle is the other identifier a listing prints, and an archived session has to answer to it
// too: the operator reads one screen and types either.
func TestAnArchivedSessionAnswersToItsHandle(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "dispatch", "remember this")

	session := onlySession(t, client)
	if _, err := client.ArchiveSession(context.Background(), &quaycrewv1.ArchiveSessionRequest{
		Id: session.GetId(),
	}); err != nil {
		t.Fatalf("archive the session: %v", err)
	}

	read, err := runQuay(t, client, "tasks", session.GetHandle()[:8])
	if err != nil {
		t.Fatalf("quay tasks by handle on an archived session: %v", err)
	}
	if !strings.Contains(read, "remember this") {
		t.Fatalf("the history says %q, want the task that was asked", read)
	}
}

// The second listing must not swallow the refusal. A session nobody has is still refused, in the
// words the live listing refuses it with, or an identifier typed wrongly would read as a crew that
// lost the session.
func TestASessionNobodyHasIsStillRefused(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "dispatch", "hello")

	_, err := runQuay(t, client, "tasks", "nosuchsession")
	if err == nil {
		t.Fatal("a session nobody has was resolved to something")
	}
	if !strings.Contains(err.Error(), "session") {
		t.Fatalf("the refusal says %q, want it to name what was not found", err)
	}
}
