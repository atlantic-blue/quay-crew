Feature: The console's keys agree with vim

  The console is shaped like k9s, and k9s is shaped like vim. Two thirds of the keys already agreed
  with vim, and the missing third sat on the keys a vim user presses hardest: an action on the
  horizontal motion keys, refresh on the key that begins every jump, and make on the key that finds
  the next match. So half the keyboard rewarded the reflex and half of it punished it, and none of it
  stuck.

  These scenarios drive the real console over the real control plane, pressing the keys an operator
  presses and reading what they are left looking at. How a key routes is a table test in
  internal/console, where it belongs.

  A key that moved says what to press now. A key that quietly does nothing is the regression this
  repository has already had once, so the way off a key is specified here beside the way onto it.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: The history is on the key that names the view it opens
    Given a session started by dispatching "hello"
    When the operator is at the console
    And the operator presses "t" in the console
    Then the console is on the "tasks" view

  # Each of these was in somebody's fingers. The refusal names the key to press instead, which is the
  # difference between a key that moved and a key that broke.
  Scenario Outline: A key that moved says what to press now
    Given a session started by dispatching "hello"
    When the operator is at the console
    And the operator presses "<pressed>" in the console
    Then the console says to press "<instead>"
    And the console is on the "sessions" view

    Examples:
      | pressed | instead |
      | l       | t       |
      | h       | t       |
      | D       | m       |
      | n       | o       |
      | N       | P       |

  # g held refresh, and holding it there is what kept gg and G off the console entirely. The second
  # key is what says so, because the first is a sequence waiting for its other half.
  Scenario: g does not refresh any more, and says where refreshing went
    Given a session started by dispatching "hello"
    When the operator is at the console
    And the operator presses "gj" in the console
    Then the console says to press "r"

  Scenario: r refreshes
    Given a session started by dispatching "hello"
    When the operator is at the console
    And the operator presses "r" in the console
    Then the console lists that session

  Scenario: o makes one thing
    When the operator is at the console
    And the operator presses "o" in the console
    Then the console is asking what to make

  # The two keys the whole change is for. They cannot exist while g refreshes, which is why the
  # refresh moved rather than the jump.
  Scenario: gg and G go to the first and the last row
    Given a session started by dispatching "one"
    And the operator dispatches "two" to a new session
    And the operator dispatches "three" to a new session
    When the operator is at the console
    And the operator presses "G" in the console
    Then the cursor is on the last row
    When the operator presses "gg" in the console
    Then the cursor is on the first row

  Scenario: A count in front of a move repeats it
    Given a session started by dispatching "one"
    And the operator dispatches "two" to a new session
    And the operator dispatches "three" to a new session
    When the operator is at the console
    And the operator presses "2j" in the console
    Then the cursor is on row 3

  # The console filters rather than searching, so what n jumps through is what the filter last
  # matched. Escape puts every row back on screen and keeps the word, which is the moment jumping
  # between the matches is worth anything: escape used to throw the word away.
  Scenario: n jumps to the next row that matched the filter, once the filter is cleared
    Given a second workspace named "other"
    When the operator is at the console on the "workspaces" view
    And the operator filters for "other" and clears the filter
    Then the console lists both workspaces again
    When the operator presses "n" in the console
    Then the cursor is on the row named "other"
