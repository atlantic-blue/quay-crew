Feature: A job verifying a slice is told how to tell a green check from a real one

  Take a slice that changes how a total is rounded. Its test calls the function, then asserts that
  the call came back without an error. It never looks at the number. The test was green before the
  change, it is green after it, and it stays green if the rounding goes the wrong way. Nothing in
  that suite can fail, so the green check is not evidence of anything.

  The verifier is the role that reads a finished slice. Its summary already promised this: it checks
  that a slice satisfies its contracts and is wired in, not only that its tests are green. It had no
  method for telling one from the other, so the answer came out of whatever the model already
  believed about tests. Two changes went past this crew's own checks that way.

  So the brief asks one question, names the three shapes a gap takes, lists what does not count as a
  test at all, and drops a finding that carries no file, no line and no search behind it. The method
  is a rewrite of one published under the MIT licence, and the role says so where a reader of the
  role reads it.

  What is read below is the memory file in the session's own container, which is what the model
  opens. A method that reached the store and not the container is a method nothing ever reads.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And the operator imports the "verifier" role this build ships
    And the operator attaches the "verifier" role to the workspace

  # The shape that ships wrong, first. This is the test the crew keeps writing, and the brief has to
  # name it as no cover at all rather than count it among the tests that pass.
  Scenario: A test that runs the code and never checks the result is not cover, and the brief says so
    Given a job titled "verify the rounding slice" in the role "verifier"
    When the controller ticks
    Then the brief that session reads carries "A test that runs the changed code and never checks the changed result."
    And the brief that session reads carries "A test that mocks away the integration the change is about."
    And the brief that session reads carries "A check that only asserts that no error was thrown."
    And the brief that session reads carries "An assertion against source text rather than against a run."

  Scenario: The session is asked one question, and given the three shapes a gap takes
    Given a job titled "verify the rounding slice" in the role "verifier"
    When the controller ticks
    Then the brief that session reads carries "If the behaviour this change produces broke where it is used, would verification fail?"
    And the brief that session reads carries "A regression gap."
    And the brief that session reads carries "A missing adoption gap."
    And the brief that session reads carries "A broken verification gap."

  # A gap nobody can locate and nobody can repeat is a sentence rather than a finding. So the report
  # carries where the behaviour is, and the search that found nothing covering it.
  Scenario: Every gap the session reports carries a file, a line and the search behind it
    Given a job titled "verify the rounding slice" in the role "verifier"
    When the controller ticks
    Then the brief that session reads carries "The file and the line of the behaviour that nothing protects."
    And the brief that session reads carries "The search that grounds it"
    And the brief that session reads carries "Read a test before you say what it covers."
    And the brief that session reads carries "Never assert what you did not verify."

  # The method adds reading to do and no writing. A verifier that fixed what it found would be the
  # author of the code it then judges.
  Scenario: The session reads and changes nothing
    Given a job titled "verify the rounding slice" in the role "verifier"
    When the controller ticks
    Then the brief that session reads carries "You never modify code, tests, or any files."

  # The method is somebody else's, published under the MIT licence, and the notice travels with the
  # text. It sits in the brief rather than only in the document that chose it, because the brief is
  # what a reader of the role has in front of them.
  Scenario: The role says where the method came from, and under which licence
    Given a job titled "verify the rounding slice" in the role "verifier"
    When the controller ticks
    Then the brief that session reads carries "bmad-code-org/BMAD-METHOD"
    And the brief that session reads carries "licensed MIT"

  # The other direction, so a pass above means the method reached the verifier rather than every
  # session. The implementer writes the code. A session judging its own tests by this method is the
  # author marking their own work.
  Scenario: The session writing the code is given none of it
    Given the operator imports the "implementer" role this build ships
    And the operator attaches the "implementer" role to the workspace
    And a job titled "write the rounding code" in the role "implementer"
    When the controller ticks
    Then the brief that session reads does not carry "If the behaviour this change produces broke where it is used, would verification fail?"
    And the brief that session reads carries "You are the implementer."
