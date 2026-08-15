-- How a secret reaches a sandbox.
--
-- The store still holds bytes under a name and knows nothing else about them. Whether those bytes
-- become an environment variable or a file is a separate choice, which is the shape Kubernetes and
-- Docker both settled on: neither writes the presentation into the store either, and this column is
-- the crew's version of the pod's volume or the service's target.
--
-- The default is the environment, so every secret set before this column existed keeps behaving the
-- way it always did.
alter table secrets add column if not exists projection text not null default 'env';
