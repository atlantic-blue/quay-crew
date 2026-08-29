Feature: A flow run starts because something happened

  A run could only ever start three ways: a person asked for one, a schedule came due, or a wait
  finished. All three are the crew talking to itself. Nothing said that the world had changed, which
  is the difference between an automation you set off and an automation that reacts.

  A trigger is that fourth way in. Something happens, and whatever noticed writes one row saying what
  should run and what it carried. A poller reads the row on its next tick, claims it, and starts the
  run, so the delay is one poll interval and nothing is held in a process in between. What the
  trigger carried becomes the run's opening state, so the first step is asked about the thing that
  happened.

  Nothing outside this process can raise one yet. There is no ingress and no broker: the crew still
  runs with QC_KAFKA_SEEDS unset and loses only the export. Reading the event log and writing a
  trigger row from it is the next slice.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And the crew holds this flow graph:
      """
      name: fix-red
      version: 1
      mode: edits
      nodes:
        arrived: { type: trigger }
        fix:     { type: dispatch, prompt: "the build at {{url}} is red. Fix it." }
      edges:
        - [arrived, fix]
        - [fix, done]
      """

  Scenario: Something happens, and a run starts on the next tick
    When something happens and raises a trigger of "fix-red" carrying "url" as "https://ci.test/9"
    Then no run of "fix-red" has started
    When the crew ticks
    Then a run of "fix-red" has started and finished
    And the run's steps were asked "the build at https://ci.test/9 is red. Fix it."
    And the trigger reads back as started, naming the run

  # One tree, and it is the job tree. A run nobody started is still a job, so stopping
  # that job stops the run and the tree budget counts what it spends.
  Scenario: The run a trigger started is a job in the tree
    When something happens and raises a trigger of "fix-red" carrying "url" as "https://ci.test/9"
    And the crew ticks
    Then the run's own job is labelled with the trigger that caused it
    And the run's steps hang under the run's own job

  # The failure this exists to stop: a trigger that does nothing and says nothing, which reads
  # exactly like a trigger nobody has got to yet.
  Scenario: A trigger naming a flow nobody imported says so on its row
    When something happens and raises a trigger of "never-imported" carrying "url" as "https://ci.test/9"
    And the crew ticks
    Then the trigger reads back as failed, saying "quay flow import"
    And no run of "never-imported" has started

  Scenario: A trigger for a graph that does not begin at a trigger node is refused on its row
    Given the crew holds this flow graph:
      """
      name: started-by-hand
      version: 1
      mode: edits
      nodes:
        fix: { type: dispatch, prompt: "fix the build" }
      edges:
        - [fix, done]
      """
    When something happens and raises a trigger of "started-by-hand" carrying "url" as "https://ci.test/9"
    And the crew ticks
    Then the trigger reads back as failed, saying "trigger"
    And no run of "started-by-hand" has started

  # Two pollers reading one row must leave one run. Getting this wrong costs a second session, a
  # second container and a second bill for one thing happening.
  Scenario: Two pollers start one run from one trigger
    When something happens and raises a trigger of "fix-red" carrying "url" as "https://ci.test/9"
    And two pollers tick at once
    Then exactly 1 run of "fix-red" has started
