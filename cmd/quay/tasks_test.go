package main

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

// Reading a thread's history must not depend on the thread still being live.
//
// A flow run archives its own thread when it ends, so the first command anybody investigating that
// run types is this one, against a thread that has just been put away. Every command that reads a
// thread resolved it through the live listing alone, so that command was refused and the turns, the
// only record of what the run actually did, were reachable through the database and nothing else.
func TestAnArchivedThreadsHistoryIsStillReadable(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "dispatch", "read the package file")

	thread := onlyThread(t, client)
	if _, err := client.ArchiveThread(context.Background(), &quaycrewv1.ArchiveThreadRequest{
		Id: thread.GetId(),
	}); err != nil {
		t.Fatalf("archive the thread: %v", err)
	}

	read, err := runQuay(t, client, "tasks", thread.GetId()[:8])
	if err != nil {
		t.Fatalf("quay tasks on an archived thread: %v", err)
	}
	if !strings.Contains(read, "read the package file") {
		t.Fatalf("the history says %q, want the task that was asked", read)
	}
}

// The handle is the other identifier a listing prints, and an archived thread has to answer to it
// too: the operator reads one screen and types either.
func TestAnArchivedThreadAnswersToItsHandle(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "dispatch", "remember this")

	thread := onlyThread(t, client)
	if _, err := client.ArchiveThread(context.Background(), &quaycrewv1.ArchiveThreadRequest{
		Id: thread.GetId(),
	}); err != nil {
		t.Fatalf("archive the thread: %v", err)
	}

	read, err := runQuay(t, client, "tasks", thread.GetHandle()[:8])
	if err != nil {
		t.Fatalf("quay tasks by handle on an archived thread: %v", err)
	}
	if !strings.Contains(read, "remember this") {
		t.Fatalf("the history says %q, want the task that was asked", read)
	}
}

// The second listing must not swallow the refusal. A thread nobody has is still refused, in the
// words the live listing refuses it with, or an identifier typed wrongly would read as a crew that
// lost the thread.
func TestAThreadNobodyHasIsStillRefused(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "dispatch", "hello")

	_, err := runQuay(t, client, "tasks", "nosuchthread")
	if err == nil {
		t.Fatal("a thread nobody has was resolved to something")
	}
	if !strings.Contains(err.Error(), "thread") {
		t.Fatalf("the refusal says %q, want it to name what was not found", err)
	}
}
