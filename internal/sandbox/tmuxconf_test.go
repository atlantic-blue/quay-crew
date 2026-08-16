package sandbox_test

import (
	"os"
	"strings"
	"testing"
)

// TestTheDetachPrefixIsNotAKeyTheTerminalEats refuses the whole class rather than the one spelling
// that broke.
//
// The prefix was ctrl-o for one release. On macOS ^O is the terminal's DISCARD character, so the line
// discipline swallows it and tmux never sees it: the key simply did nothing, and the automated test
// passed because it wrote the byte straight into a pty and never went through that layer. These are
// the characters a terminal reserves, and any of them would fail the same way.
func TestTheDetachPrefixIsNotAKeyTheTerminalEats(t *testing.T) {
	reserved := map[string]string{
		"C-c":  "interrupt",
		"C-\\": "quit",
		"C-u":  "kill line",
		"C-d":  "end of file",
		"C-q":  "start output",
		"C-s":  "stop output",
		"C-z":  "suspend",
		"C-y":  "delayed suspend",
		"C-r":  "reprint",
		"C-o":  "discard, which is what broke this",
		"C-w":  "word erase",
		"C-v":  "literal next",
	}

	config, err := os.ReadFile("../../deploy/sandbox/tmux.conf")
	if err != nil {
		t.Fatalf("reading the sandbox's tmux configuration: %v", err)
	}

	// A no prefix binding on a reserved character is allowed only where something tasks that
	// character back into a key. ctrl-q is flow control until open-conversation runs stty -ixon.
	if strings.Contains(string(config), "bind -n C-q") {
		script, err := os.ReadFile("../../deploy/sandbox/open-conversation.sh")
		if err != nil {
			t.Fatalf("reading what opens a conversation: %v", err)
		}
		if !strings.Contains(string(script), "stty -ixon") {
			t.Fatal("ctrl-q is bound but nothing tasks off flow control, so the terminal eats it")
		}
	}

	var found int
	for _, line := range strings.Split(string(config), "\n") {
		fields := strings.Fields(line)
		// set -g prefix <key>, and prefix2.
		if len(fields) != 4 || fields[0] != "set" || !strings.HasPrefix(fields[2], "prefix") {
			continue
		}
		found++
		key := fields[3]
		if why, taken := reserved[key]; taken {
			t.Fatalf("%s is %s: the terminal eats it and tmux never sees it", key, why)
		}
	}
	if found != 2 {
		t.Fatalf("found %d prefixes in the configuration, want the prefix and the second one", found)
	}
}
