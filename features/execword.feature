Feature: One word sends an exec, and the words it replaced refuse

  An exec is an entity, and an entity is one word with verbs under it. What a session runs was three
  top level words instead: ask waited for the answer, dispatch let go of it, and tasks read the
  history back. Reading the command list gave no clue that the three were one thing, so the first
  question a new operator has, "I want this done, what do I type", had three answers and no shape
  between them.

  The one word was task for a while, and the store, the console and the model runner all called the
  same thing an exec. One thing under two names is a thing an operator has to translate, so the word
  is exec everywhere and task refuses like the other three.

  So there is one word. `krewe exec` waits here for the answer, `krewe exec --dispatch` lets go, and
  `krewe exec list` reads back what a session was sent and what came of it.

  The words that went are the other half of the change, and it is the half that gets skipped.
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
    And the system listens on an address the tool can dial

  Scenario: The word on its own sends an exec and waits for the answer
    When the caller types "exec" against the project with "when is the electricity bill due"
    Then standard output carries the reply
    And the command succeeds

  Scenario: The flag lets go, and names how to read the answer back
    When the caller types "exec --dispatch" against the project with "read the repository"
    Then standard output says the system has it
    And standard output says to read it back with "krewe exec list"
    And the command succeeds

  Scenario: The list verb reads back what a session was sent
    Given a session that was sent "when is the electricity bill due"
    When the caller reads that session's execs back
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
      | gone     | names                 |
      | ask      | krewe exec            |
      | dispatch | krewe exec --dispatch |
      | tasks    | krewe exec list       |
      | task     | krewe exec            |

  # The trap a rename makes on its own. `krewe tasks <session>` becomes `krewe exec <session>`, which
  # is a good command that sends the session's own identifier to the model as a message.
  Scenario: A session named on its own is refused rather than sent as a message
    Given a session that was sent "when is the electricity bill due"
    When the caller names that session with nothing to say
    Then standard error says "krewe exec list"
    And the command fails
    And that session was sent nothing more
