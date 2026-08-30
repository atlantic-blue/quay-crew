**The room view says what the machine has left.** It was one line per sandbox, so it answered which
session to stop and never whether a session had to be stopped at all. An operator could read eighteen
rows of megabytes, add them up, and still not know how close the machine was: there was no total, no
capacity and no headroom anywhere in the view.

A line above the columns now carries what every container holds, the limit that binds, what is left,
how many more sandboxes that will hold, and one word for the state. The word is the crew's own, so
this view and the console header never say two different things about one machine. One rule sits
under it, and that one is measured: a margin that will not hold another sandbox is never drawn as
healthy. A sandbox asks for 1,536 mebibytes, read every two seconds over 808 samples of the work
these sandboxes do, in [internal/capacity/measured.go](internal/capacity/measured.go).

The figure is every container on the daemon rather than the rows underneath it, because the limit
binds all of them and the crew's own services are in there too. The line says so, because the rows
add up to less than it.
