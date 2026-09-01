Feature: A job that goes in circles is stopped and escalated rather than left running

  A session that stops making progress keeps going anyway. On the acceptance run of 30 August 2026 a
  session that could not get a check green tried the same shape of fix several times and gave the same
  reasoning each time. Nothing compared what it had just produced against what it produced before, so
  from outside a session going nowhere and a session working hard were one picture: a phase word and a
  growing bill. The operator was the loop detector, and only where he happened to read the transcript.

  So every attempt at a step goes on the record with how like the earlier attempts at that step it
  was, measured on the runs of three words they share. Three attempts the system cannot tell apart
  stop the step, and the job escalates by the route it declared: the question goes to the operator, or
  the work is handed to another role in a conversation of its own, carrying what the earlier attempts
  said so the new one does not make them again. A job escalates once, because escalating a second time
  would be the system going round the same loop with more steps in it.

  The threshold is provisional and the direction of its error is deliberate. A detector that fires on
  real progress stops work that was going to finish, so the number sits an order of magnitude above
  anything different work scores, and a session that rewords its reasoning each time is not caught.
  What replaces the number is the record this now keeps.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # The routes are refused where somebody is typing them. A route the system cannot carry out would
  # otherwise be found at the moment a job is going in circles, which is the moment there is least to
  # spare, and a route that quietly does nothing reads as a decision that was taken.
  Scenario: Escalating onto another model is refused, because this build runs one model for the whole system
    When a job is declared escalating by "model:opus"
    Then the system refuses it, saying a role that runs on that model is the way to say it

  Scenario: Escalating to a role the workspace does not hold is refused, and the refusal names the role
    When a job is declared escalating by "role:archivist"
    Then the system refuses it, naming the role

  Scenario: A word that is not a route is refused, and the two that are, are offered
    When a job is declared escalating by "retry"
    Then the system refuses it, offering ask and role

  # The case that matters most. A detector that fires on real progress is worse than none, because it
  # stops work that was going to finish.
  Scenario: Three attempts that each said something different are not a loop
    Given a job titled "get the coverage check green"
    When the attempt fails saying "the parser has no case for an empty file, so I am adding one"
    And the operator continues the job
    And the attempt fails saying "the migration runs twice against a fresh database, so the guard moves"
    And the operator continues the job
    And the attempt fails saying "the sandbox image carries no git, so the clone in step two cannot run"
    Then the job is failed rather than escalated
    And the record says what each attempt said, and how alike it was

  Scenario: Three attempts the system cannot tell apart stop the step and put the question to the operator
    Given a job titled "get the coverage check green"
    When the attempt fails saying "the check is still red, so I will try the same fix once more"
    And the operator continues the job
    And the attempt fails saying "the check is still red, so I will try the same fix once more"
    And the operator continues the job
    And the attempt fails saying "the check is still red, so I will try the same fix once more"
    Then the job is asking, and says it went in circles on step 1
    And the question carries what the attempts said
    And the system was asked to run 3 tasks

  # A job answered is a job that got there, however like the attempt before it the answer reads. The
  # detector must never take work away from a session that has just finished it.
  Scenario: An attempt that finished the job is never a loop
    Given a job titled "get the coverage check green"
    When the attempt fails saying "the check is still red, so I will try the same fix once more"
    And the operator continues the job
    And the attempt fails saying "the check is still red, so I will try the same fix once more"
    And the operator continues the job
    And the attempt answers "the check is still red, so I will try the same fix once more"
    Then the job is done

  # The change. Detecting a loop and leaving the job where it is spends the rest of the budget the
  # same way, so what the job declared happens instead.
  Scenario: A job that declared a role is handed to it, in a conversation of its own
    Given the workspace holds a role called "architect"
    And a job titled "get the coverage check green" that escalates by "role:architect"
    When the attempt fails saying "the check is still red, so I will try the same fix once more"
    And the operator continues the job
    And the attempt fails saying "the check is still red, so I will try the same fix once more"
    And the operator continues the job
    And the attempt fails saying "the check is still red, so I will try the same fix once more"
    Then the job is going again, handed to the architect role
    And the next task runs in a conversation of its own, as that role
    And the next task carries what the earlier attempts said, and says not to make them again

  # Escalating twice would be the system going round the same loop with more steps in it. The
  # attempts at a step are counted across the handoff on purpose: a new role saying what the last one
  # said is the handoff itself changing nothing, and the second attempt at the work is then the thing
  # a person has to read.
  Scenario: A job that goes in circles again after it escalated is stopped rather than escalated twice
    Given the workspace holds a role called "architect"
    And a job titled "get the coverage check green" that escalates by "role:architect"
    When the attempt fails saying "the check is still red, so I will try the same fix once more"
    And the operator continues the job
    And the attempt fails saying "the check is still red, so I will try the same fix once more"
    And the operator continues the job
    And the attempt fails saying "the check is still red, so I will try the same fix once more"
    And the attempt fails saying "the check is still red, so I will try the same fix once more"
    Then the job is stopped, and the reason says it went in circles again after being escalated
    And the job still says it was handed to the architect role
