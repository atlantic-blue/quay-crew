Feature: A parallel change does not collide in the changelog

  Six pull requests were open one evening. Two merged, and the other four went dirty within seconds.
  Every conflict was `CHANGELOG.md` and nothing else: every other file merged on its own, including
  the ones worth a human reading. The cause was the shape of the file rather than any of the changes.
  Each one wrote its entry at the top of one shared file, so any two changes written at once collided
  there by construction, and the answer was always the same and always mechanical. The system paid a
  sandbox each to work it out again.

  An entry now goes in its own file under `changelog.d/`, named after the issue it closes. Two
  changes write two different files and never touch each other, so there is nothing to resolve. A
  release assembles every fragment into one dated section, newest first, and the fragments go away in
  the same commit.

  A branch cut before this existed still writes the shared file, so the repository also tells git to
  keep both sides of a conflict there. That reaches the merge git performs in a checkout, which is
  where those four sandboxes went, and it does not reach the merge GitHub performs on the button.
  The fragments are what fixes the button.

  These scenarios run a real git merge in a repository of their own, and the real command a release
  runs. `make changelog` is a one line alias for that command.

  Scenario: Two changes written at once do not touch the same file
    Given two changes written at the same time
    When each one writes its own changelog fragment
    Then the branches merge with nothing to resolve
    And both changes are in the tree

  Scenario: The one file they used to share is where they collided
    Given two changes written at the same time, on a repository that does not keep both sides
    When each one writes its entry at the top of the changelog
    Then the merge stops on a conflict in "CHANGELOG.md"

  Scenario: A branch cut before the convention still merges
    Given two changes written at the same time
    When each one writes its entry at the top of the changelog
    Then the branches merge with nothing to resolve
    And both changes are in the tree

  Scenario: A release is every fragment in one dated section, newest first
    Given a change waiting for a release in "455-a-console-view-of-jobs.md" saying "**A console view of jobs.** What each job is doing."
    And a change waiting for a release in "480-changelog-fragments.md" saying "**One file per change.** So two changes never collide."
    When the release is assembled
    Then it prints a section dated today, holding these lines in this order:
      | - **One file per change.** So two changes never collide.  |
      | - **A console view of jobs.** What each job is doing.     |

  Scenario: A fragment nobody can trace back to an issue is refused
    Given a change waiting for a release in "changelog-fragments.md" saying "**One file per change.** So two changes never collide."
    When the release is assembled
    Then it refuses, naming "changelog-fragments.md"

  Scenario: Nothing to assemble is not a release
    Given nothing is waiting for a release
    When the release is assembled
    Then it refuses, saying there is nothing to assemble
