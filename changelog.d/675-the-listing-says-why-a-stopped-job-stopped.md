**`krewe job list` says why each stopped job stopped, on the row.** A stopped row read `stopped`, a
dash, a dash and the title. The reason was on the record and on the wire already, so the only way to
read it was one `krewe job show` for each stopped row. A person looking at ten stopped jobs typed ten
commands before they knew which one needed them
([#675](https://github.com/atlantic-blue/quay-krewe/issues/675)).

The row now carries a column between the outcome and the title. It holds the words a person typed
with `krewe job stop`, and it holds the words the system wrote when nobody typed any. This is a live
listing, from a plane running the in memory store and the echo model on 3 September 2026:

    313e183d   pending  -         -                                                  read the gas bill
    607cc3c2   stopped  -         -         the meter reading is wrong               pay the water bill
    601aefe3   stopped  -         -         the bank refused the direct debit        read the electricity bill

The column is 40 characters wide. It is there when a row of the listing stopped. It is not there at
all when no row stopped, so a listing with no stopped work reads as it read before. The cell is empty
on a row that did not stop, so every title starts in the same place.

A pending job the machine has no room for carries a reason too, and that one stays out of this
column. It is one fact about the machine, and the listing says it once under the rows. On the rows it
would be the same sentence on every held row, which buries the row that stopped.

A failed job does not fill the column yet, and a reason longer than the column takes the room it
needs. Requirement 2 of the issue carries the failed row. Requirement 3 carries the cut and the mark
that says the text goes on.
