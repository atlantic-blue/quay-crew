package sandbox

import (
	"slices"
	"strings"
	"testing"
)

// The rename writes krewe- and reads quaycrew- for one release. These hold both halves: the name a
// new sandbox gets, and the name an operator's sandbox already has.

// TestASandboxIsNamedAfterTheCommandAPersonTypes.
func TestASandboxIsNamedAfterTheCommandAPersonTypes(t *testing.T) {
	if got := ContainerName("0123456789abcdef01234567"); got != "krewe-0123456789abcdef01234567" {
		t.Fatalf("a sandbox is created as %q, and the command is krewe", got)
	}
}

// TestTheNameTheSystemWritesIsTheFirstOneItReads. Both names are tried in order, so a sandbox started
// since the rename is found on the first question and one started before it on the second. Reversed,
// every reach into a session would cost a wasted lookup for the rest of the release.
func TestTheNameTheSystemWritesIsTheFirstOneItReads(t *testing.T) {
	got := ContainerNames("0123456789abcdef01234567")

	want := []string{"krewe-0123456789abcdef01234567", "quaycrew-0123456789abcdef01234567"}
	if !slices.Equal(got, want) {
		t.Fatalf("a session's container is looked for as %v, want %v", got, want)
	}
}

// TestASandboxStartedBeforeTheRenameIsStillItsSessions is the way off the old name, which is the half
// nobody writes. An operator upgrades with sessions up. Every one of those containers carries the
// retired name, and a system that could not read it would not drain them, would not remove them, and
// would start a second container beside each one on the next task.
func TestASandboxStartedBeforeTheRenameIsStillItsSessions(t *testing.T) {
	for name, container := range map[string]string{
		"started by this build":        "krewe-0123456789abcdef01234567",
		"started before the rename":    "quaycrew-0123456789abcdef01234567",
		"started before it, and dirty": "quaycrew-aaaaaaaaaaaaaaaaaaaaaaaa",
	} {
		t.Run(name, func(t *testing.T) {
			id, isSandbox := SessionOf(container)
			if !isSandbox {
				t.Fatalf("%s is a session's sandbox and the system reads it as somebody else's container", container)
			}
			if want := container[strings.Index(container, "-")+1:]; id != want {
				t.Fatalf("%s belongs to session %q, want %q", container, id, want)
			}
		})
	}
}

// TestTheStacksOwnContainersAreNeverTakenForASandbox. The compose project is still called quaycrew,
// so the system's own store, broker and dashboards carry the retired prefix. A reader looser than the
// exact shape of a session identifier reaps the stack it is running on, which has happened before,
// and reading both prefixes is what makes that easier to do rather than harder.
func TestTheStacksOwnContainersAreNeverTakenForASandbox(t *testing.T) {
	for _, container := range []string{
		"quaycrew-postgres-1",
		"quaycrew-redpanda-1",
		"quaycrew-controlplane-1",
		"krewe-postgres-1",
		"qcci-controlplane-1",
		"quaycrew-0123456789abcdef0123456",  // one character short of a session
		"krewe-0123456789abcdef012345678",   // one character over
		"krewe-0123456789ABCDEF01234567",    // a session identifier is lowercase
		"krewe-sandbox-claude",              // the image, not a container
		"my-krewe-0123456789abcdef01234567", // the prefix has to start the name
		"quaycrew-0123456789abcdef0123456z", // not hexadecimal
	} {
		if id, isSandbox := SessionOf(container); isSandbox {
			t.Errorf("%s is read as session %q, and stopping it takes something nobody meant to stop", container, id)
		}
	}
}

// TestTheDaemonsListingIsReadUnderBothNames is what a drain and an upgrade's sweep both walk. The
// listing is the daemon's own shape: one name per line, the stack's own services among the sandboxes.
func TestTheDaemonsListingIsReadUnderBothNames(t *testing.T) {
	listing := strings.Join([]string{
		"quaycrew-postgres-1",
		"quaycrew-aaaaaaaaaaaaaaaaaaaaaaaa",
		"qcci-controlplane-1",
		"krewe-bbbbbbbbbbbbbbbbbbbbbbbb",
		"grafana",
	}, "\n")

	got := sessionsAmong(listing)

	want := []string{"aaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbb"}
	if !slices.Equal(got, want) {
		t.Fatalf("the daemon holds sandboxes for %v, want %v", got, want)
	}
}
