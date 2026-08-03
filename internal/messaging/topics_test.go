package messaging_test

import (
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/messaging"
)

func TestTopic(t *testing.T) {
	got, err := messaging.Topic("acme", "inbound")
	if err != nil {
		t.Fatalf("Topic: %v", err)
	}
	if got != "acme.inbound" {
		t.Fatalf("Topic = %q, want %q", got, "acme.inbound")
	}
}

func TestTopicRejectsBadNames(t *testing.T) {
	cases := []struct{ workspace, stream string }{
		{"", "inbound"},
		{"acme", ""},
		{"a.b", "inbound"},
		{"acme", "in bound"},
		{"acme", "in/bound"},
	}
	for _, c := range cases {
		if _, err := messaging.Topic(c.workspace, c.stream); err == nil {
			t.Fatalf("Topic(%q, %q) = nil error, want error", c.workspace, c.stream)
		}
	}
}
