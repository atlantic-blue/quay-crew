Feature: A branch on a boolean literal is refused

  A branch whose condition is the bare word `true` or `false` has a side that never runs. One of
  those sat in this repository behind a field that was always empty, and removing it was a slice of
  its own. Nothing stopped the next one going in.

  No linter here reports it. `staticcheck` is already enabled through the standard set and passes
  `if false`, and so does every other linter `golangci-lint` carries with its optional checks turned
  on. The design said adding `staticcheck` would fix this. That was measured and it was wrong, so the
  guard is a command in this repository instead.

  It parses the source rather than matching text. That is what tells a branch apart from the same
  words in a comment, in an identifier such as `falsePositive`, and in a string. A test of the guard
  can then hold the forbidden source as an ordinary string, so no directory of tests is excluded.

  It reads the literal form only. A condition that is always false through a variable still needs a
  person to see it.

  These scenarios run the real command over real directories made for the scenario, so what is proved
  here is what continuous integration runs.

  Scenario: A branch on the literal false is refused
    Given a package whose source is "func f() int { if false { return 1 }; return 0 }"
    When the guard reads the source
    Then the guard refuses
    And it names the file and the line
    And it says what to write instead

  Scenario: A branch on the literal true is refused
    Given a package whose source is "func f() int { if true { return 1 }; return 0 }"
    When the guard reads the source
    Then the guard refuses

  Scenario: A literal branch in an else is refused
    Given a package whose source is "func f(n int) int { if n > 0 { return 1 } else if false { return 2 }; return 0 }"
    When the guard reads the source
    Then the guard refuses

  Scenario: A real condition is let through
    Given a package whose source is "func f(falsePositive bool) int { if falsePositive { return 1 }; return 0 }"
    When the guard reads the source
    Then the guard lets the source through
    And it says how many files it read

  Scenario: The words in a comment are not a branch
    Given a package whose comment is "if false { is what this guard refuses"
    When the guard reads the source
    Then the guard lets the source through

  Scenario: The words in a string are not a branch
    Given a package whose string holds the words "if false {"
    When the guard reads the source
    Then the guard lets the source through

  Scenario: A tree it read no Go source in is refused, because silence is not a clean tree
    Given a directory holding no Go source
    When the guard reads the source
    Then the guard refuses
    And it says it read no Go source
