package main

import (
	"strings"
	"testing"
)

func TestAConfiguredAllowlistIsSaidToBeIgnored(t *testing.T) {
	notice, retired := sandboxSecretsRetired("GH_TOKEN,LINEAR_API_KEY")
	if !retired {
		t.Fatal("a system configured with QC_SANDBOX_SECRETS said nothing about it")
	}
	for _, want := range []string{"QC_SANDBOX_SECRETS", "no longer read", "configuration"} {
		if !strings.Contains(notice, want) {
			t.Errorf("the notice does not mention %q: %s", want, notice)
		}
	}
}

func TestNothingIsSaidWhenTheAllowlistWasNeverSet(t *testing.T) {
	if _, retired := sandboxSecretsRetired(""); retired {
		t.Error("a system with no QC_SANDBOX_SECRETS was told about one")
	}
	// Whitespace is how a commented out line comes back from compose, and a warning about a setting
	// the operator has already removed is noise.
	if _, retired := sandboxSecretsRetired("   "); retired {
		t.Error("a blank QC_SANDBOX_SECRETS was read as configured")
	}
}

// TestASystemThatHandsOutAnAddressNoSessionCanResolveSaysSo.
//
// This is the fault the notice exists for, and it is silent from both ends. The system tells a session
// running a job where it is and mints it a credential; the sandbox joins no network that
// reaches that address; and the session reports "produced zero addresses", which reads as the system
// being down. Only this process can see both halves at once.
func TestASystemThatHandsOutAnAddressNoSessionCanResolveSaysSo(t *testing.T) {
	notice, mismatched := unreachableSystem("docker", "controlplane:50051", "")

	if !mismatched {
		t.Fatal("a system handing out an address its sessions cannot resolve said nothing about it")
	}
	for _, want := range []string{"QC_SANDBOX_CONTROL_PLANE", "QC_SESSION_NETWORK", "resolve"} {
		if !strings.Contains(notice, want) {
			t.Errorf("the notice does not mention %q: %s", want, notice)
		}
	}
}

func TestNothingIsSaidWhenTheTwoHalvesAgree(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		kind, reachable, sessionNetwork string
		because                         string
	}{
		{
			name: "both set", kind: "docker", reachable: "controlplane:50051", sessionNetwork: "quaycrew_sessions",
			because: "the address is handed out and the sandbox can reach it",
		},
		{
			name: "neither set", kind: "docker",
			because: "a session is told nothing and holds no credential, so the two halves agree",
		},
		{
			name: "a network and no address", kind: "docker", sessionNetwork: "quaycrew_sessions",
			because: "nothing is handed out, so nothing is unresolvable",
		},
		{
			name: "sessions on the host", kind: "local", reachable: "controlplane:50051",
			because: "there is no container and no network to put one on",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, mismatched := unreachableSystem(tc.kind, tc.reachable, tc.sessionNetwork); mismatched {
				t.Errorf("the system was warned, and %s", tc.because)
			}
		})
	}
}

// What a session may do when it is born comes from the system's configuration. These hold the reading of
// it, and in particular hold it to refusing rather than falling back, because a system configured for
// "planning" that quietly ran everything in acceptEdits would look exactly like a system configured for
// acceptEdits, and the operator would find out when an exec did something they had asked it not to.
func TestTheBirthModeIsReadFromTheSystemsConfiguration(t *testing.T) {
	for _, tc := range []struct {
		configured string
		want       string
	}{
		{configured: "plan", want: "plan"},
		{configured: "edits", want: "acceptEdits"},
		{configured: "dangerous", want: "bypassPermissions"},
		{configured: "bypassPermissions", want: "bypassPermissions"},
		{configured: "  Plan  ", want: "plan"},
		// Nothing set is not a choice, and it keeps what every system has had until now.
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

func TestAConfiguredModeThatIsNotAModeStopsTheSystemStarting(t *testing.T) {
	for _, wrong := range []string{"planning", "yolo", "accept", "true"} {
		t.Run(wrong, func(t *testing.T) {
			_, err := birthPermissionMode(wrong)

			if err == nil {
				t.Fatalf("QC_PERMISSION_MODE=%q was accepted, and every exec would run in a mode nobody chose", wrong)
			}
			for _, name := range []string{"plan", "edits", "dangerous"} {
				if !strings.Contains(err.Error(), name) {
					t.Errorf("the refusal never names %q, so there is nowhere to learn the modes: %s", name, err)
				}
			}
		})
	}
}

// A setting that changed its name is still read under the old one, and the operator is told which
// line to rename.
//
// Silence would be the failure here. An operator who tuned the lease and then upgraded would keep a
// configuration file that looks configured while the system ran the measured default, and nothing on
// the screen would say the two disagree.
func TestASettingIsStillReadUnderTheNameItUsedToHave(t *testing.T) {
	for _, tc := range []struct {
		name    string
		env     map[string]string
		want    string
		notice  []string
		absent  bool
		because string
	}{
		{
			name:    "only the old name",
			env:     map[string]string{"QC_WORK_LEASE": "90s"},
			want:    "90s",
			notice:  []string{"QC_WORK_LEASE", "QC_JOB_LEASE", "still being read"},
			because: "the number the operator chose has to survive the rename",
		},
		{
			name:    "only the new name",
			env:     map[string]string{"QC_JOB_LEASE": "90s"},
			want:    "90s",
			absent:  true,
			because: "a warning about a line the operator does not have trains them to skip the warnings",
		},
		{
			name:    "both, and they disagree",
			env:     map[string]string{"QC_WORK_LEASE": "30s", "QC_JOB_LEASE": "90s"},
			want:    "90s",
			notice:  []string{"QC_JOB_LEASE is set too and wins"},
			because: "two lines for one setting is the one state where which of them counts has to be said",
		},
		{
			name:    "neither",
			env:     map[string]string{},
			want:    "",
			absent:  true,
			because: "a system that configured nothing is not drifting from anything",
		},
		{
			// Whitespace is how a commented out line comes back from compose.
			name:    "the old name is blank",
			env:     map[string]string{"QC_WORK_LEASE": "   "},
			want:    "",
			absent:  true,
			because: "a blank line is a setting the operator already removed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, notice := renamedSetting("QC_JOB_LEASE", func(key string) string { return tc.env[key] })
			if value != tc.want {
				t.Errorf("the lease reads %q, want %q, and %s", value, tc.want, tc.because)
			}
			if tc.absent && notice != "" {
				t.Errorf("it says %q, and %s", notice, tc.because)
			}
			for _, want := range tc.notice {
				if !strings.Contains(notice, want) {
					t.Errorf("the notice %q does not mention %q, and %s", notice, want, tc.because)
				}
			}
		})
	}
}

// Every renamed setting points at a name something actually reads, which is how this table goes
// stale: an entry is added, the reader is renamed again, and the fallback quietly stops firing.
func TestEveryRenamedSettingPointsAtANameThatIsRead(t *testing.T) {
	if len(renamedSettings) == 0 {
		t.Fatal("the renamed table is empty, so this test proves nothing")
	}
	for was, becomes := range renamedSettings {
		if was == becomes {
			t.Errorf("%s is renamed to itself", was)
		}
		value, _ := renamedSetting(becomes, func(key string) string {
			if key == was {
				return "90s"
			}
			return ""
		})
		if value != "90s" {
			t.Errorf("%s is set and %s reads %q, so the old name is in the table and nothing reads it",
				was, becomes, value)
		}
	}
}
