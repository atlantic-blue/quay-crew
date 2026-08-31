Feature: A pull request that creates infrastructure says the deploy identity may create it

  A job wrote the infrastructure for a service and opened a pull request. Every check went green in
  eleven seconds. The checks were a format check and a validate, and neither one talks to the cloud
  account. The pull request merged, and the deploy died on the first command that did: the identity
  that runs it held read only access, and could not have created any of the six resources.

  The rule against that is a skill, and a skill is a rule a session reads. This is the check. It is a
  hook, so it happens inside the sandbox at the moment the command is about to run, and it holds the
  two halves of the rule a check can be exact about: a pull request that creates infrastructure says
  what the deploy identity may do, and a pull request never opens over an action that came back denied.

  These scenarios run the entry point this build ships, which is the same file a sandbox mounts, and
  they feed it what the model runtime feeds it. The change is a real repository, because the gate reads
  the change rather than being told about it.

  Background:
    Given a running control plane
    And a workspace named "atlantic-blue"
    And a project named "transcript"

  # A gate somebody has to remember to attach is off in every system nobody set up, and that is the
  # system this failure happened in.
  Scenario: A fresh system is under the gate without anybody attaching it
    Given a system seeded with the hooks this build ships
    Then the system holds the "deploy-identity-gate" hook
    And the workspace runs under the "deploy-identity-gate" hook

  # The pull request the incident opened.
  Scenario: A pull request that creates infrastructure and says nothing is refused
    Given a change that creates infrastructure
    When a session is about to open a pull request saying "What. The transcript service, as terraform. Why. It has to run somewhere."
    Then the deploy identity gate refuses it
    And the refusal names the infrastructure it read
    And the refusal says which question to ask and how to ask it

  # There is no way through this one that opens a pull request. The identity cannot create what the
  # change declares, so the report is the deliverable.
  Scenario: A pull request that reports a denied action is refused
    Given a change that creates infrastructure
    When a session is about to open a pull request saying "Deploy identity arn:aws:iam::230345688874:user/terraform_user. s3:CreateBucket implicitDeny"
    Then the deploy identity gate refuses it
    And the refusal says the work is not ready

  # The half that still holds when the change cannot be read at all, which is every sandbox with no
  # git in it.
  Scenario: A denial is refused even where the change says nothing
    Given a change that creates nothing in the cloud
    When a session is about to open a pull request saying "dynamodb:CreateTable explicitDeny, the boundary policy refuses it"
    Then the deploy identity gate refuses it
    And the refusal says the work is not ready

  Scenario: A pull request carrying the report goes through
    Given a change that creates infrastructure
    When a session is about to open a pull request saying "Deploy identity arn:aws:iam::230345688874:role/transcript-deploy. Asked in one call: s3:CreateBucket allowed, dynamodb:CreateTable allowed, lambda:CreateFunction allowed, iam:PassRole allowed."
    Then the deploy identity gate allows it

  # The honest third answer. It is a pass here and a pass nowhere else: it puts the sentence in front
  # of whoever merges, which is the whole of what it buys.
  Scenario: A check that could not run is said out loud, and the pull request opens
    Given a change that creates infrastructure
    When a session is about to open a pull request saying "The deploy identity check did not run: this sandbox holds no cloud credential."
    Then the deploy identity gate allows it

  # The direction that decides whether this gate is worth having. Every role opens a pull request on
  # every slice, and a gate that refuses wrongly stops the system delivering anything.
  Scenario: A change that creates nothing in the cloud goes through
    Given a change that creates nothing in the cloud
    When a session is about to open a pull request saying "What. A page of documentation. Why. Somebody had to ask twice."
    Then the deploy identity gate allows it

  # The words the simulator answers with are also the words anybody uses to explain what it answers,
  # and this pull request is one of those explanations.
  Scenario: A page explaining what the decisions mean is not a report of one
    Given a change that creates nothing in the cloud
    When a session is about to open a pull request saying "allowed is a pass. implicitDeny means nothing grants it, and explicitDeny means something refuses it."
    Then the deploy identity gate allows it

  # It fires on every command every session runs, so a payload it cannot read has to go through.
  Scenario: A payload the gate cannot read lets the command run
    When a session sends the deploy identity gate a payload it cannot read
    Then the deploy identity gate allows it
