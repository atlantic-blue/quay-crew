package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/forge"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The line above every command. This is the surface for the operator who did not go looking: the
// briefing is a page they open, the job listing is a command they type, and this appears whatever
// they typed.

func aWait(id, why, want string, seconds int64, over bool) *quaycrewv1.Waiting {
	return &quaycrewv1.Waiting{Job: id, Why: why, Want: want, WaitedSeconds: seconds, OverLimit: over}
}

// The whole point: whatever the operator typed, they are told.
func TestTheLineNamesEveryJobThatWaits(t *testing.T) {
	lines := waitingLines([]*quaycrewv1.Waiting{
		aWait("f71415ba9c2e4d1a8b3c5d7e", job.WaitingAsking, "aurora or a key value store?", 3840, true),
		aWait("fe7bfea71c2e4d1a8b3c5d7e", job.WaitingBlocked, "the sandbox could not be made", 60, false),
	})

	said := strings.Join(lines, "\n")
	if !strings.Contains(said, "2 jobs wait for you") {
		t.Fatalf("the telling does not say how many wait:\n%s", said)
	}
	for _, want := range []string{"f71415ba", "aurora or a key value store?", "fe7bfea7", "the sandbox could not be made"} {
		if !strings.Contains(said, want) {
			t.Errorf("the telling does not carry %q:\n%s", want, said)
		}
	}
	if !strings.Contains(said, "waited 1 hour 4 minutes") {
		t.Errorf("the telling does not name the age of the long wait:\n%s", said)
	}
	if !strings.Contains(said, "krewe job answer") {
		t.Errorf("the telling does not say how to answer one:\n%s", said)
	}
}

// Nothing waiting prints nothing at all. A line on every command forever is a line nobody reads, and
// then the one that matters is invisible too.
func TestNothingWaitingPrintsNoLine(t *testing.T) {
	if lines := waitingLines(nil); len(lines) != 0 {
		t.Fatalf("a system with nothing waiting printed %v", lines)
	}
}

// The command the operator typed is what they came for, so a system with nine waits does not push
// their answer off the screen. What is left out is said out loud rather than quietly dropped.
func TestALongQueueIsCutAndSaysSo(t *testing.T) {
	var many []*quaycrewv1.Waiting
	for _, id := range []string{"aaaaaaaa1111", "bbbbbbbb2222", "cccccccc3333", "dddddddd4444", "eeeeeeee5555"} {
		many = append(many, aWait(id, job.WaitingAsking, "which store?", 10, false))
	}

	lines := waitingLines(many)
	said := strings.Join(lines, "\n")
	if len(lines) != 1+waitingAtMost+1 {
		t.Fatalf("five waiting jobs printed %d lines:\n%s", len(lines), said)
	}
	if !strings.Contains(said, "and 2 more") {
		t.Fatalf("the cut is silent, so three reads as all of them:\n%s", said)
	}
	if !strings.Contains(said, "krewe job list") {
		t.Errorf("the cut does not say where to read the rest:\n%s", said)
	}
}

// The line under a conversation is the one surface always in front of the person typing, so it
// carries the count beside the context window.
func TestTheStatusLineCarriesWhatWaits(t *testing.T) {
	held := conversationDir
	conversationDir = t.TempDir()
	defer func() { conversationDir = held }()

	client := &waitingClient{waiting: []*quaycrewv1.Waiting{
		aWait("f71415ba9c2e4d1a8b3c5d7e", job.WaitingAsking, "which store?", 30, false),
		aWait("fe7bfea71c2e4d1a8b3c5d7e", job.WaitingBlocked, "it stopped", 60, false),
	}}
	var out bytes.Buffer
	if err := runStatusLine(context.Background(), client, nil,
		strings.NewReader(string(payload(124_000, 1_000_000))), &out); err != nil {
		t.Fatalf("krewe statusline: %v", err)
	}

	printed := out.String()
	if !strings.Contains(printed, "context 12% used") {
		t.Errorf("the line lost the context it already carried: %q", printed)
	}
	if !strings.Contains(printed, "2 jobs wait for you") {
		t.Errorf("the line does not carry what waits: %q", printed)
	}
	if strings.Count(strings.TrimSpace(printed), "\n") != 0 {
		t.Errorf("the status line is more than one line: %q", printed)
	}
}

// Nothing waiting leaves the line exactly as it was. A status line saying "nothing waits for you" on
// every redraw is a line nobody reads by the second day.
func TestTheStatusLineSaysNothingWhenNothingWaits(t *testing.T) {
	held := conversationDir
	conversationDir = t.TempDir()
	defer func() { conversationDir = held }()

	var out bytes.Buffer
	if err := runStatusLine(context.Background(), &waitingClient{}, nil,
		strings.NewReader(string(payload(124_000, 1_000_000))), &out); err != nil {
		t.Fatalf("krewe statusline: %v", err)
	}
	if strings.TrimSpace(out.String()) != "context 12% used (124k of 1M)" {
		t.Fatalf("the line says something about waiting when nothing does: %q", out.String())
	}
}

// The runtime redraws this several times a second, and the count changes when a job stops. So the
// answer is remembered for as long as the console waits between its own refreshes, and the draws in
// between cost nothing.
func TestTheStatusLineAsksTheSystemOnceForABurstOfDraws(t *testing.T) {
	held := conversationDir
	conversationDir = t.TempDir()
	defer func() { conversationDir = held }()

	client := &waitingClient{waiting: []*quaycrewv1.Waiting{
		aWait("f71415ba9c2e4d1a8b3c5d7e", job.WaitingAsking, "which store?", 30, false),
	}}
	for draw := 0; draw < 5; draw++ {
		var out bytes.Buffer
		if err := runStatusLine(context.Background(), client, nil,
			strings.NewReader(string(payload(124_000, 1_000_000))), &out); err != nil {
			t.Fatalf("krewe statusline: %v", err)
		}
		if !strings.Contains(out.String(), "1 job waits for you") {
			t.Fatalf("draw %d lost the telling: %q", draw, out.String())
		}
	}
	if client.asked != 1 {
		t.Fatalf("five draws asked the system %d times", client.asked)
	}
}

// A system this cannot reach leaves the line as the context alone, rather than as an error where the
// conversation should be.
func TestTheStatusLineIsDrawnWhenTheSystemCannotBeReached(t *testing.T) {
	held := conversationDir
	conversationDir = t.TempDir()
	defer func() { conversationDir = held }()

	var out bytes.Buffer
	if err := runStatusLine(context.Background(), &waitingClient{refuse: errors.New("connection refused")},
		nil, strings.NewReader(string(payload(124_000, 1_000_000))), &out); err != nil {
		t.Fatalf("krewe statusline: %v", err)
	}
	if !strings.Contains(out.String(), "context 12% used") {
		t.Fatalf("an unreachable system took the whole line with it: %q", out.String())
	}
}

// waitingClient answers the one call these make, and counts them.
type waitingClient struct {
	quaycrewv1.ControlPlaneServiceClient
	waiting []*quaycrewv1.Waiting
	refuse  error
	asked   int
}

func (w *waitingClient) GetWaiting(_ context.Context, _ *quaycrewv1.GetWaitingRequest, _ ...grpc.CallOption) (
	*quaycrewv1.GetWaitingResponse, error) {
	w.asked++
	if w.refuse != nil {
		return nil, w.refuse
	}
	return &quaycrewv1.GetWaitingResponse{Waiting: w.waiting}, nil
}

// The gap between the question and the telling is the number this work is judged on, so it is
// printed where somebody reads a job back.
func TestJobShowPrintsTheGapBetweenAskingAndBeingTold(t *testing.T) {
	asked := time.Now().Add(-70 * time.Minute)
	var out bytes.Buffer
	sayWhenItWasTold(&out, &quaycrewv1.Job{
		Phase:    job.PhaseAsking,
		Question: "aurora or a key value store?",
		AskedAt:  timestamppb.New(asked),
		RaisedAt: timestamppb.New(asked.Add(64 * time.Minute)),
	})

	printed := out.String()
	for _, want := range []string{"asked at:", "told at:", "1 hour 4 minutes"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the reading does not carry %q:\n%s", want, printed)
		}
	}
}

// A wait nobody has been told about says so, which is the state the whole incident was in. Printing
// nothing there would read as a job nobody ever asked anything of.
func TestJobShowSaysWhenNobodyHasBeenTold(t *testing.T) {
	var out bytes.Buffer
	sayWhenItWasTold(&out, &quaycrewv1.Job{AskedAt: timestamppb.New(time.Now().Add(-time.Hour))})

	if !strings.Contains(out.String(), "told to nobody yet") {
		t.Fatalf("a wait nobody carried reads as:\n%s", out.String())
	}
}

// A job that asked nothing prints neither line, so an ordinary job's reading is unchanged.
func TestJobShowSaysNothingAboutAJobThatNeverAsked(t *testing.T) {
	var out bytes.Buffer
	sayWhenItWasTold(&out, &quaycrewv1.Job{})
	if out.String() != "" {
		t.Fatalf("a job that never asked prints:\n%s", out.String())
	}
}

// The gap is the number this work is judged on, so it has to belong to the wait a person is in now.
// A job that asked, was answered, ran on and then failed still carries the moment of that first
// question, and dating this wait from it reports the whole run as time somebody spent not knowing.
func TestJobShowMeasuresABlockedWaitFromWhenTheJobStopped(t *testing.T) {
	now := time.Now()
	var out bytes.Buffer
	sayWhenItWasTold(&out, &quaycrewv1.Job{
		Phase:     job.PhaseFailed,
		Reason:    "the container went away",
		AskedAt:   timestamppb.New(now.Add(-3 * time.Hour)),
		UpdatedAt: timestamppb.New(now.Add(-10 * time.Minute)),
		RaisedAt:  timestamppb.New(now.Add(-9 * time.Minute)),
	})

	printed := out.String()
	if strings.Contains(printed, "2 hours") || strings.Contains(printed, "3 hours") {
		t.Fatalf("the wait is dated from a question answered hours ago:\n%s", printed)
	}
	if !strings.Contains(printed, "1 minute") {
		t.Fatalf("the gap does not read as the minute somebody spent not knowing:\n%s", printed)
	}
	// The question belonged to a wait that ended, so this reading must not present it as this one.
	if strings.Contains(printed, "asked at:") {
		t.Fatalf("a wait nobody asked about prints a question:\n%s", printed)
	}
}

// A red board is the third kind of wait, and nothing records the moment the checks turned red: the
// forge reading deliberately leaves the row alone. So this says what it knows rather than dating the
// wait from a question that belongs to something else.
func TestJobShowSaysWhatItCannotMeasureOnARedBoard(t *testing.T) {
	now := time.Now()
	var out bytes.Buffer
	sayWhenItWasTold(&out, &quaycrewv1.Job{
		Phase:             job.PhaseDone,
		PullRequest:       "https://github.com/atlantic-blue/quay-krewe/pull/633",
		PullRequestChecks: forge.ChecksRed,
		AskedAt:           timestamppb.New(now.Add(-3 * time.Hour)),
		UpdatedAt:         timestamppb.New(now.Add(-40 * time.Minute)),
		PullRequestReadAt: timestamppb.New(now.Add(-12 * time.Minute)),
		RaisedAt:          timestamppb.New(now.Add(-11 * time.Minute)),
	})

	printed := out.String()
	if strings.Contains(printed, "2 hours") || strings.Contains(printed, "3 hours") {
		t.Fatalf("a red board is dated from a question answered hours ago:\n%s", printed)
	}
	if !strings.Contains(printed, "nothing records when the checks turned red") {
		t.Fatalf("the reading does not say what it cannot measure:\n%s", printed)
	}
}
