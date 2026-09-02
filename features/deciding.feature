Feature: A session that stops for a person says so on the record

  Four jobs stopped for a person on the afternoon of 1 September 2026. Every conversation held a
  question. The record read running for all four, and no listing, no line and no bell said anything,
  so the only way to find out was to open each conversation and look. Meanwhile nothing moved.

  A session that ends its turn and has nothing left to do is not a session doing work, and the two
  must never read the same. So the word a session ends on is read. Three of the four say what became
  of the work. The fourth says the work stopped with a person, and that one puts the job where every
  reader of what waits on you already looks: asking, with what the session wrote as the question.

  It is read off the answer rather than called for, the way the pull request address and the outcome
  already are. A session can still ask on its own, and this is the road for the session that did not.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A session that stops for a person leaves the record saying so
    Given a job whose session answers that a person has to decide
    When the controller reads what that session answered
    Then the job is waiting on a person, carrying what the session wrote
    And the record for that job says a question was put

  Scenario: What waits on a person is one read, with nobody opening a conversation
    Given a job whose session answers that a person has to decide
    When the controller reads what that session answered
    Then one job reads as waiting on a person

  Scenario: A session in the middle of its work waits on nobody
    Given a job whose session is still working on it
    When the controller ticks 3 times
    Then no job reads as waiting on a person
    And that job is still running

  Scenario: A session that finished its work waits on nobody
    Given a job whose session answers that the work is done
    When the controller reads what that session answered
    Then no job reads as waiting on a person

  # The question is the session's own words. A record that kept the system's signal line in it would
  # hand a person a machine word to answer.
  Scenario: The question a person reads is what the session wrote, without the system's own line
    Given a job whose session answers that a person has to decide
    When the controller reads what that session answered
    Then the question on the row does not carry the outcome line
