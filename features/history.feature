Feature: The system says what it has done, so a session does not have to be told

  A session could read the repository it was standing in, and nothing else. It could not read the
  jobs that ran, what they were asked, what came back, which pull requests they opened, or where one
  went wrong. The system held all of it, in the same table it writes every job to, and nothing gave a
  session a way in.

  So the operator was the memory. A job that needed to know what happened got the facts typed into
  its brief by hand, one at a time. One job to write about two days of the system's own work took a
  brief of 1,109 words, and almost every word was a fact the system already held.

  Three things stopped a session answering it alone. A listing cannot narrow by date, so "between two
  dates" could not be asked at all. A listing carries the phase and the title, and no cost, no times
  and no reason, so reading every row still did not answer it. And the cost and the reason are only
  on one job read whole, which returns the answer and every step, so a session that read two days of
  work that way had no context left to do any.

  This is one read instead. It is a command rather than a context level, because a level holds text
  somebody wrote and goes stale the moment the next job runs, and rather than a skill, because a
  skill holds a method and the method here is one sentence. The data was what was missing.

  Two ways it could fail, and both are scenarios below. It must not become a dump, so the window
  bounds it, a limit bounds it further, and it carries no brief and no answer. And it must not lie
  about being bounded, so the total always covers the whole window and the listing says how many jobs
  it did not print.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: The system says what it did, what it cost, and what failed
    Given the system did two days of work
    When a session reads the history from "2026-08-29" to "2026-08-31"
    Then the history says 5 jobs ran
    And the history says 2 of them are done, 1 failed and 1 was stopped
    And the history says the window cost 122044 tokens
    And the history says 2 jobs opened a pull request

  # What failed and why is one question. A reader who has to go and ask a second time for every
  # failure is back where they started.
  Scenario: A job that failed carries the reason with it
    Given the system did two days of work
    When a session reads the history from "2026-08-29" to "2026-08-31"
    Then the failed job says "piped through tail"

  # The window is the first bound, and it is why this read is affordable at all.
  Scenario: A history holds only the window it was asked for
    Given the system did two days of work
    When a session reads the history from "2026-08-30" to "2026-08-31"
    Then the history says 3 jobs ran

  # The one that decides whether the total can be trusted. A summary that added up only the rows it
  # printed is wrong in exactly the way a reader cannot see.
  Scenario: The total covers the window even when the listing is cut short
    Given the system did two days of work
    When a session reads the history from "2026-08-29" to "2026-08-31", taking 2 jobs
    Then the history returns 2 jobs
    And the history says 3 jobs were left out
    And the history says 5 jobs ran
    And the history says the window cost 122044 tokens

  # The read must stay small enough to leave room for the work it was made for.
  Scenario: A history carries no brief and no answer
    Given the system did two days of work
    When a session reads the history from "2026-08-29" to "2026-08-31"
    Then no job in the history carries its brief or its answer

  Scenario: A history nobody bounded reads the last week, and says so
    When a session reads the history without naming a window
    Then the history says which window it read

  Scenario: A window that ends before it starts is refused
    When a session reads the history from "2026-08-31" to "2026-08-29"
    Then the system refuses it, saying the window ends before it starts

  # The verb that already guards a job guards its digest. A history returns less than one job read
  # whole, so a second verb meaning the same thing would be a second thing to keep in step.
  Scenario: A role that may not read jobs may not read the history
    Then a role holding no verbs is refused the history, and told to ask for "job.read"
