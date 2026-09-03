package job

import (
	"fmt"
	"strings"
)

// One requirement, one branch, one pull request, and two workers on it.
//
// The fault this answers: each test worker held a sandbox of its own with a clone of its own, wrote
// its test files there and answered with three lines. The sandbox then went away with the files in
// it. Each build worker cloned the default branch again, was handed the names of the failing tests
// and was told to read them and not change them, and they were not in its checkout. The boundary the
// build stage works under guarded files that were not there.
//
// So the work lands on a branch, and the branch belongs to the requirement rather than to the job.
// A branch for each requirement is what makes one pull request able to carry that requirement from
// its first failing test to its last passing one: the worker that writes the tests opens it red, the
// worker that builds turns it green on the same branch, and the build stage opens none of its own.
// Nothing has to be merged when the stage closes, because no two requirements share a branch.
//
// It also settles the race by construction. One branch for the whole job has five workers pushing to
// one place at once and needs the delivery to replay onto whatever is there; a branch per
// requirement has one worker on it at a time, so the ordinary push is enough and the replay in
// internal/publish is what catches a worker that committed and did not push.
//
// The name is the system's rather than the session's, and it is derived rather than declared, for
// the reason a claim is: two stages have to write the same string without either of them being told
// it by the other, and a name a session invents is a name the next session cannot guess.
//
// Two workers never land on one branch. A branch belongs to one requirement, the claim already
// refuses a second job taking work a first job holds, and the two stages never run at once: the
// build stage opens only once every requirement's tests are written.

// BranchForRequirement is the branch one requirement's work lives on, for the whole of that
// requirement's life: the tests, and then the implementation that makes them pass.
//
// It carries the job as well as the number, so two jobs that accepted a list of the same length
// never write to one branch, and it is prefixed the way the git skill already prefixes a session's
// own branch, so a person reading the repository can tell what cut it.
func BranchForRequirement(job string, wanted Requirement) string {
	return fmt.Sprintf("krewe/%s-requirement-%d", job, wanted.Number)
}

// BranchFor is that branch for a job that has somewhere to push it, and nothing for a job that does
// not. A job that names no repository has no remote, so a branch named for it would be a promise
// about somewhere this job cannot reach.
func BranchFor(one *Job, wanted Requirement) string {
	if one == nil || one.Repository == "" {
		return ""
	}
	return BranchForRequirement(one.ID, wanted)
}

// Opened is what the run that wrote one requirement's tests left behind: the branch its failing
// tests are on, and the pull request they are open in.
//
// It is read off that run's row rather than copied into the record the job keeps, for the reason the
// requirement list is read off the row: a second copy of a fact could only disagree with the first.
type Opened struct {
	Branch      string
	PullRequest string
}

// Landed says whether these tests reached a branch anybody else can read.
func (o Opened) Landed() bool { return o.Branch != "" && o.PullRequest != "" }

// OpenedFor is what the runs holding one requirement left behind, and nothing where none of them
// left anything.
//
// The newest run is the one read, which is the rule the report is read by: a requirement whose first
// run died has a second, and what the stage stands on is the run that happened last.
func OpenedFor(runs []*Execution) Opened {
	if len(runs) == 0 {
		return Opened{}
	}
	worker := runs[len(runs)-1]
	return Opened{Branch: worker.Branch, PullRequest: worker.PullRequest}
}

// CutTheBranch is what the worker that writes a requirement's tests is told about where they go.
//
// The name is given rather than asked for. The worker that builds this requirement has to find these
// files, and it is a different session in a different sandbox that will never speak to this one.
//
// It asks for the commit as well as the push, and it says why: the push is also the system's, on the
// tick that reads this worker's answer, so a worker that committed and could not push has still put
// its tests where the next stage reads them.
func CutTheBranch(branch string) string {
	return fmt.Sprintf("Your tests go on the branch %s, which is this requirement's branch and "+
		"nobody else's. Cut it from the latest state of the default branch, write your tests on it, "+
		"commit every file you write, and push it:\n\n"+
		"    git fetch origin\n"+
		"    git switch --create %s origin/HEAD\n\n"+
		"Do not work on any other branch, and do not rename this one. The worker that builds this "+
		"requirement continues on it, and it finds the branch by this name. A worker that commits "+
		"nothing has written tests nobody can read, and the stage refuses it.", branch, branch)
}

// AnOpenRedPullRequest is what that worker is told about the pull request it opens.
//
// The pull request is the one the work lands in rather than a place to show a red suite. It stays
// open and red until the requirement is built, which is the ordinary state of work in progress, and
// nothing red is merged.
func AnOpenRedPullRequest(branch string) string {
	return fmt.Sprintf("Open the pull request from %s and leave it open. It is red, because every "+
		"test you wrote fails, and that is what it is for: the worker that builds this requirement "+
		"turns it green on the same branch, in the same pull request. Do not merge it and do not "+
		"close it.", branch)
}

// ContinueOnTheBranch is what the worker that builds a requirement is told about where its tests
// are: the branch that holds them, and the two commands that put them in its checkout.
//
// The commands are given rather than described. This is the whole of the change: a worker told to
// read tests it has never fetched reads nothing, and from inside the session it cannot tell that
// from tests that say nothing.
func ContinueOnTheBranch(opened Opened) string {
	said := fmt.Sprintf("The failing tests for this vertical are already written, and they are on the "+
		"branch %s. They are not in a fresh checkout, so fetch that branch and work on it:\n\n"+
		"    git fetch origin\n"+
		"    git switch %s\n\n"+
		"Every test you were given by name is in there. If git says the branch is already checked "+
		"out in another working tree, that tree belongs to a session that has finished: take a clone "+
		"of your own and check the branch out in it.", opened.Branch, opened.Branch)
	if opened.PullRequest != "" {
		said += fmt.Sprintf("\n\nThat branch already has a pull request open on it, at %s, and it is "+
			"the pull request this work lands in. Push your commits to the same branch. Do not open a "+
			"second pull request, do not open one against any other branch, and do not merge this one. "+
			"Answer with the address above, which is where your work is.", opened.PullRequest)
	}
	return said
}

// OpensThePullRequestOn is the line the system sends a session whose job carries a branch of its
// own: the branch is named, and the pull request is opened from it.
//
// It stands where EndsInAPullRequest stands on every other job, and it says the same three things:
// push, open the pull request, do not merge. What it adds is the name of the branch, because this
// job's work is read by a later job that was never told anything by this one.
func OpensThePullRequestOn(repository, branch string) string {
	return fmt.Sprintf("This job works in %s and ends in a pull request against it, from the branch "+
		"%s and from no other. Push that branch and open the pull request before you answer, and put "+
		"its address in your answer. Do not merge it: the merge is somebody else's.",
		repository, branch)
}

// ContinuesThePullRequestOn is the line the system sends a session that is continuing work somebody
// else opened: the branch is already there and so is the pull request.
//
// It is the other way off EndsInAPullRequest, and it exists because that line asks for a pull request
// to be opened. A build worker that followed it would open a second one against the same branch, and
// the work of one requirement would then be in two places.
func ContinuesThePullRequestOn(repository, branch string) string {
	return fmt.Sprintf("This job works in %s, on the branch %s, and the pull request it lands in is "+
		"already open on that branch. Push your commits to %s before you answer, and put the address "+
		"of that open pull request in your answer. Do not open a second pull request. Do not merge "+
		"it: the merge is somebody else's.", repository, branch, branch)
}

// EndsOnItsBranch is the line about the pull request the system sends a session, whichever of the
// three shapes the task is.
//
// A task that runs the job itself is asked for what every job is asked for: a pull request, wherever
// the session put the work. A run of the test stage cuts its requirement's branch and opens the pull
// request on it, and a run of the build stage continues the one already open there. The run says
// which of the two it is, so nothing has to carry a flag about it.
func EndsOnItsBranch(one *Job, run *Execution) string {
	switch {
	case one.Repository == "":
		return ""
	case run == nil || run.Branch == "":
		return EndsInAPullRequest(one.Repository)
	case run.Stage == StageBuild:
		return ContinuesThePullRequestOn(one.Repository, run.Branch)
	default:
		return OpensThePullRequestOn(one.Repository, run.Branch)
	}
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

// TheGitCommandsIn is every git command written in one of these lines, in the order they are written.
//
// It is here rather than in a test because the commands are the instruction: a test that types them
// out again proves that the test can check out a branch, which nobody doubted. Reading them back out
// of the brief is what says the words a worker is given put the tests in front of it.
func TheGitCommandsIn(said string) [][]string {
	var found [][]string
	for _, line := range strings.Split(said, "\n") {
		words := strings.Fields(line)
		if len(words) < 2 || words[0] != "git" {
			continue
		}
		found = append(found, words)
	}
	return found
}
