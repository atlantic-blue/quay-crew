Feature: The front door says what the crew can actually do

  The README is the first and often the only thing anybody reads. A reader takes it at its word,
  types what it says, and when that fails they conclude the product is broken rather than the
  sentence. Its list of what works once predated a whole day of merged work, and nothing anywhere
  said so.

  So the front door is held to the three things it can be checked against: the commands the tool
  actually has, the targets the Makefile actually declares, and the documents that are actually
  there. It is also held to being one command to a running crew, to carrying the picture of a piece
  of work, and to using no markdown a reader cannot copy back out.

  What none of this says is whether a sentence is true. A bullet that claims a capability in words
  naming no command passes every scenario here. The rest of this directory is what says whether a
  capability is real; these say the front door points at it.

  Scenario: It names no command the crew does not have
    When a reader opens the front door
    Then every command it says to run is one the crew has

  Scenario: It names no build step that is not there
    When a reader opens the front door
    Then every make target it says to run is one the Makefile declares

  Scenario: It sends nobody to a document that is gone
    When a reader opens the front door
    Then every document it points at is there

  Scenario: A first run is one command
    When a reader opens the front door
    Then the quick start is one command to a running crew

  Scenario: It carries the picture of a piece of work
    When a reader opens the front door
    Then it shows a piece of work through the controller, the lease, the session and the role

  Scenario: Everything in it can be copied back out
    When a reader opens the front door
    Then it holds no blockquote, no table and no dash used as punctuation
