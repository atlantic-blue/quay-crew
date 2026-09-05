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

  The session working in the project reads the path in its own working directory, in number order.
  That is what lets a session start from what is true: a session on step 4 reads what steps 1 to 3
  produced.

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

  # The session working in the project reads the path out of its own working directory. It is a file
  # rather than a section in the memory file, because a path grows with the project and the memory
  # file is read on every exec.

  Scenario: A session reads every step of the path in its working directory
    Given the project's path is:
      """
      ## 1. The store holds a project's brief

      What changes and why
      The design has nowhere to live, so a project cannot carry one.

      What this touches
      internal/store/store.go
      internal/store/postgres.go

      What proves it
      The operator sets a brief and reads it back.

      The scenario that proves it
      a project carries a brief

      ## 2. The store holds a project's design
      ## 3. The control plane serves the design
      """
    When the operator dispatches "hello" to the project
    Then the session's path file lists steps 1, 2, 3 in that order
    And the session's path file carries "## 1. The store holds a project's brief"
    And the session's path file carries "state: ready"
    And the session's path file carries "The design has nowhere to live, so a project cannot carry one."
    And the session's path file carries "internal/store/postgres.go"
    And the session's path file carries "a project carries a brief"

  # Numbers need not run without gaps, so the order is the number's and never the store's. A file
  # that named 12 before 5 would be a different path from the one the operator wrote.
  Scenario: The steps are in number order even when the numbers are not contiguous
    Given the project's path is:
      """
      ## 5. The command line reads it back
      ## 1. The store holds a project's brief
      ## 12. The console shows the path
      ## 2. The store holds a project's design
      """
    When the operator dispatches "hello" to the project
    Then the session's path file lists steps 1, 2, 5, 12 in that order

  # A file that exists and says nothing costs a read.
  Scenario: A project with no path has no path file
    Given the project's design is "# Bills\n"
    When the operator dispatches "hello" to the project
    Then the session has no path file
    And the session's memory file does not carry ".krewe/path.md"

  Scenario: Setting a new path and dispatching again gives the session the new text
    Given the project's path is:
      """
      ## 1. The store holds a project's brief
      """
    And a session started by dispatching "hello"
    When the operator sets the path to:
      """
      ## 1. The store holds a project's design
      """
    And the operator dispatches "and again" to the same session
    Then the session's path file carries "## 1. The store holds a project's design"
    And the session's path file does not carry "The store holds a project's brief"

  # A line naming a file that is not there sends the model to open nothing, so the summary names the
  # path only when the project has one.
  Scenario: The memory file sends the session to the path file
    Given the project's design is "# Bills\n"
    And the project's path is:
      """
      ## 1. The store holds a project's brief
      """
    When the operator dispatches "hello" to the project
    Then the session's memory file carries "Read .krewe/design.md before you start. The whole path is in .krewe/path.md."

  # The section is read again on every exec of every session in the project, and the path took a
  # piece of it. The brief is the only part whose length nobody controls, so it is the part that is
  # cut.
  Scenario: A very long brief is cut so the section stays small with a path in it
    Given the project's brief is 5000 characters
    And the project's design is "# Bills\n"
    And the project's path is:
      """
      ## 1. The store holds a project's brief
      """
    When the operator dispatches "hello" to the project
    Then the design section is under 400 characters
    And the session's memory file carries "The whole path is in .krewe/path.md."

  # Taking a step starts a session on it, and the system composes what that session is given: the
  # step whole, where it sits in the path, and what to do with it. The command line sends a number
  # and the control plane sends the words, so the console cannot send different ones.

  Scenario: Taking a step dispatches a session whose text names the step and its title
    Given the project's design is "# Bills\n"
    And the operator approved the project's design
    And the project's path is:
      """
      ## 1. The store holds a project's brief

      What changes and why
      The design has nowhere to live, so a project cannot carry one.

      What this touches
      internal/store/store.go

      What proves it
      The operator sets a brief and reads it back.

      The scenario that proves it
      a project carries a brief

      ## 2. The store holds a project's design
      ## 3. The control plane serves the design
      """
    When the operator takes step 1
    Then the step text carries "Step 1 of 3 on the path for house-bills."
    And the step text carries "The store holds a project's brief"
    And the step text carries "The design has nowhere to live, so a project cannot carry one."
    And the step text carries "internal/store/store.go"
    And the step text carries "The scenario that proves it is named: a project carries a brief"
    And the session was asked exactly what the take composed
    And step 1 is held by that session

  # The count is of the steps in the path and never of the highest number, so a path running 1, 2, 5
  # reads "of 3".
  Scenario: The count is the steps in the path and not the numbers
    Given the project's design is "# Bills\n"
    And the operator approved the project's design
    And the project's path is:
      """
      ## 1. The store holds a project's brief
      ## 2. The store holds a project's design
      ## 5. The command line reads it back
      """
    When the operator takes step 5
    Then the step text carries "Step 5 of 3 on the path for house-bills."

  Scenario: The text tells the session to build this step only
    Given the project's design is "# Bills\n"
    And the operator approved the project's design
    And the project's path is:
      """
      ## 1. The store holds a project's brief
      """
    When the operator takes step 1
    Then the step text carries "The design is in .krewe/design.md. The whole path is in .krewe/path.md. Read both."
    And the step text carries "Build this step only. Do not take work from another step."

  # A label with nothing under it is text the model has to read for nothing.
  Scenario: A step with no proof produces text with no proof label in it
    Given the project's design is "# Bills\n"
    And the operator approved the project's design
    And the project's path is:
      """
      ## 1. The store holds a project's brief

      What changes and why
      The design has nowhere to live, so a project cannot carry one.
      """
    When the operator takes step 1
    Then the step text carries "What changes and why"
    And the step text does not carry "What proves it"
    And the step text does not carry "The scenario that proves it"
    And the step text does not carry "What this touches"

  # One step is one session's. Two takes that both passed would put two sessions on one change.
  Scenario: Taking a step somebody already holds is refused, naming the session
    Given the project's design is "# Bills\n"
    And the operator approved the project's design
    And the project's path is:
      """
      ## 1. The store holds a project's brief
      """
    And the operator took step 1
    When the operator takes step 1
    Then the control plane refuses it as the wrong state
    And the refusal names the session holding step 1
    And 1 session was started

  # Gate 1. No code exists before the operator approves the path, so this refusal comes before every
  # other check, costs one line of output, and starts nothing.
  Scenario: Taking a step on a design nobody approved is refused and starts nothing
    Given the project's design is "# Bills\n"
    And the project's path is:
      """
      ## 1. The store holds a project's brief
      """
    When the operator takes step 1
    Then the control plane refuses it as the wrong state
    And the refusal suggests "krewe design approve"
    And 0 sessions were started
    And the operator reads the path
    And step 1 is ready

  Scenario: A design approved and then rewritten refuses the take again
    Given the project's design is "# Bills\n"
    And the operator approved the project's design
    And the project's path is:
      """
      ## 1. The store holds a project's brief
      """
    And the operator writes the project's design as "# Bills, rewritten\n"
    When the operator takes step 1
    Then the control plane refuses it as the wrong state
    And 0 sessions were started

  Scenario: Taking a step the path does not hold is refused, saying how many it has
    Given the project's design is "# Bills\n"
    And the operator approved the project's design
    And the project's path is:
      """
      ## 1. The store holds a project's brief
      ## 2. The store holds a project's design
      """
    When the operator takes step 7
    Then the control plane refuses it as not found
    And the refusal suggests "it has 2 steps"
    And 0 sessions were started

  Scenario: Taking a step numbered below one is refused
    Given the project's design is "# Bills\n"
    And the operator approved the project's design
    When the operator takes step 0
    Then the control plane refuses it as invalid
    And the refusal suggests "counts from one"

  # The session reads which step it is on in the design section of its own memory file, which it
  # reads on every exec.
  Scenario: The session that took a step reads which step it is on
    Given the project's design is "# Bills\n"
    And the operator approved the project's design
    And the project's path is:
      """
      ## 1. The store holds a project's brief
      ## 2. The store holds a project's design
      ## 3. The control plane serves the design
      """
    When the operator takes step 2
    Then the session's memory file carries "You are on step 2 of 3: The store holds a project's design"
    And the design section is under 400 characters

  # A session nobody gave a step to is most sessions, and the line costs context on every one of them.
  Scenario: A session that took no step reads no step line
    Given the project's design is "# Bills\n"
    And the project's path is:
      """
      ## 1. The store holds a project's brief
      """
    When the operator dispatches "hello" to the project
    Then the session's memory file does not carry "You are on step"

  # These scenarios run the command line tool as a caller runs it: its own process, its own standard
  # output, its own exit status.

  Scenario: The operator takes a step and reads what the session was asked to do
    Given the system listens on an address the tool can dial
    And the project's design is "# Bills\n"
    And the operator approved the project's design
    And the project's path is:
      """
      ## 1. The store holds a project's brief
      """
    When the caller takes step 1
    Then standard output carries "step 1 of house-bills is taken: The store holds a project's brief"
    And standard output carries "Step 1 of 1 on the path for house-bills."
    And standard output carries "Build this step only."
    And the command succeeds

  Scenario: The tool prints one line of refusal for a design nobody approved
    Given the system listens on an address the tool can dial
    And the project's design is "# Bills\n"
    And the project's path is:
      """
      ## 1. The store holds a project's brief
      """
    When the caller takes step 1
    Then standard error says "krewe design approve"
    And standard error says "nothing was started"
    And the command fails

  Scenario: Taking a step without saying which one is refused
    Given the system listens on an address the tool can dial
    When the caller takes a step without saying which one
    Then standard error says "usage: krewe step take"
    And the command fails

  # A project delivers several features at the same time. A website runs an authentication feature
  # and a payment feature at once: authentication ships sign up, then sign in, then reset, and
  # payment ships checkout, then refunds. Two paths, and neither waits for the other. A feature is
  # where each narrowed part of the project lives.
  #
  # A feature carries no design, no contracts document and no approval. Those belong to the project,
  # so gate 1 reads the project's design whichever feature a step sits in.

  Scenario: Adding two features to a project gives them the numbers 1 and 2
    When the operator adds the feature "authentication"
    And the operator adds the feature "payment"
    And the operator reads the features
    Then the features read 1, 2 in that order
    And feature 1 is titled "authentication"
    And feature 2 is titled "payment"

  # Feature numbers restart in each project, which is what makes the number the thing a person types.
  Scenario: A second project starts its features at one again
    Given a project named "the-website" beside it
    When the operator adds the feature "authentication"
    And the operator adds the feature "payment"
    And the operator adds the feature "checkout" to the project beside it
    And the operator reads the features of the project beside it
    Then the features read 1 in that order
    And feature 1 is titled "checkout"

  # The operator never chooses the number, so two adds at one moment cannot take the same one.
  Scenario: The number comes from the system and not from the caller
    When the operator adds the feature "authentication"
    Then the feature that was added took number 1
    And the feature that was added carries an identifier of its own

  Scenario: A feature with no title is refused
    When the operator adds a feature with no title
    Then the control plane refuses it as invalid
    And the operator reads the features
    And the project holds no feature

  Scenario: A fresh feature reads back open with an empty intention
    When the operator adds the feature "authentication"
    And the operator reads the features
    Then feature 1 is open
    And feature 1 narrows to ""

  Scenario: An intention reads back on the feature
    Given the project's feature "authentication"
    When the operator sets feature 1's intention to "sign up, sign in, reset, sessions"
    And the operator reads the features
    Then feature 1 narrows to "sign up, sign in, reset, sessions"

  # One line, because the intention is read beside the title in a listing. The rest goes in the
  # design.
  Scenario: An intention of two lines is refused
    Given the project's feature "authentication"
    When the operator sets feature 1's intention to "sign up\nsign in"
    Then the control plane refuses it as invalid
    And the operator reads the features
    And feature 1 narrows to ""

  # No length cap refuses text a person wrote. The line is kept whole and a person decides.
  Scenario: An intention past 200 characters warns and is kept
    Given the project's feature "authentication"
    When the operator sets feature 1's intention to 250 characters
    Then the feature write warns "over the 200 characters mark"
    And the operator reads the features
    And feature 1's intention is 250 characters

  Scenario: The features of a project that does not exist are refused
    When the operator reads the features of a project that does not exist
    Then the control plane refuses it as not found

  # A design session names the features it is about to write paths for, and naming one grants it
  # nothing it could not reach by dispatching.
  Scenario: A design session may add a feature
    When the driver adds the feature "authentication"
    And the operator reads the features
    Then the features read 1 in that order

  Scenario: Deleting a project leaves no feature of it anywhere
    Given the project's feature "authentication"
    When the operator deletes the project
    Then no feature of that project is left anywhere
    And reading its features is refused as not found

  # These scenarios run the command line tool as a caller runs it: its own process, its own standard
  # output, its own exit status.

  Scenario: A project with no feature tells the caller how to add one
    Given the system listens on an address the tool can dial
    When the caller reads the features
    Then standard output carries "has no feature yet"
    And standard output carries "krewe feature add"
    And the command succeeds

  Scenario: The caller adds a feature and reads it back
    Given the system listens on an address the tool can dial
    When the caller adds the feature "authentication"
    Then standard output carries "has feature 1: authentication"
    And standard output carries "krewe feature intention"
    And the caller reads the features
    And standard output carries "authentication"
    And standard output carries "open"
    And the command succeeds

  Scenario: Two features print in number order
    Given the system listens on an address the tool can dial
    And the project's feature "authentication"
    And the project's feature "payment"
    When the caller reads the features
    Then standard output lists 2 features in number order
    And the command succeeds

  # The count is of the feature's own steps. No step belongs to a feature yet, so every feature reads
  # none of none, and the count becomes true when the steps arrive under it. Counting the project's
  # steps instead would print one project wide number against every feature of it.
  Scenario: A feature counts the steps under it, and holds none yet
    Given the system listens on an address the tool can dial
    And the project's feature "authentication"
    When the caller reads the features
    Then standard output carries "0/0"
    And the command succeeds

  Scenario: Setting an intention shows it on the next reading
    Given the system listens on an address the tool can dial
    And the project's feature "authentication"
    When the caller sets feature 1's intention to "sign up, sign in, reset, sessions"
    Then standard output carries "narrows to: sign up, sign in, reset, sessions"
    And the caller reads the features
    And standard output carries "sign up, sign in, reset, sessions"
    And the command succeeds

  # A number nobody wrote and a number one past the end read the same to whoever typed it.
  Scenario: A feature number that names no feature is refused, naming the numbers that exist
    Given the system listens on an address the tool can dial
    And the project's feature "authentication"
    And the project's feature "payment"
    When the caller sets feature 9's intention to "sign up, sign in"
    Then standard error says "has no feature 9"
    And standard error says "it has 1, 2"
    And the command fails

  Scenario: Setting an intention without saying which feature is refused
    Given the system listens on an address the tool can dial
    When the caller sets an intention without saying which feature
    Then standard error says "usage: krewe feature intention"
    And the command fails
