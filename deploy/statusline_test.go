package deploy

import (
	"strings"
	"testing"
)

// The way off the settings this image used to ship.
//
// A file the image writes to /home/agent/.claude is a file no session ever reads: the crew mounts the
// workspace's own directory over that path in every sandbox, and a mount hides what the image put
// underneath it. That is not visible from the Dockerfile, it is not visible from any test that reads
// the Dockerfile, and nothing in continuous integration builds this image, so the whole class is
// refused here instead. What the runtime is told is rendered by the crew, in internal/hook.
func TestTheImageWritesNoSettingsWhereTheCrewMountsOverIt(t *testing.T) {
	for _, line := range strings.Split(theSandboxImage(t), "\n") {
		naked := strings.TrimSpace(line)
		if strings.HasPrefix(naked, "#") || !strings.Contains(naked, "/home/agent/.claude/settings.json") {
			continue
		}
		t.Errorf("the image writes settings the mount hides, so no session reads them:\n%s\n\n"+
			"say it in internal/hook.Settings instead, which the crew renders and mounts read only", naked)
	}
}

// The runtime is told to run quay for its status line, so the image has to carry quay.
func TestTheSandboxImageCarriesTheToolThatDrawsTheStatusLine(t *testing.T) {
	if !strings.Contains(theSandboxImage(t), "COPY --from=tool /out/quay /usr/local/bin/quay") {
		t.Error("the image carries no quay, so the command the crew's settings name is not there to run")
	}
}

// The directory has to exist and be the sandbox user's: everything the runtime keeps about a
// conversation lands there, the transcripts among them, so a directory owned by root is a
// conversation that cannot be written at all.
func TestTheRuntimesDirectoryBelongsToTheSandboxUser(t *testing.T) {
	image := theSandboxImage(t)

	made := strings.Index(image, "mkdir -p /home/agent/.claude")
	dropped := strings.Index(image, "USER agent")
	switch {
	case made < 0:
		t.Fatal("nothing makes /home/agent/.claude, so it is made by whatever writes into it first")
	case dropped < 0 || made < dropped:
		t.Error("/home/agent/.claude is made before the image drops to the sandbox user, so it belongs to root")
	}
}
