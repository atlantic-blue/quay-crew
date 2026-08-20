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

  # What is on the screen has to be typeable back, and it was not: the listing printed the session's
  # own id, the address took only the handle, and the refusal named a value that was nowhere on the
  # screen. Naming the session took the handle off the screen altogether.
  Scenario: An address takes the identifier the listing prints
    Given a session started by dispatching "remember this"
    When the operator addresses the session by the identifier the listing prints
    Then the address reaches that session

  Scenario: An address takes the identifier the listing prints for a session that has been named
    Given a session started by dispatching "remember this"
    And the session is called "the electricity bill"
    When the operator addresses the session by the identifier the listing prints
    Then the address reaches that session
    And the listing still says what the session is called

  # The session's own id is what the console acts on, what a container is named after, and what the
  # listing used to print, so it is in everybody's notes. It resolves as well.
  Scenario: An address takes the session's own id too
    Given a session started by dispatching "remember this"
    When the operator addresses the session by its own id
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
