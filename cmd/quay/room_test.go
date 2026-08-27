package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/headroom"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

const mebibyte = int64(1 << 20)

// aMachineWithNoAccounting is every Mac: there is no /proc/meminfo to read, so the command that used
// to answer from inside a sandbox has nothing to read at all.
func aMachineWithNoAccounting() fstest.MapFS { return fstest.MapFS{} }

// aSandbox is what a session stands on: a Linux machine that keeps its own memory accounting.
func aSandbox() fstest.MapFS {
	return fstest.MapFS{"proc/meminfo": &fstest.MapFile{
		Data: []byte("MemTotal:       8024876 kB\nMemFree:          208832 kB\nMemAvailable:   1539300 kB\n"),
	}}
}

// theMachineTheCrewReads is the machine on 27 August 2026, as the incident recorded it.
type theMachineTheCrewReads struct{}

func (theMachineTheCrewReads) Sample(context.Context) (headroom.Sample, error) {
	return headroom.Sample{
		Used:  headroom.Measured(3628 * mebibyte),
		Limit: headroom.Measured(7837 * mebibyte),
		Machine: headroom.Machine{
			Name:      "Docker Desktop",
			Total:     headroom.Measured(7837 * mebibyte),
			Available: headroom.Measured(1503 * mebibyte),
			SwapTotal: headroom.Measured(17408 * mebibyte),
			SwapUsed:  headroom.Measured(16402 * mebibyte),
		},
		Sandboxes: []headroom.Sandbox{
			{Session: "a00d36d6454a3de66d02c6a3", Held: headroom.Measured(1201 * mebibyte),
				Processor: headroom.MeasuredShare(42.5)},
		},
		TakenAt: time.Now(),
	}, nil
}

// aCrewThatReadsTheMachine is a crew with a machine behind it, already sampled.
func aCrewThatReadsTheMachine(t *testing.T, source headroom.Source) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Headroom: source, HeadroomEvery: time.Hour,
	})
	server.SampleHeadroom(context.Background())
	return testClientFor(t, server)
}

// A session inside a sandbox reads the machine it stands on, and asks the crew nothing. That is the
// question a session about to run a gate has, and the answer talks to no crew at all.
func TestInsideASandboxTheCommandReadsTheMachineItStandsOn(t *testing.T) {
	out := &bytes.Buffer{}
	if err := roomOf(context.Background(), nil, aSandbox(), out); err != nil {
		t.Fatalf("room: %v", err)
	}
	said := out.String()
	for _, want := range []string{"this sandbox has no memory limit of its own", "7836 MiB"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the session was told:\n%s", said)
		}
	}
	if strings.Contains(said, "Docker Desktop") {
		t.Fatal("the session was told about the crew's machine, and it asked about its own")
	}
}

// Off a machine that keeps no accounting, which is every Mac, the command used to fail outright. So
// the operator most likely to need the figure was the one who could not have it.
func TestOffAMachineWithNoAccountingTheCrewAnswersInstead(t *testing.T) {
	client := aCrewThatReadsTheMachine(t, theMachineTheCrewReads{})
	out := &bytes.Buffer{}
	if err := roomOf(context.Background(), client, aMachineWithNoAccounting(), out); err != nil {
		t.Fatalf("room: %v", err)
	}
	said := out.String()
	for _, want := range []string{
		"3628 MiB",                 // what every container holds
		"7837 MiB",                 // the limit that binds
		"4209 MiB",                 // and so what is left
		headroom.StateRoom,         // the word
		"Docker Desktop",           // the machine underneath, named rather than assumed
		"16402 MiB",                // its swap, which is the pressure the daemon's own figure hides
		"a00d36d6454a3de66d02c6a3", // and which session to stop
		"1201 MiB",
	} {
		if !strings.Contains(said, want) {
			t.Fatalf("the operator was told, and it does not carry %q:\n%s", want, said)
		}
	}
}

// Both roads shut. The refusal names both, because an operator told only about the crew goes looking
// for a crew fault when the answer is which machine they are standing on.
func TestWithNoAccountingAndNoCrewTheRefusalNamesBoth(t *testing.T) {
	client := clientOfACrewThatIsNotUp(t)
	out := &bytes.Buffer{}
	err := roomOf(context.Background(), client, aMachineWithNoAccounting(), out)
	if err == nil {
		t.Fatalf("the command answered from nothing:\n%s", out.String())
	}
	said := err.Error()
	if !strings.Contains(said, "linux sandbox") {
		t.Fatalf("the refusal does not say which machine this is: %q", said)
	}
	if !strings.Contains(said, "the crew could not be asked either") {
		t.Fatalf("the refusal does not say the crew was asked too: %q", said)
	}
}

// Rule five, at the surface an operator reads. A crew that measured nothing says the word, and never
// a zero: an operator reads zero as a machine with room on it.
func TestACrewThatMeasuredNothingSaysUnknownRatherThanZero(t *testing.T) {
	client := aCrewThatReadsTheMachine(t, nil)
	out := &bytes.Buffer{}
	if err := roomOf(context.Background(), client, aMachineWithNoAccounting(), out); err != nil {
		t.Fatalf("room: %v", err)
	}
	said := out.String()
	if !strings.Contains(said, "has not read its machine yet") {
		t.Fatalf("the operator was told:\n%s", said)
	}
	if strings.Contains(said, "0 MiB") {
		t.Fatalf("a machine nobody read is reported as holding nothing:\n%s", said)
	}
}
