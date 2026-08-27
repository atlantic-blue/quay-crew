Feature: Work is a record the crew keeps

  A caller declares a piece of work and the crew keeps it. The intent is a row, so it outlives the
  terminal that asked for it, the session that will run it, and the process that read it. Nothing
  runs the work yet: this is the record, the refusals and the read path.

  Every rule is checked at the moment of the write, while the person who wrote it is looking. A
  refusal that arrives hours later, inside a run, points at nothing.

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
