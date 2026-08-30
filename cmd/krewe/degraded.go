package main

import (
	"context"
	"fmt"
	"io"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/display"
)

// noDetail is what the line says instead of a reason when the system gave none. A part that is down
// and says nothing about why is still worth reporting, and the operator has somewhere to go.
const noDetail = "the system's own log says which write did not land"

// reportDegraded puts one line on the error stream for each part of the system its own last probe
// found down.
//
// The system already knew. On 29 August 2026 its event log had been gone for sixteen hours, the
// container health check had failed 1,467 times in a row, and that check was the only thing that
// read the answer. Every write still cost the whole export budget and every event went nowhere,
// while an operator worked through this tool all day against a system that answered every read.
//
// It goes on every command rather than on one, for the reason the drift line does: an operator
// chasing a defect types the command the defect is in, not the one that would have told them.
//
// It reads the last probe and never asks for a fresh one, so it costs a call that answers from
// memory. A system too old to have the call, and one that cannot be reached at all, both say nothing:
// this never refuses a command and never holds one up.
func reportDegraded(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, said io.Writer) {
	asking, giveUp := context.WithTimeout(ctx, driftTimeout)
	defer giveUp()
	answer, err := client.GetHealth(asking, &quaycrewv1.GetHealthRequest{})
	if err != nil {
		return
	}
	for _, line := range degraded(answer.GetComponents()) {
		fmt.Fprintln(said, line)
	}
}

// degraded is one line for each part of a reading that is down, naming the part and why.
//
// Down is the only state it says anything about. A system with no event log configured is a real system
// and a part nothing probes is the absence of a reading, so a line about either would print on every
// command forever and stop being read. All four states are said in the console's stats view, which
// is where the question "how is this system" is asked; this is for the operator who did not ask.
func degraded(components []*quaycrewv1.HealthComponent) []string {
	var lines []string
	for _, component := range components {
		if component.GetState() != display.HealthDown {
			continue
		}
		detail := component.GetDetail()
		if detail == "" {
			detail = noDetail
		}
		lines = append(lines, fmt.Sprintf(
			"krewe: this system is not serving: %s is down, so nothing it writes there lands. %s",
			component.GetName(), detail))
	}
	return lines
}
