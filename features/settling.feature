Feature: A job that names a repository does not settle on its own answer

  Every failure of the acceptance run reached the operator through one door. A session finished its
  work, it wrote an answer, and the job settled on that answer. Where the work was wrong the answer
  said it was right, and it said so in good faith: the session had no way to know otherwise. The
  answer was the only evidence, and it was written by the session being judged.

  So two sessions that did not do the work read it first. A reviewer reads the change against what
  the job was asked for. A tester runs the repository's own gates and reads their output rather than
  their exit status, because a suite that ran nothing exits zero. Each answers with one line the
  system reads, the way the address of a pull request is read: off the answer, never reported.

  Neither of them holds a credential. What a session may call on the system comes from the job it
  runs, and these run no job, so they may call nothing at all.

  A fail is not the end of the run. It goes back to the session that did the work as its next task,
  carrying the reason, and the job stays open, because the branch and the worktree are still there.
  It goes back once: every ask is a task somebody pays for, so a second fail ends the job with the
  reason on the row.

  The gate is refusable rather than optional. A job may be declared with it off, and the record says
  so, so a settled job always states whether anything independent passed it.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # The failure this exists to answer, at the smallest scale it can be seen. The session says the work
  # is done, and the job does not take its word for it.
  Scenario: The work answers, and the job does not settle on it
    Given a job titled "sort the listing" in the repository "atlantic-blue/quay-crew"
    And the model will answer "I made the change and opened https://github.com/atlantic-blue/quay-crew/pull/454"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is running
    And the reviewer was asked to read "https://github.com/atlantic-blue/quay-crew/pull/454"
    And the job says nothing passed it

  # The reviewer fails it, and the fail is the next task rather than the end of the run. The branch is
  # still in the session and the fix is usually one edit.
  Scenario: A reviewer that fails the work sends it back, and the job stays open
    Given a job titled "sort the listing" in the repository "atlantic-blue/quay-crew"
    And the session doing the work answers "I made the change and opened https://github.com/atlantic-blue/quay-crew/pull/454"
    And the reviewer answers "Verdict: fail the change adds a column and no migration"
    When the crew runs until the work comes back
    Then the job is running
    And the work went back to the session that did it, saying "a column and no migration"
    And the tester was never asked

  # Asked once and no more. A session that cannot fix what the reviewer found would otherwise be sent
  # round forever, and every round is two containers somebody pays for.
  Scenario: Work a reviewer fails twice ends the job, with what it said on the row
    Given a job titled "sort the listing" in the repository "atlantic-blue/quay-crew"
    And the session doing the work answers "I could not fix it. Still https://github.com/atlantic-blue/quay-crew/pull/454"
    And the reviewer answers "Verdict: fail the migration is missing"
    When the crew runs until the job stops
    Then the job is stopped, and the reason says the reviewer failed it twice
    And the job still names the pull request "https://github.com/atlantic-blue/quay-crew/pull/454"
    And the job says nothing passed it

  # The case a gate that fell through to a pass would let settle. An answer with no verdict has judged
  # nothing, and a job that settles on it says it was checked by somebody who checked nothing.
  Scenario: A gate that answers without a verdict stops the job rather than passing it
    Given a job titled "sort the listing" in the repository "atlantic-blue/quay-crew"
    And the session doing the work answers "I made the change and opened https://github.com/atlantic-blue/quay-crew/pull/454"
    And the reviewer answers "I read the change and it seems reasonable enough to me."
    When the crew runs until the job stops
    Then the job is stopped, and the reason says the reviewer gave no verdict

  # The change reads correctly and its suite is red, which is the half a gate stopping at the reviewer
  # would ship.
  Scenario: A tester that fails the work sends it back too
    Given a job titled "sort the listing" in the repository "atlantic-blue/quay-crew"
    And the session doing the work answers "I made the change and opened https://github.com/atlantic-blue/quay-crew/pull/454"
    And the reviewer answers "Verdict: pass it does what the brief asked"
    And the tester answers "Verdict: fail 3 of 540 test files are red"
    When the crew runs until the work comes back
    Then the job is running
    And the work went back to the session that did it, saying "3 of 540 test files are red"

  Scenario: Both gates pass, and the job settles saying which of them did
    Given a job titled "sort the listing" in the repository "atlantic-blue/quay-crew"
    And the session doing the work answers "I made the change and opened https://github.com/atlantic-blue/quay-crew/pull/454"
    And the reviewer answers "Verdict: pass it does what the brief asked"
    And the tester answers "Verdict: pass 540 test files ran, 6034 tests, all green"
    When the crew runs until the job is done
    Then the job is done, and it names the pull request "https://github.com/atlantic-blue/quay-crew/pull/454"
    And the job says the reviewer and the tester passed it
    And the answer on the row is still what the session that did the work said

  # A second opinion from the session that formed the first is not a second opinion. Each gate has a
  # conversation of its own, and a container of its own.
  Scenario: Each gate reads the work in a session that did not do it
    Given a system that sessions can reach at "controlplane:50051"
    And a job titled "sort the listing" in the repository "atlantic-blue/quay-crew"
    And the session doing the work answers "I made the change and opened https://github.com/atlantic-blue/quay-crew/pull/454"
    And the reviewer answers "Verdict: pass it does what the brief asked"
    And the tester answers "Verdict: pass 540 test files ran, all green"
    When the crew runs until the job is done
    Then the reviewer and the tester each read it in a session of their own
    And neither of them was given a credential, and the session doing the work was

  # Refusable, not optional. A run with no reviewer to spare says so once, where somebody is looking.
  Scenario: A job declared with the gate off settles on its own answer
    Given a job titled "sort the listing" in the repository "atlantic-blue/quay-crew" with the gate off
    And the model will answer "I made the change and opened https://github.com/atlantic-blue/quay-crew/pull/454"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is done, and it names the pull request "https://github.com/atlantic-blue/quay-crew/pull/454"
    And the job says it was declared with the gate off
    And the system was asked to run 1 task

  # A job with no repository produced no change, so there is nothing for a reviewer to read and nothing
  # to hold back. The gate reaches the jobs that produce work and no others.
  Scenario: A job that names no repository reaches no gate
    Given a job titled "read the electricity bill"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is done, and its answer is what the model said
    And the system was asked to run 1 task

  # What a person reads. A job that two sessions passed and a job that nothing passed must never read
  # the same, which is the whole reason the record is worth keeping.
  Scenario: krewe job show says what passed a settled job
    Given the system listens on an address the tool can dial
    And a job titled "sort the listing" in the repository "atlantic-blue/quay-crew"
    And the session doing the work answers "I made the change and opened https://github.com/atlantic-blue/quay-crew/pull/454"
    And the reviewer answers "Verdict: pass it does what the brief asked"
    And the tester answers "Verdict: pass 540 test files ran, all green"
    When the crew runs until the job is done
    Then reading that job says it was passed by the reviewer and the tester
