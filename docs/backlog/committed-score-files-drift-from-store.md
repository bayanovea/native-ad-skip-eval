---
worth: later
where: runs/*/score-*.json
added: 2026-10-31
---
# Committed score files no longer reproduce from the store

A `runs/<id>/score-<settings>.json` is written whenever `adskip score` runs and committed as is, so it is a
snapshot of the gold, the review verdicts and the scorer of that day; nothing in the file says which. On
2026-10-31, re-scoring the 40 committed headline files (`s5-cany-iou0.3`, `all` and `reviewed`) against
today's store changed 30 of them, e.g. dev v2.2 (`2026-10-21-…-v2.2-batch-nt-dev`, `reviewed`):
predictions_total 134 → 154 for the same 69 videos (155 predictions stored, 13 `no_transcript`),
gold sponsor segments 78 → 82, time-weighted `any_ad` precision 0.875 → 0.915. Causes include the 2026-10-23
scorer change (a `gold_wrong` / unclear verdict cuts only the reviewed stretch) and every review import since.
The provider rename of the store (2026-10-31) is not one: the dev report is identical before and after it.

Unknown that settles the value: does anyone read a committed score file as the current number? The numbers
that decisions cite are dated in `docs/iterations.md`, which reads them as of their day. If nobody does,
re-scoring in one commit only moves the snapshot (and drifts again with the next review import); a sturdier
fix is recording the gold state in the file (review count per source run, scorer commit), or not
committing score files at all and keeping `metrics.json` + iterations.md as the record.
