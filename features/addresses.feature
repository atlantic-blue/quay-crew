Feature: The crew is addressed by path

  You work in one place at a time and say where with an address: workspace, then project, then
  session. "me/house-bills" is a body of work; "me/house-bills/3cb04bf5" is one conversation in it.
  Every level is a name or an id, and a session may be the shortened identifier a listing prints,
  because what is on the operator's screen has to be typeable back.

  Background:
    Given a running control plane
    And a workspace named "me"
    And a project named "house-bills"

  Scenario: An address reaches a project inside a workspace
    When the operator addresses "me/house-bills"
    Then the address reaches the project

  Scenario: An address can stop at a workspace
    When the operator addresses "me"
    Then the address reaches the workspace but no project

  Scenario: An address reaches a session by the shortened id a listing prints
    Given a session started by dispatching "remember this"
    When the operator addresses the session by its first eight characters
    Then the address reaches that session

  Scenario: An address naming a project that does not exist is refused
    When the operator addresses "me/ghost"
    Then the address is refused as not found

  Scenario: An address naming a workspace that does not exist is refused
    When the operator addresses "ghost/house-bills"
    Then the address is refused as not found

  Scenario: An address deeper than a session is refused
    When the operator addresses "me/house-bills/session/deeper"
    Then the address is refused as malformed

  # A project name is only unique inside its workspace, which is what makes short names usable at all.
  Scenario: The same project name in two workspaces is not ambiguous
    Given a second workspace named "someone-else"
    And a project named "house-bills" in the second workspace
    When the operator addresses "me/house-bills"
    Then the address reaches the project
