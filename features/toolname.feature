Feature: The tool is called krewe, and the name it had refuses

  The command was quay. Quay is Red Hat's container registry: the same audience, the same words, and
  this tool runs containers. So the command is krewe, which is the word for the group that puts the
  work on.

  A name that only disappears is the worse of two failures. A shell answers a missing command with
  "command not found". That reads as a broken install, not as a rename, and nothing anywhere says the
  word moved. So the old name is still on the path. It refuses everything, and it names the new one.

  It refuses whatever follows it, not a list of the commands somebody remembered. The word left off
  such a list is always the word the next person types.

  Background:
    Given a running control plane
    And the system listens on an address the tool can dial

  Scenario: The new name drives the system
    Given a workspace named "me"
    And a project named "house-bills"
    And a session started by dispatching "remember this"
    When the caller lists the sessions
    Then standard output carries "house-bills"
    And standard output carries "1 session in this system"
    And the command succeeds

  Scenario: The old name refuses, and says what to type instead
    When the caller types the old name with "sessions"
    Then the command fails
    And standard error says to type "krewe sessions"
    And standard error never tells the operator to type the old name
    And standard output is empty

  Scenario: The old name on its own names the new name on its own
    When the caller types the old name on its own
    Then the command fails
    And standard error says to type "krewe"

  Scenario Outline: Whatever follows the old name is refused
    When the caller types the old name with "<typed>"
    Then the command fails
    And standard error says to type "krewe <typed>"

    Examples:
      | typed     |
      | workspace |
      | task      |
      | version   |
      | help      |
      | --help    |
      | sideways  |
