package session_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/session"
)

func TestLocalRunsCommand(t *testing.T) {
	proc, err := session.Local{}.Start(context.Background(), session.Spec{Argv: []string{"echo", "hi there"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
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
}

func TestLocalRejectsEmptyArgv(t *testing.T) {
	if _, err := (session.Local{}).Start(context.Background(), session.Spec{}); err == nil {
		t.Fatal("Start with empty argv = nil error, want error")
	}
}

func TestFake(t *testing.T) {
	fake := &session.Fake{Output: "canned"}
	proc, err := fake.Start(context.Background(), session.Spec{Argv: []string{"claude"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	out, _ := io.ReadAll(proc.Stdout())
	if string(out) != "canned" {
		t.Fatalf("out = %q", string(out))
	}
	if len(fake.LastSpec.Argv) != 1 || fake.LastSpec.Argv[0] != "claude" {
		t.Fatalf("LastSpec not recorded: %+v", fake.LastSpec)
	}
}

func TestNew(t *testing.T) {
	if rt, err := session.New("", "img", nil); err != nil {
		t.Fatalf("default: %v", err)
	} else if _, ok := rt.(session.Docker); !ok {
		t.Fatalf("default should be Docker, got %T", rt)
	}
	if rt, err := session.New("local", "", nil); err != nil {
		t.Fatalf("local: %v", err)
	} else if _, ok := rt.(session.Local); !ok {
		t.Fatalf("local should be Local, got %T", rt)
	}
	if _, err := session.New("nope", "", nil); err == nil {
		t.Fatal("unknown kind = nil error, want error")
	}
}
