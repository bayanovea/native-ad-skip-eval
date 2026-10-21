# How the detector got here

Every row below is a measured run from `runs/`, scored with `adskip rank` against reviewed gold
(IoU 0.3, product thresholds, segments ≥ 5 s). SCORE is the one number used for decisions:

```
SCORE = (ad seconds removed − 10 × content seconds wrongly removed) / all ad seconds
```

1.0 = every ad second removed and nothing else · 0 = no detector · negative = the detector costs
viewers more content than it saves them. Repeated runs give the min–max; a difference inside that
range is noise.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="results-dark.svg">
  <img alt="Results dashboard: headline numbers, prompt iterations, raw vs reviewed gold" src="results-light.svg">
</picture>

## Stage 1 — first number: which model, what ceiling (18 videos, 2026-10-11)

| Step | Change | SCORE | $ / video | Decision |
|---|---|---|---|---|
| 1 | Prompt v1, one call per video, model returns cue indices, JSON schema output. Three models. | Opus 0.62 · Sonnet 0.42 · Haiku n/a (6 of 18 outputs invalid) | 0.164 · 0.083 · 0.059 | Opus is the quality reference, Sonnet the production candidate. Haiku invents cue indices and overlapping segments; kept only as a cost floor. |
| 2 | First error analysis by hand: 23 disagreements of the Opus run. | — | — | 11 of 23 were ads the humans had not labelled, 4 were human labels that were not ads. The raw labels are a floor, not a measurement → the review layer (`gold_reviews`) was built before any more prompt work. |

## Stage 2 — cost down 3× at equal quality (18 videos, 2026-10-12)

Spend is 99% input tokens, so the levers are in the request, not in the model's output.

| Step | Change | SCORE (min–max) | $ / video | Decision |
|---|---|---|---|---|
| 3 | **v1.1: drop cue timestamps from the request.** The model answers with cue indices and never reads the times. | 0.28 (0.22–0.34, n=4) | 0.052 (−36%) | Kept. The drop against v1 is inside the spread of repeated runs on this set (see step 7). |
| 4 | **v1.1 + cues merged into 8 s windows.** 1,400 cues → 270, another −15%. | 0.09 | 0.046 | **Rejected.** On segment precision/recall it looked free; the seconds metric showed 4× more content cut by mistake (228 s vs 45–58 s). This run is why SCORE counts seconds, not segments. |
| 5 | **v2: category rules rewritten** (giveaways, studio work, guests), Message Batches API for every offline run. | 0.19 (0.19–0.19, n=2) | 0.027 (−50%) | Batches kept. The prompt lost: the new rules moved borderline pieces into the wrong category. |
| 6 | **v2.1: one test for both categories, "who paid for this piece"**; short promotions reported by the model and cut by length in code. | 0.43 (0.40–0.46, n=2) | 0.027 | Kept. Same level as v1 at a third of the price. |
| 7 | **Noise floor.** The same configuration run twice. | sponsor agreement between repeats 0.93–0.96; ±0.03 on dev, ±0.1 on 18 videos | — | Rule: run a setup twice before comparing two setups. On 18 videos (~2,700 s of ad) one 300 s block decides the score. |
| 8 | **v2.1 + extended thinking, high effort.** A gain seen through `claude -p` on the same videos. | 0.38 (0.32–0.45, n=2) | 0.028 | Not reproduced on the API; later on dev-v1 the gap is 0.04 inside the repeat range. Thinking stays off. |
| 9 | **Three transports calibrated:** Messages API, Batches API, `claude -p`. | Opus identical on API and `-p`; Sonnet differs by less than two API runs differ | — | `-p` for cheap experiments on a subscription, API for headline numbers, Batches for every offline run. |

## Stage 3 — a wider set, and gold that had to be fixed first (dev-v1, 69 videos, 40 channels)

| Step | Change | SCORE | Decision |
|---|---|---|---|
| 10 | dev-v1: at most 3 videos per channel, plus ~20 videos from channels with no assessor labels. v2.1, two repeats. | raw gold **−0.63**; wrongly cut 13.9 s/h | A negative headline with incomplete gold says nothing about the model. Review the disagreements first. |
| 11 | Blind review sheet over the union of all runs (the reviewer does not see which run produced a stretch); every flagged stretch reviewed on the product's review page. | reviewed **0.20** (0.17–0.23); wrongly cut 5.0 s/h | About 70% of the "wrongly cut" seconds were unlabelled ads, ~20% videos whose subtitle track did not match the video, under 10% model errors. |
| 12 | **Human–human ceiling** measured in the same SCORE: each contributor against the union of the others, 384 videos. | **−5.96**; one person's seconds confirmed by others 22%, covering 17% | Crowd labels are partial: each person marks the ad they noticed. "AI at human level" would be passed by anything, so the release gate is defined on reviewed gold plus a bound on wrongly cut content. |
| 13 | **Subtitle timing audit** against the audio on 29 videos. | ~1 video in 5 off by > 2 s (steps and offsets of 15–90 s) | No audio analysis in the product. Cheap gate instead: last cue ends > 3 s after the video → the detector's times are not trusted on that video. Eval reports the clean-timeline slice next to the full set. |
| 14 | **Release gate fixed before touching the held-out set:** SCORE ≥ 0.15 and wrongly cut ≤ 6 s/h on reviewed held-out gold. | — | Just under the dev level, so an honest held-out can pass without being a copy of dev; 6 s/h is one wrong cut of a few seconds per hour of video. |
| 15 | **v2.2: one rule added** (a staged scene that leads into the pitch belongs to the sponsor segment). Two repeats. | **0.31** (0.26–0.37); ad removed 61%, wrongly cut 3.7 s/h | Kept on dev; not yet run on the held-out set. |

## Stage 4 — the held-out set, scored once (217 videos, 2026-10-18)

| Gold | SCORE | ad time removed | content wrongly cut |
|---|---|---|---|
| raw crowd labels | −0.89 | 48% | 15.5 s/h |
| reviewed (178 disagreements eye-checked, blind to the run) | **0.34** | 44% | **1.3 s/h** |

Gate passed on the first and only scoring. Cost of the run: $5.36 for 217 videos ($0.025 each).
Of 292 held-out videos, 75 had no subtitle track: coverage is a number of its own.

What the held-out run says in plain words: when the detector cuts, it is almost always an ad
(seconds precision 0.95), and it still leaves about half of the ad time in (seconds recall 0.49).
Recall is the next error class to work on; the review sheets so far only cover what the detector
flagged, so the misses are the least examined part.

## Rules this history produced

1. Review disagreements before reading a number against crowd labels.
2. Run the same setup twice before comparing two setups; report the spread.
3. Build review sheets blind, over the union of runs; a sheet from one run's errors favours that run.
4. Score in seconds under the product's own skip rule; segment metrics hid a 4× regression.
5. Report with and without the channel that dominates a set, and with and without videos whose
   subtitle timeline is broken.
6. Fix the gate before the held-out set is touched, and score it once.
