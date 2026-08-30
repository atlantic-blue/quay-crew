alter table workspace_limits
    drop column if exists request_memory_mib,
    drop column if exists request_processor_percent;
