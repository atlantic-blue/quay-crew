Feature: The operator can find the files the model reads

  Context is files on the operator's machine, mounted into every sandbox: one directory per workspace
  and one per project, with CLAUDE.md in either read by the model as its memory. The mounts have
  existed for a while and nothing said where they were, so the feature worked and nobody could find
  it: both directories were empty and the identifiers in their paths are twenty four characters of
  hexadecimal.

  The control plane answers this rather than either client working it out, because the tool runs on
  the operator's machine and the layout belongs to the crew. The console and the command line are two
  clients of one call, which is the only way the two cannot drift.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: The crew says where a project's context lives
    When the operator asks where context lives
    Then it names the crew, a workspace and a project
    And it names a workspace directory and a project directory
    And each one says where it appears inside a sandbox
    And each one names the memory file the model reads

  Scenario: A directory with nothing in it says so
    When the operator asks where context lives
    Then no context has been written yet

  Scenario: Setting a project's context is what the listing then reports
    When the operator sets the project's context to "pay the water bill first"
    And the operator asks where context lives
    Then the project's context has been written
    And the project's context reads "pay the water bill first"

  # The store is where context lives, and the file in a sandbox is a rendering of it, written when the
  # sandbox is made. Rendering it is the whole point: the model only reads files.
  Scenario: A project's context reaches the file its sessions read
    When the operator sets the project's context to "pay the water bill first"
    And the operator dispatches "hello" to the project
    Then the session's memory file carries "pay the water bill first"

  # Four levels, two files. The outer two are in the conversation store every session in the workspace
  # reads; the inner two are in this session's own working directory.
  Scenario: Every level reaches the session that should read it
    When the operator sets context at scope "crew" to "no acronyms"
    And the operator sets the project's context to "pay the water bill first"
    And the operator dispatches "hello" to the project
    Then the session's memory file carries "pay the water bill first"
    And the workspace's memory file carries "no acronyms"

  # An agent that writes something into its own memory has learned something. Overwriting that on the
  # next task would make the crew's memory strictly worse than a text file, so the file wins and is
  # taken into the store.
  Scenario: What an agent writes into its own memory is kept
    Given a session started by dispatching "hello"
    When something inside the sandbox writes "the account number is 4471" into its memory
    And the operator dispatches "and again" to the same session
    Then the session's context reads "the account number is 4471"

  # Context only ever reached a sandbox when that sandbox was made, so telling a session something
  # while it was running did nothing you could see until it was replaced, and nobody replaces a
  # container to deliver a note.
  Scenario: A context change reaches a session that is already running
    Given a session started by dispatching "hello"
    When the operator sets the project's context to "pay the water bill first"
    Then the session's memory file carries "pay the water bill first"

  # A memory file with none of the crew's marks was written by somebody who had never seen what the
  # store holds, so it is not an edit of it. Read as one it replaced the store's body outright, which
  # is how a driver taught what quay is lost the manual again moments later.
  Scenario: A memory file the crew never wrote does not replace what the store holds
    Given a session started by dispatching "hello"
    When the operator sets the session's context to "the meter is under the stairs"
    And the sandbox's memory file is replaced with "notes from before any of this" and no marks
    And the operator dispatches "and again" to the same session
    Then the session's memory file carries "the meter is under the stairs"
    And the session's memory file carries "notes from before any of this"

  Scenario: A scope the crew does not have is refused
    When the operator sets context at scope "everything" to "no"
    Then the control plane refuses it as invalid

  Scenario: One workspace directory however many projects it holds
    Given a second project named "gardening"
    When the operator asks where context lives
    Then it names 1 workspace directory and 2 project directories

  # A session sitting beside the console knows nothing about the crew it is next to. The manual is
  # quay describing itself, and loading it as a project's context is how a session is told.
  Scenario: The manual can be loaded as a project's context
    When the operator loads the manual as the project's context
    And the operator asks where context lives
    Then the project's context names the words a crew is made of
    And the project's context says how to set a context

  # A level could be written and never read back, so it could only be overwritten. Adding a paragraph
  # meant already holding the whole text, and during an acceptance run the only way to recover a
  # workspace's context before adding to it was to read the contexts table in the database directly.
  #
  # These scenarios run the command line tool as a caller runs it: its own process, its own standard
  # output, its own exit status. What is specified is what a redirection captures, and none of that
  # exists inside a test process.

  Scenario: A level says what it says, and standard output carries that and nothing else
    Given the crew listens on an address the tool can dial
    And the operator sets the project's context to "pay the water bill first"
    When the caller reads the project's context back
    Then standard output is exactly "pay the water bill first"
    And the command succeeds

  Scenario: Reading a level out and writing it back leaves it unchanged
    Given the crew listens on an address the tool can dial
    And the operator sets the project's context to "pay the water bill first"
    When the caller reads the project's context back
    And the caller writes back what it read
    And the caller reads the project's context back
    Then standard output is exactly "pay the water bill first"

  # The whole point of the read: a level can be added to now, where before a caller had to already
  # hold the whole text to change one paragraph of it.
  Scenario: A level is added to rather than overwritten
    Given the crew listens on an address the tool can dial
    And the operator sets the project's context to "pay the water bill first"
    When the caller reads the project's context back
    And the caller writes back what it read with "then the electricity bill" added
    And the caller reads the project's context back
    Then standard output carries "pay the water bill first"
    And standard output carries "then the electricity bill"

  # The crew's level is the one every session in the crew is told, and the one the editor refuses by
  # name. It is read by the same word that writes it.
  Scenario: The crew's own level is read by name
    Given the crew listens on an address the tool can dial
    And the operator sets context at scope "crew" to "no acronyms"
    When the caller reads the crew's context back
    Then standard output is exactly "no acronyms"
    And the command succeeds

  # Silence is what a broken read looks like too. A redirection writes an empty file either way, so
  # the exit status is the only thing that tells a caller which of the two it got.
  Scenario: A level that says nothing is refused rather than printed as silence
    Given the crew listens on an address the tool can dial
    When the caller reads the project's context back
    Then standard output is empty
    And standard error says "says nothing yet"
    And the command fails
