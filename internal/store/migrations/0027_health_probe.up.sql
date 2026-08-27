-- One row a health check writes over, so a crew can be asked whether it still takes a write rather
-- than only whether it still answers a read.
--
-- A control plane that served every listing and dispatched nothing reported itself healthy for an
-- hour, because nothing ever asked it to write. One row, written over each time: the question is
-- whether the write lands, and the history of the answers is not worth keeping.
create table if not exists health_probe (
    id         integer     primary key,
    written_at timestamptz not null default now()
);
