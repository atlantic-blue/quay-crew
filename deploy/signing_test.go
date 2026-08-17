package deploy

import (
	"os"
	"strings"
	"testing"
)

// TestTheSandboxImageCanMakeEitherSignature holds the image to both programs git signs with.
//
// Git makes a signature by running another program: ssh-keygen for the ssh format, gpg for the
// OpenPGP one. Neither is in a node image by default, and a missing one is not a signature that
// fails to verify, it is a commit that cannot be made at all: git answers "cannot run gpg" before it
// reads the key, and every commit in that sandbox fails whatever the key is. That is what happened
// to the ssh half for two days.
//
// The keyring goes with gpg. A key imported into the home directory sits on the container's writable
// layer, which the daemon keeps on disk until the container is removed, and that undoes the whole
// reason a key is mounted rather than set. The image points gpg at memory instead.
func TestTheSandboxImageCanMakeEitherSignature(t *testing.T) {
	dockerfile, err := os.ReadFile(sandboxDockerfile)
	if err != nil {
		t.Fatalf("reading the sandbox dockerfile: %v", err)
	}
	image := string(dockerfile)

	// What is installed, not what is written: a package named in a comment reads the same to a
	// search and installs nothing. This test passed on an image with no gnupg in it until the
	// comment above the line was the only thing holding it up.
	installed := map[string]bool{}
	for _, line := range strings.Split(image, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") || !strings.Contains(line, "apt-get install") {
			continue
		}
		for _, word := range strings.Fields(line) {
			installed[word] = true
		}
	}
	if len(installed) == 0 {
		t.Fatal("this test found no apt-get install line to read, so it proves nothing")
	}
	for program, comesFrom := range map[string]string{
		"ssh-keygen": "openssh-client",
		"gpg":        "gnupg",
	} {
		if !installed[comesFrom] {
			t.Errorf("the image never installs %s, so git cannot run %s and a session that is told to sign cannot commit at all", comesFrom, program)
		}
	}

	if !strings.Contains(image, "ENV GNUPGHOME=/dev/shm/") {
		t.Error("the image does not put gpg's keyring in memory, so an imported signing key lands on the container's writable layer and stays on the host's disk")
	}
}
