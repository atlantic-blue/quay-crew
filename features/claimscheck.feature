Feature: A job verifying a slice reads what the change says about itself

  A page answered "No video with that id" for a video that exists and has captions. The description
  that shipped it said the page reports what it could not read. That sentence is a claim about the
  world, and nothing tested it.

  A pull request body, a commit message and a code comment are the author telling you what they meant
  to write. None of the fifteen roles read that prose against the code it describes, so a run took the
  author's account of the change as a finding about the change. This crew's own rule about never
  showing a rendered sample as observed output had nowhere to live either.

  So the verifier now extracts each checkable claim, tries to break each one against the code, and
  reports the ones it broke. A claim it could not break produces nothing at all, because a report
  that returns every claim is one nobody opens, and the false claim hides inside it.

  The claims are read last. The tracing finishes first, so a claim cannot steer the trace that would
  have caught it.

  What is read below is the memory file in the session's own container, which is what the model
  opens. A rule that reached the store and not the container is a rule nothing ever reads.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And the operator imports the "verifier" role this build ships
    And the operator attaches the "verifier" role to the workspace

  # The rule the rest of it rests on. A comment saying the code rounds down is the same claim the
  # description made, written a second time by the same person.
  Scenario: The prose that came with the change is testimony, and the session is told so
    Given a job titled "verify the captions slice" in the role "verifier"
    When the controller ticks
    Then the brief that session reads carries "The narrative is testimony, not evidence."
    And the brief that session reads carries "A claim repeated in a comment is the same"
    And the brief that session reads carries "Extract each checkable claim."
    And the brief that session reads carries "Try to falsify each one"

  # A claim nobody can locate is a disagreement rather than a finding, so a falsified one carries
  # where the code contradicts it and what the code does instead.
  Scenario: A claim the session breaks is reported with the line that breaks it
    Given a job titled "verify the captions slice" in the role "verifier"
    When the controller ticks
    Then the brief that session reads carries "The file and the line where the code contradicts the claim."
    And the brief that session reads carries "The claim itself, quoted."
    And the brief that session reads carries "What the code does instead."
    And the brief that session reads carries "What goes wrong for a person who believed it."

  # The other half, and the one a check like this loses first. A session that returns every claim it
  # read satisfies the scenario above and is still useless, so the silence is its own scenario.
  Scenario: A claim the session cannot break produces nothing
    Given a job titled "verify the captions slice" in the role "verifier"
    When the controller ticks
    Then the brief that session reads carries "A claim you could not falsify produces nothing."
    And the brief that session reads carries "A claim you could not falsify appears nowhere in this report."

  # Rule 45, which no role stated. A screenshot generated to illustrate and a screenshot captured
  # from a running system look identical on the page and are worth different amounts.
  Scenario: A picture shown as observed output is a claim as well
    Given a job titled "verify the captions slice" in the role "verifier"
    When the controller ticks
    Then the brief that session reads carries "A rendered sample shown as observed output is a claim too."

  # Nothing at run time enforces an order in prose, so what ships is the instruction and the position
  # of the section, and the scenario reads the instruction.
  Scenario: The claims are read after the tracing, and the brief says why
    Given a job titled "verify the captions slice" in the role "verifier"
    When the controller ticks
    Then the brief that session reads carries "Read this section last."
    And the brief that session reads carries "steers the trace"
    And the brief that session reads carries "If the behaviour this change produces broke where it is used, would verification fail?"

  # The method is somebody else's, published under the MIT licence, and the notice travels with the
  # text. It sits in the brief, because the brief is what a reader of the role has in front of them.
  Scenario: The role says where the claims check came from, and under which licence
    Given a job titled "verify the captions slice" in the role "verifier"
    When the controller ticks
    Then the brief that session reads carries "claims-check.md"
    And the brief that session reads carries "licensed MIT"

  # The other direction, so a pass above means the claims check reached the verifier rather than
  # every session. The implementer writes the description this check reads. An author falsifying
  # their own claims is the author marking their own work.
  Scenario: The session writing the change is given none of it
    Given the operator imports the "implementer" role this build ships
    And the operator attaches the "implementer" role to the workspace
    And a job titled "write the captions code" in the role "implementer"
    When the controller ticks
    Then the brief that session reads does not carry "The narrative is testimony, not evidence."
    And the brief that session reads carries "You are the implementer."
