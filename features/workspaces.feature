Feature: Workspaces hold the crew's work

  A workspace is the unit of tenancy. Channels attach to a workspace, secrets belong to a workspace, and
  every session runs inside one. Workspaces are created at runtime through the interface, never
  committed to the repository, so an open source checkout carries no operator's data.

  Background:
    Given a running control plane

  Scenario: Creating a workspace and finding it again
    When the operator creates a workspace named "acme"
    Then the workspace is listed
    And the workspace can be fetched by its id

  Scenario: A workspace id that does not exist
    When the operator fetches the workspace "ghost"
    Then the control plane refuses it as not found

  Scenario: A workspace needs a name
    When the operator creates a workspace named ""
    Then the control plane refuses it as invalid

  # A name is half of an address: "me/house-bills" says which project of which workspace. So a name
  # has to survive being typed on a command line without quoting, and a name containing a slash would
  # break the address rather than be part of it.
  Scenario: A workspace name that could not be part of an address is refused
    When the operator creates a workspace named "House Bills"
    Then the control plane refuses it as invalid
    And the refusal suggests "house-bills"

  Scenario: A workspace name containing a slash is refused
    When the operator creates a workspace named "me/bills"
    Then the control plane refuses it as invalid

  Scenario: Deleting a workspace
    Given a workspace named "acme"
    When the operator deletes the workspace
    Then the workspace is no longer listed

  Scenario: A workspace can be reached by its name instead of its id
    Given a workspace named "acme"
    When the operator refers to the workspace as "acme"
    Then the reference resolves to the workspace

  Scenario: A workspace can still be reached by its id
    Given a workspace named "acme"
    When the operator refers to the workspace by its id
    Then the reference resolves to the workspace

  # An id wins over a name, so a workspace mischievously named after another workspace's id still
  # resolves to itself rather than shadowing the other one.
  Scenario: An id wins over a name that copies it
    Given a workspace named "acme"
    And a second workspace named after the first workspace's id
    When the operator refers to the workspace by its id
    Then the reference resolves to the workspace

  Scenario: A reference matching nothing is refused
    Given a workspace named "acme"
    When the operator refers to the workspace as "ghost"
    Then the reference is refused as not found

  Scenario: A name shared by two workspaces is refused, naming both
    Given a workspace named "acme"
    And a second workspace named "acme"
    When the operator refers to the workspace as "acme"
    Then the reference is refused as ambiguous
    And the refusal names both workspaces

  # An operator needs to know the token is there. What it says is the crew's business: there is no call
  # that returns a value, so this cannot leak one by mistake rather than by policy.
  Scenario: The crew says which secrets a workspace has, and never what they say
    Given a workspace named "acme"
    And the workspace has the subscription token "tok-xyz"
    When the operator asks which secrets the workspace has
    Then it names "CLAUDE_CODE_OAUTH_TOKEN"
    And the answer carries no value

  Scenario: A workspace with nothing set has nothing to list
    Given a workspace named "acme"
    When the operator asks which secrets the workspace has
    Then it names nothing

  Scenario: A channel cannot be attached to a workspace that does not exist
    When the operator attaches a "telegram" channel called "family-chat" to workspace "ghost"
    Then the control plane refuses it as not found

  Scenario: A secret is stored in the secrets backend and never returned
    Given a workspace named "acme"
    When the operator sets the secret "CLAUDE_CODE_OAUTH_TOKEN" to "tok-xyz"
    Then the secrets backend holds "tok-xyz" for that workspace
    And the response carries no secret value

  Scenario: A secret cannot be set on a workspace that does not exist
    When the operator sets the secret "CLAUDE_CODE_OAUTH_TOKEN" to "tok-xyz" on workspace "ghost"
    Then the control plane refuses it as not found
