Feature: A brief is held against the request that produced it

  A person says one sentence. Somebody, or something, writes a brief from it. The system then runs
  that brief, faithfully and fast. Nothing compares the brief with the sentence, and the person who
  said the sentence never reads the brief. So a misreading of one sentence becomes two days of
  correct work in the wrong direction, and it looks like progress the whole way, because every check
  is green and every job did what it was told.

  Measured twice in one week. A request for an article about what had been built became a brief for
  a diary of throughput. A request to paste a link and get the text became a design whose address
  takes a video identifier, and the product was built from that design over two days.

  So a job keeps the request in the words it was asked in, and the system reads the brief against
  it at the moment of the write. It refuses nothing: a false alarm would stop work that was right,
  and the person who said the request is often not the person at the terminal. It says which words
  of the request the brief never says, once, while somebody is looking, and it tells the session the
  same thing above its brief.

  The silence is the feature. A line printed on every job is a line nobody reads, and a person asked
  to approve every brief is the cost this system exists to remove.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # First, because a check that speaks about every brief is the same as no check at all.
  Scenario: A brief that says what the request says is declared in silence
    When the caller declares a job asked for as "paste a youtube link and get the text back" with a brief that serves it
    Then the job is declared
    And the system says nothing about the brief drifting

  Scenario: A brief that drops what was asked for is declared and says so
    When the caller declares a job asked for as "paste a youtube link and get the text back" with a brief about video identifiers
    Then the job is declared
    And the system names the words the brief never says

  Scenario: The request is on the job, in the words it was asked in
    When the caller declares a job asked for as "paste a youtube link and get the text back" with a brief that serves it
    Then the job carries the request "paste a youtube link and get the text back"

  # The half that works with nobody watching. The session reads what was asked for, unrewritten,
  # above the brief somebody wrote from it.
  Scenario: The session doing the job is given the request above its brief
    When the caller declares a job asked for as "paste a youtube link and get the text back" with a brief about video identifiers
    Then the session doing that job is told what was asked for
    And the session doing that job is told which words its brief never says

  Scenario: A job that states no request is declared exactly as before
    Given a job titled "read the electricity bill"
    Then the job is declared
    And the system says nothing about the brief drifting

  # What the person at the terminal actually reads. The tool is run in its own process, because a
  # line the call carries and a line the tool prints are two different claims.
  Scenario: The tool names the dropped words where the brief drifted
    Given the system listens on an address the tool can dial
    When the caller declares a drifting job through the tool
    Then the tool exits successfully
    And standard output names the words the brief never says

  Scenario: The tool says nothing about drift where the brief is faithful
    Given the system listens on an address the tool can dial
    When the caller declares a faithful job through the tool
    Then the tool exits successfully
    And standard output says nothing about the brief drifting

  Scenario: Reading the job back says what was asked for and what the brief dropped
    Given the system listens on an address the tool can dial
    When the caller declares a drifting job through the tool
    And the caller reads that job through the tool
    Then standard output carries the request as it was asked
    And standard output names the words the brief never says
