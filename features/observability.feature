Feature: A call can be followed after it happened

  When a task hangs or fails, the question is which of the tool, the crew, the sandbox and the model
  it is sitting in. A log line in each cannot answer that, so every call the crew serves runs inside
  a span, and every log line written while it runs carries that call's correlation id. The id is the
  trace id rather than a second identifier beside it, because an id you have to join to the trace id
  to use is one nobody joins.

  Background:
    Given a running control plane
    And a workspace named "me"
    And a project named "house-bills"

  Scenario: A call the crew serves is recorded as one span
    When the operator dispatches "remember the number" to the project
    Then the crew records a span named "quaycrew.v1.ControlPlaneService/Dispatch"

  Scenario: A call the crew refuses is recorded too
    When a caller presents no token
    Then the crew records a span named "quaycrew.v1.ControlPlaneService/ListWorkspaces"

  Scenario: A log line written while a call runs carries that call's correlation id
    Given an event log that refuses what it is given
    When the operator dispatches "remember the number" to the project
    Then the crew says the task could not be exported
    And that line carries the correlation id of the call it happened under

  Scenario: A line written before any call arrived carries no correlation id
    When the crew logs on its way up
    Then that line names the service and carries no correlation id
