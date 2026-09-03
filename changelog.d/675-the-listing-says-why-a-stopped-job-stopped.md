**`krewe job list` says why each stopped job stopped, on the row.** A stopped row read `stopped`, a
dash, a dash and the title. The reason was on the record and on the wire already, so the only way to
read it was one `krewe job show` for each stopped row. A person looking at ten stopped rows typed ten
commands before they knew which one needed them
([#675](https://github.com/atlantic-blue/quay-krewe/issues/675)).

The row now carries the reason in a column between the outcome and the title. It holds the words a
person typed with `krewe job stop`. It holds the words the system wrote when nobody typed any. The
cell is empty on a row that did not stop. Every title then starts in the same place, and the listing
reads down the screen. `krewe job list system` and `krewe job list --phase stopped` say the same
thing, because they are the same listing.

This is a real listing. A control plane drew it on 3 September 2026, with the in memory store, the
echo model and the local sandbox provider:

    c9db0133   pending  -         -                                                  check the council tax band
    3c08cbf7   stopped  -         -         this job's answer states no outcome, so… check the council tax
    aa03780d   stopped  -         -         the meter reading is wrong               pay the water bill
    1c462838   stopped  -         -         the bank refused the direct debit        read the electricity bill

The column, its width and its cut are the ones the failed row uses. Read the two entries beside this
one for when the column is there at all, and for what happens to a reason longer than it.
