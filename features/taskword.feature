Feature: One word sends a task, and the three it replaced refuse

  A task is an entity, like a job and like a flow, and each of those is one word with verbs
  under it. A task was three top level words instead: ask waited for the answer, dispatch let go of
  it, and tasks read the history back. Reading the command list gave no clue that the three were one
  thing, so the first question a new operator has, "I want this done, what do I type", had three
  answers and no shape between them.

  So there is one word. `quay task` waits here for the answer, `quay task --dispatch` lets go, and
  `quay task list` reads back what a session was sent and what came of it.

  The three words that went are the other half of the change, and it is the half that gets skipped.
  They are in fingers, in scripts and in notes, so each one refuses, exits non zero, and names what
  to type. None of them is a quiet alias, and none of them is absorbed into the next argument: a word
  that reaches the model as the first two words of a message succeeds, and the operator finds out
  from the history an hour later.

  The scenarios below run the command line tool as a caller runs it: its own process, its own
  standard output, its own exit status.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And the crew listens on an address the tool can dial

  Scenario: The word on its own sends a task and waits for the answer
    When the caller types "task" against the project with "when is the electricity bill due"
    Then standard output carries the reply
    And the command succeeds

  Scenario: The flag lets go, and names how to read the answer back
    When the caller types "task --dispatch" against the project with "read the repository"
    Then standard output says the crew has it
    And standard output says to read it back with "quay task list"
    And the command succeeds

  Scenario: The list verb reads back what a session was sent
    Given a session that was sent "when is the electricity bill due"
    When the caller reads that session's tasks back
    Then standard output carries "when is the electricity bill due"
    And the command succeeds

  Scenario Outline: A word that went refuses, names what to type, and fails
    When the caller types "<gone>" against the project with "remember the number"
    Then standard error says "<names>"
    And standard error does not carry the message
    And standard output is empty
    And the command fails
    And no session was started

    Examples:
      | gone     | names                  |
      | ask      | quay task              |
      | dispatch | quay task --dispatch   |
      | tasks    | quay task list         |

  # The trap a rename makes on its own. `quay tasks <session>` becomes `quay task <session>`, which is
  # a good command that sends the session's own identifier to the model as a message.
  Scenario: A session named on its own is refused rather than sent as a message
    Given a session that was sent "when is the electricity bill due"
    When the caller names that session with nothing to say
    Then standard error says "quay task list"
    And the command fails
    And that session was sent nothing more
