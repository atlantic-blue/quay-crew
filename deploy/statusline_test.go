package deploy

import (
	"strings"
	"testing"
)

// The status line is a file the image copies in and a binary the image carries, and neither one is
// worth anything without the other. A settings file that lands nowhere the runtime reads, or a
// runtime asked to run a tool that is not on its PATH, both leave an attached operator with the blank
// line they had before.
func TestTheSandboxImageGivesTheRuntimeAStatusLine(t *testing.T) {
	image := theSandboxImage(t)

	const at = "/home/agent/.claude/settings.json"
	copied := ""
	for _, line := range strings.Split(image, "\n") {
		if strings.HasPrefix(line, "COPY") && strings.HasSuffix(strings.TrimSpace(line), at) {
			copied = line
		}
	}
	if copied == "" {
		t.Fatalf("the image never puts a settings file at %s, so the runtime opens a conversation with no status line", at)
	}
	if !strings.Contains(copied, "deploy/sandbox/claude-settings.json") {
		t.Errorf("the settings at %s come from somewhere other than the file this repository holds, "+
			"so the test that reads that file is reading something the image does not ship:\n%s", at, copied)
	}
	// The runtime rewrites this file as it runs, and it runs as the sandbox user.
	if !strings.Contains(copied, "--chown=agent:agent") {
		t.Errorf("the settings file lands owned by root, so the runtime cannot write it:\n%s", copied)
	}
	if !strings.Contains(image, "COPY --from=tool /out/quay /usr/local/bin/quay") {
		t.Error("the image carries no quay, so the command its settings name is not there to run")
	}
}

// The directory has to exist and be the sandbox user's before anything is copied into it: everything
// the runtime keeps about a conversation lands there, the transcripts among them, so a directory
// owned by root is a conversation that cannot be written at all.
func TestTheRuntimesDirectoryBelongsToTheSandboxUser(t *testing.T) {
	image := theSandboxImage(t)

	made := strings.Index(image, "mkdir -p /home/agent/.claude")
	dropped := strings.Index(image, "USER agent")
	copied := strings.Index(image, "/home/agent/.claude/settings.json")
	switch {
	case made < 0:
		t.Fatal("nothing makes /home/agent/.claude, so it is made by whatever writes into it first")
	case dropped < 0 || made < dropped:
		t.Error("/home/agent/.claude is made before the image drops to the sandbox user, so it belongs to root")
	case copied < 0:
		t.Error("the settings are never copied into /home/agent/.claude, so nothing here checks their order")
	case copied < made:
		t.Error("the settings are copied in before the directory is made, so the copy makes it instead")
	}
}
