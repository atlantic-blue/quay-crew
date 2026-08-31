Feature: The operator reads the system in a browser

  A terminal pane is a poor place to read a long reply with code in it. `krewe web` serves the system to
  a browser on the operator's own machine, and it only reads: the interface it holds names no call
  that can change anything, so a page cannot dispatch a task or delete a workspace.

  How a page is built is a table test in internal/web, where it belongs. What cannot be said there is
  this: that the pages carry the control plane's actual sessions and their actual history.

  The view is served to this machine and nowhere else. The control plane listens on a local only port
  behind one shared token, and this server holds that token. Where another device fits was decided on
  31 August 2026: the front door stays on this machine, and the work reaches a phone through a chat
  channel. A wider front door needs a credential for each device, a way to withdraw one device, and a
  rule about encryption on the path. The system holds none of the three, so the refusal names all
  three and sends the reader to the decision in `docs/ARCHITECTURE.md`.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  Scenario: The listing carries every live conversation
    When the operator dispatches "hello" to the project
    And the operator dispatches "a different subject" to a new session
    And the operator opens the web view
    Then the web view lists 2 sessions

  Scenario: A conversation reads back in the browser
    When the operator dispatches "when is the electricity bill due" to the project
    And the operator opens the web view on that session
    Then the page carries "when is the electricity bill due"

  Scenario: A system nobody has spoken to says so, rather than showing an empty page
    When the operator opens the web view
    Then the page carries "no live conversations"

  Scenario: A session the system does not have is not found
    When the operator opens the web view on a session that does not exist
    Then the page is not found

  Scenario: The web view is served to this machine and nowhere else
    When the operator asks for the web view on "0.0.0.0:8080"
    Then the web view refuses, because that address is reachable from another machine

  # The wall is a decision rather than a default, so the refusal has to carry the decision. An
  # operator told only that the address is refused goes to the source to find out why, and the reason
  # he finds there is a code comment nobody is holding to the document that decided it.
  Scenario: The refusal says what the system lacks, rather than only saying no
    When the operator asks for the web view on "0.0.0.0:8080"
    Then the refusal names each thing a wider front door needs, as the architecture document lists them
    And the refusal names the chat channel as the road taken instead

  Scenario: The decision that built the wall is written where the next reader finds it
    Then the architecture document records the decision, the three things and the road taken
