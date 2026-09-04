package sandbox

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/atlantic-blue/quay-krewe/internal/contextspend"
)

// Usage is what a conversation has cost, in tokens.
//
// Four numbers rather than two, because two would be a lie by omission. On a real conversation the
// input was 52 tokens and the cache read was 1,723,404: almost everything sent is the context being
// read again on every exec, and a report of "inbound and outbound" would show the 52 and hide the
// rest.
type Usage struct {
	// Input is what was sent and charged as new, not counting anything served from the cache.
	Input int64
	// Output is what came back.
	Output int64
	// CacheRead is context the model read from its cache rather than being sent again. It is the
	// largest of these by far on any conversation with real context behind it.
	CacheRead int64
	// CacheWritten is context put into the cache to be read on later execs.
	CacheWritten int64
}

// Add sums two, so a caller can total a project or a system without knowing what is in one.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		Input:        u.Input + other.Input,
		Output:       u.Output + other.Output,
		CacheRead:    u.CacheRead + other.CacheRead,
		CacheWritten: u.CacheWritten + other.CacheWritten,
	}
}

// Total is everything the model was charged for, as one number, which is what a ceiling compares
// against. Cache reads are in it: they are the largest of the four by far, so leaving them out
// would be a ceiling that never stops anything.
func (u Usage) Total() int64 {
	return u.Input + u.Output + u.CacheRead + u.CacheWritten
}

// Carried is the context an exec sent: everything in, and nothing that came back. The model's own
// status line counts it the same way, so what the console says and what the operator reads under the
// prompt cannot disagree.
func (u Usage) Carried() int64 {
	return u.Input + u.CacheRead + u.CacheWritten
}

// Empty says nothing has been spent, which is a conversation nobody has had rather than one that cost
// nothing.
func (u Usage) Empty() bool {
	return u == Usage{}
}

// transcriptLine is the part of a conversation record this cares about. The model's command line tool
// writes one record per line and carries the usage on the assistant's messages.
//
// IsSidechain marks a record belonging to a sub agent rather than to the conversation itself. A sub
// agent has a context window of its own, so its messages say nothing about how full this one is,
// and the last record in a live transcript is often one of them.
type transcriptLine struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		Usage struct {
			Input        int64 `json:"input_tokens"`
			Output       int64 `json:"output_tokens"`
			CacheRead    int64 `json:"cache_read_input_tokens"`
			CacheWritten int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
		// Content is the message itself, left undecoded here because it is a string on some records
		// and a list of blocks on others. spend.go reads it, and only the accounting needs it.
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// ConversationUsage is what one conversation has cost, read from the transcript the model keeps.
//
// The model's own store is the source, because the interesting conversations never go through the
// control plane at all: an operator talking in the panel is talking to the sandbox directly, and the
// only record of it is the file the tool writes as it goes.
//
// It takes the session's configuration rather than its workspace, because where a conversation is
// kept is decided by the same layout the mounts come from: a session running as a role keeps its own,
// and reading the workspace's store for it would answer zero forever.
//
// It answers zero for a conversation with no transcript, which is one nobody has spoken in yet, and
// zero for anything it cannot read. A cost is not worth failing a listing over.
func (s Storage) ConversationUsage(cfg Config, conversation string) Usage {
	path, found := s.transcript(cfg, conversation)
	if !found {
		return Usage{}
	}
	return usageCache.of(path).usage
}

// ConversationContext is how full the model's context window is, which is what the last answer in the
// conversation carried: everything sent on it, including what was read back from the cache.
//
// Not the total this conversation has cost, which is a different question with a much larger number:
// cost only grows, and the window empties again when the model compacts. Only the last answer counts,
// because everything before it was sent again as part of it.
//
// It takes the session's configuration for the same reason ConversationUsage does: a session running
// as a role keeps its own conversation store, and reading the workspace's store for it would answer
// that a full window was empty.
//
// Zero for a conversation nobody has spoken in, and for anything this cannot read.
func (s Storage) ConversationContext(cfg Config, conversation string) Usage {
	path, found := s.transcript(cfg, conversation)
	if !found {
		return Usage{}
	}
	return usageCache.of(path).carried
}

// ConversationSpend is where this conversation's context went: the files it read, what every other
// tool returned, its own words, and what it was told.
//
// It is what the share of a full window is missing. A session at eighty per cent says nothing about
// what to change; this says which of the four to look at first.
//
// The whole conversation rather than only the live window, because compaction is invisible from
// here: the transcript keeps every record and never says which of them the model still holds. So
// this answers what filled the context over the session's life, and Check holds it against the
// model's own count of the same span.
//
// Empty for a conversation nobody has spoken in, and for anything this cannot read.
func (s Storage) ConversationSpend(cfg Config, conversation string) contextspend.Spend {
	path, found := s.transcript(cfg, conversation)
	if !found {
		return contextspend.Spend{}
	}
	return usageCache.of(path).spend
}

// transcript is where the model keeps a conversation, and whether it is there. The working directory
// is the same inside every sandbox, so the tool files them all under one directory per conversation
// store and the name of the file is the name of the conversation.
func (s Storage) transcript(cfg Config, conversation string) (string, bool) {
	if s.Dir == "" || cfg.Workspace == "" || conversation == "" {
		return "", false
	}
	if usableAsPath("workspace", cfg.Workspace) != nil || !plainIdentifier(conversation) {
		return "", false
	}
	store, found := s.conversationDir(cfg)
	if !found {
		return "", false
	}
	matches, err := filepath.Glob(filepath.Join(store, "projects", "*", conversation+ConversationFile))
	if err != nil || len(matches) == 0 {
		return "", false
	}
	return matches[0], true
}

// conversationDir is where this session's conversation store sits, read from the same layout the
// mounts come from so the two cannot drift.
func (s Storage) conversationDir(cfg Config) (string, bool) {
	if cfg.Role != "" {
		for _, part := range []struct{ kind, value string }{
			{"project", cfg.Project}, {"session", cfg.ID},
		} {
			if usableAsPath(part.kind, part.value) != nil {
				return "", false
			}
		}
	}
	for _, one := range layout(cfg) {
		if one.target == ConversationPath {
			return filepath.Join(append([]string{s.Dir}, one.parts...)...), true
		}
	}
	return "", false
}

// usageCache keeps what each transcript came to, so a console refreshing every few seconds does not
// reread and reparse every conversation in the system each time.
var usageCache = &transcripts{read: map[string]counted{}}

// counted is one transcript's total and what the file looked like when it was counted. A transcript
// is appended to and never rewritten, so its size and time answer whether the total still holds.
type counted struct {
	usage Usage
	// carried is what the last answer in the conversation sent, which is how full the window is now
	// rather than what the whole conversation has cost.
	carried Usage
	// spend is where the conversation's characters went, by category. It comes out of the same read
	// as the two above, because a listing that asked for it separately would parse every transcript
	// in the system a second time on every refresh.
	spend contextspend.Spend
	size  int64
	when  int64
}

type transcripts struct {
	mu   sync.Mutex
	read map[string]counted
}

func (t *transcripts) of(path string) counted {
	info, err := os.Stat(path)
	if err != nil {
		return counted{}
	}

	t.mu.Lock()
	cached, known := t.read[path]
	t.mu.Unlock()
	if known && cached.size == info.Size() && cached.when == info.ModTime().UnixNano() {
		return cached
	}

	total, carried, spent := sum(path)
	read := counted{usage: total, carried: carried, spend: spent,
		size: info.Size(), when: info.ModTime().UnixNano()}
	t.mu.Lock()
	t.read[path] = read
	t.mu.Unlock()
	return read
}

// sum reads a transcript once and answers three questions: what the whole conversation cost, what
// its last answer carried, and where its characters went.
//
// A line it cannot read is skipped rather than failing the file. The tool writes this as it goes, so
// the last line of a live conversation is regularly half written, and refusing to count a whole
// conversation because of that would mean the number only ever appeared when nothing was happening.
func sum(path string) (total, carried Usage, where contextspend.Spend) {
	file, err := os.Open(path)
	if err != nil {
		return Usage{}, Usage{}, contextspend.Spend{}
	}
	defer func() { _ = file.Close() }()

	// calls names each tool the session asked for, so the result that comes back can be read as a
	// file read or as any other tool. The name is only ever on the call, never on the result.
	calls := map[string]call{}
	forEachRecord(file, func(record transcriptLine) {
		spent := Usage{
			Input:        record.Message.Usage.Input,
			Output:       record.Message.Usage.Output,
			CacheRead:    record.Message.Usage.CacheRead,
			CacheWritten: record.Message.Usage.CacheWritten,
		}
		total = total.Add(spent)
		// The last answer the conversation itself had. A sub agent's answer is skipped: it fills a
		// window of its own, and saying it filled this one would report a conversation as nearly full
		// while the operator is still typing into an empty one.
		if record.Type == assistantRecord && !record.IsSidechain && !spent.Empty() {
			carried = spent
		}
		// A sub agent's messages fill its own window, so they are left out of this one for the same
		// reason they are left out of the figure above.
		if !record.IsSidechain {
			countSpend(&where, record, calls)
		}
	})
	return total, carried, where
}

// forEachRecord reads a transcript one record at a time.
//
// A line it cannot read is skipped rather than failing the file, for the reason sum gives: the tool
// writes this as it goes, so the last line of a live conversation is regularly half written.
func forEachRecord(from io.Reader, each func(transcriptLine)) {
	lines := bufio.NewScanner(from)
	// A conversation record carries whole messages, so the default limit is not enough.
	lines.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for lines.Scan() {
		var record transcriptLine
		if err := json.Unmarshal(lines.Bytes(), &record); err != nil {
			continue
		}
		each(record)
	}
}

// assistantRecord is the record type carrying what an answer cost. A user record and a summary carry
// no usage at all, so reading every record as an answer would leave the window at whatever the last
// record that happened to have numbers on it said.
const assistantRecord = "assistant"

// ContextWindowFile is where a session writes down how big the model's context window is, inside the
// conversation directory the system mounts into every sandbox.
//
// The system cannot work the size out for itself: it is not in the transcript, and a list of models in
// the code would be right today and quietly wrong at the next one. The model runtime knows it and
// says it to the status line, so the status line writes it down where the system already reads.
const ContextWindowFile = "context-window"

// ContextWindowSize is how big the model's context window is for a workspace, and whether anything
// has said. It is a property of the model the system runs rather than of one conversation, so the last
// session to be told answers for all of them.
func (s Storage) ContextWindowSize(workspace string) (int64, bool) {
	if s.Dir == "" || usableAsPath("workspace", workspace) != nil {
		return 0, false
	}
	at := filepath.Join(s.Dir, "workspaces", workspace, "claude", ContextWindowFile)
	said, err := os.ReadFile(at) //nolint:gosec // a path built from the system's own data directory
	if err != nil {
		return 0, false
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(said)), 10, 64)
	if err != nil || size <= 0 {
		return 0, false
	}
	return size, true
}

// HasConversation says whether the model runtime has opened this conversation already, which is what
// decides whether an exec starts it or resumes it.
//
// The transcript is the answer because the transcript is the conversation: the runtime writes one as
// it goes, under the name it was given, into the store this session mounts. The script that opens a
// conversation for an operator asks the same question of the same file from inside the sandbox, so
// the two cannot disagree about whether a conversation exists.
//
// False for anything it cannot read, and for a system that keeps no state on the host at all. Both mean
// the conversation is not there as far as anything here can tell, and starting a conversation that is
// somehow already there is refused loudly, where resuming one that is not there fails with a sentence
// about no conversation found.
func (s Storage) HasConversation(cfg Config, conversation string) bool {
	_, found := s.transcript(cfg, conversation)
	return found
}
