// Package deploy holds the rule for where a project ships.
//
// A workspace is a bag of secrets and a project is a name, so which cloud account a body of work
// deploys into lived in one person's memory and was said out loud when somebody asked. The cost of
// getting that wrong is a tree of jobs that writes correct infrastructure for an account it can
// never reach, and nothing in the crew could tell.
//
// The rule lives here rather than in the command line tool because every way in writes through the
// same control plane: the tool, the console, and every channel.
//
// It is written for one cloud, because this crew has one and a free text field is a record nobody
// can check. The shapes below are what an account, a region and an assumable role look like on
// Amazon Web Services.
package deploy

import (
	"fmt"
	"regexp"
	"strings"
)

// Target is where a project ships: the account, the region inside it, and the identity a pipeline
// assumes to get there.
type Target struct {
	Account  string
	Region   string
	Identity string
}

// IsZero reports whether nothing was declared.
func (t Target) IsZero() bool {
	return t == Target{}
}

// String is the target on one line, account and region, which is what a listing has room for. The
// identity is left out: it repeats the account and it is three times as wide as the column.
func (t Target) String() string {
	if t.IsZero() {
		return ""
	}
	return t.Account + "/" + t.Region
}

var (
	// An account identifier is twelve digits, and nothing else is one.
	accountPattern = regexp.MustCompile(`^[0-9]{12}$`)
	// A region is lowercase words and a number: eu-west-2, us-gov-west-1, cn-north-1. An
	// availability zone is a region with a letter after it, and deploying is done at a region.
	regionPattern = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]+$`)
	// An identity is a role, in any of the three partitions, because a pipeline assumes one. A user
	// is a long lived credential somebody holds, which is not what deploys here.
	identityPattern = regexp.MustCompile(`^arn:(aws|aws-cn|aws-us-gov):iam::([0-9]{12}):role/(.+)$`)
)

// ParseTarget tidies what somebody typed into a target, or says what would have worked.
//
// Three empty values are a project that has not said where it deploys, which every project is until
// somebody says, so they are accepted and read back as nothing. Anything else has to be whole: half
// a target reads as an answer to "where does this go" and is not one.
func ParseTarget(account, region, identity string) (Target, error) {
	target := Target{
		Account:  strings.TrimSpace(account),
		Region:   strings.TrimSpace(region),
		Identity: strings.TrimSpace(identity),
	}
	if target.IsZero() {
		return Target{}, nil
	}
	for _, missing := range []struct {
		what  string
		value string
		looks string
	}{
		{"account", target.Account, "123456789012"},
		{"region", target.Region, "eu-west-2"},
		{"identity", target.Identity, "arn:aws:iam::123456789012:role/quay-deploy"},
	} {
		if missing.value == "" {
			return Target{}, fmt.Errorf(
				"a deploy target says all three or none, and this one has no %s: give one like %q",
				missing.what, missing.looks)
		}
	}
	if !accountPattern.MatchString(target.Account) {
		return Target{}, fmt.Errorf(
			"the account %q is not an account: an account is twelve digits, for example \"123456789012\"",
			target.Account)
	}
	if !regionPattern.MatchString(target.Region) {
		return Target{}, fmt.Errorf(
			"the region %q is not a region: a region is lowercase words and a number, for example \"eu-west-2\"",
			target.Region)
	}
	named := identityPattern.FindStringSubmatch(target.Identity)
	if named == nil {
		return Target{}, fmt.Errorf(
			"the identity %q is not a role a pipeline can assume: give an address like "+
				"\"arn:aws:iam::123456789012:role/quay-deploy\"", target.Identity)
	}
	// The check the record exists for. Pasting the role from the other account is invisible until a
	// pipeline runs, and by then a tree of jobs has written infrastructure for somewhere it cannot
	// reach.
	if named[2] != target.Account {
		return Target{}, fmt.Errorf(
			"the identity is in account %s and this target deploys to %s: a role is assumed in the "+
				"account it belongs to, so one of the two is the wrong one", named[2], target.Account)
	}
	return target, nil
}
