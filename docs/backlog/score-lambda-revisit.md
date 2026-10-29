---
worth: later
where: cmd/adskip/scorecore.go (lambda default), docs/iterations.md, README
added: 2026-10-26
---
# Revisit the ×10 penalty in SCORE (maybe 5)

`SCORE = (ad s removed − λ × content s wrongly cut) / ad s`, λ = 10 (O16). The factor was set as a product judgement
("a cut second of content is much worse than a missed ad second"), not measured. Owner's note (2026-10-26): review it
deliberately, λ = 5 is a candidate. Not urgent.

What would make it a decision rather than a taste: a cost model of the two errors for the viewer (a wrong cut =
confusion + undo + lost trust; a missed ad = the ad plays as before, i.e. the status quo), and the undo / dislike
rate on AI-served segments from rollout week 2 as the measured price of a wrong cut. Then re-rank the existing runs
with `-lambda 5` (predictions are stored raw, no paid rerun): the order of v2.1 / v2.2 / Opus should not change; what
changes is the gate value (0.15 was fixed under λ = 10) and the threshold question (D37: `self_promotion medium`
read 0.35 vs 0.40 under λ = 10, closer under λ = 5). Record the change in `docs/iterations.md` and the README; the
held-out headline moves with it.
