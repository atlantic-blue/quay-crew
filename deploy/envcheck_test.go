package deploy

import (
	"os/exec"
	"strings"
	"testing"
)

// TestTheDriftCheckNamesConfigurationTheOperatorDoesNotHave.
//
// An upgrade adds configuration and nobody's own file grows with it. Compose fills a key that is
// not there with an empty string, so whatever it turns on is simply off, and nothing anywhere says
// why: a driver whose crew had no address reported that the control plane was refusing connections
// for an evening, while the control plane was up the whole time.
//
// It runs the real target rather than reading the Makefile, because what is being checked is what the
// operator is told, not that a recipe contains a word.
func TestTheDriftCheckNamesConfigurationTheOperatorDoesNotHave(t *testing.T) {
	for _, tc := range []struct {
		name    string
		file    string
		want    []string
		absent  []string
		because string
	}{
		{
			name: "a copy made before the driver existed",
			file: "deploy/testdata/partial.env",
			want: []string{"QC_SANDBOX_NETWORK", "QC_SANDBOX_CONTROL_PLANE"},
			because: "these are what let a session reach the crew, and without them it reaches " +
				"localhost inside its own container",
		},
		{
			name:    "a copy with everything in it",
			file:    "deploy/testdata/complete.env",
			absent:  []string{"does not set"},
			because: "there is no drift, and a note about nothing trains the operator to skip the notes",
		},
		{
			name:    "no configuration at all",
			file:    "deploy/testdata/absent.env",
			want:    []string{"make config"},
			because: "the stack comes up on the compose defaults, which is worth saying once",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := exec.Command("make", "-C", "..", "--no-print-directory",
				"env-check", "ENV_FILE="+tc.file).CombinedOutput()
			if err != nil {
				t.Fatalf("make env-check: %v\n%s", err, out)
			}
			said := string(out)
			for _, want := range tc.want {
				if !strings.Contains(said, want) {
					t.Errorf("it never names %s, and %s\n%s", want, tc.because, said)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(said, absent) {
					t.Errorf("it complains about %q, and %s\n%s", absent, tc.because, said)
				}
			}
		})
	}
}

// TestUpgradeChecksTheOperatorsConfiguration, so the drift is named at the moment it is introduced
// rather than only by somebody who thinks to run the check.
func TestUpgradeChecksTheOperatorsConfiguration(t *testing.T) {
	if recipe := target(t, "upgrade"); !strings.Contains(recipe, "env-check") {
		t.Errorf("make upgrade never checks the operator's configuration:\n%s", recipe)
	}
}
