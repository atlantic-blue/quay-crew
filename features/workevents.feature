Feature: Every movement of a piece of work is on the record, and carries its trace

  The store is the source of truth and the event log is an audit export. So a movement is written to
  the crew's own tables in the same transaction as the row it describes, and offered to the log
  afterwards. A crew with no broker configured loses the export and nothing else, and a broker that
  refuses every record costs the export and never the work.

  Each record carries the trace the work belongs to. The trace is minted once, kept as a column, and
  inherited by everything under it, so one identifier joins a piece of work, the task it dispatched,
  the spans around them and every log line written under either. Before this, the durable record of
  what the crew did joined to neither the trace nor the lines: weeks later the logs are gone and the
  row is all that is left. See issue 346.

  What a caller types can be a credential, and everything recorded here is persisted, so every
  detail goes through the crew's redactor before it is written or exported.

  Nothing reads the log back. There is no consumer and no projection, which is the expected state
  rather than a fault.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: Declaring work puts a record on the crew's own log
    When a piece of work titled "read the electricity bill" is declared
    Then the log carries a "work.declared" record for that work
    And that record is keyed by the work, so one piece of work keeps its order
    And that record carries the trace the work belongs to

  Scenario: Every movement the controller makes reaches the log
    Given a piece of work titled "read the electricity bill"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the log carries "work.declared", "work.claimed", "work.started" and "work.answered" for that work
    And every record on the log carries the same trace

  # The rule the whole export hangs off. The work already happened and the store already holds it, so
  # a broker that will not take a record costs the copy and nothing else.
  Scenario: A broker that refuses every record does not fail the work
    Given the crew's event log refuses every record
    And a piece of work titled "read the electricity bill"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the work is done, and its answer is what the model said
    And the records for that work read "work.declared", "work.claimed", "work.started", "work.answered"

  # The default. A crew with no broker at all keeps the whole history and exports none of it.
  Scenario: A crew with no broker keeps every record and exports nothing
    Given the crew has no event log configured
    And a piece of work titled "read the electricity bill"
    When the controller ticks
    And the task the controller sent lands
    And the controller ticks again
    Then the work is done, and its answer is what the model said
    And the records for that work read "work.declared", "work.claimed", "work.started", "work.answered"

  # One identifier, so a reader holding either half reaches the other.
  Scenario: One trace joins a piece of work to the task it dispatched
    Given a piece of work titled "read the electricity bill"
    When the controller ticks
    Then the task that ran the work carries the work's own trace

  Scenario: A secret in what a caller typed reaches neither the record nor the log
    Given the workspace holds the secret "ANTHROPIC_API_KEY"
    When a piece of work whose title carries that secret is declared
    Then the record for that work does not carry the secret
    And the record names the secret that was taken out
    And the log does not carry the secret
