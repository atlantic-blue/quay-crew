Feature: A session is given the skills the crew has

  A session opens knowing nothing about how the operator works. A skill is a capability written down
  as code: a brief the model reads, the binaries it needs, the secrets it names, and its own setup.
  The design is in docs/SKILLS.md.

  These scenarios drive the control plane over its real interface. The sandbox is a double, so they
  say what a session is given rather than that a real daemon mounted it; the mounting itself is proved
  against Docker in the sandbox package.

  Background:
    Given a running control plane
    And a workspace named "acme"
    And a project named "house-bills"

  # A crew with no skills is every crew before this, and the memory file must not grow a heading for
  # something that does not exist.
  Scenario: A session with no skills is told nothing about any
    When the operator dispatches "hello" to the project
    Then the session's memory file mentions no skill

  # One line per skill, in the file the session already reads, marked the same way every other section
  # is. The line says the skill exists and where to read it; the brief is a file beside it, opened when
  # that kind of work comes up. A page per skill on every conversation is what this avoids, and the
  # number behind that is measured: this crew's context reached 51,727 bytes at the workspace.
  Scenario: The memory file names a skill and where to read it
    Given the crew has a skill "git" that says "Branch first. Stage named files."
    When the operator dispatches "hello" to the project
    Then the memory file names the "git" skill and where its brief is
    And the memory file does not carry "Branch first. Stage named files."

  # The detail costs nothing until the model opens it, which is the whole reason a brief can be short.
  Scenario: The rest of a skill is mounted, and is not in the memory file
    Given the crew has a skill "git" that says "Branch first."
    And the git skill has a file "reference.md" saying "every flag, at length"
    When the operator dispatches "hello" to the project
    Then the sandbox mounts the git skill read only
    And the memory file does not carry "every flag, at length"

  # A skill is code the operator wrote and the session is given, not something it edits: a session
  # that can rewrite its own instructions can give itself a capability nobody approved.
  Scenario: A session cannot write to its skills
    Given the crew has a skill "git" that says "Branch first."
    When the operator dispatches "hello" to the project
    Then the sandbox mounts the git skill read only

  # The index is rendered from what the session holds, every task. Taken into the crew's context it
  # would be stored, then rendered beside itself, then again, which is exactly what happens to unmarked
  # text in a memory file and is by design. It is marked so it cannot be mistaken for that.
  Scenario: The index is never taken into the crew's own context
    Given the crew has a skill "git" that says "Branch first."
    When the operator dispatches "hello" to the project
    And the operator dispatches "and again" to the same session
    Then no context the crew holds mentions the git skill
    And the memory file names the "git" skill exactly once

  # A capability that silently does not work is worse than one that is absent, because the model
  # improvises around it and the operator reads the improvisation as the answer. So a skill whose
  # secret the workspace has not set is left out of the session entirely, and the listing carries the
  # reason for a person to read.
  #
  # Refusing the whole task was the earlier answer. It made one unusable skill enough to stop every
  # conversation in the workspace, which is the wrong trade the moment a skill is held crew wide
  # rather than attached one workspace at a time.
  Scenario: A skill needing a secret the workspace has not set is left out, and the task still runs
    Given the crew has a skill "github" needing the secret "GH_TOKEN"
    When the operator dispatches "hello" to the project
    Then the reply is "you said: hello"
    And the sandbox does not mount the github skill
    And the memory file does not name the "github" skill

  Scenario: The listing says which secret left a skill out
    Given the crew has a skill "github" needing the secret "GH_TOKEN"
    When the operator dispatches "hello" to the project
    And the operator lists the session's skills
    Then the listing says the "github" skill was left out, needing "GH_TOKEN"

  # Setting the secret is the whole of it, and a sandbox is born with its capabilities, so it is the
  # next sandbox that holds the skill rather than the one already running.
  Scenario: Setting the secret is enough for the next sandbox to hold the skill
    Given the crew has a skill "github" needing the secret "GH_TOKEN"
    When the operator dispatches "hello" to the project
    And the workspace has the secret "GH_TOKEN" set to "ghp-1234"
    And the operator dispatches "a different subject" to a new session
    Then the newest sandbox mounts the crew's github skill read only

  Scenario: A skill needing a binary the image does not carry is refused, and names the image
    Given the crew has a skill "github" needing the binary "gh"
    And the sandbox image does not carry "gh"
    When the operator dispatches "hello" to the project
    Then the control plane refuses it as the wrong state
    And the refusal names the binary and the image to add it to

  # A skill needs a secret set on the workspace, and that is the whole of it. There was once a
  # second list naming which secrets were allowed to reach a sandbox at all, which meant a secret
  # could be set, and its skill attached, and the task still refused for a reason that lived in a
  # file on the host. Setting the secret is the operator saying yes.
  Scenario: A skill's secret reaches the sandbox with nothing else to set
    Given the crew has a skill "github" needing the secret "GH_TOKEN"
    And the workspace has the secret "GH_TOKEN" set to "ghp-1234"
    When the operator dispatches "hello" to the project
    Then the sandbox carries "GH_TOKEN" set to "ghp-1234"

  Scenario: A skill naming the crew's own configuration is refused at import
    When the operator imports a skill whose manifest names the secret "QC_TOKEN"
    Then the crew refuses it saying "the crew's own"
    And the crew holds no imported skills

  # A sandbox is born with its capabilities and never drifts: the mount, the secrets and the setup
  # only ever happen at container creation. So attaching or detaching a skill changes what future
  # sandboxes are born with, and a session whose live sandbox predates the current set is marked
  # stale rather than lied to. Restarting it builds a new sandbox, born with the current skills.
  Scenario: A session whose sandbox predates a new skill is marked stale
    Given the operator imported the "github" skill
    And the workspace has the secret "GH_TOKEN" set to "ghp-1234"
    And a session started by dispatching "hello"
    When the operator attaches the "github" skill to the workspace
    Then the listing marks that session stale

  Scenario: A session whose sandbox holds the current set is not stale
    Given the operator imported the "github" skill
    And the workspace has the secret "GH_TOKEN" set to "ghp-1234"
    And the operator attached the "github" skill to the workspace
    When the operator dispatches "hello" to the project
    Then the listing does not mark that session stale

  Scenario: Stopping and restarting a stale session is born with the current skills
    Given the operator imported the "github" skill
    And the workspace has the secret "GH_TOKEN" set to "ghp-1234"
    And a session started by dispatching "hello"
    And the operator attaches the "github" skill to the workspace
    When the operator stops the session
    And the operator restarts the session
    Then the listing does not mark that session stale
    And the newest sandbox mounts the workspace's github skill read only

  Scenario: A stopped session is never stale
    Given the operator imported the "github" skill
    And the workspace has the secret "GH_TOKEN" set to "ghp-1234"
    And a session started by dispatching "hello"
    And the operator stops the session
    When the operator attaches the "github" skill to the workspace
    Then the listing does not mark that session stale

  # What a session holds is one question with one answer: the same resolver that builds its sandbox
  # answers its listing, so the listing cannot say one thing while the sandbox does another.
  Scenario: The listing for a session says what it actually holds
    Given the crew has a skill "git" that says "Branch first."
    And the operator imported the "notes" skill
    And the operator attached the "notes" skill to the workspace
    And a session started by dispatching "hello"
    When the operator lists the session's skills
    Then the session holds the "git" and "notes" skills

  # The crew's skills directory is one way in and reaches every session. The other is importing a skill
  # into the store and attaching it to a workspace, which is where a credential belongs: a token for one
  # capability should not be handed to every session the crew has.
  #
  # The files travel rather than the path, because the control plane runs in a container where a
  # directory on the operator's machine means nothing, and a crew on a pod has no host directory to go
  # back to for whatever it did not copy.
  Scenario: A workspace with no skills attached says so
    When the operator lists the workspace's skills
    Then the workspace holds no skills

  Scenario: A skill is imported and the crew says what it can do
    When the operator imports the "github" skill
    Then the crew holds the "github" skill
    And the listing says the skill needs "git"
    And the listing names the secret "GH_TOKEN" and what it is for

  # A skill given to the crew is held by every workspace, including the ones made after it, which is
  # the difference between setting a crew up once and setting each workspace up again. The crew's own
  # directory already does this for skills the operator keeps as files. This is the same level reached
  # from the tool, for a skill that was imported, and it takes the word crew where a workspace goes
  # exactly as quay context set does.
  #
  # The files are rendered into each workspace's own directory and mounted from there, the same path a
  # workspace's own skill takes, so the writing out, the sweeping and the staleness all come for free.
  Scenario: A skill the crew holds reaches a workspace that attached nothing
    Given the operator imported the "github" skill
    And the workspace has the secret "GH_TOKEN" set to "ghp-1234"
    When the operator attaches the "github" skill to the crew
    And the operator dispatches "hello" to the project
    Then the memory file names the "github" skill and where its brief is
    And the sandbox mounts the workspace's github skill read only

  Scenario: A workspace created after the crew took a skill holds it too
    Given the operator imported the "github" skill
    And the operator attached the "github" skill to the crew
    When a second workspace named "widgets" with a project
    Then the second workspace holds the "github" skill

  Scenario: The listing says a skill came from the crew
    Given the operator imported the "github" skill
    And the operator attached the "github" skill to the crew
    When the operator lists the workspace's skills
    Then the listing says the "github" skill is held by the crew

  # Two separate statements, and the wider one does not undo the narrower one.
  Scenario: Taking a skill off the crew leaves a workspace's own attachment alone
    Given the operator imported the "github" skill
    And the operator attached the "github" skill to the crew
    And the operator attached the "github" skill to the workspace
    When the operator detaches the "github" skill from the crew
    Then the workspace holds the "github" skill
    And the listing says the "github" skill is the workspace's own

  # The holding is asserted before it is taken away, because a scenario that only checks the end state
  # passes just as happily against a crew attach that never did anything.
  Scenario: Taking a skill off the crew takes it off a workspace that only had it that way
    Given the operator imported the "github" skill
    And the operator attached the "github" skill to the crew
    And the workspace holds the "github" skill
    When the operator detaches the "github" skill from the crew
    Then the workspace holds no skills

  # A crew that starts with nothing makes every operator do the same setup before it can do anything:
  # import each skill, then attach each one to each workspace. The files are in the image already, so
  # a fresh crew is simply given them.
  #
  # These seed from skills/ at the root of this repository, the same directory the image carries, so a
  # shipped manifest that stops loading fails here rather than on somebody's first run.
  Scenario: A fresh crew is given the skills this build ships with
    When the crew starts, seeded from the skills this build ships with
    Then the workspace holds the "git" skill
    And the listing says the "git" skill is held by the crew

  Scenario: A skill a fresh crew was only offered is in the catalogue and not in front of anybody
    When the crew starts, seeded from the skills this build ships with
    Then the crew holds the "terraform" skill
    And the workspace does not hold the "terraform" skill

  # Seeding is what a fresh crew gets, never a policy that reasserts itself. An operator who takes a
  # skill off the crew has said something, and starting the control plane again must not undo it.
  Scenario: Starting again leaves what the operator decided alone
    Given the crew started, seeded from the skills this build ships with
    And the operator detaches the "github" skill from the crew
    When the crew starts, seeded from the skills this build ships with
    Then the workspace does not hold the "github" skill

  Scenario: A crew that already holds a skill is not seeded over
    Given the operator imported the "github" skill
    When the crew starts, seeded from the skills this build ships with
    Then the workspace holds no skills

  Scenario: A malformed skill is refused, and says what is wrong
    When the operator imports a skill whose manifest has no version
    Then the crew refuses it saying "no version"
    And the crew holds no imported skills

  Scenario: A brief longer than a page is refused
    When the operator imports a skill whose brief is longer than a page
    Then the crew refuses it saying "read only when they are needed"

  Scenario: Attaching a skill puts it in front of the workspace's sessions
    Given the operator imported the "github" skill
    When the operator attaches the "github" skill to the workspace
    And the workspace has the secret "GH_TOKEN" set to "ghp-1234"
    And the operator dispatches "hello" to the project
    Then the memory file names the "github" skill and where its brief is
    And the sandbox mounts the workspace's github skill read only
    And the brief the memory file points at is on disk

  Scenario: A skill reaches a session that is already running
    Given the operator imported the "github" skill
    And the workspace has the secret "GH_TOKEN" set to "ghp-1234"
    And a session started by dispatching "hello"
    When the operator attaches the "github" skill to the workspace
    Then the memory file names the "github" skill and where its brief is

  # A brief left behind is a capability the model reads about and no longer has, which is worse than
  # never having had it, because it will try.
  Scenario: Detaching a skill takes it off the sessions that held it
    Given the operator imported the "github" skill
    And the workspace has the secret "GH_TOKEN" set to "ghp-1234"
    And the operator attached the "github" skill to the workspace
    And a session started by dispatching "hello"
    When the operator detaches the "github" skill from the workspace
    Then the memory file does not name the "github" skill
    And the "github" skill's directory is gone from the workspace

  Scenario: Detaching one of two skills leaves the other
    Given the operator imported the "github" skill
    And the operator imported the "notes" skill
    And the workspace has the secret "GH_TOKEN" set to "ghp-1234"
    And the operator attached the "github" skill to the workspace
    And the operator attached the "notes" skill to the workspace
    And a session started by dispatching "hello"
    When the operator detaches the "github" skill from the workspace
    Then the memory file does not name the "github" skill
    And the memory file names the "notes" skill and where its brief is
    And the "github" skill's directory is gone from the workspace
    And the "notes" skill's directory is still there

  Scenario: A skill attached to one workspace does not reach another
    Given a second workspace named "other"
    And the operator imported the "github" skill
    When the operator attaches the "github" skill to the workspace
    Then the second workspace holds no skills

  # A workspace pins the version it attached. Importing a newer revision must not change what a session
  # already using the older one can do.
  Scenario: A newer revision does not move a workspace on its own
    Given the operator imported the "github" skill
    And the operator attached the "github" skill to the workspace
    When the operator imports version 2 of the "github" skill
    Then the workspace still holds version 1 of the "github" skill
    When the operator attaches the "github" skill to the workspace
    Then the workspace holds version 2 of the "github" skill

  Scenario: Importing a different skill at the same version is refused
    Given the operator imported the "github" skill
    When the operator imports a different "github" skill at the same version
    Then the crew refuses it saying "already imported and is a different skill"

  # Context is read back when something inside a sandbox edits it, because an agent writing into its own
  # memory has learned something. A skill is the opposite: it is a capability somebody granted, so an
  # edit from inside is not an edit of it.
  Scenario: A brief edited on the host does not survive
    Given the operator imported the "github" skill
    And the workspace has the secret "GH_TOKEN" set to "ghp-1234"
    And the operator attached the "github" skill to the workspace
    And a session started by dispatching "hello"
    When something rewrites the "github" brief where the session reads it
    And the operator dispatches "and again" to the same session
    Then the "github" brief reads what the crew holds

  # A name can be held by both: the crew's directory and a workspace that imported one. Two mounts on one
  # target is a container that will not start, so the workspace's wins, being the narrower and more
  # deliberate statement of what that workspace should hold.
  Scenario: A workspace's own skill wins over the crew's of the same name
    Given the crew has a skill "github" that says "the crew's own version"
    And the operator imported the "github" skill
    And the workspace has the secret "GH_TOKEN" set to "ghp-1234"
    When the operator attaches the "github" skill to the workspace
    And the operator dispatches "hello" to the project
    Then the sandbox mounts the workspace's github skill read only
    And the memory file names the "github" skill exactly once

  # A build before the index moved wrote it into the session's own memory file. Read back by a build
  # that only knew the mark in the outer file, the whole index was swept into session context, stored
  # as though the operator had typed it, and rendered again on every task from then on. The mark is
  # recognised in every file now, and what sits under it is dropped rather than swept.
  Scenario: A skills index left in the session's own memory file by an earlier build is dropped
    Given the crew has a skill "git" that says "Branch first."
    When the operator dispatches "hello" to the project
    And an earlier build left a skills index in the session's own memory file
    And the operator dispatches "and again" to the same session
    Then the session's context does not mention the git skill
    And the session's own memory file does not carry a skills index

  # Where the sweep already happened, the stored context is the index and nothing else, so the read
  # back has nothing to save and the store has to be put right where it renders.
  Scenario: A skills index already swept into session context is cleaned on the next task
    Given the crew has a skill "git" that says "Branch first."
    When the operator dispatches "hello" to the project
    And the session's stored context is only a swept skills index
    And the operator dispatches "and again" to the same session
    Then the session's context does not mention the git skill

  # The mark is recognised without becoming the default. The last scope in the read back is where
  # unmarked text belongs, so putting the skills mark there would file an agent's appended note as
  # index and drop it, which is how the workspace file quietly lost notes.
  Scenario: A note appended to the workspace memory file is kept as workspace context
    When the operator dispatches "hello" to the project
    And something appends "the vendor prefers invoices on Fridays" to the workspace memory file
    And the operator dispatches "and again" to the same session
    Then the workspace context carries "the vendor prefers invoices on Fridays"

  # A session commits as the operator, and on a repository that requires verified signatures a commit
  # it cannot sign produces a branch nobody can merge. The key is mounted, so it lands in the
  # sandbox as a file and the crew never handles the value at all.
  #
  # An ssh key rather than a gpg one: signing with ssh needs one private key file and no agent, no
  # keyring and no pinentry prompt to hang a task nobody is watching.
  Scenario: A workspace mounting a signing key gets sandboxes that sign
    Given the workspace mounts the secret "GIT_SSH_SIGNING_KEY" holding "-----BEGIN OPENSSH PRIVATE KEY-----"
    When the operator dispatches "hello" to the project
    Then the sandbox was set up to sign commits
    And the sandbox is given the file "/run/secrets/GIT_SSH_SIGNING_KEY" holding "-----BEGIN OPENSSH PRIVATE KEY-----"

  # A private key is the most sensitive thing this crew would carry, so a workspace that has not
  # opted in gets no key, and nothing pointed at one.
  #
  # What such a sandbox is told about signing is in gitconfig.feature. It is told not to sign, which
  # is not the same as being told nothing: the operator's own configuration reaches a session now,
  # and it usually asks for signing.
  Scenario: A workspace with no signing key gets no key
    When the operator dispatches "hello" to the project
    Then no signing key reaches the sandbox

  # The way off the old form. Setting it put the private key in every container's environment for the
  # life of the container, and a refusal that does not say what to type instead is a dead end.
  Scenario: Setting a signing key, rather than mounting it, is refused
    When the operator tries to set the secret "GIT_SSH_SIGNING_KEY" to "-----BEGIN OPENSSH PRIVATE KEY-----"
    Then the crew refuses it, saying to mount the key instead

  Scenario: Setting a gpg key, rather than mounting it, is refused
    When the operator tries to set the secret "GPG_SIGNING_KEY" to "-----BEGIN PGP PRIVATE KEY BLOCK-----"
    Then the crew refuses it, saying to mount the key instead

  # A passphrase in the environment unlocks the key beside it, so it is worth what the key is worth.
  Scenario: Setting a gpg key's passphrase, rather than mounting it, is refused
    When the operator tries to set the secret "GPG_SIGNING_KEY_PASSPHRASE" to "open sesame"
    Then the crew refuses it, saying to mount the key instead

