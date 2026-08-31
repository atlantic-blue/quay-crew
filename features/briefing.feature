Feature: The briefing answers the operator's questions before it says what is running

  Four jobs ran at once and the operator had no visibility. Three surfaces answered one question
  between them, and it was the question he cared about least: `krewe sessions`, the console and
  `krewe web` all listed the sessions that were live. What he produced, what was stuck and what
  waited on him reached him because another person typed it into a terminal for him.

  So the front door of the web view is the briefing. It answers three questions in the order a
  decision needs them: what needs you, what is blocked, what the system produced. What is running comes
  last. The session listing keeps its page and loses the door.

  Jobs are drawn as the tree the orchestration design describes, so a child that asked a question is
  drawn under the work it belongs to rather than as a row with nothing behind it.

  Nothing here changes anything, and the page is still served to this machine and nowhere else.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # The empty case first. A page with nothing blocked and a page that failed to read the system look
  # identical, and one of them is a defect.
  Scenario: A system with nothing on it says so, block by block
    When the operator opens the briefing
    Then the briefing says nothing is waiting on the operator
    And the briefing says nothing is blocked
    And the briefing says the system has produced nothing

  Scenario: A job that failed is blocked, and the row carries its reason
    Given a job titled "read the electricity bill" that the model refused, saying "the model refused"
    When the operator opens the briefing
    Then the briefing carries "read the electricity bill" under "blocked"
    And the briefing carries "the model refused"

  Scenario: A job waiting on a person is the first thing the briefing says
    Given a job titled "choose where the transcripts are stored" whose session is still working
    When the session running that job asks "on demand, or a cluster that bills at rest?"
    And the operator opens the briefing
    Then the briefing carries "on demand, or a cluster that bills at rest?" under "waiting"
    And the briefing carries the command that answers that job
    And "waiting" comes before "running" on the briefing

  # The system keeps the address of a pull request and has never read it back, so the checks are a thing
  # it does not know. A row that said anything else would be inventing it.
  Scenario: A job that landed says where the work is, and says the checks were never read
    Given a job titled "make the listing sort by the clock" that landed a pull request
    When the operator opens the briefing
    Then the briefing carries "https://github.com/atlantic-blue/quay-crew/pull/454" under "produced"
    And the briefing says the checks were not read

  Scenario: The front door is no longer the session listing
    When the operator dispatches "hello" to the project
    And the operator opens the briefing
    Then the briefing lists no sessions
    And the session listing still carries that conversation

  Scenario: The briefing is served to this machine and nowhere else
    When the operator asks for the web view on "0.0.0.0:8080"
    Then the web view refuses, and says a reader would have to be authenticated first
