package deploy

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A system that has not been told which model to run still runs Opus.
//
// The choice lives in the compose file rather than in the code, so a system whose configuration file
// was written before the key existed gets it anyway. That makes the compose default the thing the
// behaviour actually rests on: delete it and the control plane passes no model, the command line
// tool picks Sonnet for itself, every session quietly drops a tier, and every test in internal/model
// stays green because each half is still correct on its own.
func TestASystemThatWasNotToldWhichModelStillRunsOpus(t *testing.T) {
	fallback := composeDefault(t, "QC_CLAUDE_MODEL")
	if !strings.HasPrefix(fallback, "claude-opus-") {
		t.Errorf("with nothing configured the system runs %q, so a session that should be on Opus is on "+
			"whatever the command line tool picks, which is Sonnet", fallback)
	}
}

// The template names the key as well, so an operator editing their own file can find it. `make
// env-check` reads this file to say what a configuration written before the key is missing, and a key
// that is only in the compose file is a key nobody is ever told about.
func TestTheConfigurationTemplateNamesTheModel(t *testing.T) {
	template, err := os.ReadFile("env.example")
	if err != nil {
		t.Fatalf("reading the configuration template: %v", err)
	}
	if !strings.Contains(string(template), "\nQC_CLAUDE_MODEL=") {
		t.Error("env.example never sets QC_CLAUDE_MODEL, so make env-check cannot tell an operator " +
			"their configuration is missing it")
	}
}

// composeDefault returns what an environment key falls back to when the system's configuration file does
// not set it, read out of the ${KEY:-default} the compose file interpolates.
func composeDefault(t *testing.T, key string) string {
	t.Helper()
	contents, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("reading the compose file: %v", err)
	}
	var compose struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(contents, &compose); err != nil {
		t.Fatalf("parsing the compose file: %v", err)
	}
	for name, service := range compose.Services {
		value, set := service.Environment[key]
		if !set {
			continue
		}
		_, fallback, found := strings.Cut(value, ":-")
		if !found {
			t.Fatalf("service %s sets %s to %q, which has no default, so a system that does not "+
				"configure it gets an empty value", name, key, value)
		}
		return strings.TrimSuffix(fallback, "}")
	}
	t.Fatalf("no service in the compose file sets %s, so the control plane never sees it", key)
	return ""
}
