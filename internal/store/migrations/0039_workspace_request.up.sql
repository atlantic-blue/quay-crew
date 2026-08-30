-- What one sandbox in a workspace asks the machine for.
--
-- The crew used to admit work by counting it: a workspace said `max running 8`, nine jobs were
-- declared, and the ninth was admitted because eight is not nine. Sandboxes are not the same size,
-- so a count cannot protect a machine. On 30 August 2026 the container runtime filled, stopped
-- answering, and exited with the control plane, the database and eight running jobs inside it.
--
-- So a sandbox declares what it needs, the way a pod declares requests, and the crew adds those up
-- against what its runtime has. These two columns are that declaration, per workspace, because a
-- workspace whose jobs compile is not the same size as one whose jobs read a mailbox.
--
-- Zero on either takes the crew's own measured request, which is what every workspace starts with.
alter table workspace_limits
    add column if not exists request_memory_mib int not null default 0,
    add column if not exists request_processor_percent int not null default 0;
