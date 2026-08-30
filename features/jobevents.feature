Feature: Every movement of a job is on the record, and carries its trace

  The store is the source of truth and the event log is an audit export. So a movement is written to
  the system's own tables in the same transaction as the row it describes, and offered to the log
  afterwards. A system with no broker configured loses the export and nothing else, and a broker that
  refuses every record costs the export and never the job.

  Each record carries the trace the job belongs to. The trace is minted once, kept as a column, and
  inherited by everything under it, so one identifier joins a job, the task it dispatched,
  the spans around them and every log line written under either. Before this, the durable record of
  what the system did joined to neither the trace nor the lines: weeks later the logs are gone and the
  row is all that is left. See issue 346.

  What a caller types can be a credential, and everything recorded here is persisted, so every
  detail goes through the system's redactor before it is written or exported.

  Nothing reads the log back. There is no consumer and no projection, which is the expected state
  rather than a fault.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: Declaring a job puts a record on the system's own log
    When a job titled "read the electricity bill" is declared
    Then the log carries a "job.declared" record for that job
    And that record is keyed by the job, so one job keeps its order
    And that record carries the trace the job belongs to

  Scenario: Every movement the controller makes reaches the log
    Given a job titled "read the electricity bill"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the log carries "job.declared", "job.claimed", "job.started" and "job.answered" for that job
    And every record on the log carries the same trace

  # The rule the whole export hangs off. The job already happened and the store already holds it, so
  # a broker that will not take a record costs the copy and nothing else.
  Scenario: A broker that refuses every record does not fail the job
    Given the system's event log refuses every record
    And a job titled "read the electricity bill"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is done, and its answer is what the model said
    And the records for that job read "job.declared", "job.claimed", "job.started", "job.answered"

  # The default. A system with no broker at all keeps the whole history and exports none of it.
  Scenario: A system with no broker keeps every record and exports nothing
    Given the system has no event log configured
    And a job titled "read the electricity bill"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the job is done, and its answer is what the model said
    And the records for that job read "job.declared", "job.claimed", "job.started", "job.answered"

  # One identifier, so a reader holding either half reaches the other.
  Scenario: One trace joins a job to the task it dispatched
    Given a job titled "read the electricity bill"
    When the controller ticks
    Then the task that ran the job carries the job's own trace

  Scenario: A secret in what a caller typed reaches neither the record nor the log
    Given the workspace holds the secret "ANTHROPIC_API_KEY"
    When a job whose title carries that secret is declared
    Then the record for that job does not carry the secret
    And the record names the secret that was taken out
    And the log does not carry the secret
