Feature: Work is a record the crew keeps

  A caller declares a piece of work and the crew keeps it. The intent is a row, so it outlives the
  terminal that asked for it, the session that will run it, and the process that read it. Nothing
  runs the work yet: this is the record, the refusals and the read path.

  Every rule is checked at the moment of the write, while the person who wrote it is looking. A
  refusal that arrives hours later, inside a run, points at nothing.

  What a piece of work cannot be done without is what it requires. The flag was called --hands, and
  the word needed explaining every time somebody read it. --requires also reads correctly in both
  directions: this work requires context, and the architect role receives context. The old flag is in
  fingers, in scripts and in notes, so it refuses and names what to type instead.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: Intent survives the caller
    Given a piece of work titled "read the electricity bill"
    When the caller goes away and the crew is asked again
    Then the work is still there, pending, with its brief whole

  Scenario: A piece of work opens pending, at depth zero, with no parent
    Given a piece of work titled "read the electricity bill"
    Then the work is pending
    And the work is at depth 0 with no parent
    And the work carries the moment it was declared

  Scenario: The crew assigns the identifier
    When the caller declares work carrying an identifier of its own
    Then the crew refuses it and says it assigns the identifier

  Scenario: The parent is never taken from the request
    When the caller declares work carrying a parent
    Then the crew refuses it and says the parent comes from the credential

  Scenario: Work with no title is refused
    When the caller declares work with no title
    Then the crew refuses it and says a title is needed

  Scenario: A title of 201 bytes is refused
    When the caller declares work with a title of 201 bytes
    Then the crew refuses it and says the ceiling is 200

  Scenario: A brief of 16385 bytes is refused
    When the caller declares work with a brief of 16385 bytes
    Then the crew refuses it and says the ceiling is 16384

  Scenario: Work naming a role the workspace does not hold is refused
    When the caller declares work in the role "backlog-clearer"
    Then the crew refuses it and names the role

  Scenario: Work in a role the workspace holds is pinned to the version it holds
    Given the workspace holds the role "backlog-clearer" at version 1
    When the caller declares work in the role "backlog-clearer"
    Then the work carries the role at version 1

  # A role declares what it receives and a piece of work declares what it cannot be done without.
  # Where the two disagree the work is refused, while the person who wrote it is looking.
  Scenario: Work that requires material its role does not receive is refused
    Given the workspace holds the role "test-writer" at version 1 receiving "work"
    When the caller declares work in the role "test-writer" requiring "context"
    Then the crew refuses it, naming the role, the material and what to change
    And no work was written

  Scenario: Work that requires what its role does receive is kept
    Given the workspace holds the role "backlog-clearer" at version 1 receiving "work, context"
    When the caller declares work in the role "backlog-clearer" requiring "context"
    Then the work requires "context"

  Scenario: Work that requires something the crew does not hand out is refused
    When the caller declares work requiring "the codebase"
    Then the crew refuses it and lists the material it hands out

  # Work that names no role requires its material of nobody, so nothing here applies to it.
  Scenario: Work with no role is held to no boundary
    When the caller declares work requiring "context"
    Then the work requires "context"

  Scenario: Work naming a mode that is not a mode is refused
    When the caller declares work in the mode "yolo"
    Then the crew refuses it and lists the modes

  Scenario: Work whose expected file is absolute is refused
    When the caller declares work expecting the file "/etc/passwd"
    Then the crew refuses it and says the path is read inside the working directory

  Scenario: Work whose expected file climbs out of the working directory is refused
    When the caller declares work expecting the file "../secrets.txt"
    Then the crew refuses it and says the path climbs out

  Scenario: Work waiting on something that does not exist is refused
    When the caller declares work after "0123456789abcdef01234567"
    Then the crew refuses it and names the identifier it cannot find

  Scenario: Work waits for work that exists
    Given a piece of work titled "read the electricity bill"
    When the caller declares work after the first piece of work
    Then the work waits for the first piece of work

  Scenario: A budget below zero is refused
    When the caller declares work with a budget of -1 tokens
    Then the crew refuses it and says a budget cannot be below zero

  Scenario: Seventeen labels are refused
    When the caller declares work carrying 17 labels
    Then the crew refuses it and says the ceiling is 16

  Scenario: A label value of 64 characters is refused
    When the caller declares work carrying a label value of 64 characters
    Then the crew refuses it and says the ceiling is 63

  Scenario: Work in a workspace that does not exist is refused
    When the caller declares work in a project that does not exist
    Then the control plane refuses it as not found

  # The tool, in its own process, because what is specified here is the exit status and which stream
  # the sentence went to, and neither exists inside the test process.
  Scenario: The tool declares what a piece of work requires
    Given the crew listens on an address the tool can dial
    When the caller declares work with "--requires context" through the tool
    Then the command succeeds
    And reading that work back says it requires "context"

  # The way off the old flag. A removed flag that is quietly ignored reads as a command that worked,
  # and the operator finds out from the record later that the boundary was never declared.
  Scenario: The flag that went refuses, names what to type, and fails
    Given the crew listens on an address the tool can dial
    When the caller declares work with "--hands context" through the tool
    Then standard error says "--hands is gone"
    And standard error says "--requires"
    And standard output is empty
    And the command fails
    And no work was written

  Scenario: A listing says what a project holds, newest first
    Given a piece of work titled "read the electricity bill"
    And a piece of work titled "pay the electricity bill"
    When the caller lists the work in the project
    Then the listing holds both pieces of work, newest first

  Scenario: A listing carries no answers
    Given a piece of work titled "read the electricity bill" that answered "the bill is due on the 14th"
    When the caller lists the work in the project
    Then the listing carries the title and not the answer
    And reading that one piece of work carries the answer whole

  Scenario: A listing is narrowed by phase
    Given a piece of work titled "read the electricity bill"
    And a piece of work titled "pay the electricity bill"
    When the caller stops the first piece of work saying "the bill is not due yet"
    And the caller lists the work that is pending
    Then the listing holds only "pay the electricity bill"

  Scenario: A person stops a piece of work and the reason is kept
    Given a piece of work titled "read the electricity bill"
    When the caller stops the first piece of work saying "the bill is not due yet"
    Then the work is stopped, and the reason is "the bill is not due yet"
    And the work carries the moment it finished

  Scenario: Work that already stopped is not stopped again
    Given a piece of work titled "read the electricity bill"
    When the caller stops the first piece of work saying "the bill is not due yet"
    And the caller stops the first piece of work saying "changed my mind"
    Then the crew refuses it and says the work already ended
    And the reason on the work is still "the bill is not due yet"

  Scenario: Work nobody has is refused by name
    When the caller asks for a piece of work that does not exist
    Then the control plane refuses it as not found

  # The store is the source of truth in this slice, so the record of what happened is a row beside
  # the row it describes, written in the same transaction. Nothing is published to the log yet.
  Scenario: Declaring work writes the record of the declaration
    Given a piece of work titled "read the electricity bill"
    Then the crew holds a "work.declared" record for it, naming the title

  Scenario: Stopping work writes the record of the stop
    Given a piece of work titled "read the electricity bill"
    When the caller stops the first piece of work saying "the bill is not due yet"
    Then the crew holds a "work.stopped" record for it, naming the reason
