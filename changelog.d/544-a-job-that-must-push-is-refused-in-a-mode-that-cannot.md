**A job that must push is refused while the operator is looking, rather than after the session is
spent.** A job that names a repository has to clone, push and open a pull request, and every one of
those needs the network. The narrower modes ask a person before they run a network command, and
nobody stands beside a dispatched job, so the approval never arrived. The system held both facts at
the moment of the write, wrote them into the same row, ran the job, asked the session a second time in
the same mode, and only then said that the work was inside the session and nowhere else.

The pair is now refused at the declaration, and the refusal names the repository, the mode and the
mode to declare it in. It is held again once the repository comes from the project and the mode comes
from the system, which is the path nobody types a flag for and the path this was reported on. No row
is written. `model.PermissionModeReachesTheNetwork` is the one place that answers whether a mode may
run a network command, so the declaration and the controller cannot drift apart on it.

The controller no longer asks a second time for a pull request in a mode that could never push. That
ask is a whole task, it asks for the command the mode stops, and it ends where the first task ended.
The job stops with the mode named as the reason instead.

**What this costs.** A job that works in a repository must now say `--mode dangerous`, or run on a
crew whose `QC_PERMISSION_MODE` is set to it. Nothing widens a mode on the job's behalf: a repository
on the project could have made every job declared in it born in the widest mode, and an upgrade that
quietly grants what nobody asked for is the worst way to learn a setting exists.
