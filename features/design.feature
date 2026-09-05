Feature: A project carries what it is for and what was designed

  A project had nowhere to keep two things a person needs before any work starts: what the project is
  for, and what was designed for it. Both lived in somebody's head, or in a file on one machine, so a
  session starting in the project was told neither.

  The system keeps them on the project itself. The brief is one paragraph. The design is a document,
  written whole, and read back whole so it can be piped.

  Approval is a statement about one text. Any write to the design clears it, and the write says so,
  so a person learns the rule by reading the output rather than by being surprised later.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A project that nobody designed says so
    When the operator reads the project's design
    Then the project has no design yet

  Scenario: A brief says what the project is for
    When the operator sets the project's brief to "keep the household bills paid on time"
    And the operator reads the project's design
    Then the brief reads "keep the household bills paid on time"

  # An empty brief is a value, not an absence. Clearing one is the only way back, and a command that
  # refused the empty string would leave a wrong brief in place for ever.
  Scenario: An empty brief clears what was there
    Given the project's brief is "keep the household bills paid on time"
    When the operator sets the project's brief to ""
    And the operator reads the project's design
    Then the brief reads ""

  # The body is the largest text in the system, and a design read short is a design read wrong.
  Scenario: A design body is read back whole
    When the operator writes the project's design as "# Bills\n\nOne paragraph.\n\n- a point\n"
    And the operator reads the project's design
    Then the design body reads "# Bills\n\nOne paragraph.\n\n- a point\n"

  Scenario: A brief and a design body are separate statements
    Given the project's brief is "keep the household bills paid on time"
    When the operator writes the project's design as "# Bills\n"
    And the operator reads the project's design
    Then the brief reads "keep the household bills paid on time"
    And the design body reads "# Bills\n"

  Scenario: The design of a project that does not exist is refused
    When the operator reads the design of a project that does not exist
    Then the control plane refuses it as not found

  Scenario: A design call that names no project is refused
    When the operator reads the design without saying which project
    Then the control plane refuses it as invalid

  # A brief is one paragraph naming what the project is for. A page of prose in that field is a
  # design in the wrong column. Nothing is refused: the text is kept whole and the length is said.
  Scenario: A brief over the mark warns and is kept whole
    When the operator sets the project's brief to 2500 characters
    Then the write warns about the length
    And the brief is kept whole

  Scenario: A design body under the mark warns about nothing
    When the operator writes a design of 500 characters
    Then the write warns about nothing

  # The session that wrote the design is recorded, and the operator records nobody. It is a claim the
  # system keeps rather than one it checks, and it grants nothing.
  Scenario: A design written by a session records that session
    When the session "sess-1" writes the project's design as "# Bills\n"
    And the operator reads the project's design
    Then the design says it was written by "sess-1"

  Scenario: A design written by the operator records nobody
    Given the session "sess-1" wrote the project's design as "# Bills\n"
    When the operator writes the project's design as "# Bills again\n"
    And the operator reads the project's design
    Then the design says it was written by ""

  # These scenarios run the command line tool as a caller runs it: its own process, its own standard
  # output, its own exit status.

  Scenario: The operator writes a brief and reads it back with the tool
    Given the system listens on an address the tool can dial
    When the caller sets the project's brief to "keep the household bills paid on time"
    And the caller reads the project's design
    Then standard output carries "keep the household bills paid on time"
    And the command succeeds

  Scenario: The operator writes a design from a file and reads it back
    Given the system listens on an address the tool can dial
    And a design file saying "# Bills\n\nPay the water bill first.\n"
    When the caller writes the design from that file
    And the caller reads the project's design
    Then standard output carries "Pay the water bill first."
    And the command succeeds

  # Said on every write, whether or not the design was approved before it. A person who reads it
  # twice learns that approval is a statement about one text.
  Scenario: Writing a design says the approval is cleared
    Given the system listens on an address the tool can dial
    And a design file saying "# Bills\n"
    When the caller writes the design from that file
    Then standard output carries "the approval is cleared"
    And the command succeeds

  Scenario: A project with no design tells the caller how to write one
    Given the system listens on an address the tool can dial
    When the caller reads the project's design
    Then standard output carries "has no design yet"
    And standard output carries "krewe design set"
    And the command succeeds

  Scenario: An empty design file is refused
    Given the system listens on an address the tool can dial
    And a design file saying ""
    When the caller writes the design from that file
    Then standard error says "an empty design is not a design"
    And the command fails

  Scenario: Writing a design without saying which file is refused
    Given the system listens on an address the tool can dial
    When the caller writes the design without naming a file
    Then standard error says "usage: krewe design set"
    And the command fails

  # The session working in the project reads what the project is for, on every exec, out of its own
  # memory file. The design itself is a file beside it: the summary is read every time and the
  # document is opened by a model that decides it needs it.

  Scenario: A session reads what the project is for
    Given the project's brief is "keep the household bills paid on time"
    And the project's design is "# Bills\n\nPay the water bill first.\n"
    When the operator dispatches "hello" to the project
    Then the session's memory file carries "This project is house-bills."
    And the session's memory file carries "keep the household bills paid on time"

  Scenario: A session finds the whole design in its working directory
    Given the project's design is "# Bills\n\nPay the water bill first.\n"
    When the operator dispatches "hello" to the project
    Then the session's design file reads "# Bills\n\nPay the water bill first.\n"

  Scenario: The memory file sends the session to the design file
    Given the project's design is "# Bills\n"
    When the operator dispatches "hello" to the project
    Then the session's memory file carries "Read .krewe/design.md before you start."

  # A line telling the model to open a file that is not there sends it to open nothing.
  Scenario: A project with a brief and no design does not send the session to a file
    Given the project's brief is "keep the household bills paid on time"
    When the operator dispatches "hello" to the project
    Then the session's memory file carries "keep the household bills paid on time"
    And the session's memory file does not carry ".krewe/design.md"
    And the session has no design file

  Scenario: A project with no design puts no design section in the memory file
    Given the operator sets the project's context to "pay the water bill first"
    When the operator dispatches "hello" to the project
    Then the session's memory file carries "pay the water bill first"
    And the session's memory file carries no design section

  # The section is read again on every exec of every session in the project, so its cost is paid per
  # exec. The brief is the only part whose length nobody controls, so it is the part that is cut.
  Scenario: A very long brief is cut so the section stays small
    Given the project's brief is 5000 characters
    And the project's design is "# Bills\n"
    When the operator dispatches "hello" to the project
    Then the design section is under 400 characters
    And the session's memory file carries "Read .krewe/design.md before you start."

  # The section is rendered state, never context. A mark the read back does not know is swept into
  # the session's own context, stored as though a person typed it, and rendered again underneath
  # itself on the next exec.
  Scenario: The design section is not carried twice
    Given the project's brief is "keep the household bills paid on time"
    And the project's design is "# Bills\n"
    When the operator dispatches "hello" to the project
    And the operator dispatches "and again" to the same session
    Then the memory file carries one design section
    And the session's context does not carry the design section

  Scenario: Rewriting the design gives the session the new text
    Given the project's design is "# Bills\n"
    And a session started by dispatching "hello"
    When the operator writes the project's design as "# Bills, again\n"
    Then the session's design file reads "# Bills, again\n"

  # A design emptied on purpose must not stay readable in the working directory, or what the session
  # reads and what the store holds disagree.
  Scenario: Emptying the design takes the file away
    Given the project's design is "# Bills\n"
    And a session started by dispatching "hello"
    When the operator writes the project's design as ""
    Then the session has no design file
