// Package deploy holds the compose stack. It carries no Go code, only tests, which hold the compose
// file and the makefile to the rules that are easy to break and expensive to notice.
package deploy

import (
	"os"
	"os/exec"
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

// TestOnlyTheControlPlaneIsOnTheNetworkSessionsJoin.
//
// A session's sandbox joins one network so it can reach the control plane. Everything else on that
// network is reachable by a session too, and a session runs model output, so what is on it is the
// whole of the boundary: put Postgres there and any session can open a connection to the crew's
// store.
//
// Compose puts a service with no networks key on the default network, so this reads the file the way
// compose does rather than looking for an absence.
func TestOnlyTheControlPlaneIsOnTheNetworkSessionsJoin(t *testing.T) {
	compose := composeFile(t)

	if _, declared := compose.Networks[sessionNetwork]; !declared {
		t.Fatalf("the compose file declares no %q network, so there is nothing for a sandbox to join",
			sessionNetwork)
	}
	if len(compose.Services) == 0 {
		t.Fatal("no services found in the compose file, so this test proves nothing")
	}

	var on []string
	for name, service := range compose.Services {
		if _, joined := service.Networks[sessionNetwork]; joined {
			on = append(on, name)
		}
	}
	if len(on) != 1 || on[0] != "controlplane" {
		t.Fatalf("the services a session can reach are %v, want the control plane and nothing else", on)
	}
}

// TestTheControlPlaneStaysOnTheCrewsOwnNetworkToo. Naming any network at all takes a service off the
// default one, so the store, the broker and the collector would all go out of reach in the same edit
// that put the control plane where sessions are.
func TestTheControlPlaneStaysOnTheCrewsOwnNetworkToo(t *testing.T) {
	joined := composeFile(t).Services["controlplane"].Networks

	if _, onDefault := joined["default"]; !onDefault {
		t.Fatalf("the control plane is on %v, and naming a network takes it off the default one, so it "+
			"reaches neither Postgres nor the collector", joined)
	}
}

// TestTheNetworkComposeMakesIsTheOneTheControlPlaneJoinsSandboxesTo.
//
// Two spellings of one name is how a session ends up on a network the crew is not on, and the failure
// is invisible until a session makes a call: the container starts, the crew looks healthy, and every
// call from inside dies resolving the name. So both come from one variable, and this holds them to it.
func TestTheNetworkComposeMakesIsTheOneTheControlPlaneJoinsSandboxesTo(t *testing.T) {
	compose := composeFile(t)

	made := compose.Networks[sessionNetwork].Name
	told := compose.Services["controlplane"].Environment["QC_SESSION_NETWORK"]

	if made == "" {
		t.Fatal("the sessions network is left to compose to name, and the control plane cannot read that name")
	}
	if made != told {
		t.Fatalf("compose creates %q and the control plane joins sandboxes to %q, so no session reaches the crew",
			made, told)
	}
}

// TestACrewToldWhereItIsPutsItsSessionsWhereTheyCanReachIt. The address and the network are two halves
// of one thing: an address handed to a session that cannot resolve it is the defect this pair exists
// to close, and it reads to the session as the crew being down.
//
// Both are read as what an operator who set neither actually gets. An empty value inside
// ${NAME:-} is still a whole line in the file, so reading the line rather than the default would pass
// on a stack that hands out nothing.
func TestACrewToldWhereItIsPutsItsSessionsWhereTheyCanReachIt(t *testing.T) {
	environment := composeFile(t).Services["controlplane"].Environment

	address := unsetGives(environment["QC_SANDBOX_CONTROL_PLANE"])
	network := unsetGives(environment["QC_SESSION_NETWORK"])
	if address == "" {
		t.Fatal("a crew nobody configured tells a session no address, so a session running work holds a credential it cannot spend")
	}
	if network == "" {
		t.Fatalf("a crew nobody configured hands out %q and puts no session on a network that reaches it", address)
	}
}

// TestTheCrewAnswersToTheNameItHandsOutOnTheNetworkItHandsItOutOn.
//
// A network alias belongs to one network rather than to a container, so the name a service answers to
// is a per network fact. A session has nothing to dial but the name in QC_SANDBOX_CONTROL_PLANE, and
// the whole call fails resolving it if the two ever differ, which reads to the session as the crew
// being down rather than as a name.
func TestTheCrewAnswersToTheNameItHandsOutOnTheNetworkItHandsItOutOn(t *testing.T) {
	crew := composeFile(t).Services["controlplane"]

	handedOut, _, found := strings.Cut(unsetGives(crew.Environment["QC_SANDBOX_CONTROL_PLANE"]), ":")
	if !found || handedOut == "" {
		t.Fatalf("the crew hands out %q, which names no host to resolve",
			crew.Environment["QC_SANDBOX_CONTROL_PLANE"])
	}
	answersTo := crew.Networks[sessionNetwork].Aliases
	for _, alias := range answersTo {
		if alias == handedOut {
			return
		}
	}
	t.Fatalf("the crew hands a session the name %q and answers to %v on the network that session is on",
		handedOut, answersTo)
}

// unsetGives is what a compose value comes out as when the operator set nothing: the default inside
// ${NAME:-default}, or the value itself where it names no variable.
func unsetGives(value string) string {
	inside, found := strings.CutPrefix(value, "${")
	if !found {
		return value
	}
	inside = strings.TrimSuffix(inside, "}")
	_, fallback, found := strings.Cut(inside, ":-")
	if !found {
		return ""
	}
	return fallback
}

// sessionNetwork is the compose file's own key for the network a sandbox joins.
const sessionNetwork = "sessions"

// composeStack is as much of the compose file as these tests read.
type composeStack struct {
	Services map[string]composeService `yaml:"services"`
	Networks map[string]struct {
		Name string `yaml:"name"`
	} `yaml:"networks"`
}

type composeService struct {
	Networks    map[string]composeEndpoint `yaml:"networks"`
	Environment map[string]string          `yaml:"environment"`
}

// composeEndpoint is what a service says about one of its networks. A null value is the ordinary
// case, which is a service saying only that it is on the network.
type composeEndpoint struct {
	Aliases []string `yaml:"aliases"`
}

// composeFile reads the stack, with compose's own rule that a service naming no network is on the
// default one applied, so a test reads what compose builds rather than what the file happens to say.
func composeFile(t *testing.T) composeStack {
	t.Helper()
	contents, err := os.ReadFile("docker-compose.yml")
	if err != nil {
		t.Fatalf("reading the compose file: %v", err)
	}
	var compose composeStack
	if err := yaml.Unmarshal(contents, &compose); err != nil {
		t.Fatalf("parsing the compose file: %v", err)
	}
	for name, service := range compose.Services {
		if len(service.Networks) == 0 {
			service.Networks = map[string]composeEndpoint{"default": {}}
			compose.Services[name] = service
		}
	}
	return compose
}

// TestTwoStacksOnOneMachineDoNotShareASessionNetwork.
//
// PROJECT=<name> exists to give a fully isolated stack. A network named in the compose file alone
// would be one name for every stack on the machine, so a session in the isolated stack would sit on a
// network with the other stack's control plane on it. It is computed from the compose project
// instead, and this reads what make actually computes rather than a pattern matched over the text.
func TestTwoStacksOnOneMachineDoNotShareASessionNetwork(t *testing.T) {
	theirs := makeVariable(t, "QC_SESSION_NETWORK")

	out, err := exec.Command("make", "-C", "..", "--no-print-directory",
		"print-QC_SESSION_NETWORK", "PROJECT=demo").CombinedOutput()
	if err != nil {
		t.Fatalf("make print-QC_SESSION_NETWORK PROJECT=demo: %v\n%s", err, out)
	}
	isolated := strings.TrimSpace(string(out))

	if theirs == "" || isolated == "" {
		t.Fatal("the makefile computes no session network, so compose falls back to one name for every stack")
	}
	if theirs == isolated {
		t.Fatalf("both stacks put their sessions on %q, so a session in one can address the other's control plane",
			theirs)
	}
}
