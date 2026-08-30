Feature: A merge is refused, and the crew is what refuses it

  Every role in this crew pushes a branch and opens a pull request, and no role merges. A push
  applies nothing. A merge runs the pipeline, and the pipeline is what spends money and changes
  infrastructure, so the merge is the operator's gate.

  Until this, that gate was a sentence in a brief. What a role may do is a list of the verbs a
  session calls on the crew, and merging is not one of them: it is a github action a session takes
  with a credential a skill gave it. So the boundary the whole shape rests on was the one thing
  nothing checked, while smaller boundaries were held by a credential.

  The gate is a hook, so it is checked inside the sandbox at the moment the command is about to run.
  These scenarios run the entry point this build ships, which is the same file a sandbox mounts, and
  they feed it what the model runtime feeds it.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # A gate somebody has to remember to attach is off in every crew nobody set up, which is where the
  # boundary matters most.
  Scenario: A fresh crew is under the merge gate without anybody attaching it
    Given a crew seeded with the hooks this build ships
    Then the crew holds the "merge-gate" hook
    And the workspace runs under the "merge-gate" hook

  # An operator who takes it off has said something, and that is the way out. The boundary is a thing
  # the crew holds and a person can remove deliberately, rather than a sentence a model may keep.
  Scenario: An operator who takes the gate off can merge again
    Given a crew seeded with the hooks this build ships
    When the operator takes the hook "merge-gate" off the crew
    Then the workspace runs under no "merge-gate" hook

  Scenario Outline: A session about to merge is refused, and told what to do instead
    When a session is about to run: <command>
    Then the merge gate refuses it
    And the refusal says to open a pull request and leave the merge to the operator

    Examples: the command itself, however it is spelled
      | command                                                      |
      | gh pr merge 12                                               |
      | gh pr merge 12 --squash --delete-branch                      |
      | gh --repo atlantic-blue/quay-crew pr merge 12                |
      | git push -u origin work && gh pr merge 12                    |
      | sudo gh pr merge 12                                          |
      | bash -c "gh pr merge 12"                                     |

    Examples: the same merge asked for another way
      | command                                                      |
      | gh api -X PUT repos/atlantic-blue/quay-crew/pulls/12/merge   |
      | curl -X PUT https://api.github.com/repos/o/r/pulls/12/merge  |

    Examples: landing a commit on the branch a pull request merges into
      | command                                                      |
      | git push origin main                                         |
      | git push origin HEAD:refs/heads/master                       |

  # The other direction, and the one that decides whether this hook is worth having. A hook that
  # refuses wrongly blocks the work, and every role pushes and opens a pull request on every slice,
  # so a wrong refusal here stops the crew delivering anything.
  Scenario Outline: The work every role does on every slice goes through
    When a session is about to run: <command>
    Then the merge gate allows it

    Examples:
      | command                                                      |
      | git push -u origin 473-feat-a-merge-is-refused               |
      | gh pr create --title "473: feat: a gate" --body "What. Why." |
      | gh pr view 12                                                |
      | gh pr checks 12                                              |
      | gh api repos/o/r/pulls/12/merge                              |
      | git commit -m "fix: gh pr merge is refused now"              |
      | git merge origin/main                                        |
      | git checkout main                                            |

  # It fires on every command every session runs, so a payload it cannot read has to go through. A
  # gate that refuses what it does not understand refuses the work, and a broken hook must not be
  # able to stop a crew.
  Scenario: A payload the gate cannot read lets the command run
    When a session sends the merge gate a payload it cannot read
    Then the merge gate allows it
