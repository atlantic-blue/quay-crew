package sandbox

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/atlantic-blue/krewe/internal/contextspend"
)

// largeConversation is the size above which a conversation holds enough for the check against the
// model's own count to mean something. It is a round number and not a measured one: it exists to split
// a corpus of mostly tiny conversations from the ones the budget actually goes on.
const largeConversation = 100_000

// MeasureVariable names the directory of transcripts to measure. It is a variable rather than a path
// in the code because the conversations belong to whoever ran them, and this must never read a
// directory nobody pointed it at.
const MeasureVariable = "KREWE_MEASURE_TRANSCRIPTS"

// TestTheContextSpendMeasurement is the run behind docs/CONTEXT-SPEND.md.
//
// It is here rather than in a document alone because a number nobody can reproduce is a number
// nobody can argue with. Point it at a directory of conversation transcripts and it prints the same
// report the document quotes:
//
//	KREWE_MEASURE_TRANSCRIPTS=~/.claude/projects/-home-agent-workspace \
//	  go test ./internal/sandbox/ -run TestTheContextSpendMeasurement -v -count=1
//
// It skips where nothing named a directory, so it costs an ordinary run nothing. Where it does run it
// checks two things rather than only printing: that every conversation's parts add up to its total,
// and that the whole corpus can be held against the model's own count. A report that only prints is
// a report that cannot fail.
func TestTheContextSpendMeasurement(t *testing.T) {
	dir := os.Getenv(MeasureVariable)
	if dir == "" {
		t.Skipf("set %s to a directory of transcripts to run the measurement", MeasureVariable)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*"+ConversationFile))
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	if len(paths) == 0 {
		t.Fatalf("%s holds no transcripts, so this would measure nothing and say it had", dir)
	}
	sort.Strings(paths)

	type conversation struct {
		name    string
		spend   contextspend.Spend
		counted int64
	}
	var read []conversation
	for _, path := range paths {
		_, carried, spent := sum(path)
		if spent.Empty() {
			continue
		}
		read = append(read, conversation{filepath.Base(path), spent, carried.Carried()})
	}
	if len(read) == 0 {
		t.Fatalf("none of the %d transcripts in %s held a conversation", len(paths), dir)
	}

	var whole contextspend.Spend
	var counted int64
	dominates := map[contextspend.Category]int{}
	for _, one := range read {
		var parts int64
		for _, category := range contextspend.Categories() {
			parts += one.spend.Of(category)
		}
		if parts != one.spend.Total() {
			t.Errorf("%s: the parts add up to %d and the total says %d", one.name, parts, one.spend.Total())
		}
		whole = whole.Add(one.spend)
		counted += one.counted
		dominates[one.spend.Largest()]++
	}

	check := whole.Against(counted)
	if !check.Known() {
		t.Fatal("the corpus cannot be held against the model's own count, so the measurement says nothing")
	}

	t.Logf("%d conversations, %d characters", len(read), whole.Total())
	for _, category := range contextspend.Categories() {
		t.Logf("  %-6s %12d  %3d%%  dominates %d conversations",
			category, whole.Of(category), whole.Share(category), dominates[category])
	}
	t.Logf("  largest overall: %s", whole.Largest())
	t.Logf("  %s", check.Line())

	// The same check over the conversations that hold most of the characters. The part no transcript
	// holds is a fixed floor per conversation, so it is nearly the whole window on a short one, and
	// the share the breakdown explains rises with the size of the conversation. A single figure over
	// a corpus of mostly short conversations hides that.
	var large contextspend.Spend
	var largeCounted, largeCount int64
	for _, one := range read {
		if one.spend.Total() <= largeConversation {
			continue
		}
		large = large.Add(one.spend)
		largeCounted += one.counted
		largeCount++
	}
	t.Logf("of the %d conversations over %d characters, holding %d of them:",
		largeCount, largeConversation, large.Total())
	for _, category := range contextspend.Categories() {
		t.Logf("  %-6s %12d  %3d%%", category, large.Of(category), large.Share(category))
	}
	t.Logf("  %s", large.Against(largeCounted).Line())

	sort.Slice(read, func(i, j int) bool { return read[i].spend.Total() > read[j].spend.Total() })
	for i, one := range read {
		if i >= 10 {
			break
		}
		t.Logf("  %s %9d characters  reads %3d%% tools %3d%% turns %3d%% told %3d%%, explained %3d%%",
			one.name[:8], one.spend.Total(),
			one.spend.Share(contextspend.Reads), one.spend.Share(contextspend.Tools),
			one.spend.Share(contextspend.Turns), one.spend.Share(contextspend.Told),
			one.spend.Against(one.counted).Share())
	}

	characters, tokens, measured := charactersPerToken(paths)
	if tokens <= 0 {
		t.Fatal("no conversation held two answers, so the ratio of characters to tokens cannot be measured")
	}
	t.Logf("characters a token: %.3f over %d conversations, %d characters against %d tokens",
		float64(characters)/float64(tokens), measured, characters, tokens)
}

// charactersPerToken measures how many characters of a conversation one token holds.
//
// The method needs no assumption. Between one answer and the next, the transcript grew by so many
// characters and the model's own count of what it carries grew by so many tokens, and the ratio of
// the two is the answer. Only the growth is measured, because what the first answer carried is the
// system prompt and the tool definitions, which the transcript does not hold.
//
// It is the run behind the ratio the accounting converts with, so the constant in the code and the
// figure in docs/CONTEXT-SPEND.md come from one place.
func charactersPerToken(paths []string) (characters, tokens int64, conversations int) {
	for _, path := range paths {
		file, err := os.Open(path) //nolint:gosec // a directory the operator named
		if err != nil {
			continue
		}
		var running, atFirst, firstCarried, lastCarried int64
		seen := false
		calls := map[string]call{}
		forEachRecord(file, func(record transcriptLine) {
			if record.IsSidechain {
				return
			}
			var grew contextspend.Spend
			countSpend(&grew, record, calls)
			running += grew.Total()
			carried := record.Message.Usage.Input +
				record.Message.Usage.CacheRead + record.Message.Usage.CacheWritten
			if record.Type != assistantRecord || carried <= 0 {
				return
			}
			if !seen {
				firstCarried, atFirst, seen = carried, running, true
			}
			lastCarried = carried
		})
		_ = file.Close()
		if grewBy := lastCarried - firstCarried; grewBy > 0 {
			characters += running - atFirst
			tokens += grewBy
			conversations++
		}
	}
	return characters, tokens, conversations
}
