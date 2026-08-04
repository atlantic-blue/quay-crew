// Package deploy holds the compose stack. It carries no Go code, only this test, which holds the
// compose file to a rule that is easy to break and expensive to notice.
package deploy

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestEveryServicePointedAtAConfigFileIsGivenOne refuses the whole class rather than the one service
// that broke.
//
// tempo was started with -config.file=/etc/tempo.yaml and nothing put a file there: the image ships
// no default, so the container exited on startup and `make up-observability` came up one service
// short with nothing on screen to say which one. Any service configured the same way would fail the
// same way, so this checks all of them.
func TestEveryServicePointedAtAConfigFileIsGivenOne(t *testing.T) {
	contents, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("reading the compose file: %v", err)
	}

	var compose struct {
		Services map[string]struct {
			Command []string `yaml:"command"`
			Volumes []string `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(contents, &compose); err != nil {
		t.Fatalf("parsing the compose file: %v", err)
	}
	if len(compose.Services) == 0 {
		t.Fatal("no services found in the compose file, so this test proves nothing")
	}

	var checked int
	for name, service := range compose.Services {
		for _, argument := range service.Command {
			path, pointsAtAFile := configPath(argument)
			if !pointsAtAFile {
				continue
			}
			checked++
			if !mounted(service.Volumes, path) {
				t.Errorf("service %s is started with %q and nothing mounts %s, so it exits on startup",
					name, argument, path)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no service in the compose file names a config file, which cannot be right: this test would pass with the whole stack deleted")
	}
}

// configPath returns the path a command line argument points at, for the spellings the images in
// this stack use.
func configPath(argument string) (string, bool) {
	for _, flag := range []string{"-config.file=", "--config.file=", "--config=", "-config="} {
		if strings.HasPrefix(argument, flag) {
			return strings.TrimPrefix(argument, flag), true
		}
	}
	return "", false
}

// mounted says whether one of the service's volumes lands on path. A compose volume reads
// source:target[:mode], and the target is what the process inside opens.
func mounted(volumes []string, path string) bool {
	for _, volume := range volumes {
		parts := strings.Split(volume, ":")
		if len(parts) >= 2 && parts[1] == path {
			return true
		}
	}
	return false
}
