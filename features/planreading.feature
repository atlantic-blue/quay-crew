Feature: A plan is read by several roles, and only what none of them settled is put to a person

  A plan used to be read by one role, in one session, once. One reading finds what that reading
  looks for. A design named the address shape of a page, /videos?id=<video id>, and nobody asked
  what a person types into it. A test writer asks that first, because an example needs an input and
  an output, and this system holds seventeen roles of which one read the plan.

  The discipline is example mapping run by three amigos: several people with different jobs write
  concrete examples for one rule, and the questions nobody can answer become explicit. So several
  readings run, each in its own session, each given the same plan. A reading writes what its own
  lens could not settle as a row. The reading after it is handed every row still open, and never
  the earlier reader's prose, so it cannot be led by a reading it could not use anyway.

  What every lens left open is what a person is asked, and nothing else is. A gate that put every
  question every reader raised is a gate a person stops reading, and the reader that comes second is
  the thing most likely to answer the first one's question.

  A reading that settles everything asks nobody. That is a result rather than a failure: a graph
  that always asks is an interrogation, and a reading that invented a finding to justify itself
  would be worth less than one that said it found nothing.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "transcript"
    And the workspace holds the roles that read a plan
    And the system holds this flow graph:
      """
      name: three-readers
      version: 1
      mode: edits
      product: a person reads a plan and learns what nobody could settle
      nodes:
        critic:
          type: dispatch
          role: plan-critic
          prompt: "Read the plan. Write down what you cannot settle."
        tester:
          type: dispatch
          role: test-writer
          prompt: "Read the same plan. Still open:\n{{questions.open}}"
        anything:
          type: choice
          on: { questions.open: "" }
        ask:
          type: ask
          text: "nobody could settle these:\n{{questions.open}}"
      edges:
        - [critic, tester]
        - [tester, anything]
        - [anything, done, "true"]
        - [anything, ask, "false"]
        - [ask, done]
      """

  # The whole of it, end to end. The second lens settles the first lens's row and writes the one
  # only it would ask, and that is the row the person gets.
  Scenario: A question only one lens would ask is what reaches the person
    Given the reading "critic" writes down "which store holds the text"
    And the reading "tester" settles row 1 with "the key value store, on demand"
    And the reading "tester" writes down "what does a person type, and what comes back"
    When the operator starts the flow "three-readers" in the project
    Then the run is asking, and the question carries "what does a person type"
    And the question does not carry "which store holds the text"
    And the plan carries 2 questions, 1 of them settled

  # The rows travel to the next reading and the reading behind them does not.
  Scenario: The reading that comes second is handed the open rows and no answer
    Given the reading "critic" writes down "which store holds the text"
    When the operator starts the flow "three-readers" in the project
    Then the reading "tester" was given "which store holds the text"
    And the reading "tester" was not given the reading before it

  # The quiet case, and the one that decides whether this is a gate or an interrogation.
  Scenario: A reading that settled everything asks nobody
    Given the reading "critic" writes down "which store holds the text"
    And the reading "tester" settles row 1 with "the key value store, on demand"
    When the operator starts the flow "three-readers" in the project
    Then the flow run is done
    And nobody was asked anything

  # No reading at all is the same case one step further out: a plan nobody could fault reaches the
  # work, and the run costs two readings and no question.
  Scenario: A plan every lens could settle reaches the work
    When the operator starts the flow "three-readers" in the project
    Then the flow run is done
    And nobody was asked anything
    And the plan carries no questions

  # One lens failing must not take the reading down. What the lenses before it wrote still reaches
  # the person, because the alternative is one broken reading swallowing every row.
  Scenario: One reading failing does not take the reading down
    Given the reading "critic" writes down "what does a person type, and what comes back"
    And the reading "tester" fails
    When the operator starts the flow "three-readers" in the project
    Then the run is asking, and the question carries "what does a person type"

  # And through to what the person does next, which is the half a scenario that stops at the
  # question never sees.
  Scenario: The person answers, and the run carries on to the work
    Given the reading "critic" writes down "what does a person type, and what comes back"
    When the operator starts the flow "three-readers" in the project
    And the operator answers the run with "a link, and the text of it comes back"
    Then the flow run is done
