Feature: A listing says which address it read

  The operator ran `quay job list`, read one row, and asked where the other nine jobs were. They
  were in the project next door. The listing was correct and it was misleading, because nothing on
  the screen said a scope had been applied: a narrowed result and an empty crew look identical.

  So every listing here names what it read, the way k9s keeps the namespace in its header. A bare
  listing still reads where you are standing, because that is what `quay use` is for and every other
  verb obeys it. What changes is that it says so, and it says the word that widens it. The word is
  `crew`, which already means the level above every workspace elsewhere in this tool, and a listing
  that reads every project carries the address of each row.

  These scenarios run the command line tool as a caller runs it, in its own process, because what is
  specified is the sentence on the operator's screen.

  Background:
    Given a running control plane
    And a workspace named "atlantic-blue"
    And a project named "quay-crew"
    And the crew listens on an address the tool can dial

  Scenario: A listing narrowed to where the operator stands says where it looked
    Given a job titled "the observability slice"
    And a second project named "transcript" holding a job titled "a page that turns a video into text"
    When the operator moves to "atlantic-blue/transcript" and lists the jobs through the tool
    Then standard output says "1 job in atlantic-blue/transcript"
    And standard output does not carry "the observability slice"

  Scenario: The narrowed listing offers a word that widens it
    Given a job titled "the observability slice"
    And a second project named "transcript" holding a job titled "a page that turns a video into text"
    When the operator moves to "atlantic-blue/transcript" and lists the jobs through the tool
    Then standard output says "quay job list crew"

  Scenario: That word reads every project, and each row carries its address
    Given a job titled "the observability slice"
    And a second project named "transcript" holding a job titled "a page that turns a video into text"
    When the operator lists the jobs of the whole crew through the tool
    Then standard output says "2 jobs in this crew"
    And standard output says "atlantic-blue/quay-crew"
    And standard output says "atlantic-blue/transcript"

  Scenario: An empty listing says where it looked, and what would widen it
    Given a job titled "the observability slice"
    And a second project named "transcript" holding no jobs
    When the operator moves to "atlantic-blue/transcript" and lists the jobs through the tool
    Then standard output says "no jobs in atlantic-blue/transcript"
    And standard output says "quay job list crew"

  Scenario: A session listing says the same thing, in the same words
    Given a second project named "transcript" holding no jobs
    When the operator moves to "atlantic-blue/transcript" and lists the sessions through the tool
    Then standard output says "no sessions in atlantic-blue/transcript"
    And standard output says "quay sessions crew"
