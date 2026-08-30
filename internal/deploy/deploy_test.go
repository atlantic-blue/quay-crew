package deploy_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/deploy"
)

func TestATargetIsReadWhole(t *testing.T) {
	target, err := deploy.ParseTarget(
		"  123456789012 ", " eu-west-2\n", "arn:aws:iam::123456789012:role/quay-deploy ")
	if err != nil {
		t.Fatalf("ParseTarget: %v", err)
	}
	want := deploy.Target{
		Account:  "123456789012",
		Region:   "eu-west-2",
		Identity: "arn:aws:iam::123456789012:role/quay-deploy",
	}
	if target != want {
		t.Fatalf("read %+v, want %+v", target, want)
	}
	if target.IsZero() {
		t.Fatal("a target that was read reports itself as nothing declared")
	}
	if got := target.String(); got != "123456789012/eu-west-2" {
		t.Fatalf("the one line form is %q", got)
	}
}

func TestNothingDeclaredIsNothing(t *testing.T) {
	var none deploy.Target
	if !none.IsZero() {
		t.Fatal("the zero target reports something declared")
	}
	if got := none.String(); got != "" {
		t.Fatalf("the zero target reads as %q", got)
	}
}

// Every region this crew could reach, and the shapes that are not one. A region is checked because
// an account with no region is a target that cannot be acted on, and a typed region that is not one
// fails at the moment somebody deploys rather than at the moment they declare it.
func TestTheRegionsThatAreRegions(t *testing.T) {
	for _, region := range []string{"eu-west-2", "us-east-1", "ap-southeast-3", "us-gov-west-1", "cn-north-1"} {
		if _, err := deploy.ParseTarget("123456789012", region, "arn:aws:iam::123456789012:role/r"); err != nil {
			t.Errorf("the region %q was refused: %v", region, err)
		}
	}
	for reason, region := range map[string]string{
		"a country":            "england",
		"capitals":             "EU-WEST-2",
		"no number":            "eu-west",
		"a space":              "eu west 2",
		"an availability zone": "eu-west-2a",
	} {
		if _, err := deploy.ParseTarget("123456789012", region, "arn:aws:iam::123456789012:role/r"); err == nil {
			t.Errorf("the region %q was accepted (%s)", region, reason)
		}
	}
}

func TestAnAccountIsTwelveDigits(t *testing.T) {
	for reason, account := range map[string]string{
		"eleven digits":       "12345678901",
		"thirteen":            "1234567890123",
		"a name":              "atlantic-blue",
		"digits and a letter": "12345678901a",
	} {
		_, err := deploy.ParseTarget(account, "eu-west-2", "arn:aws:iam::123456789012:role/r")
		if err == nil {
			t.Errorf("the account %q was accepted (%s)", account, reason)
			continue
		}
		if !strings.Contains(err.Error(), "twelve digits") {
			t.Errorf("the refusal of %q does not say what an account is: %v", account, err)
		}
	}
}

// The check the whole record exists for. Pasting the role from the other account is the mistake that
// produces a tree of jobs writing correct infrastructure for somewhere it can never reach, and it is
// invisible until a pipeline runs.
func TestAnIdentityFromAnotherAccountIsRefused(t *testing.T) {
	_, err := deploy.ParseTarget("123456789012", "eu-west-2", "arn:aws:iam::999999999999:role/quay-deploy")
	if err == nil {
		t.Fatal("an identity in another account was accepted")
	}
	for _, want := range []string{"999999999999", "123456789012"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %s: %v", want, err)
		}
	}
}

func TestAnIdentityIsARoleSomethingCanAssume(t *testing.T) {
	for reason, identity := range map[string]string{
		"a bare name":     "quay-deploy",
		"a user":          "arn:aws:iam::123456789012:user/julian",
		"another service": "arn:aws:s3:::a-bucket",
		"no role name":    "arn:aws:iam::123456789012:role/",
	} {
		if _, err := deploy.ParseTarget("123456789012", "eu-west-2", identity); err == nil {
			t.Errorf("the identity %q was accepted (%s)", identity, reason)
		}
	}
	for _, identity := range []string{
		"arn:aws:iam::123456789012:role/quay-deploy",
		"arn:aws:iam::123456789012:role/service-role/quay-deploy",
		"arn:aws-cn:iam::123456789012:role/quay-deploy",
		"arn:aws-us-gov:iam::123456789012:role/quay-deploy",
	} {
		if _, err := deploy.ParseTarget("123456789012", "eu-west-2", identity); err != nil {
			t.Errorf("the identity %q was refused: %v", identity, err)
		}
	}
}

// A target is whole or it is absent. Half of one reads as an answer to "where does this go" and is
// not one, and the cost of believing it is the same as the cost of having no target at all.
func TestHalfATargetIsRefused(t *testing.T) {
	whole := deploy.Target{
		Account:  "123456789012",
		Region:   "eu-west-2",
		Identity: "arn:aws:iam::123456789012:role/quay-deploy",
	}
	for missing, given := range map[string]deploy.Target{
		"account":  {Region: whole.Region, Identity: whole.Identity},
		"region":   {Account: whole.Account, Identity: whole.Identity},
		"identity": {Account: whole.Account, Region: whole.Region},
	} {
		_, err := deploy.ParseTarget(given.Account, given.Region, given.Identity)
		if err == nil {
			t.Errorf("a target with no %s was accepted", missing)
			continue
		}
		if !strings.Contains(err.Error(), missing) {
			t.Errorf("the refusal does not name the missing %s: %v", missing, err)
		}
	}
}

// Nothing at all is not half a target: it is a project that has not said, which is every project
// until somebody says. Clearing has to stay possible through the same door that sets.
func TestNothingAtAllIsAccepted(t *testing.T) {
	target, err := deploy.ParseTarget("", "  ", "")
	if err != nil {
		t.Fatalf("declaring nothing was refused: %v", err)
	}
	if !target.IsZero() {
		t.Fatalf("declaring nothing read as %+v", target)
	}
}
