package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
)

// runTasks prints one session's history: what was asked, what came back, in the order it happened.
//
// This reads the history the dispatch path writes rather than the model's own conversation store, so it
// answers without starting a container, and it keeps answering for a session whose sandbox is long
// gone. What it does not have is the working inside a task, the tool calls and the thinking: for
// that, `quay attach` opens the conversation itself.
func runTasks(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: quay tasks <session>\n\na session is its id, its handle, or its address")
	}

	sessionID, err := resolveSession(ctx, client, args[0])
	if err != nil {
		return err
	}
	resp, err := client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: sessionID})
	if err != nil {
		return err
	}
	if len(resp.GetTasks()) == 0 {
		fmt.Fprintf(out, "no tasks recorded for %s\n", display.ShortID(sessionID))
		return nil
	}

	for _, task := range resp.GetTasks() {
		when := task.GetOccurredAt().AsTime().Local().Format("15:04:05")
		fmt.Fprintf(out, "%s  you  %s\n", when, oneLine(task.GetPrompt()))
		if task.GetStatus() == "failed" {
			fmt.Fprintf(out, "          failed: %s\n", oneLine(task.GetFailure()))
			continue
		}
		fmt.Fprintf(out, "          %s\n", oneLine(task.GetReply()))
	}
	return nil
}

// oneLine keeps a listing readable when a reply runs to paragraphs: a history is for finding the
// task you want, and `quay attach` is for reading it.
func oneLine(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	const most = 120
	if len(flat) <= most {
		return flat
	}
	return flat[:most] + "..."
}
