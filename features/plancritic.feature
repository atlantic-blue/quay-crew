Feature: A role reads the plan before the build and reports what it does not answer

  A tree of jobs built a design document and delivered it complete. Every check was green, and the
  operator opened it two days later and could not use it. Nothing in a run had ever asked whether the
  document was the product. A run reads a design and builds it, and no role reads a design and asks
  whether it holds together.

  The plan critic is that role. It reads the design, the contracts and the build order before any
  code exists, and it reads them against the one sentence the job carries. It reports where the three
  disagree, and where none of them answers the sentence. It writes no code, it changes no file, and
  it declares nothing: its answer is the report.

  The method is imported from spec-kit, which is MIT licensed, and the brief records where each half
  came from. Six classes of finding are the source's. The seventh, a requirement the plan does not
  trace to the sentence, is this crew's, because the source checks a plan against itself and never
  asks whether the plan is the right product.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # The boundary first. A critic that could declare a job could turn its own report into a build, and
  # then nobody has read the plan from outside it.
  Scenario: A session running as the plan critic declares nothing
    Given the workspace lets one session declare 2 jobs
    And a job running as the "plan-critic" role this build ships
    When that session declares a job running as the "test-writer" role
    Then the system refuses it and names the verb it lacks

  # And the other refusal, at the moment the material would be handed over. The standards a plan is
  # checked against are in the context, so a critic reading a plan without it would report on the
  # plan's agreement with nothing.
  Scenario: A plan critic job whose role stopped receiving the context never reaches a container
    Given the operator imports the "plan-critic" role this build ships
    And the operator attached the "plan-critic" role to the workspace
    And a job titled "read the plan" in the role "plan-critic" requiring "context"
    And the operator narrows the "plan-critic" role so it no longer receives the context
    When the controller ticks
    Then the system was asked to run 0 tasks
    And the system built 0 sandboxes
    And the job is stopped, saying the "plan-critic" role does not receive "context"

  # The job it exists to do. The session runs as the role, and what it was told is the brief, so the
  # seven classes and the rule about where a finding sits are in front of it.
  Scenario: The plan critic is told the seven classes and where a finding has to point
    Given the operator imports the "plan-critic" role this build ships
    And the operator attached the "plan-critic" role to the workspace
    And a job titled "read the plan before the build" in the role "plan-critic" requiring "context"
    When the controller ticks
    Then the system was asked to run 1 task
    And the session doing that job runs as the "plan-critic" role
    And that session is told the seven classes a finding can be
    And that session is told to say where each finding is
    And that session is told to change no file

  # The half with no published source. The source checks a plan against itself, and a plan can be
  # perfectly consistent about the wrong product.
  Scenario: The plan critic reads the plan against the sentence the job carries
    Given the operator imports the "plan-critic" role this build ships
    And the operator attached the "plan-critic" role to the workspace
    And a job titled "read the plan before the build" in the role "plan-critic" saying a person "pastes a link and gets the text back"
    When the controller ticks
    Then the session was told a person "pastes a link and gets the text back", and that the sentence wins
    And that session is told to report a requirement the sentence does not ask for

  # A plan that passes, which is the direction a test about catching a bad plan never reaches. A role
  # that reports something every time is a role every run learns to skip, and a role that refuses
  # everything stops every run.
  Scenario: The plan critic is told what a plan that holds up gets
    Given the operator imports the "plan-critic" role this build ships
    And the operator attached the "plan-critic" role to the workspace
    And a job titled "read the plan before the build" in the role "plan-critic" requiring "context"
    When the controller ticks
    Then that session is told to say so in one line where the plan holds up
    And that session is told to invent no finding

  # Where the method came from, kept where a reader of the role finds it. The brief is what krewe role
  # show prints, and it prints no other file.
  Scenario: The role carries the licence and the address the method was read at
    Given the operator imports the "plan-critic" role this build ships
    When the operator reads the "plan-critic" role back
    Then the role comes back with a brief naming its source and its licence
