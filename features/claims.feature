Feature: A job claims the piece of work it is doing

  Two sessions picked up the same issue and built it under different names. Nobody knew until two
  pull requests conflicted on files both of them had created, and the two designs disagreed in small
  places, which is the part that cost the most: putting them back together by hand was more work
  than either one would have been alone.

  Neither session was in the other's way in the filesystem, because each one had its own working
  copy. They were in each other's way over the work itself, and no record anywhere said who was
  doing what.

  So a job says which piece of work it takes: an issue, a branch, or a name two people would both
  use for the same thing. A second job that claims the same one is refused, and the refusal names
  the job holding it and how old that claim is. This is not a lock on a file. It is a record of
  intent, and both sessions would have read it before they started.

  A claim ends three ways: the job finishes, somebody stops it, or nothing moves the job for longer
  than a claim lives. The third one is the one that matters. A claim that never runs out passes
  every test about claiming and then stops all work on that issue the first time a container dies.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A second job claiming work another job holds is refused
    Given a job claiming "atlantic-blue/quay-krewe#540"
    When the caller declares a second job claiming "atlantic-blue/quay-krewe#540"
    Then the system refuses it and names the job holding the work
    And the refusal says how old the claim is
    And the system holds one job on that piece of work

  # Two people naming the same piece of work from memory write it two ways. A claim that misses over
  # a capital letter is a claim that did nothing at all.
  Scenario: The same piece of work written another way is the same claim
    Given a job claiming "atlantic-blue/quay-krewe#540"
    When the caller declares a second job claiming "  Atlantic-Blue/Quay-Krewe#540 "
    Then the system refuses it and names the job holding the work

  Scenario: A job claiming different work is declared
    Given a job claiming "atlantic-blue/quay-krewe#540"
    When the caller declares a second job claiming "atlantic-blue/quay-krewe#541"
    Then the job is declared
    And the system holds one job on that piece of work

  # The expiry. Without it one dead container holds a piece of work for as long as the system runs.
  Scenario: Work nothing has moved for longer than a claim lives is claimed again
    Given a job that claimed "atlantic-blue/quay-krewe#540" and then stopped moving
    When the caller declares a second job claiming "atlantic-blue/quay-krewe#540"
    Then the job is declared

  Scenario: Work a stopped job claimed is claimed again
    Given a job claiming "atlantic-blue/quay-krewe#540"
    When the caller stops the first job saying "the issue was closed"
    And the caller declares a second job claiming "atlantic-blue/quay-krewe#540"
    Then the job is declared

  # Every job written before this existed claims nothing, and most jobs after it will too.
  Scenario: Jobs that claim nothing never block one another
    Given a job titled "read the electricity bill"
    When the caller declares a job titled "pay the electricity bill"
    Then the job is declared

  Scenario: A claim longer than a title is refused
    When the caller declares a job with a claim of 201 bytes
    Then the system refuses it and says the ceiling is 200
    And no job was written

  # The listing is where somebody looks before starting. A claim nobody can see is a record of
  # intent nobody reads.
  Scenario: The listing says what is claimed and by which job
    Given the system listens on an address the tool can dial
    And a job claiming "atlantic-blue/quay-krewe#540"
    When the caller lists the jobs through the tool
    Then standard output says "atlantic-blue/quay-krewe#540"
    And standard output names the job holding the work
    And the command succeeds

  # The tool in its own process, because the exit status and which stream carried the sentence do not
  # exist inside the test process. A refusal that exits zero reads as a command that worked.
  Scenario: The tool refuses the second declaration, names the holder, and fails
    Given the system listens on an address the tool can dial
    And a job claiming "atlantic-blue/quay-krewe#540"
    When the caller declares a job claiming "atlantic-blue/quay-krewe#540" through the tool
    Then standard error says "atlantic-blue/quay-krewe#540"
    And standard error names the job holding the work
    And standard output is empty
    And the command fails
    And the system holds one job on that piece of work
