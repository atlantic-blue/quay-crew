package sandbox

import (
	"strings"
	"testing"
)

// TestEverySandboxJoinsTheNetworkThatReachesTheCrew.
//
// A session running a job is handed the crew's address and a credential minted for that job.
// Both are worthless in a container with no route to the address, which is what an ordinary
// session had: every call died resolving the name, so the verb boundary refused nothing and a
// permission system that has never refused anything cannot be told from one that is not wired up.
//
// The network is not the permission. It decides what a session can address, and the credential
// decides what it may do there.
func TestEverySandboxJoinsTheNetworkThatReachesTheCrew(t *testing.T) {
	provider := DockerProvider{Image: "img", SessionNetwork: "quaycrew_sessions"}

	got := strings.Join(provider.runArgs("quaycrew-s2", Config{ID: "s2"}, nil), " ")
	if !strings.Contains(got, "--network quaycrew_sessions") {
		t.Fatalf("a session joins no network that reaches the crew:\n%s", got)
	}
}

// TestASessionIsKeptOffTheCrewsOwnNetwork. That network carries Postgres, Redpanda and the
// observability stack, and a session runs model output, so the session network is a second network
// with the control plane on it rather than a wider grant of this one.
func TestASessionIsKeptOffTheCrewsOwnNetwork(t *testing.T) {
	provider := DockerProvider{
		Image: "img", Network: "quaycrew_default", SessionNetwork: "quaycrew_sessions",
		DriverMounts: []string{"/hub:/hub:ro"},
	}

	got := strings.Join(provider.runArgs("quaycrew-s2", Config{ID: "s2"}, nil), " ")
	if strings.Contains(got, "quaycrew_default") {
		t.Fatalf("a session joins the crew's own network, where the store and the broker are:\n%s", got)
	}
	if strings.Contains(got, "/hub") {
		t.Fatalf("a session gets the driver's host paths:\n%s", got)
	}
}

// TestTheDriverJoinsTheCrewsOwnNetworkWhenTheOperatorNamedOne. The driver is the deliberate widening,
// and it is the operator who asks for it by naming the network.
func TestTheDriverJoinsTheCrewsOwnNetworkWhenTheOperatorNamedOne(t *testing.T) {
	provider := DockerProvider{
		Image: "img", Network: "quaycrew_default", SessionNetwork: "quaycrew_sessions",
		DriverMounts: []string{"/hub:/hub:ro"},
	}

	got := strings.Join(provider.runArgs("quaycrew-s1", Config{ID: "s1", Driver: true}, nil), " ")
	if !strings.Contains(got, "--network quaycrew_default") {
		t.Fatalf("the driver does not join the network the operator named:\n%s", got)
	}
	if !strings.Contains(got, "-v /hub:/hub:ro") {
		t.Fatalf("the driver does not get the host paths it was given:\n%s", got)
	}
}

// TestTheDriverFallsBackToTheSessionNetwork, so a crew that named no wider network still has a driver
// that can drive it. Reaching the control plane is the whole of what driving needs.
func TestTheDriverFallsBackToTheSessionNetwork(t *testing.T) {
	provider := DockerProvider{Image: "img", SessionNetwork: "quaycrew_sessions"}

	got := strings.Join(provider.runArgs("quaycrew-s1", Config{ID: "s1", Driver: true}, nil), " ")
	if !strings.Contains(got, "--network quaycrew_sessions") {
		t.Fatalf("the driver joins nothing, so it cannot reach the crew it exists to drive:\n%s", got)
	}
}

// TestASandboxJoinsExactlyOneNetwork. A sandbox keeps what it was created with and there is no
// promotion, so this decision is made once, here, and two --network flags would be one of them
// silently ignored.
func TestASandboxJoinsExactlyOneNetwork(t *testing.T) {
	provider := DockerProvider{Image: "img", Network: "quaycrew_default", SessionNetwork: "quaycrew_sessions"}

	for _, cfg := range []Config{{ID: "s1", Driver: true}, {ID: "s2"}} {
		got := provider.runArgs("quaycrew-"+cfg.ID, cfg, nil)
		joined := 0
		for _, arg := range got {
			if arg == "--network" {
				joined++
			}
		}
		if joined != 1 {
			t.Fatalf("the sandbox for %+v joins %d networks, want exactly 1:\n%s", cfg, joined, strings.Join(got, " "))
		}
	}
}

// TestASandboxJoinsNothingWithNoNetworkConfigured: a crew run outside the compose stack may have no
// network to put a session on, and it says so by joining none rather than by inventing a name.
func TestASandboxJoinsNothingWithNoNetworkConfigured(t *testing.T) {
	provider := DockerProvider{Image: "img"}

	for _, cfg := range []Config{{ID: "s1", Driver: true}, {ID: "s2"}} {
		got := strings.Join(provider.runArgs("quaycrew-"+cfg.ID, cfg, nil), " ")
		if strings.Contains(got, "--network") {
			t.Fatalf("the sandbox for %+v joins a network with none configured:\n%s", cfg, got)
		}
	}
}

// TestTheSandboxStillCarriesItsEnvironmentAndMounts, so the network did not displace anything.
func TestTheSandboxStillCarriesItsEnvironmentAndMounts(t *testing.T) {
	provider := DockerProvider{Image: "img", Network: "net", Mounts: []string{"/a:/b:ro"}}
	got := strings.Join(provider.runArgs("quaycrew-s1", Config{
		ID: "s1", Driver: true, Env: []string{"QC_GRPC_ADDR=controlplane:50051"},
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

// Every sandbox gets the memory backed directory a mounted secret lands in, whether or not its
// workspace has mounted one. A mount is a create time decision, so the alternative is that the first
// workspace to mount a secret needs a fresh container before it can have the directory at all.
//
// It names the sandbox user. Without that the directory belongs to root, and the crew writes into it
// as the sandbox's own user, so every write is refused.
func TestEverySandboxGetsSomewhereToPutAMountedSecret(t *testing.T) {
	got := strings.Join(DockerProvider{Image: "img"}.runArgs("quaycrew-s1", Config{ID: "s1"}, nil), " ")

	want := "--tmpfs /run/secrets:mode=0700,uid=1001,gid=1001"
	if !strings.Contains(got, want) {
		t.Fatalf("the sandbox has nowhere to put a mounted secret, want %q:\n%s", want, got)
	}
}

// TestASandboxIsGivenTheMemoryItWasConfiguredWith, and its swap is capped with it.
//
// Told a memory limit and nothing else, the daemon allows swap of the same size again, so a session
// could take twice what the operator said and reach it by thrashing rather than by being told.
func TestASandboxIsGivenTheMemoryItWasConfiguredWith(t *testing.T) {
	provider := DockerProvider{Image: "img", Memory: "4g"}
	got := strings.Join(provider.runArgs("quaycrew-s1", Config{ID: "s1"}, nil), " ")

	for _, want := range []string{"--memory 4g", "--memory-swap 4g"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the sandbox is missing %q:\n%s", want, got)
		}
	}
}

// TestASandboxWithNoMemoryConfiguredIsGivenNoLimit. The figure is a share of one machine between the
// stack, the sessions already running and this one, so it is the operator's to choose. Unset leaves
// every session exactly where it was.
func TestASandboxWithNoMemoryConfiguredIsGivenNoLimit(t *testing.T) {
	got := strings.Join(DockerProvider{Image: "img"}.runArgs("quaycrew-s1", Config{ID: "s1"}, nil), " ")

	if strings.Contains(got, "--memory") {
		t.Fatalf("a sandbox is limited with nothing configured:\n%s", got)
	}
}
