package job

import (
	"fmt"
	"strings"
)

// One requirement, one branch, one pull request, and two workers on it.
//
// The tests one stage wrote never reached the next one. Each test worker took its own sandbox and
// its own clone, wrote its test files there and answered with three lines, and the sandbox then went
// away with the files in it. The build worker for the same requirement took another fresh clone, and
// it was told to read tests that were not in it. The boundary that stage works under guarded files
// that were not there.
//
// So the work lands on a branch. The worker that writes a requirement's tests cuts the branch, pushes
// it and opens the pull request from it, and that pull request is the one the work lands in: it stays
// open and red, carrying the failing tests for its requirement and nothing else. The worker that
// builds the same requirement fetches that branch, checks it out, and turns those tests green on it.
// The build stage opens no pull request at all.
//
// The branch is the system's to name rather than the session's, for the reason the claim is: two
// workers have to agree on one name without either of them being told by the other, and a name a
// session invents is a name the next session cannot guess. It is derived from the job and the
// requirement number, so the same requirement reads back the same branch on every tick, and two
// requirements never share one.
//
// Nothing new stops two workers landing on one branch. A branch belongs to one requirement, the
// claim already refuses a second job taking work a first job holds, and the two stages never run at
// once: the build stage opens only once every requirement's tests are written.

// BranchForRequirement is the branch one requirement's work lives on, for the whole of that
// requirement's life: the tests, and then the implementation that makes them pass.
//
// It carries the job as well as the number, so two jobs that accepted a list of the same length
// never write to one branch, and it is prefixed the way the git skill already prefixes a session's
// own branch, so a person reading the repository can tell what cut it.
func BranchForRequirement(job string, wanted Requirement) string {
	return fmt.Sprintf("krewe/%s-requirement-%d", job, wanted.Number)
}

// Opened is what the worker that wrote one requirement's tests left behind: the branch its failing
// tests are on, and the pull request they are open in.
//
// It is read off that worker's row rather than copied into the record the job keeps, for the reason
// the requirement list is read off the row: a second copy of a fact could only disagree with the
// first.
type Opened struct {
	Branch      string
	PullRequest string
}

// Landed says whether these tests reached a branch anybody else can read.
func (o Opened) Landed() bool { return o.Branch != "" && o.PullRequest != "" }

// OpenedFor is what the workers holding one requirement left behind, and nothing where none of them
// left anything.
//
// The newest worker is the one read, which is the rule the report is read by: a requirement whose
// first worker died has a second, and what the stage stands on is the run that happened last.
func OpenedFor(workers []*Job) Opened {
	if len(workers) == 0 {
		return Opened{}
	}
	worker := workers[len(workers)-1]
	return Opened{Branch: worker.Branch, PullRequest: worker.PullRequest}
}

// CutTheBranch is what the worker that writes a requirement's tests is told about where they go.
//
// The name is given rather than asked for. The worker that builds this requirement has to find these
// files, and it is a different session in a different sandbox that will never speak to this one.
func CutTheBranch(branch string) string {
	return fmt.Sprintf("Your tests go on the branch %s, which is this requirement's branch and "+
		"nobody else's. Cut it from the latest state of the default branch, write your tests on it, "+
		"commit them and push it:\n\n"+
		"    git fetch origin\n"+
		"    git switch --create %s origin/HEAD\n\n"+
		"Do not work on any other branch, and do not rename this one. The worker that builds this "+
		"requirement continues on it, and it finds the branch by this name.", branch, branch)
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
// read tests it has never fetched reads nothing, and it cannot tell that from tests that say
// nothing.
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
// three shapes this job is.
//
// A job with no branch is every job this system ran before a requirement had one, and it is asked for
// what it was always asked for. A branch on a job that builds is a branch somebody else cut, and a
// branch on any other job is this job's own to cut.
func EndsOnItsBranch(one *Job) string {
	switch {
	case one.Repository == "":
		return ""
	case one.Branch == "":
		return EndsInAPullRequest(one.Repository)
	case one.Building:
		return ContinuesThePullRequestOn(one.Repository, one.Branch)
	default:
		return OpensThePullRequestOn(one.Repository, one.Branch)
	}
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
