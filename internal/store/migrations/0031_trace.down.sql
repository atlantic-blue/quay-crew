alter table work_events drop column if exists seq;
alter table work_events drop column if exists trace_id;
alter table work drop column if exists parent_span_id;
alter table work drop column if exists trace_id;
alter table tasks drop column if exists trace_id;
