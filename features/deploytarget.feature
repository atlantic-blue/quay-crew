Feature: A project says where it deploys

  A workspace is a bag of secrets and a project is a name, so which cloud account a body of work
  ships to lived in one person's memory. On the acceptance run of 29 August 2026 the operator had to
  say "use atlantic blue instead", then "otherwise where are you going to deploy it?", because
  nothing in the crew held that fact.

  So a project declares it: the account, the region inside it, and the role a pipeline assumes to get
  there. Three values, all of them or none. Half a target reads as an answer to "where does this go"
  and is not one, and the cost of believing it is a tree of jobs that writes correct infrastructure
  for an account it can never reach.

  The identity has to belong to the account the project names. Pasting the role from the other
  account is the mistake that produces exactly that tree of jobs, and it is invisible until a
  pipeline runs.

  Nothing here deploys anything. Infrastructure ships through the repository's own pipeline; this is
  the record of which account that pipeline is aimed at.

  Background:
    Given a running control plane
    And a workspace named "me"
    And a project named "house-bills"

  Scenario: A project nobody has told deploys nowhere
    Then the project deploys nowhere

  Scenario: A project declares where it ships, and every read of it says so
    When the project is declared to deploy to account "123456789012" in "eu-west-2" as "arn:aws:iam::123456789012:role/quay-deploy"
    Then the project deploys to account "123456789012" in "eu-west-2"
    And the listing of projects says the project deploys to account "123456789012"

  # The check the whole record exists for.
  Scenario: A role from another account is refused
    When the project is declared to deploy to account "123456789012" in "eu-west-2" as "arn:aws:iam::999999999999:role/quay-deploy"
    Then the control plane refuses it as invalid
    And the refusal names both accounts
    And the project deploys nowhere

  Scenario: A target missing one of its three is refused
    When the project is declared to deploy to account "123456789012" in "" as "arn:aws:iam::123456789012:role/quay-deploy"
    Then the control plane refuses it as invalid
    And the project deploys nowhere

  Scenario: An account that is not an account is refused
    When the project is declared to deploy to account "atlantic-blue" in "eu-west-2" as "arn:aws:iam::123456789012:role/quay-deploy"
    Then the control plane refuses it as invalid
    And the project deploys nowhere

  Scenario: A region that is not a region is refused
    When the project is declared to deploy to account "123456789012" in "england" as "arn:aws:iam::123456789012:role/quay-deploy"
    Then the control plane refuses it as invalid
    And the project deploys nowhere

  # A wrong account recorded is worse than none recorded, so the door that wrote it opens the other
  # way.
  Scenario: A project can stop shipping anywhere
    Given the project is declared to deploy to account "123456789012" in "eu-west-2" as "arn:aws:iam::123456789012:role/quay-deploy"
    When the project is declared to deploy nowhere
    Then the project deploys nowhere

  Scenario: One project's target is not another's
    Given a second project named "gardening"
    When the project is declared to deploy to account "123456789012" in "eu-west-2" as "arn:aws:iam::123456789012:role/quay-deploy"
    Then the second project deploys nowhere

  Scenario: Saying where a project that does not exist deploys is refused
    When a project that does not exist is declared to deploy to account "123456789012" in "eu-west-2" as "arn:aws:iam::123456789012:role/quay-deploy"
    Then the control plane refuses it as not found
