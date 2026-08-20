package main

import (
	"strings"
	"testing"
)

// What is on the operator's screen has to be typeable back. It was not: the listing printed the
// session's own id, no command took it, and `quay dispatch me/website/a4db600a` came back with "this
// crew has no session a4db600a. it has: 5ae35d77", which named a value that was nowhere on the
// screen. Naming a session then took the handle off the screen as well, so nothing printed was
// usable at all.

// theIdentifierOnScreen is the first column of the crew's one session, copied the way an operator
// copies it.
func theIdentifierOnScreen(t *testing.T, listed string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(listed), "\n")
	if len(lines) < 2 {
		t.Fatalf("the listing has no session in it:\n%s", listed)
	}
	fields := strings.Fields(lines[1])
	if len(fields) == 0 {
		t.Fatalf("the listing's first row is empty:\n%s", listed)
	}
	return fields[0]
}

// The issue's own acceptance: every identifier the listing prints is taken by every command that
// names a session, with a label on the session and without one.
func TestEveryCommandTakesTheIdentifierTheListingPrints(t *testing.T) {
	for _, named := range []bool{false, true} {
		name := "with no name on the session"
		if named {
			name = "with a name on the session"
		}
		t.Run(name, func(t *testing.T) {
			client := testClient(t)
			mustRun(t, client, "workspace", "create", "me")
			mustRun(t, client, "project", "create", "website")
			mustRun(t, client, "dispatch", "remember this")
			if named {
				mustRun(t, client, "label", onlySession(t, client).GetHandle()[:8], "the electricity bill")
			}

			identifier := theIdentifierOnScreen(t, mustRun(t, client, "sessions"))

			// As an address, which is the form the issue was raised about.
			address := "me/website/" + identifier
			if said, err := runQuay(t, client, "dispatch", address, "carry on"); err != nil {
				t.Fatalf("quay dispatch %s: %v (%s)", address, err, said)
			}
			// And on its own, which is how the commands that name a session take one.
			for _, command := range [][]string{
				{"tasks", identifier},
				{"mode", identifier},
				{"label", identifier},
				{"use", address},
			} {
				if said, err := runQuay(t, client, command...); err != nil {
					t.Errorf("quay %s: %v (%s)", strings.Join(command, " "), err, said)
				}
			}
		})
	}
}

// A named session still says what it is called. The name is a column of its own now, rather than
// something that replaces the identifier and takes it off the screen.
func TestNamingASessionDoesNotTakeItsIdentifierOffTheScreen(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "website")
	mustRun(t, client, "dispatch", "remember this")

	handle := onlySession(t, client).GetHandle()[:8]
	mustRun(t, client, "label", handle, "the electricity bill")

	listed := mustRun(t, client, "sessions")
	if !strings.Contains(listed, handle) {
		t.Fatalf("naming the session took its identifier off the screen:\n%s", listed)
	}
	if !strings.Contains(listed, "the electricity bill") {
		t.Fatalf("the listing does not say what the session is called:\n%s", listed)
	}
	if theIdentifierOnScreen(t, listed) != handle {
		t.Fatalf("the first column is %q, want the handle %q", theIdentifierOnScreen(t, listed), handle)
	}
}

// The session's own id is in everybody's notes, in the console's actions and in a container's name,
// so it keeps working as an address even though the listing no longer prints it.
func TestTheSessionsOwnIdIsStillTakenAsAnAddress(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "website")
	mustRun(t, client, "dispatch", "remember this")

	id := onlySession(t, client).GetId()
	for _, reference := range []string{id, id[:8]} {
		if said, err := runQuay(t, client, "dispatch", "me/website/"+reference, "carry on"); err != nil {
			t.Errorf("quay dispatch me/website/%s: %v (%s)", reference, err, said)
		}
	}
	// One session throughout: an id that started a new conversation would be worse than a refusal.
	if listed := mustRun(t, client, "sessions"); !strings.Contains(listed, "1 in me/website") {
		t.Fatalf("an id as an address did not continue the session it names:\n%s", listed)
	}
}

// A refusal must offer what the operator can see. It used to name the handle while the screen showed
// the id, which reads as the crew having lost the session.
func TestARefusalOffersWhatTheListingPrints(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "website")
	mustRun(t, client, "dispatch", "remember this")

	onScreen := theIdentifierOnScreen(t, mustRun(t, client, "sessions"))
	err := refused(t, client, "use", "me/website/ffffffff")
	if !strings.Contains(err.Error(), onScreen) {
		t.Fatalf("the refusal says %q, and the screen says %q", err, onScreen)
	}
}
