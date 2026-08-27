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

  Scenario: An address reaches a session by the shortened handle a listing prints
    Given a session started by dispatching "remember this"
    When the operator addresses the session by its first eight characters
    Then the address reaches that session

  # A listing prints two identifiers. The id has a column of its own, and the handle sits in the name
  # column until a label or a description takes that place. So on a session anybody has named, the id
  # is the only identifier on the operator's screen, and it was the one form an address refused.
  Scenario: An address reaches a session by the id the listing prints
    Given a session started by dispatching "remember this"
    When the operator addresses the session by the id in the listing
    Then the address reaches that session

  Scenario: An address reaches a labelled session, whose handle is nowhere on the screen
    Given a session started by dispatching "remember this"
    And the operator labels the session "the bills"
    When the operator addresses the session by the id in the listing
    Then the address reaches that session

  # The operator does the next thing with it, which is the point of an address.
  Scenario: A task is dispatched to the id the listing prints
    Given a session started by dispatching "remember this"
    And the operator labels the session "the bills"
    When the operator dispatches "and again" to the session at the id in the listing
    Then the reply is "you said: and again"
    And both tasks ran in the same session

  # A refusal that offers a value the operator's screen does not carry sends them looking for
  # something they cannot find. It offers the session column, which is what they read it off.
  Scenario: A refused session offers the identifier the listing prints
    Given a session started by dispatching "remember this"
    And the operator labels the session "the bills"
    When the operator addresses "me/house-bills/ffffffff"
    Then the address is refused as not found
    And the refusal offers the identifier the listing prints

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
