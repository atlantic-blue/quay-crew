package forge_test

import (
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/forge"
)

// The refusals first. A reading nobody took must never read as a passing one, so what this package
// says about an absence is worth more than what it says about an answer.

func TestAReadingNobodyTookIsUnknownAndIsNotGreen(t *testing.T) {
	nothing := forge.Unread()
	if nothing.Status != forge.StatusUnknown || nothing.Checks != forge.ChecksUnknown {
		t.Fatalf("a reading nobody took says %q and %q", nothing.Status, nothing.Checks)
	}
	if nothing.Review != forge.ReviewUnknown {
		t.Fatalf("a review nobody read says %q", nothing.Review)
	}
	if nothing.Taken() {
		t.Fatal("a reading nobody took says it was taken")
	}
	if nothing.Red() {
		t.Fatal("a reading nobody took says a check went red")
	}
	if nothing.Settled() {
		t.Fatal("a pull request nobody read is settled, so nothing would ever read it")
	}
	if said := nothing.String(); !strings.Contains(said, "nothing has read it yet") {
		t.Fatalf("a reading nobody took reads as %q", said)
	}
}

// The zero value is what a row written before these columns existed holds, and it must read exactly
// like a pull request nobody has got to yet.
func TestTheZeroReadingReadsAsUnknown(t *testing.T) {
	var empty forge.Reading
	filled := empty.Or()
	if filled.Status != forge.StatusUnknown || filled.Checks != forge.ChecksUnknown ||
		filled.Review != forge.ReviewUnknown {
		t.Fatalf("the zero reading fills in as %+v", filled)
	}
	if empty.Red() || empty.Settled() || empty.Taken() {
		t.Fatal("the zero reading claims something was read")
	}
}

// A read that was attempted and failed keeps its reason, stays unknown, and is still worth reading
// again. A failure that settled a pull request would freeze it as unknown for ever.
func TestAReadThatFailedIsUnknownWithItsReason(t *testing.T) {
	at := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	failed := forge.Unreadable(at, "the rate limit is spent")
	if failed.Status != forge.StatusUnknown || failed.Checks != forge.ChecksUnknown {
		t.Fatalf("a failed read says %q and %q", failed.Status, failed.Checks)
	}
	if failed.Taken() {
		t.Fatal("a read that failed reports itself as a reading")
	}
	if failed.Settled() {
		t.Fatal("a read that failed settled the pull request, so nothing would read it again")
	}
	if !strings.Contains(failed.String(), "the rate limit is spent") {
		t.Fatalf("a failed read reads as %q and never says why", failed)
	}
}

func TestOnlyMergedAndClosedAreSettled(t *testing.T) {
	for _, one := range []struct {
		status  string
		settled bool
	}{
		{forge.StatusMerged, true},
		{forge.StatusClosed, true},
		{forge.StatusOpen, false},
		{forge.StatusUnknown, false},
		{"", false},
	} {
		if got := (forge.Reading{Status: one.status}).Settled(); got != one.settled {
			t.Fatalf("%q settled = %v, want %v", one.status, got, one.settled)
		}
	}
}

func TestWhatAPersonReadsBesideTheAddress(t *testing.T) {
	read := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	for _, one := range []struct {
		name    string
		reading forge.Reading
		says    string
	}{
		{
			name:    "a pull request that merged with everything green",
			reading: forge.Reading{Status: forge.StatusMerged, Checks: forge.ChecksGreen, Review: forge.ReviewApproved, ReadAt: read},
			says:    "merged, checks green",
		},
		{
			name:    "a red check names the check",
			reading: forge.Reading{Status: forge.StatusOpen, Checks: forge.ChecksRed, FailedCheck: "unit", Review: forge.ReviewNone, ReadAt: read},
			says:    "open, checks red: unit",
		},
		{
			name:    "a review that asked for changes is said out loud",
			reading: forge.Reading{Status: forge.StatusOpen, Checks: forge.ChecksGreen, Review: forge.ReviewChangesRequested, ReadAt: read},
			says:    "a review asked for changes",
		},
		{
			name:    "a pull request with no checks at all",
			reading: forge.Reading{Status: forge.StatusOpen, Checks: forge.ChecksNone, Review: forge.ReviewNone, ReadAt: read},
			says:    "open, no checks",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			if said := one.reading.String(); !strings.Contains(said, one.says) {
				t.Fatalf("it reads as %q, and never says %q", said, one.says)
			}
		})
	}
}

// The address is what a forge is asked about, so anything that is not one is refused rather than
// guessed at: a guessed number is a number the system would then go and read.
func TestWhatIsNotAPullRequestAddressIsRefused(t *testing.T) {
	for _, typed := range []string{
		"",
		"not an address at all",
		"https://github.com/atlantic-blue/quay-crew",
		"https://github.com/atlantic-blue/quay-crew/issues/549",
		"https://github.com/atlantic-blue/quay-crew/pull/",
		"https://github.com/atlantic-blue/quay-crew/pull/nine",
		"https://github.com/atlantic-blue/quay-crew/pull/0",
		"https://github.com/atlantic-blue/quay-crew/pull/12/files",
	} {
		if _, err := forge.Parse(typed); err == nil {
			t.Fatalf("%q was read as a pull request address", typed)
		}
	}
}

func TestAPullRequestAddressIsItsThreeParts(t *testing.T) {
	at, err := forge.Parse("https://github.com/atlantic-blue/quay-crew/pull/549")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if at.Host != "github.com" || at.Repository() != "atlantic-blue/quay-crew" || at.Number != 549 {
		t.Fatalf("the address reads as %+v", at)
	}
	if at.String() != "https://github.com/atlantic-blue/quay-crew/pull/549" {
		t.Fatalf("the address writes back as %q", at)
	}
}
