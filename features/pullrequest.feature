Feature: The system reads back the pull request it opened

  A job that names a repository ends in a pull request, and the address lands on the row. That was
  the whole of what the system knew about the work it opened. Nothing read the address again, so a
  change that merged and a change whose checks went red an hour later read exactly the same:
  produced. One of the three questions the operator asks could not be answered at all, because "a
  check is red" was not a state anything held.

  So the system reads the pull request back, on a timer of its own, and keeps four things on the job:
  whether it is open, merged or closed, what its checks say, whether a review asked for changes, and
  when it was read.

  Two rules decide whether any of this is worth having.

  A reading nobody took reads as unknown, never as green. An operator picks up what is stuck, so a
  pull request that reads as fine because nothing could read it is the one they will not look at.
  The machine reading already holds this rule: it writes the word rather than a zero.

  And no page calls a forge while it draws. Every command reads the row, so a forge that is slow
  slows the reading and never the operator.

  The credential is the system's own secret rather than a workspace's, because one process does this
  reading. It is set once, as GH_TOKEN at the system level, and every workspace's pull requests are
  read with it.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # The one that decides whether the rest is worth anything. A gate that always passes satisfies every
  # test about passing.
  Scenario: A pull request that could not be read says unknown, and never says it passed
    Given a job that opened the pull request "https://github.com/atlantic-blue/quay-crew/pull/454"
    And the forge will not answer about that pull request, saying "the rate limit is spent"
    When the system reads the pull requests it opened
    Then the job says nothing is known about its pull request
    And the job says why it could not be read, naming "rate limit"

  # A pull request the timer has not reached yet is the same answer, and it is the state every job is
  # in for the first two minutes of its life.
  Scenario: A pull request nothing has read yet says so in words
    Given a job that opened the pull request "https://github.com/atlantic-blue/quay-crew/pull/454"
    Then the job says nothing is known about its pull request
    And the job carries no moment of a reading

  # The state a newly installed system is in. The refusal names the command, because an operator
  # reading unknown on every pull request has no other way to find out why.
  Scenario: A system with no forge credential says so, and names the command that sets one
    Given this system holds no forge credential
    And a job that opened the pull request "https://github.com/atlantic-blue/quay-crew/pull/454"
    When the system reads the pull requests it opened
    Then the job says nothing is known about its pull request
    And the job says why it could not be read, naming "krewe secret set system GH_TOKEN"

  Scenario: A job whose pull request merged says merged
    Given a job that opened the pull request "https://github.com/atlantic-blue/quay-crew/pull/454"
    And the forge says that pull request merged with its checks green
    When the system reads the pull requests it opened
    Then the job says its pull request merged
    And the job carries the moment it was read

  # The question that could not be asked at all before this.
  Scenario: A job whose checks are red says red, and names the check that failed
    Given a job that opened the pull request "https://github.com/atlantic-blue/quay-crew/pull/454"
    And the forge says that pull request is open and the check "integration" failed
    When the system reads the pull requests it opened
    Then the job says a check is red, naming "integration"

  Scenario: A review that asked for changes is on the record
    Given a job that opened the pull request "https://github.com/atlantic-blue/quay-crew/pull/454"
    And the forge says a review of that pull request asked for changes
    When the system reads the pull requests it opened
    Then the job says a review asked for changes

  # A merged or closed pull request is read once more and then left alone. Without this the system
  # pays for every pull request it has ever opened, every two minutes, for ever.
  Scenario: A settled pull request is not read again
    Given a job that opened the pull request "https://github.com/atlantic-blue/quay-crew/pull/454"
    And the forge says that pull request merged with its checks green
    When the system reads the pull requests it opened
    And the system reads the pull requests it opened again
    Then the forge was asked about that pull request once

  # A page reads the row. A command that asked a forge while it drew would wait as long as the forge
  # takes, which is the rule the machine reading and the health reading already hold.
  Scenario: Reading a job never calls a forge
    Given a job that opened the pull request "https://github.com/atlantic-blue/quay-crew/pull/454"
    And the forge says that pull request is open with its checks green
    When the system reads the pull requests it opened
    And the caller reads the job three times
    Then the forge was asked about that pull request once

  # Work that never goes near a forge pays for none of this.
  Scenario: A job that opened no pull request is never read
    Given a job titled "read the electricity bill"
    When the system reads the pull requests it opened
    Then the forge was asked about nothing
