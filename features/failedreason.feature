Feature: The job listing says why a failed job failed

  A person opens `krewe job list` to find out what needs them. A row that failed said "failed" and
  nothing else. The reason was already on the record, so the answer cost one `krewe job show` for
  each failed row, and a listing of four failures cost four commands to read.

  The row now carries the reason in a column beside the outcome. Each row carries its own, because
  two jobs fail for two different reasons, and one reason under the listing tells the reader of the
  fourth row the wrong thing.

  The scenarios below run the command line tool as a caller runs it: its own process, its own
  standard output, its own exit status.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And the system listens on an address the tool can dial

  Scenario: The row of a failed job says why it failed
    Given a job titled "read the electricity bill" that the model refused, saying "no credential"
    When the caller lists the jobs through the tool
    Then standard output says "no credential"
    And the command succeeds
