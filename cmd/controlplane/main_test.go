package main

import (
	"strings"
	"testing"
)

func TestAConfiguredAllowlistIsSaidToBeIgnored(t *testing.T) {
	notice, retired := sandboxSecretsRetired("GH_TOKEN,LINEAR_API_KEY")
	if !retired {
		t.Fatal("a crew configured with QC_SANDBOX_SECRETS said nothing about it")
	}
	for _, want := range []string{"QC_SANDBOX_SECRETS", "no longer read", "configuration"} {
		if !strings.Contains(notice, want) {
			t.Errorf("the notice does not mention %q: %s", want, notice)
		}
	}
}

func TestNothingIsSaidWhenTheAllowlistWasNeverSet(t *testing.T) {
	if _, retired := sandboxSecretsRetired(""); retired {
		t.Error("a crew with no QC_SANDBOX_SECRETS was told about one")
	}
	// Whitespace is how a commented out line comes back from compose, and a warning about a setting
	// the operator has already removed is noise.
	if _, retired := sandboxSecretsRetired("   "); retired {
		t.Error("a blank QC_SANDBOX_SECRETS was read as configured")
	}
}

// What a session may do when it is born comes from the crew's configuration. These hold the reading of
// it, and in particular hold it to refusing rather than falling back, because a crew configured for
// "planning" that quietly ran everything in acceptEdits would look exactly like a crew configured for
// acceptEdits, and the operator would find out when a task did something they had asked it not to.
func TestTheBirthModeIsReadFromTheCrewsConfiguration(t *testing.T) {
	for _, tc := range []struct {
		configured string
		want       string
	}{
		{configured: "plan", want: "plan"},
		{configured: "edits", want: "acceptEdits"},
		{configured: "dangerous", want: "bypassPermissions"},
		{configured: "bypassPermissions", want: "bypassPermissions"},
		{configured: "  Plan  ", want: "plan"},
		// Nothing set is not a choice, and it keeps what every crew has had until now.
		{configured: "", want: ""},
	} {
		t.Run(tc.configured, func(t *testing.T) {
			got, err := birthPermissionMode(tc.configured)
			if err != nil {
				t.Fatalf("QC_PERMISSION_MODE=%q was refused: %v", tc.configured, err)
			}
			if got != tc.want {
				t.Fatalf("QC_PERMISSION_MODE=%q became %q, want %q", tc.configured, got, tc.want)
			}
		})
	}
}

func TestAConfiguredModeThatIsNotAModeStopsTheCrewStarting(t *testing.T) {
	for _, wrong := range []string{"planning", "yolo", "accept", "true"} {
		t.Run(wrong, func(t *testing.T) {
			_, err := birthPermissionMode(wrong)

			if err == nil {
				t.Fatalf("QC_PERMISSION_MODE=%q was accepted, and every task would run in a mode nobody chose", wrong)
			}
			for _, name := range []string{"plan", "edits", "dangerous"} {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("the refusal never names %q, so there is nowhere to learn the modes: %s", name, err)
				}
			}
		})
	}
}
