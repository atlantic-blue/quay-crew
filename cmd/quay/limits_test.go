package main

import (
	"strings"
	"testing"
)

// A ceiling is what a workspace lets its sessions declare, and an operator reads and sets it here. A
// session cannot: a caller that could raise its own ceiling has none.

func TestLimitsReadsTheDefaultsAndSaysWhatZeroMeans(t *testing.T) {
	client := aCrewToJobIn(t)

	said := mustRun(t, client, "limits")

	for _, want := range []string{"max depth      0", "no session here may declare a job", "max running    unset"} {
		if !strings.Contains(said, want) {
			t.Errorf("quay limits does not say %q: %q", want, said)
		}
	}
	if !strings.Contains(said, "quay limits") {
		t.Fatalf("quay limits does not say how to raise one: %q", said)
	}
}

func TestLimitsSetsTheCeilingAndReadsItBack(t *testing.T) {
	client := aCrewToJobIn(t)

	said := mustRun(t, client, "limits", "me", "--max-depth", "2", "--max-running", "4",
		"--budget-tokens", "5000", "--lease", "90s")

	for _, want := range []string{"max depth      2", "max running    4", "budget tokens  5000", "lease          1m30s"} {
		if !strings.Contains(said, want) {
			t.Errorf("quay limits does not say %q: %q", want, said)
		}
	}
	read := mustRun(t, client, "limits", "me")
	if !strings.Contains(read, "max depth      2") {
		t.Fatalf("the ceiling did not survive being set: %q", read)
	}
}

// The lease reads as the length of a job and it is not one. It is the crew's hold on a job, renewed
// on every tick, and it does not reach the credential a session runs under. An operator who read the
// two as one number set this to cover their work and got no change to the credential at all, so the
// line says what it is not, next to the number.
func TestLimitsSaysTheLeaseIsNotTheLifeOfACredential(t *testing.T) {
	client := aCrewToJobIn(t)

	said := mustRun(t, client, "limits", "me", "--lease", "15m")

	for _, want := range []string{"lease          15m", "not the life of a session's credential"} {
		if !strings.Contains(said, want) {
			t.Errorf("quay limits does not say %q: %q", want, said)
		}
	}
}

// Setting one number leaves the rest, because the tool reads the row first and sends it back whole.
func TestSettingOneLimitLeavesTheOthers(t *testing.T) {
	client := aCrewToJobIn(t)
	mustRun(t, client, "limits", "me", "--max-depth", "2", "--max-running", "4")

	said := mustRun(t, client, "limits", "me", "--max-depth", "3")

	if !strings.Contains(said, "max depth      3") {
		t.Fatalf("the depth is not what was just set: %q", said)
	}
	if !strings.Contains(said, "max running    4") {
		t.Fatalf("setting the depth cleared the concurrency: %q", said)
	}
}

func TestALimitThatIsNotANumberIsRefused(t *testing.T) {
	client := aCrewToJobIn(t)

	_, err := runQuay(t, client, "limits", "me", "--max-depth", "deep")
	if err == nil {
		t.Fatal("a depth that is not a number was accepted")
	}
	if !strings.Contains(err.Error(), "--max-depth") {
		t.Fatalf("the refusal says %q, want it to name the flag", err)
	}
}

func TestALeaseThatIsNotALengthOfTimeIsRefused(t *testing.T) {
	client := aCrewToJobIn(t)

	_, err := runQuay(t, client, "limits", "me", "--lease", "a while")
	if err == nil {
		t.Fatal("a lease that is not a length of time was accepted")
	}
	if !strings.Contains(err.Error(), "60s") {
		t.Fatalf("the refusal says %q, want it to show the shape of a length of time", err)
	}
}

// The crew's refusal reaches the operator whole.
func TestALimitBelowZeroIsRefusedByTheCrew(t *testing.T) {
	client := aCrewToJobIn(t)

	_, err := runQuay(t, client, "limits", "me", "--max-depth", "-1")
	if err == nil {
		t.Fatal("a depth below zero was accepted")
	}
	if !strings.Contains(err.Error(), "below zero") {
		t.Fatalf("the refusal says %q, want the crew's own sentence", err)
	}
}

// The flags this command takes reach it, and a flag no command takes is still refused.
func TestTheFlagsLimitsTakesReachTheCommand(t *testing.T) {
	client := aCrewToJobIn(t)

	if _, err := runQuay(t, client, "limits", "me", "--max-running", "2"); err != nil {
		t.Fatalf("a flag quay limits takes was refused: %v", err)
	}
	if _, err := runQuay(t, client, "limits", "me", "--max-height", "2"); err == nil {
		t.Fatal("a flag no command takes was accepted")
	}
}

func TestLimitsNeedsAWorkspace(t *testing.T) {
	client := testClient(t)

	_, err := runQuay(t, client, "limits")
	if err == nil {
		t.Fatal("a ceiling was read with no workspace to read it for")
	}
}

// The two times a session's life is measured by. They ship unset, and unset has to say out loud that
// the crew does nothing: a reader who has met a timeout before reads a missing number as a default.
func TestTheReclaimAndArchiveTimesReadAsUnsetAndSayWhatThatMeans(t *testing.T) {
	client := aCrewToJobIn(t)

	said := mustRun(t, client, "limits")

	for _, want := range []string{
		"reclaim        unset", "no session here gives its container back",
		"archive        unset", "nothing here is filed away on its own",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("quay limits does not say %q: %q", want, said)
		}
	}
}

func TestTheReclaimAndArchiveTimesAreSetAndReadBack(t *testing.T) {
	client := aCrewToJobIn(t)

	said := mustRun(t, client, "limits", "me", "--reclaim", "15m", "--archive", "24h")

	for _, want := range []string{"reclaim        15m0s", "archive        24h0m0s"} {
		if !strings.Contains(said, want) {
			t.Errorf("quay limits does not say %q: %q", want, said)
		}
	}
	read := mustRun(t, client, "limits", "me")
	if !strings.Contains(read, "reclaim        15m0s") {
		t.Fatalf("the reclaim time did not survive being set: %q", read)
	}
	// Set, so the sentence about what unset does is gone.
	if strings.Contains(read, "gives its container back") {
		t.Fatalf("the answer still says what unset means over a number that is set: %q", read)
	}
}

func TestATimeThatIsNotALengthIsRefused(t *testing.T) {
	client := aCrewToJobIn(t)

	for _, flag := range []string{"--reclaim", "--archive"} {
		if _, err := runQuay(t, client, "limits", "me", flag, "soon"); err == nil {
			t.Fatalf("%s soon was accepted", flag)
		}
	}
}
