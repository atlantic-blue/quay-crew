Feature: An attached operator sees how much of the model's context window is used

  Attaching to a session puts you in the conversation, talking to the model directly. Everything the
  crew knows about that conversation is on the other screen, and one number decides whether the
  conversation is still worth continuing: how much of the model's context window it has filled. It was
  nowhere. Not in the console, not in the panel's header, and asking the model for it costs a task and
  fills a little more of the window to answer.

  So the session says it itself. The model runtime keeps a line under the prompt and redraws it every
  time the conversation moves, and the sandbox image points that line at quay. The line is always
  there, it costs nothing, and from thirty per cent it stops being information and starts being a
  warning: what an operator does about a full window, finishing the task, compacting, or opening a
  fresh session, is much cheaper decided at thirty than at ninety.

  The window's size is the runtime's to say rather than this build's to remember. A runtime that does
  not say says so on the line, because a guessed window is a confident wrong number and a blank line
  reads as the crew being broken.

  Scenario: The line says the share and the count it came from
    Given a conversation that has used 124000 of a 1000000 token context window
    When the runtime draws its status line
    Then the line says "context 12% used (124k of 1M)"

  Scenario: A conversation nobody has spoken in still has a line
    Given a conversation that has used 0 of a 1000000 token context window
    When the runtime draws its status line
    Then the line says "context 0% used (0 of 1M)"

  Scenario: From thirty per cent the line warns
    Given a conversation that has used 300000 of a 1000000 token context window
    When the runtime draws its status line
    Then the line warns that it is over the 30% mark

  Scenario: Under thirty per cent it does not
    Given a conversation that has used 290000 of a 1000000 token context window
    When the runtime draws its status line
    Then the line does not warn

  Scenario: A runtime that does not report the window says so rather than guessing
    Given a model runtime that says nothing about the context window
    When the runtime draws its status line
    Then the line says the runtime does not report it
    And the line claims no share

  Scenario: The sandbox image points the runtime's status line at quay
    Given the settings the sandbox image ships
    Then they tell the runtime to draw its status line by running quay
