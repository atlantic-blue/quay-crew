package main

import (
	"strings"
	"testing"
)

// What the gate refuses, and what it must not.
//
// The second list is the one that decides whether this hook is worth having. A hook that refuses
// wrongly blocks the work and costs the operator an interruption, which is worse than no hook, and
// every role in this system pushes a branch and opens a pull request on every slice. So a false
// refusal here stops the system delivering anything.
func TestTheGateRefusesAMerge(t *testing.T) {
	refused := []struct {
		name    string
		command string
		says    string
	}{
		{"the command itself", "gh pr merge 12", "gh pr merge"},
		{"with the flags it is usually typed with", "gh pr merge 12 --squash --delete-branch", "gh pr merge"},
		{"named by address rather than number", "gh pr merge https://github.com/atlantic-blue/quay-crew/pull/12", "gh pr merge"},
		{"with a repository named first", "gh --repo atlantic-blue/quay-crew pr merge 12", "gh pr merge"},
		{"with the short form of that flag", "gh -R atlantic-blue/quay-crew pr merge 12", "gh pr merge"},
		{"after another command", "git push -u origin work && gh pr merge 12", "gh pr merge"},
		{"after a semicolon", "gh pr create --fill; gh pr merge 12", "gh pr merge"},
		{"through a shell", `bash -c "gh pr merge 12"`, "gh pr merge"},
		{"through a shell inside a shell", `sh -c 'bash -c "gh pr merge 12"'`, "gh pr merge"},
		{"under sudo", "sudo gh pr merge 12", "gh pr merge"},
		{"with a variable set in front of it", "GH_TOKEN=x gh pr merge 12", "gh pr merge"},
		{"by full path", "/usr/bin/gh pr merge 12", "gh pr merge"},
		{"in a substitution", `echo "$(gh pr merge 12)"`, "gh pr merge"},
		{"over the interface underneath it", "gh api -X PUT repos/atlantic-blue/quay-crew/pulls/12/merge", "merges a pull request"},
		{"with the long form of the method", "gh api --method PUT repos/o/r/pulls/12/merge", "merges a pull request"},
		{"with a field, which makes it a write", "gh api repos/o/r/pulls/12/merge -f merge_method=squash", "merges a pull request"},
		{"with the endpoint written as a full address", "gh api https://api.github.com/repos/o/r/pulls/12/merge -X PUT", "merges a pull request"},
		{"as a graphql mutation", `gh api graphql -f query='mutation { mergePullRequest(input: {pullRequestId: "x"}) { clientMutationId } }'`, "mergePullRequest"},
		{"with curl", "curl -X PUT https://api.github.com/repos/o/r/pulls/12/merge", "merges a pull request"},
		{"as a push straight onto the branch a pull request merges into", "git push origin main", "git push"},
		{"onto master", "git push origin master", "git push"},
		{"with a refspec", "git push origin HEAD:main", "git push"},
		{"with a fully qualified refspec", "git push origin HEAD:refs/heads/main", "git push"},
		{"forced", "git push --force origin +work:main", "git push"},
	}
	for _, one := range refused {
		t.Run(one.name, func(t *testing.T) {
			refusal, stopped := Decide(one.command)
			if !stopped {
				t.Fatalf("%q was allowed, and it merges", one.command)
			}
			if !strings.Contains(refusal.String(), one.says) {
				t.Errorf("the refusal of %q does not say %q:\n%s", one.command, one.says, refusal)
			}
			// A refusal that does not name the way through is a session trying the next spelling of
			// the same command until its budget runs out.
			if !strings.Contains(refusal.String(), "open a pull request") {
				t.Errorf("the refusal of %q does not say what to do instead:\n%s", one.command, refusal)
			}
		})
	}
}

func TestTheGateAllowsTheWorkEveryRoleActuallyDoes(t *testing.T) {
	allowed := []struct {
		name    string
		command string
	}{
		{"pushing a branch", "git push -u origin 473-feat-a-merge-is-refused"},
		{"pushing with no arguments at all", "git push"},
		{"pushing a branch whose name holds the word main", "git push -u origin fix-the-main-menu"},
		{"pushing a branch named after a merge", "git push origin 12-fix-the-merge-window"},
		{"opening a pull request", `gh pr create --title "473: feat: a merge is refused" --body "What. Why."`},
		{"reading one", "gh pr view 12"},
		{"listing them", "gh pr list --state open"},
		{"watching its checks", "gh pr checks 12"},
		{"asking whether it is merged", "gh api repos/o/r/pulls/12/merge"},
		{"asking with the method spelled out", "gh api -X GET repos/o/r/pulls/12/merge"},
		{"editing the pull request itself", "gh api --method PATCH repos/o/r/pulls/12 -f title=x"},
		{"asking for a review", "gh api -X POST repos/o/r/pulls/12/requested_reviewers -f reviewers[]=x"},
		{"bringing the branch up to date, which merges into the branch", "gh api -X POST repos/o/r/merge-upstream -f branch=work"},
		{"another service whose path happens to end the same way", "curl -X PUT https://example.com/pulls/merge"},
		{"moving onto the default branch", "git checkout main"},
		{"switching to it", "git switch main"},
		{"deleting a local branch called main", "git branch -D main"},
		{"reading its log", "git log origin/main --oneline"},
		{"reading a pull request over the interface", "gh api repos/o/r/pulls/12"},
		{"a commit message holding a separator and the command", `git commit -m "done; gh pr merge 12 is the operator's"`},
		{"a comment telling the operator the exact command", `gh pr comment 12 --body "when you are happy: gh pr merge 12 --squash"`},
		{"a commit message that talks about merging", `git commit -m "fix: gh pr merge is refused now"`},
		{"a commit message mentioning main", `git commit -m "chore: rebase onto main"`},
		{"merging main into the branch, which applies nothing anywhere", "git merge origin/main"},
		{"telling somebody how to merge it", `gh pr comment 12 --body "ready to merge, over to you"`},
		{"an echo about it", `echo "the merge is the operator's: gh pr merge 12"`},
		{"reading the file this hook lives in", "cat hooks/merge-gate/gate.go"},
		{"a grep for the word", `grep -rn "gh pr merge" docs/`},
		{"nothing at all", ""},
		{"a branch called main-menu on the far side of a refspec", "git push origin HEAD:main-menu"},
	}
	for _, one := range allowed {
		t.Run(one.name, func(t *testing.T) {
			if refusal, stopped := Decide(one.command); stopped {
				t.Fatalf("%q was refused, and it is what every role does on every slice:\n%s",
					one.command, refusal)
			}
		})
	}
}

// A line built to nest forever must not hold the session's tool call open until the runtime's
// timeout, which would be a gate that stops the work by hanging rather than by refusing.
func TestTheGateStopsReadingBeforeItRunsOutOfStack(t *testing.T) {
	command := strings.Repeat(`bash -c "`, 200) + "gh pr merge 12" + strings.Repeat(`"`, 200)
	Decide(command)
}
