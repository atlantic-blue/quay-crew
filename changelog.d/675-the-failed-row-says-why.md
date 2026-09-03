**A person reads why a job failed on the row of `krewe job list`.** A failed row said `failed` and
nothing else. The reason was already on the record and already on the wire, so `krewe job show`
printed it. A person with four failed rows opened four jobs to learn which failure was the work and
which was the machine.

The listing now draws a reason column between the outcome and the title. Each failed row carries its
own reason in it. The column is 40 characters wide. A longer reason is cut there, and the cut is
marked with the character the claim column beside it cuts with. The record keeps the whole text.

The column is only there when a row of the listing has a reason to give. A listing with nothing
failed prints the row it always printed. A pending job the system holds back for room keeps its words
on the line under the listing. That reason is one fact about the machine, and not one fact per row.

The listing that reads every project says the same thing, because it is the same listing.
