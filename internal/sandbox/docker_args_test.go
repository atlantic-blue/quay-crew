package sandbox

import (
	"strings"
	"testing"
)

// TestASandboxJoinsTheNetworkItWasGiven. Reaching the control plane is what lets a session drive the
// crew, and a sandbox on the daemon's default network cannot reach it at all.
func TestASandboxJoinsTheNetworkItWasGiven(t *testing.T) {
	provider := DockerProvider{Image: "img", Network: "quaycrew_default"}
	got := strings.Join(provider.runArgs("quaycrew-s1", Config{ID: "s1"}, nil), " ")

	if !strings.Contains(got, "--network quaycrew_default") {
		t.Fatalf("the sandbox does not join the network it was given:\n%s", got)
	}
}

// TestASandboxJoinsNothingWithNoNetworkConfigured: a session that can reach the crew can also stop
// other sessions, so it is turned on rather than assumed.
func TestASandboxJoinsNothingWithNoNetworkConfigured(t *testing.T) {
	provider := DockerProvider{Image: "img"}
	got := strings.Join(provider.runArgs("quaycrew-s1", Config{ID: "s1"}, nil), " ")

	if strings.Contains(got, "--network") {
		t.Fatalf("the sandbox joins a network with none configured:\n%s", got)
	}
}

// TestTheSandboxStillCarriesItsEnvironmentAndMounts, so the network did not displace anything.
func TestTheSandboxStillCarriesItsEnvironmentAndMounts(t *testing.T) {
	provider := DockerProvider{Image: "img", Network: "net", Mounts: []string{"/a:/b:ro"}}
	got := strings.Join(provider.runArgs("quaycrew-s1", Config{
		ID: "s1", Env: []string{"QC_GRPC_ADDR=controlplane:50051"},
	}, []Mount{{Source: "/host", Target: "/home/agent/.claude"}}), " ")

	for _, want := range []string{
		"--env QC_GRPC_ADDR=controlplane:50051",
		"-v /host:/home/agent/.claude",
		"-v /a:/b:ro",
		"img sleep infinity",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the run is missing %q:\n%s", want, got)
		}
	}
}
