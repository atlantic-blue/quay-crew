Feature: A project holds a numbered path of steps

  A design says what to build. It does not say what to build first, and a project had nowhere to keep
  the answer, so the atomised changes lived in whoever wrote them down.

  The path is that list, kept on the project. Each step carries one intention, the files it writes,
  what proves it, and the name of the scenario krewe runs to check it.

  The control plane parses the document rather than the caller, so the console and the command line
  send the same words and cannot drift on the grammar.

  A step that names no predecessor waits for the number below it, and step one waits for nobody. A
  default of nothing would make the gate worthless, because every step would be ready at once.

  There is no way to empty a path. A document with no step heading is refused, so a wrong file path
  cannot take somebody's path away.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A path of five steps is written and read back in number order
    When the operator sets the path to:
      """
      # The path

      Five steps, and the order they go in.

      ## 3. The control plane serves the design
      ## 1. The store holds a project's brief
      ## 5. The command line reads it back
      ## 2. The store holds a project's design
      ## 4. The session reads the design
      """
    And the operator reads the path
    Then the path holds 5 steps
    And the path reads 1, 2, 3, 4, 5 in that order
    And step 1 is titled "The store holds a project's brief"
    And step 5 is titled "The command line reads it back"

  Scenario: Every block of a step reaches the store
    When the operator sets the path to:
      """
      ## 1. The store holds a project's brief

      What changes and why
      The design has nowhere to live, so a project cannot carry one.

      What this touches
      internal/store/store.go
      internal/store/postgres.go

      What proves it
      The operator sets a brief and reads it back, so the project says what it is for.

      The scenario that proves it
      a project carries a brief

      After
      0
      """
    And the operator reads the path
    Then step 1 says its intention is "The design has nowhere to live, so a project cannot carry one."
    And step 1 touches "internal/store/store.go\ninternal/store/postgres.go"
    And step 1 says its proof is "The operator sets a brief and reads it back, so the project says what it is for."
    And step 1 names the scenario "a project carries a brief"
    And step 1 waits for step 0
    And step 1 is ready

  # A numbered path is a chain unless the document says otherwise. Every step ready at once is the
  # same as no order at all, which is what the gate exists to stop.
  Scenario: A step with no named predecessor waits for the number below it
    When the operator sets the path to:
      """
      ## 1. The store holds a project's brief
      ## 2. The store holds a project's design
      ## 3. The control plane serves the design
      """
    And the operator reads the path
    Then step 1 waits for step 0
    And step 2 waits for step 1
    And step 3 waits for step 2

  # Saying otherwise is an After block with nothing in it. It is the only way to say a step waits for
  # nobody, which is why an absent block cannot mean the same thing.
  Scenario: An After block holding nothing says the step waits for nobody
    When the operator sets the path to:
      """
      ## 1. The store holds a project's brief

      ## 2. The store holds a project's design

      After
      """
    And the operator reads the path
    Then step 2 waits for step 0

  Scenario: A step may wait for a step that is not the one below it
    When the operator sets the path to:
      """
      ## 1. The store holds a project's brief
      ## 2. The store holds a project's design
      ## 5. The command line reads it back

      After
      1
      """
    And the operator reads the path
    Then step 5 waits for step 1

  # The person fixing this has to see the line they forgot as well as the one they are looking at.
  Scenario: A document with a duplicate number is refused, naming both lines
    When the operator sets the path to:
      """
      ## 1. The store holds a project's brief
      ## 3. The control plane serves the design
      ## 3. The command line reads it back
      """
    Then the control plane refuses it as invalid
    And the refusal names lines 2 and 3
    And the project has no path

  # There is no way to empty a path, and that is deliberate: a wrong file path would otherwise delete
  # somebody's work with no refusal.
  Scenario: A document with no step heading is refused
    When the operator sets the path to:
      """
      # The path

      Five steps, and the order they go in. I never wrote the steps.
      """
    Then the control plane refuses it as invalid
    And the refusal suggests "## 1. <title>"

  Scenario: A path already written survives a document with no step heading
    Given the project's path is:
      """
      ## 1. The store holds a project's brief
      """
    When the operator sets the path to:
      """
      nothing in here is a step
      """
    Then the control plane refuses it as invalid
    And the operator reads the path
    And the path holds 1 steps

  Scenario: A step numbered zero is refused
    When the operator sets the path to:
      """
      ## 0. The store holds a project's brief
      """
    Then the control plane refuses it as invalid
    And the refusal names line 1

  Scenario: A step numbered below zero is refused
    When the operator sets the path to:
      """
      ## 1. The store holds a project's brief
      ## -2. The store holds a project's design
      """
    Then the control plane refuses it as invalid
    And the refusal names line 2

  # A number the column cannot hold is refused rather than wrapped. Wrapped, the document would say
  # one number and the store would hold another, with nothing saying so.
  Scenario: A step numbered larger than the column holds is refused
    When the operator sets the path to:
      """
      ## 4294967297. The store holds a project's brief
      """
    Then the control plane refuses it as invalid
    And the refusal names line 1
    And the refusal suggests "4294967297"

  Scenario: A step with no title is refused
    When the operator sets the path to:
      """
      ## 1. The store holds a project's brief
      ## 2.
      """
    Then the control plane refuses it as invalid
    And the refusal names line 2

  Scenario: An After that is not a number is refused
    When the operator sets the path to:
      """
      ## 1. The store holds a project's brief

      ## 2. The store holds a project's design

      After
      the first one
      """
    Then the control plane refuses it as invalid
    And the refusal names line 5

  Scenario: An After naming a step the document does not have is refused
    When the operator sets the path to:
      """
      ## 1. The store holds a project's brief

      ## 2. The store holds a project's design

      After
      7
      """
    Then the control plane refuses it as invalid
    And the refusal names line 5
    And the refusal suggests "no step 7"

  Scenario: An After that is not lower than the step's own number is refused
    When the operator sets the path to:
      """
      ## 1. The store holds a project's brief

      ## 2. The store holds a project's design

      After
      2
      """
    Then the control plane refuses it as invalid
    And the refusal names line 5

  # Krewe runs the scenario a step names, so a step that named two would leave it choosing.
  Scenario: A scenario block holding more than one line is refused
    When the operator sets the path to:
      """
      ## 1. The store holds a project's brief

      The scenario that proves it
      a project carries a brief
      a project carries a design
      """
    Then the control plane refuses it as invalid
    And the refusal names line 5

  # A step whose title needs the word "and" is two steps, and the design session enforces that. The
  # system never refuses a title for holding a word.
  Scenario: A title holding the word and is accepted
    When the operator sets the path to:
      """
      ## 1. Add the table and the index
      """
    And the operator reads the path
    Then step 1 is titled "Add the table and the index"

  # No warning refuses a document. The step is kept as it is and a person decides.
  Scenario: A step that says nothing under its labels warns and is kept
    When the operator sets the path to:
      """
      ## 1. The store holds a project's brief
      """
    Then the path write warns that step 1 says nothing under "What this touches"
    And the operator reads the path
    And the path holds 1 steps

  # Harder than the others, because this is the one krewe cannot work around: it runs the scenario a
  # step names, and there is nothing to run.
  Scenario: A step naming no scenario is warned about by name
    When the operator sets the path to:
      """
      ## 1. The store holds a project's brief
      """
    Then the path write warns "krewe step check will refuse this step"

  Scenario: A step that fills every block warns about nothing
    When the operator sets the path to:
      """
      ## 1. The store holds a project's brief

      What changes and why
      The design has nowhere to live.

      What this touches
      internal/store/store.go

      What proves it
      The operator sets a brief and reads it back.

      The scenario that proves it
      a project carries a brief
      """
    Then the path write warns about nothing

  Scenario: A project with no path answers with nothing
    When the operator reads the path
    Then the path holds 0 steps

  Scenario: The path of a project that does not exist is refused
    When the operator reads the path of a project that does not exist
    Then the control plane refuses it as not found

  Scenario: A path written for no project at all is refused
    When the operator sets a path without saying which project
    Then the control plane refuses it as invalid

  # A design session writes the path. Writing one grants it nothing it could not reach by dispatching,
  # so it is not one of the calls the operator keeps.
  Scenario: A design session may write the path
    When the driver sets the path to:
      """
      ## 1. The store holds a project's brief
      """
    And the operator reads the path
    Then the path holds 1 steps

  # These scenarios run the command line tool as a caller runs it: its own process, its own standard
  # output, its own exit status.

  Scenario: The operator writes a path from a file and reads it back
    Given the system listens on an address the tool can dial
    And a path file saying:
      """
      ## 1. The store holds a project's brief
      ## 2. The store holds a project's design
      """
    When the caller writes the path from that file
    And the caller reads the path
    Then standard output carries "The store holds a project's brief"
    And standard output carries "The store holds a project's design"
    And the command succeeds

  Scenario: A path of five steps prints five lines in number order
    Given the system listens on an address the tool can dial
    And a path file saying:
      """
      ## 1. The store holds a project's brief
      ## 2. The store holds a project's design
      ## 3. The control plane serves the design
      ## 4. The session reads the design
      ## 5. The command line reads it back
      """
    And the caller wrote the path from that file
    When the caller reads the path
    Then standard output lists 5 steps in number order
    And the command succeeds

  Scenario: A project with no path tells the caller how to write one
    Given the system listens on an address the tool can dial
    When the caller reads the path
    Then standard output carries "has no path yet"
    And standard output carries "krewe path set"
    And the command succeeds

  Scenario: A file with a duplicate step number is refused and nothing is written
    Given the system listens on an address the tool can dial
    And a path file saying:
      """
      ## 1. The store holds a project's brief
      ## 3. The control plane serves the design
      ## 3. The command line reads it back
      """
    When the caller writes the path from that file
    Then standard error says "line 2"
    And standard error says "line 3"
    And standard error says "nothing was written"
    And the command fails

  Scenario: Writing a path without saying which file is refused
    Given the system listens on an address the tool can dial
    When the caller writes the path without naming a file
    Then standard error says "usage: krewe path set"
    And the command fails
