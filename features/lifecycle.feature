Feature: A session gives its container back and keeps its history

  The system never put a session away on its own. Nothing removed a container without somebody asking,
  so a session that answered one question in March still held its container in August.

  This is the loop that does. It is the same controller that runs jobs, reading a fourth query: the
  sessions nothing is holding open. One of those, idle for longer than its workspace allows, gives its
  container back and reads "reclaimed". Everything else it has stays, so the next task builds a fresh
  container over the same conversation and carries on.

  Reclaimed is not stopped, deliberately. A stop is somebody's decision and a reader goes looking for
  who made it. A reclaim is the system saving memory on a session nobody is using, and a reader looks
  for nothing, because the next dispatch fixes it.

  Both times ship unset, and unset means the controller does nothing at all. Three measurements decide
  the numbers and none has been taken, so this system refuses a number it was never given.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # The rule this slice ships on. It is first because it is the one that must never break.
  Scenario: With both times unset the controller takes nothing back and files nothing away
    Given a session started by dispatching "hello"
    When the controller ticks 20 times
    Then the session is reported as idle
    And the system still holds its container

  Scenario: A workspace nobody configured says both times are unset
    Then the workspace's reclaim time is unset
    And the workspace's archive time is unset

  Scenario: A settled session gives its container back once the workspace names a time
    Given the workspace reclaims a session after 1 second
    And a session started by dispatching "hello"
    When the reclaim time passes
    And the controller ticks
    Then the session is reported as reclaimed
    And its container is gone

  # The promise the word "reclaimed" makes: nothing was lost.
  Scenario: A task sent to a reclaimed session builds a fresh container and answers
    Given the workspace reclaims a session after 1 second
    And a session started by dispatching "hello"
    When the reclaim time passes
    And the controller ticks
    And the operator dispatches "are you still there" to the same session
    Then the session answers
    And the session still holds both tasks, oldest first
    And the session is reported as idle

  # The dangerous one. The system could not tell who was in a container before this slice.
  Scenario: A container an operator is typing into is never taken
    Given the workspace reclaims a session after 1 second
    And a session started by dispatching "hello"
    And an operator has that session's conversation open
    When the reclaim time passes
    And the controller ticks 5 times
    Then the session is reported as idle
    And the system still holds its container

  Scenario: A session a job is still running in is never taken
    Given the workspace reclaims a session after 1 second
    And a job titled "read the electricity bill"
    And the model takes longer over a task than anybody will wait
    When the controller ticks
    And the reclaim time passes
    And the controller ticks again
    Then no session was reclaimed

  Scenario: A session reclaimed for longer than the archive time is filed away
    Given the workspace reclaims a session after 1 second
    And the workspace archives a reclaimed session after 1 second
    And a session started by dispatching "hello"
    When the reclaim time passes
    And the controller ticks
    And the archive time passes
    And the controller ticks again
    Then the session is archived

  Scenario: A reclaim time on its own files nothing away
    Given the workspace reclaims a session after 1 second
    And a session started by dispatching "hello"
    When the reclaim time passes
    And the controller ticks 10 times
    Then the session is reported as reclaimed
    And the session is not archived

  # Issue 395. There was no way to stop one session: krewe drain puts the whole system down, and killing
  # the dispatch client is not an interface. The same kill ended one task at once and left another
  # working for sixteen more minutes, merging two pull requests after the operator believed it had
  # stopped.
  Scenario: An operator stops the task one session is running, and the session survives
    Given the model takes longer over a task than anybody will wait
    And a task dispatched without waiting for it
    And a task is under way
    When the operator stops the session saying "it is doing the wrong thing"
    Then the task reads stopped, with that reason
    And the session is reported as idle
    And the system still holds its container

  Scenario: A stopped session continues on the next dispatch
    Given the model takes longer over a task than anybody will wait
    And a task dispatched without waiting for it
    And a task is under way
    When the operator stops the session saying "wrong branch"
    And the model answers again
    And the operator dispatches "try that again" to the same session
    Then the session answers
    And the session still holds both tasks, oldest first

  Scenario: Stopping a session with nothing running says so and changes nothing
    Given a session started by dispatching "hello"
    When the operator stops the session saying "just in case"
    Then the system says there was nothing to stop
    And the session still holds 1 task

  # What happens to the job that was running in it. A stop is not a crash, so the job is stopped
  # with the operator's reason rather than failed with whatever the runtime said about being killed.
  Scenario: A job running in a session an operator stopped is stopped, not failed
    Given a job titled "read the electricity bill"
    And the model takes longer over a task than anybody will wait
    When the controller ticks
    And the operator stops the session the job is running in saying "the bill is not due yet"
    And the controller ticks again
    Then the job reads stopped, saying "the bill is not due yet"
    And the job carries no answer
