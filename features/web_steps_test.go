package features_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/atlantic-blue/krewe/internal/web"
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
		// sessionID is the system's own identifier, which is what a link in the listing carries.
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

	// The refusal is read against the document, so a wall that stops naming one of the three fails
	// here, and so does a document that quietly drops one.
	sc.Step(`^the refusal names each thing a wider front door needs, as the architecture document lists them$`, func(ctx context.Context) error {
		err := webFrom(ctx).err
		if err == nil {
			return fmt.Errorf("the web view served an address that is reachable from another machine")
		}
		needs, readErr := theThreeThingsAWiderDoorNeeds()
		if readErr != nil {
			return readErr
		}
		refusal := strings.ToLower(err.Error())
		for _, need := range needs {
			if !strings.Contains(refusal, need) {
				return fmt.Errorf("the refusal does not name %q, so the operator cannot tell what is missing: %v", need, err)
			}
		}
		return nil
	})

	sc.Step(`^the refusal names the chat channel as the road taken instead$`, func(ctx context.Context) error {
		err := webFrom(ctx).err
		if err == nil {
			return fmt.Errorf("the web view served an address that is reachable from another machine")
		}
		refusal := strings.ToLower(err.Error())
		for _, want := range []string{"chat channel", "docs/architecture.md"} {
			if !strings.Contains(refusal, want) {
				return fmt.Errorf("the refusal does not name %q, so it refuses without saying where the work goes instead: %v", want, err)
			}
		}
		return nil
	})

	sc.Step(`^the architecture document records the decision, the three things and the road taken$`, func(_ context.Context) error {
		text, err := os.ReadFile(theDecisionDocument)
		if err != nil {
			return fmt.Errorf("read the decision: %w", err)
		}
		written := string(text)
		for _, want := range []string{
			"Decided 31 August 2026",
			"the front door stays on this machine",
			"chat channel",
			"https://github.com/atlantic-blue/quay-crew/issues/9",
			"https://github.com/atlantic-blue/quay-crew/issues/10",
		} {
			if !strings.Contains(written, want) {
				return fmt.Errorf("%s does not say %q, so the decision is not written where the next reader finds it", theDecisionDocument, want)
			}
		}
		if _, err := theThreeThingsAWiderDoorNeeds(); err != nil {
			return err
		}
		return nil
	})
}

// theDecisionDocument is where the decision of 31 August 2026 is written, a directory up from here.
const theDecisionDocument = "../docs/ARCHITECTURE.md"

// theThreeThingsAWiderDoorNeeds reads them out of the architecture document rather than holding a
// copy of them. The document is the record, and the refusal is measured against it, so the two cannot
// drift apart in silence.
//
// It refuses to return a different number than three. A parse that finds nothing reads exactly like a
// refusal that names everything, which is the shape of a check that passes while proving nothing.
func theThreeThingsAWiderDoorNeeds() ([]string, error) {
	text, err := os.ReadFile(theDecisionDocument)
	if err != nil {
		return nil, fmt.Errorf("read the decision: %w", err)
	}
	const opens = "A wider front door needs three things first"
	_, after, found := strings.Cut(string(text), opens)
	if !found {
		return nil, fmt.Errorf("%s does not say what a wider front door needs, so nothing records this decision", theDecisionDocument)
	}
	// The rest of that sentence first, then the block under it, which is the list itself.
	_, under, _ := strings.Cut(after, "\n\n")
	block, _, _ := strings.Cut(under, "\n\n")

	var needs []string
	for _, line := range strings.Split(strings.TrimSpace(block), "\n") {
		bullet, isOne := strings.CutPrefix(strings.TrimSpace(line), "- ")
		if !isOne {
			return nil, fmt.Errorf("%s does not list what a wider front door needs under the sentence that opens the list, it holds %q", theDecisionDocument, line)
		}
		need, _, _ := strings.Cut(bullet, ",")
		needs = append(needs, strings.ToLower(strings.TrimSuffix(strings.TrimSpace(need), ".")))
	}
	if len(needs) != 3 {
		return nil, fmt.Errorf("%s names %d things a wider front door needs, and the decision named three: %v", theDecisionDocument, len(needs), needs)
	}
	return needs, nil
}
