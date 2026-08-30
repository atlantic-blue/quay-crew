Feature: What this build does is a command, and not a view

  The specification is embedded in the binary, so the answer to "what does this thing do" travels
  with the build. The scenarios in it fail the build when they stop being true. The command that
  prints them is quay features.

  The console listed the same scenarios in a view of its own. Its second column was headed "proved
  by". The column named a scenario. It never said whether that scenario passed on this build, so it
  claimed evidence that nobody had checked. The view also asked the control plane nothing. It said
  the same thing whichever system was on screen.

  So there is one list, and it is the command. The console's command bar runs commands, so the word
  still works there. The list arrives in the output panel. The short spellings that opened the view
  have no command behind them, so each one says what to type instead. A word that becomes an
  unknown command reads as the console being broken.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: The command prints what this build does
    Given the system listens on an address the tool can dial
    When the caller asks what this build does
    Then standard output names a feature of this build
    And standard output names a scenario under that feature
    And the command succeeds

  Scenario: The console has no view for it
    Then typing "features" in the console opens nothing

  Scenario: Typing the word in the console prints the list rather than opening a view
    When the operator types "features" in the console
    Then the console shows what this build does

  Scenario Outline: A word that opened the view says what to type instead
    When the operator types "<gone>" in the console
    Then the console says to type "features"
    And the console never reached the tool

    Examples:
      | gone         |
      | f            |
      | feature      |
      | capabilities |
