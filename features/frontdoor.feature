Feature: The front door says what the system is and how to start it

  The README is the first and often the only thing anybody reads. A reader takes it at its word,
  types what it says, and when that fails they conclude the product is broken rather than the
  sentence. It once ran to 253 lines: a forty item list of what works, a principles list, a stack
  list, a roadmap and a page of prior art. Nobody read any of it, and the list of what works had
  already gone stale.

  So it is held to four things and no more. What the system is, the words for what it holds, one quick
  start, and where to read next. Everything else it used to hold lives in the documents it points at,
  and the scenarios in this directory say what the system actually does.

  Each promise it makes is checked against something with an answer elsewhere in this repository:
  the commands the tool actually has, the targets the Makefile actually declares, and the documents
  that are actually there. It is also held to a length a person reads, and to using no markdown a
  reader cannot copy back out.

  What none of this says is whether a sentence is true. A bullet that claims a capability in words
  naming no command passes every scenario here. The rest of this directory is what says whether a
  capability is real; these say the front door points at it.

  Scenario: It says what the system is, the words for what it holds, how to start it, and where to read next
    When a reader opens the front door
    Then it holds those four parts and no other section

  Scenario: It is short enough that somebody reads it
    When a reader opens the front door
    Then it is shorter than the length a person gives it

  Scenario: It names no command the system does not have
    When a reader opens the front door
    Then every command it says to run is one the system has

  Scenario: It names no build step that is not there
    When a reader opens the front door
    Then every make target it says to run is one the Makefile declares

  Scenario: It sends nobody to a document that is gone
    When a reader opens the front door
    Then every document it points at is there

  Scenario: A first run is one command
    When a reader opens the front door
    Then the quick start is one command to a running system

  Scenario: The picture of a job is where it sends a reader for it
    When a reader opens the front door
    Then the document it names for the job carries a picture of one, through the controller, the lease, the session and the role

  # A word a reader meets for the first time inside a command is a word they guess at. The eleven are
  # the whole of what the system holds, so the list is exact: the twelfth resource is defined here in
  # the change that adds it, or a reader finds it in the help text and has to work out what it is.
  Scenario: It defines every resource the system keeps
    When a reader opens the front door
    Then it defines the eleven resources, in order, and says which of them are not resources

  Scenario: Everything in it can be copied back out
    When a reader opens the front door
    Then it holds no blockquote, no table and no dash used as punctuation

  # A reader's first question is not what a job is. It is how a job differs from the task they already
  # know how to send. The front door used to answer it and now points at the document that does, so
  # what is held is that a reader is still sent somewhere that gives them the answer.
  Scenario: It sends a reader somewhere that says how a task and a job differ
    When a reader opens the front door
    Then the document it names for the words tells a task and a job apart
