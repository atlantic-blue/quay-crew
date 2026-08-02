Feature: Projects hold the crew's work

  A project is the unit of tenancy. Channels attach to a project, secrets belong to a project, and
  every session runs inside one. Projects are created at runtime through the interface, never
  committed to the repository, so an open source checkout carries no operator's data.

  Background:
    Given a running control plane

  Scenario: Creating a project and finding it again
    When the operator creates a project named "acme"
    Then the project is listed
    And the project can be fetched by its id

  Scenario: A project id that does not exist
    When the operator fetches the project "ghost"
    Then the control plane refuses it as not found

  Scenario: A project needs a name
    When the operator creates a project named ""
    Then the control plane refuses it as invalid

  Scenario: Deleting a project
    Given a project named "acme"
    When the operator deletes the project
    Then the project is no longer listed

  Scenario: A channel cannot be attached to a project that does not exist
    When the operator attaches a "telegram" channel called "family-chat" to project "ghost"
    Then the control plane refuses it as not found

  Scenario: A secret is stored in the secrets backend and never returned
    Given a project named "acme"
    When the operator sets the secret "CLAUDE_CODE_OAUTH_TOKEN" to "tok-xyz"
    Then the secrets backend holds "tok-xyz" for that project
    And the response carries no secret value

  Scenario: A secret cannot be set on a project that does not exist
    When the operator sets the secret "CLAUDE_CODE_OAUTH_TOKEN" to "tok-xyz" on project "ghost"
    Then the control plane refuses it as not found
