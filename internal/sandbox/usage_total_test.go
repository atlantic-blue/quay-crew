package sandbox_test

import (
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

// A ceiling is one number, so the four have to add up to one. Cache reads are counted: on a real
// conversation they were 1,723,404 against 52 of fresh input, so a total that left them out would
// be a ceiling that never stops anything.
func TestUsageTotalCountsEverythingTheModelWasCharged(t *testing.T) {
	used := sandbox.Usage{Input: 52, Output: 300, CacheRead: 1_723_404, CacheWritten: 900}
	if got := used.Total(); got != 1_724_656 {
		t.Fatalf("the total is %d, want every number added up", got)
	}
	if (sandbox.Usage{}).Total() != 0 {
		t.Fatal("a conversation nobody has had costs something")
	}
}
