Feature: One word for declared intent, and the word it replaced refuses

  Declared intent is a job. It was called work, and the word cost real time twice: a reader who has
  sent a task cannot tell what "work" is from the word alone, and neither could the person reading
  the command list.

  Kubernetes had already answered it, and the system had borrowed half of its vocabulary: a lease, a
  phase, a role, verbs on a resource. A Kubernetes Job is declared intent, run to completion, watched
  by a controller, with a disposable container underneath, which is this down to the phase field. So
  it is a job.

  The word that went is the other half of the change, and it is the half that gets skipped. It is in
  fingers, in scripts and in notes, so it refuses, exits non zero, and names what to type. It is not
  a quiet alias, and it is never absorbed into the next argument: a command that reads as one that
  worked is worse than one that never existed, because the operator finds out from the record later.

  The same holds for the words a role manifest carries. A role is a file in somebody's repository,
  and a manifest still asking to receive work, or to call `work.create`, is refused at import by
  name rather than accepted and quietly meaning nothing.

  The scenarios below run the command line tool as a caller runs it: its own process, its own
  standard output, its own exit status.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And the system listens on an address the tool can dial

  Scenario: The word declares a job and says how to read it back
    When the caller declares a job through the tool, titled "read the electricity bill"
    Then standard output says it is declared
    And standard output says to read it back with "quay job show"
    And the command succeeds

  Scenario Outline: The word that went refuses, names what to type, and fails
    When the caller types "<gone>" against the project with "remember the number"
    Then standard error says "quay job"
    And standard error does not carry the message
    And standard output is empty
    And the command fails
    And no job was written

    Examples:
      | gone         |
      | work         |
      | work create  |
      | work list    |
      | work show    |
      | work stop    |

  # The refusal has to be about the word rather than about the flags on it, because somebody who
  # typed a whole command that is gone cannot act on advice about one part of it.
  Scenario: The whole command refuses on the word, not on its flags
    When the caller declares a job through the tool with the word that went
    Then standard error says "there is no work command"
    And standard error says "quay job <create|list|show|stop>"
    And the command fails
    And no job was written

  Scenario: A role that asks to receive the word that went is refused, and named
    When the operator imports a role that receives "work"
    Then the refusal names "job"
    And the system holds no such role

  Scenario: A role that asks to call the verb that went is refused, and named
    When the operator imports a role that may "work.create"
    Then the refusal names "job.create"
    And the system holds no such role
