package job

import "fmt"

// The tests one stage writes reach the stage that builds against them, on a branch.
//
// The fault this answers: each test worker held a sandbox of its own with a clone of its own, wrote
// its test files there, and was never asked to commit them. The report it answered with named the
// tests that fail, and that report was the only thing that outlived the sandbox. Each build worker
// then cloned the default branch again, was handed those names, and was told to read tests that were
// not in its checkout. The boundary the build stage works under guarded files that were not there.
//
// One branch for the job, not one for each worker. The job is what the work belongs to, and a branch
// for each worker would need something to merge five of them at the moment the stage closes, which
// is git running outside a container: the control plane is a static binary with no git and no
// credential. So each worker's commits are put on top of what is already on the branch, and the
// remote decides who was first. See internal/publish.
//
// The name is derived from the job rather than declared or recorded, for the reason a claim is: two
// stages have to write the same string without either of them being told it, and a second copy of a
// fact could only disagree with the first. The job identifier is what makes it this job's branch and
// nobody else's.

// TestBranch is the branch the tests of one job go on, and the branch its build workers read them
// off.
//
// Empty for a job that names no repository. There is nowhere to push to, so a branch named for it
// would be a promise about a remote this job does not have.
func TestBranch(one *Job) string {
	if one == nil || one.Repository == "" {
		return ""
	}
	return fmt.Sprintf("krewe/tests/%s", one.ID)
}

// TheTestsGoOnABranch is what a test worker is told about where its tests have to end up.
//
// It asks for the commit and not for the push. The push is the system's: five workers reach this
// branch at the same time, and a session told to resolve a race it cannot see writes the file a
// second time or takes another worker's away. What the worker owes is the commit, which is the one
// half of it nobody else can do.
func TheTestsGoOnABranch(branch string) string {
	return fmt.Sprintf("The tests you write are read by another worker in the stage after this one, "+
		"in a sandbox of its own, so they have to leave this one. Commit every test file you write. "+
		"The system then puts your commits on the branch %s, which is where the tests of this job "+
		"live, and it replays them onto whatever the other workers put there. Do not push that branch "+
		"yourself and do not merge anything into it. A worker that commits nothing has written tests "+
		"nobody can read, and the stage refuses it.", branch)
}

// TheTestsAreOnABranch is what a build worker is told about where the tests it may not change are.
//
// It says to cut its own branch from that one rather than to work on it. Every vertical is built by
// a worker of its own at the same time, and all of them pushing an implementation onto the branch
// the tests live on is the race the delivery of the tests already had to answer once.
func TheTestsAreOnABranch(branch string) string {
	return fmt.Sprintf("The failing tests for this job are on the branch %s, and they are not on the "+
		"default branch. Fetch that branch and start your work from it before you build anything:\n\n"+
		"    git fetch origin %s\n"+
		"    git switch -c <your branch> origin/%s\n\n"+
		"The tests you are told not to change are in that checkout. A build cut from the default "+
		"branch instead is a build with no tests in front of it.", branch, branch, branch)
}

// TestsNotOnTheBranch is why a requirement whose worker answered a report is refused anyway: the
// tests it reported on are not on the branch, so nothing the next stage reads holds this requirement.
//
// A report is a session's word about its own run, and the branch is the thing anybody else can read.
// A stage that closed on the first of those would hand the build stage a list of test names and an
// empty checkout, which is the fault this whole road exists to end.
func TestsNotOnTheBranch(requirement Requirement, branch, why string) string {
	said := fmt.Sprintf("requirement %d, %q: its tests are not on %s",
		requirement.Number, requirement.Text, branch)
	if why != "" {
		said += ", " + why
	}
	return said
}
