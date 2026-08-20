package sandbox

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Usage is what a conversation has cost, in tokens.
//
// Four numbers rather than two, because two would be a lie by omission. On a real conversation the
// input was 52 tokens and the cache read was 1,723,404: almost everything sent is the context being
// read again on every task, and a report of "inbound and outbound" would show the 52 and hide the
// rest.
type Usage struct {
	// Input is what was sent and charged as new, not counting anything served from the cache.
	Input int64
	// Output is what came back.
	Output int64
	// CacheRead is context the model read from its cache rather than being sent again. It is the
	// largest of these by far on any conversation with real context behind it.
	CacheRead int64
	// CacheWritten is context put into the cache to be read on later tasks.
	CacheWritten int64
}

// Add sums two, so a caller can total a project or a crew without knowing what is in one.
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

// Empty says nothing has been spent, which is a conversation nobody has had rather than one that cost
// nothing.
func (u Usage) Empty() bool {
	return u == Usage{}
}

// transcriptLine is the part of a conversation record this cares about. The model's command line tool
// writes one record per line and carries the usage on the assistant's messages.
type transcriptLine struct {
	Message struct {
		Usage struct {
			Input        int64 `json:"input_tokens"`
			Output       int64 `json:"output_tokens"`
			CacheRead    int64 `json:"cache_read_input_tokens"`
			CacheWritten int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
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
	return usageCache.of(path)
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
// reread and reparse every conversation in the crew each time.
var usageCache = &transcripts{read: map[string]counted{}}

// counted is one transcript's total and what the file looked like when it was counted. A transcript
// is appended to and never rewritten, so its size and time answer whether the total still holds.
type counted struct {
	usage Usage
	size  int64
	when  int64
}

type transcripts struct {
	mu   sync.Mutex
	read map[string]counted
}

func (t *transcripts) of(path string) Usage {
	info, err := os.Stat(path)
	if err != nil {
		return Usage{}
	}

	t.mu.Lock()
	cached, known := t.read[path]
	t.mu.Unlock()
	if known && cached.size == info.Size() && cached.when == info.ModTime().UnixNano() {
		return cached.usage
	}

	total := sum(path)
	t.mu.Lock()
	t.read[path] = counted{usage: total, size: info.Size(), when: info.ModTime().UnixNano()}
	t.mu.Unlock()
	return total
}

// sum reads a transcript and adds up what every message in it cost.
//
// A line it cannot read is skipped rather than failing the file. The tool writes this as it goes, so
// the last line of a live conversation is regularly half written, and refusing to count a whole
// conversation because of that would mean the number only ever appeared when nothing was happening.
func sum(path string) Usage {
	file, err := os.Open(path)
	if err != nil {
		return Usage{}
	}
	defer func() { _ = file.Close() }()

	var total Usage
	lines := bufio.NewScanner(file)
	// A conversation record carries whole messages, so the default limit is not enough.
	lines.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for lines.Scan() {
		var record transcriptLine
		if err := json.Unmarshal(lines.Bytes(), &record); err != nil {
			continue
		}
		total = total.Add(Usage{
			Input:        record.Message.Usage.Input,
			Output:       record.Message.Usage.Output,
			CacheRead:    record.Message.Usage.CacheRead,
			CacheWritten: record.Message.Usage.CacheWritten,
		})
	}
	return total
}
