package main

import (
	"strings"
	"testing"
)

// A listing narrowed to where the operator is standing looks exactly like a system that holds
// nothing else. The operator ran `krewe job list`, read one row, and asked where the other nine
// were: they were in the project next door, and nothing on the screen said a scope had been
// applied. So every listing names what it read.
func TestEveryListingNamesTheAddressItRead(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "job", "create", "--title", "read the bill", "--brief", "open it")

	for _, listing := range []struct {
		args  []string
		names string
	}{
		{[]string{"job", "list"}, "me/house-bills"},
		{[]string{"flow", "list"}, "me/house-bills"},
		{[]string{"sessions"}, "me/house-bills"},
		{[]string{"workspace", "list"}, "this system"},
		{[]string{"project", "list"}, "this system"},
		{[]string{"project", "list", "me"}, "me"},
		{[]string{"secret", "list"}, "this system"},
		{[]string{"skill", "list"}, "the system"},
		{[]string{"role", "list"}, "the system"},
		{[]string{"hook", "list"}, "the system"},
	} {
		said := mustRun(t, client, listing.args...)
		if !strings.Contains(said, listing.names) {
			t.Errorf("krewe %s does not say it read %s:\n%s",
				strings.Join(listing.args, " "), listing.names, said)
		}
	}
}

// The listing from the acceptance run, with the jobs that were one address away put back. What was
// missing was not the rows: it was the sentence saying a scope had been applied at all.
func TestANarrowedJobListingSaysWhereItLookedAndHowToWiden(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "atlantic-blue")
	mustRun(t, client, "project", "create", "quay-crew")
	mustRun(t, client, "job", "create", "--title", "the observability slice", "--brief", "wire it up")
	mustRun(t, client, "project", "create", "transcript")
	mustRun(t, client, "job", "create", "--title", "a page that turns a video into text", "--brief", "serve it")

	narrowed := mustRun(t, client, "job", "list")
	if strings.Contains(narrowed, "the observability slice") {
		t.Fatalf("the listing was not narrowed to where the operator stands:\n%s", narrowed)
	}
	if !strings.Contains(narrowed, "1 job in atlantic-blue/transcript") {
		t.Errorf("the listing does not say what it read:\n%s", narrowed)
	}
	if !strings.Contains(narrowed, "krewe job list system") {
		t.Errorf("the listing does not say what would widen it:\n%s", narrowed)
	}
}

// The word that widens it. A listing that reads every project has to say which project each row is
// in, or the rows are a heap of identifiers with no address on any of them.
func TestTheSystemWordReadsEveryProjectAndMarksEachRow(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "atlantic-blue")
	mustRun(t, client, "project", "create", "quay-crew")
	mustRun(t, client, "job", "create", "--title", "the observability slice", "--brief", "wire it up")
	mustRun(t, client, "project", "create", "transcript")
	mustRun(t, client, "job", "create", "--title", "a page that turns a video into text", "--brief", "serve it")

	whole := mustRun(t, client, "job", "list", "system")
	if !strings.Contains(whole, "the observability slice") || !strings.Contains(whole, "a page that turns") {
		t.Fatalf("the system word did not read every project:\n%s", whole)
	}
	if !strings.Contains(whole, "atlantic-blue/quay-crew") || !strings.Contains(whole, "atlantic-blue/transcript") {
		t.Errorf("the rows do not say which project each job is in:\n%s", whole)
	}
	if !strings.Contains(whole, "2 jobs in this system") {
		t.Errorf("the listing does not say it read the whole system:\n%s", whole)
	}
}

// An empty listing has the same problem in reverse: "no jobs here yet" reads as a system with no work
// in it, when the work is one address away.
func TestAnEmptyJobListingSaysWhereItLooked(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "atlantic-blue")
	mustRun(t, client, "project", "create", "quay-crew")
	mustRun(t, client, "job", "create", "--title", "the observability slice", "--brief", "wire it up")
	mustRun(t, client, "project", "create", "transcript")

	empty := mustRun(t, client, "job", "list")
	if !strings.Contains(empty, "no jobs in atlantic-blue/transcript") {
		t.Errorf("an empty listing does not say where it looked:\n%s", empty)
	}
	if !strings.Contains(empty, "krewe job list system") {
		t.Errorf("an empty listing does not say what would widen it:\n%s", empty)
	}
}

// The same word on the other two listings that narrow to where you stand.
func TestTheSystemWordWidensEveryListingThatNarrows(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "task", "hello")
	mustRun(t, client, "workspace", "create", "elsewhere")
	mustRun(t, client, "project", "create", "other")

	if said := mustRun(t, client, "sessions", "system"); !strings.Contains(said, "house-bills") {
		t.Errorf("krewe sessions system did not read every workspace:\n%s", said)
	}
	if said := mustRun(t, client, "flow", "list", "system"); !strings.Contains(said, "this system") {
		t.Errorf("krewe flow list system did not say it read the whole system:\n%s", said)
	}
}

// The advice has to be advice that works. The sentence under a narrowed session listing used to
// read "krewe sessions on its own lists the whole system", and on its own is exactly what the operator
// had just typed: standing somewhere, it narrows again.
func TestTheWideningAdviceIsTypeable(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "task", "hello")
	mustRun(t, client, "workspace", "create", "elsewhere")
	mustRun(t, client, "project", "create", "other")

	narrowed := mustRun(t, client, "sessions")
	if strings.Contains(narrowed, "on its own") {
		t.Errorf("the listing offers advice that narrows again:\n%s", narrowed)
	}
	if !strings.Contains(narrowed, "krewe sessions system") {
		t.Errorf("the listing does not say what would widen it:\n%s", narrowed)
	}
	if said := mustRun(t, client, "sessions", "system"); !strings.Contains(said, "house-bills") {
		t.Errorf("the advice it gave does not widen anything:\n%s", said)
	}
}
