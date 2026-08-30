**Nothing reviewed a pull request, so now a flow does.** Every review of the acceptance run was done
by the operator by hand. The crew shipped two flow graphs and both belonged to the transcript
project, so the crew opened pull requests all day and read none of them.

[flows/pull-request-review.yaml](flows/pull-request-review.yaml) reads one open pull request and
makes three passes over it, in the order a decision needs them. Security first, because a security
finding blocks the merge whatever else is true. Then what the change does to the product and what it
breaks, which is the pass that says whether the thing works end to end today. Then what is missing:
the tests at the three tiers, the scenario, the changelog entry, the way off an interface that was
removed. Each pass reads the result of the one before it, reports nothing a linter already reports,
and says so in one line where it finds nothing.

It posts nothing on its own. The run stops at an `ask` node and shows the operator the whole draft,
and only a yes posts it. An answer that is not yes ends the run and the pull request never hears from
the crew.

There is no trigger node yet, [#433](https://github.com/atlantic-blue/quay-crew/issues/433), so the
graph picks its own subject on a schedule and skips a head commit that already carries a review. A
run that finds nothing eligible costs one step and ends.

Two things this does not do. No test proves a pass finds a real defect, because the model does the
finding; what is held is the shape of the flow, what the operator is shown, and that nothing is
posted without a yes. And two runs started fifteen minutes apart can both pick the same pull request
while the first one is still waiting to be answered, because eligibility is read from the forge and
the first run has posted nothing yet.
