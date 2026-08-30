Feature: The system reviews a pull request

  Nothing reviewed a pull request. Every review of the acceptance run was done by the operator by
  hand, and a system that opens pull requests all day and reads none of them leaves the reading to one
  person. The graph the system ships reads one open pull request and makes three passes over it, in
  the order a decision needs them: security first, because a security finding blocks the merge
  whatever else is true, then what the change does to the product and what it breaks, then what is
  missing.

  Posting a review is sending a message to a person, so the run stops and shows the operator the
  whole draft. Only a yes posts anything. An answer that is not yes ends the run, and the pull
  request never hears from the system.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"
    And the system holds the flow graph it ships as "pull-request-review"
    And the model does the work every step of that graph expects

  Scenario: A review makes its three passes and stops before it posts anything
    Given the model will answer "https://github.com/atlantic-blue/quay-crew/pull/512"
    And then the model will answer "no secret is in the diff"
    And then the model will answer "the reader is not deployed in any environment"
    And then the model will answer "there is no scenario for the new behaviour"
    And then the model will answer "1. the reader has no policy, so every request fails."
    When the operator starts the flow "pull-request-review" in the project
    Then the run read the pull request for security, then for features, then for completeness
    And the flow run is asking, and the question carries "1. the reader has no policy"
    And nothing has been posted

  Scenario: A review told no ends and posts nothing
    Given the model will answer "https://github.com/atlantic-blue/quay-crew/pull/512"
    And then the model will answer "a pass found something"
    And then the model will answer "1. the reader has no policy, so every request fails."
    When the operator starts the flow "pull-request-review" in the project
    And the operator answers the run with "no"
    Then the flow run is done
    And nothing has been posted

  # The other direction, without which the scenario above would read the same against a graph that
  # can never post at all.
  Scenario: A review told yes posts the draft the operator read
    Given the model will answer "https://github.com/atlantic-blue/quay-crew/pull/512"
    And then the model will answer "a pass found something"
    And then the model will answer "1. the reader has no policy, so every request fails."
    When the operator starts the flow "pull-request-review" in the project
    And the operator answers the run with "yes"
    Then the flow run is done
    And the review was posted, carrying "1. the reader has no policy"

  # There is no trigger node yet, so the graph picks its own subject, and most of the time there is
  # nothing eligible. A run that finds a head commit already reviewed must end rather than review it
  # again and say the same thing to the same person twice.
  Scenario: A run that finds nothing to review ends without reviewing anything
    Given the model will answer "none"
    When the operator starts the flow "pull-request-review" in the project
    Then the flow run is done
    And the run's steps were asked 1 task
    And nothing has been posted
