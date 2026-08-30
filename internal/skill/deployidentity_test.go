package skill

import (
	"strings"
	"testing"
)

// The deploy identity skill is the rule that a piece of infrastructure is not ready until the
// identity that will apply it has been asked whether it may. It exists because of one merged pull
// request: six resources, every check green, and an apply that died on its first write because the
// user it ran as held read only access to the account.
//
// It is the first shipped skill the system gives itself rather than offers, so its words are load
// bearing in the same way the ste skill's are: every session reads its summary, and an infrastructure
// job reads the brief instead of finding out from a failed deploy.
func TestTheShippedDeployIdentitySkillNamesTheSimulator(t *testing.T) {
	brief := flowed(shippedSkill(t, "deploy-identity").Brief)

	// The whole mechanism. Without the action name there is nothing to search for, and without the
	// command there is a rule nobody can follow.
	for _, said := range []string{
		"iam:simulateprincipalpolicy",
		"aws iam simulate-principal-policy",
		"--policy-source-arn",
		"--action-names",
	} {
		if !strings.Contains(brief, said) {
			t.Errorf("the brief never says %q, so the one question that would have caught this cannot be asked", said)
		}
	}
	// An answer nobody can read is an answer nobody acts on: the API says implicitDeny for a
	// permission that was simply never granted, which is the shape the incident actually had.
	for _, decision := range []string{"allowed", "implicitdeny", "explicitdeny"} {
		if !strings.Contains(brief, decision) {
			t.Errorf("the brief never says what %q means, so the result is read as a wall of output", decision)
		}
	}
}

// Both traps cost a cycle on the incident, and each one is invisible from inside the mistake.
func TestTheShippedDeployIdentitySkillCarriesBothTraps(t *testing.T) {
	brief := flowed(shippedSkill(t, "deploy-identity").Brief)

	if !strings.Contains(brief, "a plan needs fewer permissions than an apply") {
		t.Error("the brief does not say a plan proves less than an apply, so a green plan will be read as evidence")
	}
	if !strings.Contains(brief, "the first denial hides the rest") {
		t.Error("the brief does not say the first denial hides the rest, so the gap is found one deploy at a time")
	}
	if !strings.Contains(brief, "every action in one call") {
		t.Error("the brief does not say to ask about every action at once, which is what makes one report name the whole gap")
	}
}

// The half of the rule that is not a command. A check that ran and was not reported is a check
// nobody can act on, and a job that reports ready over a denial has handed the failure to whoever
// merges.
func TestTheShippedDeployIdentitySkillStopsAJobReportingReady(t *testing.T) {
	brief := flowed(shippedSkill(t, "deploy-identity").Brief)

	if !strings.Contains(brief, "a denied action stops you reporting the work as ready") {
		t.Error("the brief does not stop the job, so a denial becomes a note under a pull request that merges anyway")
	}
	for _, carried := range []string{"the identity", "every action you checked", "denied"} {
		if !strings.Contains(brief, carried) {
			t.Errorf("the brief does not say the pull request carries %q, and a reader who cannot see what was checked assumes nothing was", carried)
		}
	}
	// The honest third answer. A session with no cloud credential, or a cloud with no simulator,
	// must not report a check it never ran as a pass.
	if !strings.Contains(brief, "not run is not the same as passed") {
		t.Error("the brief does not say an unrun check is not a pass, which is the answer where the check cannot run at all")
	}
}

// What it asks of a sandbox, which is nothing.
//
// It is a rule rather than a tool, and the two ways a skill can fail to reach a session are a binary
// the image does not carry and a secret the workspace has not set. Either one would take the rule out
// of the sessions it exists for: a workspace whose pipeline authenticates by federated identity holds
// no cloud credential, and a system level skill naming a binary refuses every task in every workspace
// on an image that lags. The brief says what to do when the command line or the credential is not
// there, which is to say the check did not run.
func TestTheShippedDeployIdentitySkillAsksForNothing(t *testing.T) {
	held := shippedSkill(t, "deploy-identity")

	if len(held.Binaries) != 0 {
		t.Errorf("it declares the binaries %v, so an image without one refuses every task in the system over a rule", held.Binaries)
	}
	if len(held.Secrets) != 0 {
		t.Errorf("it names the secrets %v, so a workspace that has not set them loses the rule as well as the check", held.SecretNames())
	}
}
