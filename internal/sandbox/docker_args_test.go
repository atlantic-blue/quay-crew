package sandbox

import (
	"strings"
	"testing"
)

// TestTheDriverJoinsTheNetworkAndAnOrdinarySessionDoesNot. Reaching the control plane is what lets a
// session drive the crew, and a session that can drive the crew can also stop other sessions, so it is
// the one session marked for it rather than every session in the crew.
func TestTheDriverJoinsTheNetworkAndAnOrdinarySessionDoesNot(t *testing.T) {
	provider := DockerProvider{Image: "img", Network: "quaycrew_default", DriverMounts: []string{"/hub:/hub:ro"}}

	driver := strings.Join(provider.runArgs("quaycrew-s1", Config{ID: "s1", Driver: true}, nil), " ")
	if !strings.Contains(driver, "--network quaycrew_default") {
		t.Fatalf("the driver does not join the network:\n%s", driver)
	}
	if !strings.Contains(driver, "-v /hub:/hub:ro") {
		t.Fatalf("the driver does not get the host paths it was given:\n%s", driver)
	}

	ordinary := strings.Join(provider.runArgs("quaycrew-s2", Config{ID: "s2"}, nil), " ")
	if strings.Contains(ordinary, "--network") {
		t.Fatalf("an ordinary session joins the crew's network:\n%s", ordinary)
	}
	if strings.Contains(ordinary, "/hub") {
		t.Fatalf("an ordinary session gets the driver's host paths:\n%s", ordinary)
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
