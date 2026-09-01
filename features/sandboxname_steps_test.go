package features_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/cucumber/godog"
)

// Steps for the name a session's sandbox carries.
//
// They ask the sandbox package the same question every reader of a container name asks it: the drain,
// the upgrade's sweep, the memory reader and the attach all go through this one answer, so a scenario
// here is a scenario about all of them.

type namedContainer struct{ name string }

func initializeSandboxNameSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the daemon holds a container named "([^"]*)"$`, func(ctx context.Context, name string) (context.Context, error) {
		return context.WithValue(ctx, containerKey{}, &namedContainer{name: name}), nil
	})

	sc.Step(`^the system reads it as the sandbox of session "([^"]*)"$`, func(ctx context.Context, want string) error {
		held := containerFrom(ctx)
		got, isSandbox := sandbox.SessionOf(held.name)
		if !isSandbox {
			return fmt.Errorf("%s is read as no session's sandbox, so nothing drains it and nothing removes it", held.name)
		}
		if got != want {
			return fmt.Errorf("%s is read as session %q, want %q", held.name, got, want)
		}
		return nil
	})

	sc.Step(`^the system reads it as no session's sandbox$`, func(ctx context.Context) error {
		held := containerFrom(ctx)
		if got, isSandbox := sandbox.SessionOf(held.name); isSandbox {
			return fmt.Errorf("%s is read as session %q, and stopping it takes down something nobody meant to stop",
				held.name, got)
		}
		return nil
	})

	sc.Step(`^a new sandbox for session "([^"]*)" is named "([^"]*)"$`, func(_ context.Context, session, want string) error {
		if got := sandbox.ContainerName(session); got != want {
			return fmt.Errorf("a new sandbox is created as %q, want %q", got, want)
		}
		return nil
	})

	sc.Step(`^the system looks for session "([^"]*)" as "([^"]*)", then "([^"]*)"$`,
		func(_ context.Context, session, first, second string) error {
			got := sandbox.ContainerNames(session)
			if want := []string{first, second}; strings.Join(got, " ") != strings.Join(want, " ") {
				return fmt.Errorf("the system looks for %v, want %v", got, want)
			}
			return nil
		})
}

type containerKey struct{}

func containerFrom(ctx context.Context) *namedContainer {
	held, ok := ctx.Value(containerKey{}).(*namedContainer)
	if !ok {
		return &namedContainer{}
	}
	return held
}
