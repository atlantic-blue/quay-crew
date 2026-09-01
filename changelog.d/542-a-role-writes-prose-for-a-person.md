**A role writes prose for a person outside the work, so a writing job carries the subject and the
material and nothing else.** Fifteen roles wrote contracts, tests, code, infrastructure, security
findings and a marketing plan. None of them wrote a blog post, an announcement or a page, so a job
that had to write one ran as a plain session with no method and no boundary, and everything a role
would have carried was typed into its brief instead: read the voice specification, read three
existing pieces first, do not use these words, state the cost as well as the result, invent no number
that is not in this material. That brief ran to over a thousand words and almost none of it was about
the subject. The next writing job would have typed them again, slightly differently, and the two
pieces would not have read like one person wrote them.

`writer` ships in [`roles/`](roles) at version 1, on opus, receiving `job`, `context` and `skills`.
It reads the voice specification and the three most recent published pieces before it writes a word,
and where there is neither it says so and stops rather than inventing a voice. It refuses two drafts:
one carrying a figure the material does not carry, and one that states no cost. It grants no verb,
which is the whole of holding no numbers of its own. `marketing` may read this system's own records;
a writer with the same grant would have a second source of figures, and the material would stop being
the only one.

Krewe reads no draft, and the brief opens by saying so. Nothing here compares a figure against the
material and nothing refuses a draft that states no cost, so both refusals are the session's to keep.
What the system does is put them in front of it every task, out of a role that is versioned and
reviewed with the code, rather than out of a brief somebody typed again.
