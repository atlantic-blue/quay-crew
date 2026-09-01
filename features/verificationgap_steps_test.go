package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/cucumber/godog"
)

// The method a verifier is given for telling a green check from a real one.
//
// What is read is the memory file inside the session's own container, rather than the role in the
// store or the file in roles/. A brief that reached the row and not the container is a method
// nothing ever opens, and the store cannot tell the two apart.
func initializeVerificationGapSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the brief that session reads carries "([^"]*)"$`,
		func(ctx context.Context, want string) error {
			brief, err := briefTheJobSessionReads(ctx)
			if err != nil {
				return err
			}
			if !strings.Contains(brief, want) {
				return fmt.Errorf("the brief in front of that session does not say %q", want)
			}
			return nil
		})

	sc.Step(`^the brief that session reads does not carry "([^"]*)"$`,
		func(ctx context.Context, absent string) error {
			brief, err := briefTheJobSessionReads(ctx)
			if err != nil {
				return err
			}
			if strings.Contains(brief, absent) {
				return fmt.Errorf("the brief in front of that session says %q, which belongs to another role", absent)
			}
			return nil
		})
}

// briefTheJobSessionReadsFloor is a floor on the file, not a measurement. An empty memory file
// carries no sentence of the method and no sentence of anything else, so without it a session that
// was told nothing would satisfy every step above that only asks what is absent.
const briefTheJobSessionReadsFloor = 1024

// briefTheJobSessionReads is the memory file the session doing the job opens.
//
// The task is detached, so the container and its files are built while the scenario is already past
// the tick. The read is waited for rather than taken once, which is what keeps this a check on what
// the session was given rather than a race with the machine it runs on.
func briefTheJobSessionReads(ctx context.Context) (string, error) {
	session, err := sessionDoingTheJob(ctx)
	if err != nil {
		return "", err
	}

	var body string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if read, found := memoryOfSession(ctx, session.GetId()); found {
			body = read
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if body == "" {
		return "", fmt.Errorf("session %s was given no memory file, so no brief is in front of it", session.GetId())
	}
	if len(body) < briefTheJobSessionReadsFloor {
		return "", fmt.Errorf("the memory file is %d bytes, so there is no brief in it to read:\n%s", len(body), body)
	}
	return body, nil
}

// memoryOfSession reads the file through the configuration the system built that session's container
// from, which is the same road the container itself takes to it.
func memoryOfSession(ctx context.Context, session string) (string, bool) {
	w := worldFrom(ctx)
	for _, box := range w.provider.Configurations() {
		if box.ID != session {
			continue
		}
		dirs := w.storage.MyDirs(box)
		if len(dirs) == 0 {
			return "", false
		}
		return sandbox.ReadMemory(dirs[0])
	}
	return "", false
}
