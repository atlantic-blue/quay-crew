Feature: A job can put a question to a person

  A session doing a job sometimes reaches a decision no measurement settles. Which store, which
  region, what a page shows when the captions are in the wrong language. Until now it had two moves
  and both were bad: guess, and the operator finds out when the answer lands, or stop, and the whole
  run ends over one sentence.

  So a job can ask. The question goes on the row, the job stops there, and nothing moves it until a
  person answers. The answer is sent back into the same conversation as the session's next task, so
  the session carries on from where it stopped rather than starting the job again.

  This is what the acceptance run of 29 August 2026 needed and could not do. A session read a project
  context asking for nothing that bills while idle, agreed with it, and chose a store that bills a
  minimum capacity continuously. The operator found out by asking a question of their own. The choice
  was invisible until it was built, and a question at the top of the work is what makes it visible.

  Asking needs no verb, because the alternative to asking is guessing and no role should leave a
  session with only that. Answering is the operator's, because a run that answers its own question is
  a gate that decorates rather than holds.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And a job titled "choose where the transcripts are stored" whose session is still working

  Scenario: A question stops the job, and the question is on the row
    When the session running that job asks "aurora serverless version two bills a minimum capacity continuously. a key value store on demand bills nothing at rest. Which?"
    Then the job is asking, and the row carries the question it put

  Scenario: Nothing moves an asking job but an answer
    Given the session running that job asked its question
    When the task the controller sent lands
    And the controller ticks 3 times
    Then the job is still asking
    And the crew was asked to run 1 task

  Scenario: The answer arrives as the session's next task
    Given the session running that job asked its question
    And the task the controller sent lands
    When the operator answers the job with "the key value store, on demand, because nothing bills while nobody uses it"
    And the controller ticks again
    Then the crew was asked to run 2 tasks
    And the second task carries the answer and the question it answers
    And the second task does not send the brief again
    And the job is running again

  Scenario: The record says a question was put and what was decided
    Given the session running that job asked its question
    When the operator answers the job with "the key value store, on demand"
    Then the records for that job read "job.declared", "job.claimed", "job.started", "job.asked", "job.told"

  # A session that could answer the question a person was asked is a run taking its own word for a
  # decision. The verb exists and no call is mapped to it, so nothing a role grants reaches this.
  Scenario: The session that asked cannot answer itself
    Given the session running that job asked its question
    When that session tries to answer its own question
    Then the crew refuses it, and the job is still asking

  # The identifier is checked against the credential rather than trusted. A caller that could name
  # any job could stop any job.
  Scenario: A session cannot ask about somebody else's job
    Given another job titled "read the electricity bill"
    When the session running the first job asks about the other one
    Then the crew refuses it, naming the job the credential is for
