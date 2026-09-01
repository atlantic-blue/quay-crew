package store

import (
	"os"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/forge"
)

// The query that finds what is still worth reading writes its two words out rather than passing them
// as parameters, because Postgres cannot prove a parameter equals a literal in an index predicate and
// would read the whole table instead. That leaves the words in three places: the constant, the query
// and the index. This is what holds the three together.
func TestTheUnsettledQueryUsesTheWordsTheForgeWrites(t *testing.T) {
	for _, word := range []string{forge.StatusMerged, forge.StatusClosed} {
		if !strings.Contains(unsettled, "'"+word+"'") {
			t.Fatalf("the query does not settle on %q, so a %s pull request would be read for ever", word, word)
		}
	}
	if strings.Contains(unsettled, "'"+forge.StatusOpen+"'") {
		t.Fatal("the query settles on an open pull request, so nothing would ever read one again")
	}
}

// And the index, whose predicate has to be the same words or the query reads the whole table. It is
// read off the migration rather than described here, so a change to one and not the other fails.
func TestTheIndexCoversTheQueryThatReadsIt(t *testing.T) {
	body, err := os.ReadFile("migrations/0050_job_pull_request_state.up.sql")
	if err != nil {
		t.Fatalf("read the migration: %v", err)
	}
	migration := string(body)
	for _, want := range []string{
		"pull_request <> ''",
		"pull_request_status not in ('merged', 'closed')",
		"pull_request_read_at nulls first",
	} {
		if !strings.Contains(migration, want) {
			t.Fatalf("the index does not carry %q, so the reading query cannot use it", want)
		}
		if !strings.Contains(unsettled, want) {
			t.Fatalf("the query does not carry %q, so it cannot use the index", want)
		}
	}
}
