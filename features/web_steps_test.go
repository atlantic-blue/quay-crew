package features_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/web"
	"github.com/cucumber/godog"
)

// webWorld is what the browser received, kept beside the shared world so the web scenarios do not
// widen what every other scenario carries.
type webWorld struct {
	body   string
	status int
	err    error
}

type webKey struct{}

func webFrom(ctx context.Context) *webWorld {
	w, _ := ctx.Value(webKey{}).(*webWorld)
	return w
}

// visit drives the real routes against the live control plane and keeps what came back, which is
// what a browser on the operator's machine would have been sent.
func (v *webWorld) visit(ctx context.Context, path string) error {
	handler, err := web.Handler(worldFrom(ctx).client)
	if err != nil {
		return fmt.Errorf("build the web view: %w", err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
	v.body, v.status = recorder.Body.String(), recorder.Code
	return nil
}

func initializeWebSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, webKey{}, &webWorld{}), nil
	})

	sc.Step(`^the operator opens the web view$`, func(ctx context.Context) error {
		return webFrom(ctx).visit(ctx, "/")
	})

	sc.Step(`^the operator opens the web view on that session$`, func(ctx context.Context) error {
		tasks := worldFrom(ctx).tasks
		if len(tasks) == 0 {
			return fmt.Errorf("no session has been dispatched to yet")
		}
		// sessionID is the crew's own identifier, which is what a link in the listing carries.
		// handle beside it names the conversation rather than the row.
		return webFrom(ctx).visit(ctx, "/session/"+tasks[len(tasks)-1].sessionID)
	})

	sc.Step(`^the operator opens the web view on a session that does not exist$`, func(ctx context.Context) error {
		return webFrom(ctx).visit(ctx, "/session/no-session-by-this-name")
	})

	sc.Step(`^the web view lists (\d+) sessions?$`, func(ctx context.Context, want int) error {
		got := strings.Count(webFrom(ctx).body, `<li class="session">`)
		if got != want {
			return fmt.Errorf("the listing carries %d sessions, want %d:\n%s", got, want, webFrom(ctx).body)
		}
		return nil
	})

	sc.Step(`^the page carries "([^"]*)"$`, func(ctx context.Context, want string) error {
		if !strings.Contains(webFrom(ctx).body, want) {
			return fmt.Errorf("the page does not carry %q:\n%s", want, webFrom(ctx).body)
		}
		return nil
	})

	sc.Step(`^the page is not found$`, func(ctx context.Context) error {
		if got := webFrom(ctx).status; got != http.StatusNotFound {
			return fmt.Errorf("the page answered %d, want %d", got, http.StatusNotFound)
		}
		return nil
	})

	sc.Step(`^the operator asks for the web view on "([^"]*)"$`, func(ctx context.Context, addr string) error {
		webFrom(ctx).err = web.Serve(ctx, worldFrom(ctx).client, addr, &strings.Builder{})
		return nil
	})

	sc.Step(`^the web view refuses, because that address is reachable from another machine$`, func(ctx context.Context) error {
		err := webFrom(ctx).err
		if err == nil {
			return fmt.Errorf("the web view served an address that is reachable from another machine")
		}
		if !strings.Contains(err.Error(), "this machine only") {
			return fmt.Errorf("the refusal does not say why: %v", err)
		}
		return nil
	})
}
