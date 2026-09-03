Feature: A job counts the steers it took, so the next one can be compared with it

  A steer is one moment the operator had to say something the system should have known, asked for, or
  refused on its own. It is the whole score of an acceptance job, and until now a person counted them
  by hand: thirteen across two days, written out afterwards into a markdown file and numbered from
  memory. Nothing in the system knew any of them happened, so every improvement after it was argued
  rather than measured.

  A steer looks exactly like an ordinary message, because that is what it is. Only the person typing
  it knows it is one, so the mark is theirs to make, and it has to be one word: a mark that takes a
  form to fill in does not get made in the moment, with a hand already halfway through the next
  sentence.

  The count belongs to the job at the top. A steer made against a child three levels down still moves
  the number, because what is being scored is the whole tree and not whichever session happened to be
  working at the time.

  Setup before the job was declared is not a steer, and an answer to a question the system asked is
  not a steer. That definition ships with the tool, printed where the marks are read back, because a
  number counted two ways compares with nothing.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And the system listens on an address the tool can dial

  Scenario: One word marks a moment, and the job carries the count
    Given a job in flight titled "build the transcripts page"
    When the operator steers the job in flight with "the workspace has no secrets"
    Then the command succeeds
    And reading that job back through the tool says "1 steer"

  # A steer belongs to the job it landed on. The job whose session declared that one is a job in its
  # own right, so its count does not move.
  Scenario: A steer against a job a session declared counts on that job alone
    Given a job in flight titled "build the transcripts page"
    And the session running it declared a job of its own
    When the operator steers that job with "it chose a store that bills while idle"
    Then reading that job back through the tool says "0 steers"

  Scenario: The report says what was said, when, and which job it landed on
    Given a job in flight titled "build the transcripts page"
    And the operator steered the job in flight with "the workspace has no secrets"
    And the operator steered the job in flight with "it chose a store that bills while idle"
    When the operator reads the steers of that job back
    Then the report says "the workspace has no secrets" before "it chose a store that bills while idle"
    And the report says "2 steers"
    And the report names the job each steer landed on
    And the report says what a steer is

  Scenario: Two jobs are compared by count
    Given a job titled "the job before" that took 3 steers
    And a job titled "the job after" that took 1 steer
    When the operator reads the steers of this project back
    Then the report says "2 fewer than the job before it"

  # A steer counted against the wrong tree is worse than one nobody recorded, because the number then
  # reads as measured.
  Scenario: With two jobs in flight the mark refuses rather than guessing
    Given a job in flight titled "build the transcripts page"
    And a job in flight titled "write the migration"
    When the operator steers whatever is in flight with "the workspace has no secrets"
    Then the command fails
    And standard error says to name the job

  Scenario: A session cannot record a steer against the job it is running
    Given a job in flight titled "build the transcripts page"
    When the session running it tries to record a steer
    Then the system refuses it, saying what a session may call
