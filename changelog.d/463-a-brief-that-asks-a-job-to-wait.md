**A brief that asks a job to wait is refused, and the refusal names the flow.** A job was briefed to
fix a defect, push, watch the checks and merge on green. It did the first two well, then answered
"Ill hold here until the checks land the monitor will notify me on each one", and reported done.
There is no monitor. A job runs once and answers, so a session asked to wait has two moves, both
wrong: hold a container open through a five minute pipeline and pay for it, or answer and stop. It
took a third and invented the wait, and the pull request sat green with nobody left who intended to
merge it.

So the brief is read where a caller declares a job. A brief that asks the job to wait for a forge
pipeline, or to merge on the result of one, is refused while the person who typed it is looking. The
refusal quotes their own words back and names the shape that can do it: a flow, three nodes, a
dispatch that pushes and opens the pull request, a wait, then a choice on the check result. The flow
engine has the wait node, and a job never will.

A step of a flow is not held to it. The graph around a step already holds the wait, so the node after
it merges the pull request and means it, and refusing that would refuse the very graph the refusal
tells a caller to write.

The rule reads English, so it is held narrow, and what it leaves alone matters as much as what it
stops. A waiting word has to point at a pipeline, a merge has to point at a pull request or at the
result of one, and a phrase the brief negates is left alone, so `merge origin/main into the branch`
and `do not merge the pull request` are both still declared. A refusal that fires on ordinary work is
the rule everybody learns to word around.

What this does not do. It does not give a job a wait, and it does not ship the graph: the three nodes
are still written by hand each time, and a brief worded around the rule is still a brief the crew
cannot run.
