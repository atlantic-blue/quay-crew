package main

import (
	"strings"
	"testing"
)

// A listing narrowed to where the operator is standing looks exactly like a system that holds
// nothing else. The operator ran a listing, read one row, and asked where the other nine were:
// they were in the project next door, and nothing on the screen said a scope had been applied.
// So every listing names what it read.
func TestEveryListingNamesTheAddressItRead(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "exec", "read the bill")

	for _, listing := range []struct {
		args  []string
		names string
	}{
		{[]string{"sessions"}, "me/house-bills"},
		{[]string{"workspace", "list"}, "this system"},
		{[]string{"project", "list"}, "this system"},
		{[]string{"project", "list", "me"}, "me"},
		{[]string{"secret", "list"}, "this system"},
		{[]string{"skill", "list"}, "the system"},
		{[]string{"hook", "list"}, "the system"},
	} {
		said := mustRun(t, client, listing.args...)
		if !strings.Contains(said, listing.names) {
			t.Errorf("krewe %s does not say it read %s:\n%s",
				strings.Join(listing.args, " "), listing.names, said)
		}
	}
}

// The word that widens a listing narrowed to where you stand.
func TestTheSystemWordWidensEveryListingThatNarrows(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "exec", "hello")
	mustRun(t, client, "workspace", "create", "elsewhere")
	mustRun(t, client, "project", "create", "other")

	if said := mustRun(t, client, "sessions", "system"); !strings.Contains(said, "house-bills") {
		t.Errorf("krewe sessions system did not read every workspace:\n%s", said)
	}
}

// The advice has to be advice that works. The sentence under a narrowed session listing used to
// read "krewe sessions on its own lists the whole system", and on its own is exactly what the operator
// had just typed: standing somewhere, it narrows again.
func TestTheWideningAdviceIsTypeable(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "exec", "hello")
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
