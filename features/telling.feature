Feature: A job that stops for a person tells them

  On 1 September 2026 four jobs put a plan up for approval and stopped. Nothing told anybody. The
  oldest waited more than one hour, and the person they were waiting for found out because he asked
  what the state was.

  Everything that could answer "what waits on me" waited to be opened. The briefing is a page. The
  job listing is a command. The console draws to whoever is looking at it. The transition wrote
  job.asked to the event log and nothing read it.

  So the system answers it in one read, and the surfaces a person already has open say it: the
  console rings once and draws a line when the count goes up, and any command prints it above its
  own output. Past a limit the telling names how long the job has waited, because a job that stopped
  a second ago and one that stopped an hour ago are not the same thing.

  Nothing leaves this machine. No phone, no chat, no mail: a device off this machine needs a
  credential the system does not have, and that is a different piece of work.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And a job titled "choose where the transcripts are stored" whose session is still working

  Scenario: A job that enters asking tells the person, without anybody typing a command
    Given the operator is looking at the console
    When the session running that job asks its question
    And the console draws again on its own
    Then the console rang the bell once
    And the console says the job is waiting, and what it asks

  Scenario: Nothing waiting rings nothing and draws nothing
    Given the operator is looking at the console
    When the console draws again on its own
    Then the console rang the bell no times
    And the console says nothing about anything waiting

  Scenario: A wait past the limit says how long it has been
    Given a workspace where a wait lasts 1 second
    And the session running that job asked its question
    When the wait passes that limit
    Then the telling names how long the job has waited

  Scenario: The next command an operator types says it first
    Given the session running that job asked its question
    When the operator runs any command
    Then the command says the job is waiting above its own output

  Scenario: A sealed value in a question is not in the telling
    Given the workspace seals a token
    And the session running that job asked about that token
    Then the telling says what was asked and not the token

  Scenario: The moment the telling went out is on the record
    Given the session running that job asked its question
    When two surfaces draw the same waiting job
    Then the record holds one telling, naming the surface that carried it
    And krewe job show prints the gap between the question and the telling

  Scenario: A wait that follows an answered question is not dated from that question
    Given the session running that job asked its question
    And a person answered it and the job ran on
    When that job fails and a surface names it
    Then krewe job show dates the wait from the failure, not from the answered question

  # A cap that refused a long question becomes a warning, so the record carries a question of any
  # length. The line an operator reads is still one line, so it cuts what it draws there and says
  # where the whole question is.
  Scenario: A question too long for one line is cut where it is drawn, and whole in the record
    Given the session running that job asked a question longer than a terminal line
    When the operator runs any command
    Then the telling fits a narrow terminal, and says where the whole question is
    And krewe job show prints the whole question
