Feature: A session says where its context went

  Context is the budget that decides how good the work is. The system says how full a session's window
  is and stops there, so a session at eighty per cent could have filled up on the code it had to read,
  on tool output it read once, or on its own repeated attempts, and nobody could say which. A share
  that cannot be acted on is a display.

  So every conversation is read back and split four ways. Reads is the contents of files, however the
  session opened them, with a reading tool or with a reading command in the shell. Tools is what every
  other tool returned. Turns is the session's own words, what it wrote, what it thought and the calls
  it made. Told is what reached it from outside a tool: the exec it was given and the answers to its
  questions.

  The count is characters, not tokens. The transcript holds text, every model counts tokens its own
  way, and a token count worked out here would be a made up number sitting beside the real one in the
  context column.

  The four add up to the total, and the total is held against the model's own count of the same
  context, because a breakdown whose parts do not add up to the model's total is a number that will be
  trusted and is wrong. The part no transcript holds, the system prompt and the definitions of every
  tool the session carries, is named rather than folded into the four.

  It costs nothing to say. The cost, the window and the breakdown all come out of one pass over the
  transcript, kept until the file changes.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: A session nobody has spoken in reports no breakdown, rather than four noughts
    Given the operator dispatches "hello" to the project
    When the operator lists the sessions
    Then the session reports no breakdown of its context

  Scenario: A session that read a large file shows that read in its breakdown
    Given the operator dispatches "hello" to the project
    And the session read 40000 characters of a file
    When the operator lists the sessions
    Then the session reports 40000 characters read from files
    And the listing says its context went on "reads"

  # The command decides, not the tool. A test run and a file are the same tool and the same shape of
  # result, and putting them in one column would answer the question this exists to answer with a
  # number nobody can act on.
  Scenario: Output from a command that was not reading a file is not a read
    Given the operator dispatches "hello" to the project
    And the session ran a command that printed 40000 characters
    When the operator lists the sessions
    Then the session reports nothing read from files
    And the session reports 40000 characters of tool output
    And the listing says its context went on "tools"

  Scenario: A session that spent its context on its own turns says so
    Given the operator dispatches "hello" to the project
    And the session wrote 40000 characters of its own
    When the operator lists the sessions
    Then the session reports 40000 characters of its own turns
    And the listing says its context went on "turns"

  Scenario: The four parts add up to the total
    Given the operator dispatches "hello" to the project
    And the session read 40000 characters of a file
    And the session ran a command that printed 20000 characters
    And the session wrote 10000 characters of its own
    When the operator lists the sessions
    Then the four parts of the breakdown add up to its total

  # The check the whole measurement rests on. The transcript holds the conversation and nothing else,
  # so a breakdown that accounted for the whole window would be the suspicious answer.
  Scenario: The total is held against the model's own count of the context
    Given the operator dispatches "hello" to the project
    And the session read 190000 characters of a file
    And the model says it carries 125000 tokens of context
    When the operator lists the sessions
    Then the breakdown accounts for 80 per cent of what the model counted
    And what it does not account for is named
