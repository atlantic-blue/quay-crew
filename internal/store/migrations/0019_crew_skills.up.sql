-- Which skills the whole crew holds, at the version it pinned when it was attached.
--
-- The crew's own directory on disk already reaches every session, and it is the right home for a
-- skill the operator keeps as files. This is the same level reached from the tool instead, for a
-- skill that was imported, so a crew can be set up once rather than each workspace being set up
-- again. A workspace's own attachment stays separate and stays narrower, and wins on a name.
--
-- One row per skill: the crew holds a skill once, at one version. Attaching again moves it.
create table if not exists crew_skills (
    name        text        not null,
    version     integer     not null,
    attached_at timestamptz not null default now(),
    primary key (name),
    foreign key (name, version) references skills (name, version)
);
