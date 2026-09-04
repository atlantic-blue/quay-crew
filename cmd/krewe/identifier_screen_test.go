package main

import (
	"strings"
	"testing"
)

// What is on the operator's screen has to be typeable back, and an address is the form that says
// where the session is. Two shapes of it are not covered elsewhere: the whole session id, which is
// what the console acts on and what a container is named after, and the address a session scoped
// command takes when the operator moves into the session with `use`.

// theIdentifierOnScreen is the first cell of the system's one session, read the way an operator reads
// it: off the listing, rather than out of the code that produced it.
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

// The listing shortens the id, so every other case types eight characters. The whole id is what the
// console acts on, what names the sandbox container and what is in everybody's notes, and an address
// carrying it has to reach the session it names rather than start a second one.
func TestAnAddressCarriesTheWholeSessionId(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)
	id := onlySession(t, client).GetId()

	for _, reference := range []string{id, id[:8]} {
		if said, err := runKrewe(t, client, "exec", "me/house-bills/"+reference, "and again"); err != nil {
			t.Errorf("krewe exec me/house-bills/%s: %v (%s)", reference, err, said)
		}
	}
	// One session throughout. An address that started a second conversation would be worse than a
	// refusal, because nothing on the screen would say the job had gone somewhere else.
	if reached := onlySession(t, client); reached.GetId() != id {
		t.Fatalf("the address reached session %s, want %s", reached.GetId(), id)
	}
}

// Moving into a session is the surface the other cases leave out, and it is the one an operator
// reaches for after reading the listing. It takes the address, so the identifier printed in the
// session column has to work inside one, with a name on the session and without.
func TestUseTakesAnAddressCarryingTheIdentifierTheListingPrints(t *testing.T) {
	for _, named := range []bool{false, true} {
		name := "with no name on the session"
		if named {
			name = "with a name on the session"
		}
		t.Run(name, func(t *testing.T) {
			client, _ := aSessionWatchingTheModel(t)
			if named {
				mustRun(t, client, "label", onlySession(t, client).GetId()[:8], "the bills")
			}

			identifier := theIdentifierOnScreen(t, mustRun(t, client, "sessions"))

			address := "me/house-bills/" + identifier
			moved := mustRun(t, client, "use", address)
			if !strings.Contains(moved, identifier) {
				t.Fatalf("krewe use %s said %q, want the session it moved into", address, moved)
			}
		})
	}
}
