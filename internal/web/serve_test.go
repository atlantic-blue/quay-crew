package web

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestServeAnswersARealRequestOnThisMachine drives the command's own path rather than the handler
// alone: it binds a socket, says where it came up, answers a request over the network and stops when
// the operator does. A handler that renders in a recorder proves none of that.
func TestServeAnswersARealRequestOnThisMachine(t *testing.T) {
	client := aCrew(t)
	dispatch(t, client, projectOf(t, client), "", "when is the electricity bill due")

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	said := &saidAloud{}
	served := make(chan error, 1)
	// Port zero, so the test never fights whatever is already on 8080.
	go func() { served <- Serve(ctx, client, "127.0.0.1:0", said) }()

	where := waitForAddress(t, said)
	listing := fetch(t, where)
	if !strings.Contains(listing, "me/house-bills/") {
		t.Fatalf("the served listing does not carry the session:\n%s", listing)
	}

	// Follow the link the operator would click. A listing that renders and links nowhere useful is
	// the half of this that a recorder cannot catch.
	conversation := fetch(t, where+linkTo(t, listing))
	if !strings.Contains(conversation, "when is the electricity bill due") {
		t.Errorf("the conversation behind the link does not carry the task:\n%s", conversation)
	}
	stop()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serving ended badly: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the view kept serving after the operator stopped it")
	}
}

// saidAloud is what the view printed, written by the goroutine serving and read by the one asserting,
// so it holds a lock. A plain builder here is a race the detector fails the build over.
type saidAloud struct {
	mu   sync.Mutex
	said strings.Builder
}

func (s *saidAloud) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.said.Write(p)
}

func (s *saidAloud) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.said.String()
}

// waitForAddress reads the address off what Serve printed, because the port is chosen by the kernel
// and the operator is told it the same way.
func waitForAddress(t *testing.T, said *saidAloud) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, after, found := strings.Cut(said.String(), "http://"); found {
			return "http://" + strings.TrimSpace(after)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the view never said where it came up, it said %q", said.String())
	return ""
}

// linkTo is the first conversation the listing links to, read out of the page rather than rebuilt
// from an identifier, so a link that points at the wrong place fails here.
func linkTo(t *testing.T, listing string) string {
	t.Helper()
	_, after, found := strings.Cut(listing, `<a href="/session/`)
	if !found {
		t.Fatalf("the listing links to no conversation:\n%s", listing)
	}
	id, _, found := strings.Cut(after, `"`)
	if !found {
		t.Fatalf("the listing's link never ends:\n%s", listing)
	}
	return "/session/" + id
}

func fetch(t *testing.T, where string) string {
	t.Helper()
	resp, err := http.Get(where)
	if err != nil {
		t.Fatalf("get %s: %v", where, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", where, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s answered %d", where, resp.StatusCode)
	}
	return string(body)
}
