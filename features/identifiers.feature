Feature: One identifier reaches every surface

  A session carries two identifiers. The id is the crew's own: it is the primary key of the row and
  it names the session's container. The handle is the name a channel dispatches to, because a chat
  channel knows what it calls a conversation before the crew has a session for it. It sends that
  name, and the crew makes the session under it. When nobody supplies one, the crew mints one.

  A listing prints the id, in a column headed session, and the name column carries what the operator
  called the session and nothing else. The handle stays valid, because it is in notes and in scripts
  and it is what a channel sends, but it is no longer printed as though it were a name.

  This is the rule these scenarios hold up: whatever the listing prints, every surface takes.

  Background:
    Given a running control plane
    And a workspace named "me"
    And a project named "house-bills"

  Scenario: The listing heads its identifier with the word every command takes
    Given a session started by dispatching "remember this"
    Then the listing heads its first column "session"
    And the first cell of that row is the session id

  Scenario: The name column carries a name and nothing else
    Given a session started by dispatching "remember this"
    Then the name cell of that row is empty
    When the operator labels the session "the bills"
    Then the name cell of that row reads "the bills"

  Scenario: Every surface takes the identifier the listing prints
    Given a session started by dispatching "remember this"
    When the operator copies the identifier out of the listing
    Then dispatch on what was copied continues that session
    And tasks on what was copied lists that session's history
    And attach on what was copied opens that session's conversation
    And label on what was copied names that session
    And mode on what was copied sets that session's mode

  # The row that used to have nothing typeable on it at all: the label took the handle's place, and
  # the id was refused.
  Scenario: Every surface takes it on a session somebody has named
    Given a session started by dispatching "remember this"
    And the operator labels the session "the bills"
    When the operator copies the identifier out of the listing
    Then dispatch on what was copied continues that session
    And tasks on what was copied lists that session's history
    And attach on what was copied opens that session's conversation
    And label on what was copied names that session
    And mode on what was copied sets that session's mode

  # Neither form is withdrawn, so the handle has to keep reaching the session from every surface too.
  Scenario: Every surface still takes the handle, which is what a channel sends
    Given a session started by dispatching "remember this"
    When the operator copies the handle out of the crew
    Then dispatch on what was copied continues that session
    And tasks on what was copied lists that session's history
    And attach on what was copied opens that session's conversation
    And label on what was copied names that session
    And mode on what was copied sets that session's mode

  Scenario: Every surface still takes the address
    Given a session started by dispatching "remember this"
    When the operator copies the address of that session
    Then dispatch on what was copied continues that session
    And tasks on what was copied lists that session's history
    And attach on what was copied opens that session's conversation
    And label on what was copied names that session
    And mode on what was copied sets that session's mode

  # The identifier used to become the first word of the message, and the task went to a new session.
  Scenario: A bare identifier continues that session rather than joining the message
    Given a session started by dispatching "remember this"
    When the operator types the identifier and then "and again"
    Then the reply is "you said: and again"
    And both tasks ran in the same session

  # The way off the old behaviour. A word shaped like an identifier that names nothing must stop,
  # never be absorbed.
  Scenario: A word shaped like an identifier that names no session is refused
    Given a session started by dispatching "remember this"
    When the operator types "ffffffffff" and then "and again"
    Then the dispatch is refused
    And the refusal names the identifier the listing prints
    And the refusal says how to send it as the message instead
    And that session was left with 1 task
    And the crew holds 1 session

  Scenario: An ordinary first word is still the start of the message
    When the operator types "hello" and then "there"
    Then the reply is "you said: hello there"

  # Enter is driven through the console's own reducer, the command it produces is run, and the answer
  # is fed back the way the runtime feeds it. What is asserted is where the operator is left.
  Scenario: Enter on a console row opens that session
    Given a session started by dispatching "remember this"
    When the operator opens the console over the crew
    And the operator presses the enter key on the session row
    Then the conversation that opened belongs to that session
    And the console is back on its list with nothing to report
    And the next key still works

  Scenario: A conversation that cannot be opened says why, and the refresh does not blank it
    Given a session started by dispatching "remember this"
    And the terminal cannot run what the console starts
    When the operator opens the console over the crew
    And the operator presses the enter key on the session row
    Then the console says why the conversation did not open
    And the refreshed list is under it, with the reason still on the screen
    And the next key clears the reason
