package sandbox_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

func TestLocalProviderExec(t *testing.T) {
	box, err := sandbox.LocalProvider{}.Create(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	proc, err := box.Exec(context.Background(), sandbox.Spec{Argv: []string{"echo", "hi there"}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	out, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if strings.TrimSpace(string(out)) != "hi there" {
		t.Fatalf("stdout = %q, want 'hi there'", string(out))
	}
	if err := box.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestLocalExecRejectsEmptyArgv(t *testing.T) {
	box, _ := sandbox.LocalProvider{}.Create(context.Background(), "s")
	if _, err := box.Exec(context.Background(), sandbox.Spec{}); err == nil {
		t.Fatal("Exec with empty argv = nil error, want error")
	}
}

func TestFakeProvider(t *testing.T) {
	provider := &sandbox.FakeProvider{Output: "canned"}
	box, err := provider.Create(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(provider.Created) != 1 || provider.Created[0] != "sess-1" {
		t.Fatalf("Created not recorded: %+v", provider.Created)
	}
	proc, _ := box.Exec(context.Background(), sandbox.Spec{Argv: []string{"claude"}})
	out, _ := io.ReadAll(proc.Stdout())
	if string(out) != "canned" {
		t.Fatalf("out = %q", string(out))
	}
	if err := box.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !provider.Boxes[0].Closed {
		t.Fatal("Close did not mark the sandbox closed")
	}
}

func TestNewProvider(t *testing.T) {
	if p, err := sandbox.NewProvider("", "img", nil); err != nil {
		t.Fatalf("default: %v", err)
	} else if _, ok := p.(sandbox.DockerProvider); !ok {
		t.Fatalf("default should be DockerProvider, got %T", p)
	}
	if p, err := sandbox.NewProvider("local", "", nil); err != nil {
		t.Fatalf("local: %v", err)
	} else if _, ok := p.(sandbox.LocalProvider); !ok {
		t.Fatalf("local should be LocalProvider, got %T", p)
	}
	if _, err := sandbox.NewProvider("nope", "", nil); err == nil {
		t.Fatal("unknown kind = nil error, want error")
	}
}
