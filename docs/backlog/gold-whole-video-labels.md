---
worth: yes
where: internal/score/score.go
added: 2026-10-26
---
# gold labels that span most of a video mask false positives as ad time

Human markups like `sponsor 0–4222 s` on `-111_222` (the whole 4208-s video), `sponsor 1806–21579 s` on `-111_223` or `self_promotion 1839–9039 s` on `-111_224` are not ads: a mis-click, a "skip the rest of the stream" markup, or vandalism. The scorer merges them into the gold union, so every prediction inside counts as ad time removed and never as content wrongly cut: on the held-out v2.2 re-score with `self_promotion: medium` (2026-10-26), 155 s of medium FPs on `-111_222` were scored as hits for that reason, and SCORE's wrong-cut term is understated on every such video.

As of 2026-10-26: 59 labels cover ≥ 50 % of their video, 88 are ≥ 30 min (query: `labels l JOIN videos v … (l.end-l.start) >= 0.5*v.duration`). Fix options, by blast radius: drop them in the scorer with a rule (longer than X min or ≥ Y % of the duration, logged), or import them as `gold_wrong` verdicts after a look at each. Either way re-score the held-out runs: the drop changes the gold of a dozen videos, so the 0.40 / 0.32 numbers in `docs/iterations.md` move.
