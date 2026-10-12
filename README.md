# ad-skip-segment-eval

Can an LLM find sponsor segments in long Russian-language videos from the auto-generated
subtitles alone, and how close does it get to crowdsourced human labels? One platform first; the
fetch layer is per provider, everything after it is not.

This repository is the public, measurable half of a private product: a browser extension and API
where viewers mark ad segments (sponsor, intro, self-promotion, credits) and others skip them.
Here lives the **detector**, the **eval harness** that scores it against human labels, the
prompts, the aggregate numbers, and the write-up. Written in Go. The dataset itself (video ids,
labels, transcripts, per-video predictions) stays private; see Data policy.

**Status (2026-10-12, evening):** steps 1–4 done, step 5 partly. Since the first table below: three ways to call the model (API, Batches API, `claude -p`) calibrated against each other, a cheaper request format, a blind review over all runs, a per-video subtitle offset, a time-weighted metric and one headline number. Thinking was tested on the API as well: the gain seen through `claude -p` did not reproduce on 18 videos, so the decision waits for the wider dev set. Next: human–human ceiling, a dev set with at most one video per channel.

## Why this exists

The product has ~12.5k human markups on ~6.3k videos, 92% of them from a single contributor per
video. The open questions are engineering questions, not research ones:

1. How good is an LLM at this task, measured by precision/recall against humans, per category?
2. What is the human–human agreement ceiling on the videos that have several labellers?
3. What does it cost per video, and how much quality is lost with a cheaper model?

Every change to the prompt or the model gets a before/after number in `runs/`.

## Results so far

Eval set `v0`: 20 videos (10 labelled by assessors, 10 by several ordinary contributors), 18 with
subtitles, 28 min to 1.9 h long (~13 h of video), 39 human sponsor and 13 self-promotion intervals. Prompt `v1`,
one call per video, scored at **IoU ≥ 0.3** after dropping predictions under 5 s. "Raw gold" is the
human labels as exported. "Reviewed gold" is the same labels after one person checked every
disagreement of the Opus run (23 items: 11 ads the humans had not labelled, 4 labels that were not
ads, 3 genuinely unclear, 5 where the model was wrong) — see *Scorer* for how verdicts are applied.

| Model | Valid outputs | $ / video | p50 latency | Gold | sponsor P / R | self_promotion P / R | any ad P / R |
|---|---|---|---|---|---|---|---|
| Opus 5.5 | 18 / 18 | 0.16 | 7.5 s | raw | 0.92 / 0.70 | 0.32 / 0.70 | 0.66 / 0.76 |
| | | | | reviewed | 0.96 / 0.83 | 0.75 / 0.79 | 0.87 / 0.85 |
| Sonnet 5.5 | 17 / 18 | 0.08 | 2.8 s | raw | 0.73 / 0.63 | 0.25 / 0.71 | 0.54 / 0.69 |
| | | | | reviewed | 0.77 / 0.77 | 0.72 / 0.81 | 0.75 / 0.81 |
| Haiku 4.5 | 12 / 18 | 0.04 | — | raw | 0.82 / 0.50 | 0.25 / 0.67 | 0.48 / 0.56 |
| | | | | reviewed | 0.82 / 0.64 | 0.47 / 0.70 | 0.61 / 0.70 |

"Any ad" collapses the categories and merges adjacent segments: what a skip button sees. Haiku
is scored on the 12 videos where its output passed validation (it invented cue indices or
overlapping segments on the other 6). Keeping only `confidence = high` lifts Opus "any ad"
precision to 0.94 (reviewed) at recall 0.65; for sponsor the level carries almost no
information (22 of 24 hits are already `high`).

What the first error analysis changed:

- **The raw labels understate the model on self-promotion.** 10 of the 13 Opus self-promotion
  predictions without a human label were real promotion the humans had skipped; none were
  hallucinated. The raw number is a floor, not a measurement.
- **Most "missed" sponsors were not the model's.** Of 10 human sponsor labels the model did not
  hit: 4 were not ads (show intros, a song), 2 were category disputes (a studio presenting its own
  client work: human said sponsor, model said self-promotion), 1 unclear, 1 visual-only with no
  speech, 2 real misses of short mentions (4 s and 8 s).
- **Boundaries sit inside the human ones by construction.** Start median +3.6 s later, end median
  −3.9 s earlier than humans; about two thirds of starts and half of ends are within 5 s. The tail
  (p90 15–40 s) is a few long blocks where the model took one part. Subtitle timing itself drifts
  by ~10 s late in one 1.9 h video, which caps what any text-based boundary can do.
- **Caveat on "reviewed":** the review sheet was built from the Opus run's disagreements, so its
  predictions became gold where the reviewer agreed; the reviewed row favours Opus. A review
  built from the union of all runs, blind to the source, is step 5.

Per-run aggregates: `runs/<run>/metrics.json` (cost, latency, validity) and
`runs/<run>/score-<settings>.json` (matching, per-confidence precision, boundary quantiles).

## Update (2026-10-12): cost, transports and one number

The table above was made before three corrections: the review sheet was rebuilt blind over the union of all runs, one video with subtitles ~11 s late got a per-video offset, and category rules were settled (see *Prompts*). It is kept for history; the numbers here supersede it.

**One number.** `SCORE = (ad time removed − 10 × content wrongly removed) / all ad time`, in seconds, under the skip rule of the product (sponsor from medium confidence, self-promotion only at high, at least 5 s), against reviewed gold. 1.0 = every ad second removed and nothing else; 0 = no detector; negative = the detector cuts more content than it saves ads. The factor 10 is the purpose of the system written as arithmetic: the player skips without asking. `adskip score` prints it, `-detail` prints the full table, `adskip rank "label=run,run"` compares variants with the spread of repeated runs.

| Variant (18 videos; n = repeated runs) | SCORE | without the dominant channel (13 videos) | $ / video |
|---|---|---|---|
| Opus 5.5, prompt v1 | 0.62 | 0.60 | 0.17 |
| Sonnet 5.5, v2.1, thinking + high effort (n=3) | 0.51 (0.49–0.53) | 0.44 | ≈0.06 (batch ≈0.03) |
| Sonnet 5.5, v2.1 (n=2) | 0.45 (0.44–0.45) | 0.39 | 0.054 (batch 0.026) |
| Sonnet 5.5, v1.1 (n=5) | 0.30 (0.22–0.36) | 0.31 | 0.054 |
| Haiku 4.5, v1.1 | 0.37 | 0.26 | 0.028 |

Read with care: 18 videos and ~2,700 s of ad; one 300 s block that is found in one run and missed in the next moves a score by ±0.1. Sonnet versions overlap within their own spread; Opus is about twice any of them; thinking is a likely, not a proven, gain.

**Cost per video** (Sonnet, same 18 videos): $0.085 → $0.054 by dropping cue timestamps from the request (the model answers with cue indices and never reads them; −36%, quality inside the noise) → **$0.026** with the Message Batches API (−50%, ~2 minutes for 18 videos). A lever that looked free on segment metrics (8 s windows, −15% more) was rejected after the time-weighted metric showed 4× more content cut by mistake.

**Three ways to call a model, and what each is for.**

| Transport | Flag | Use | Difference |
|---|---|---|---|
| Messages API | `-transport api` | headline numbers, latency, exact usage | constrained decoding of the JSON schema |
| Batches API | `-transport batch` | every offline run; half price | no latency (queue time), resumable by `-batch-id` |
| `claude -p` | `-transport cli` | experiments and repeats, paid from a subscription quota | schema through a synthetic tool call, no temperature control, CLI version recorded in the run config; harness overhead cut from 28.8k to 419 tokens with flags; thinking off unless `-thinking` |

Calibration on the same videos: Opus gives identical metrics on the API and through `-p`; Sonnet differs by less than two API runs of the same configuration differ from each other.

**Measurement rules learned the hard way.** Run the same setup twice before comparing two setups (the noise floor). Split an aggregate gap per video before explaining it: a gap blamed on the transport was two borderline videos where the model flips between two categories. A review sheet built from one run's errors favours that run; build it blind over the union of runs (`adskip sheet`). Report with and without a channel that dominates the set (`score -exclude-owner`). Subtitle timelines can run late by ~10 s in a video (`adskip offset`). Optimisation decisions, with numbers and the options that were rejected, are kept in a decision log.

**Prompts.** `v1` (timestamps in the request), `v1.1` (indices only), `v2` and `v2.1`: category test is *who paid for this piece* (sponsor = an outside party paid the author; self-promotion = the speaker, their studio or their guest promotes themselves), short promotions are reported and cut by length in code, confidence is independent of length.

## Pipeline

```
native_id list ──fetch──> VTT + info.json (cache) ──collapse──> transcript.jsonl
                                                                    │
gold export (JSONL) ──import──> adskip.db ───────┐                  │ detect
                                                 ▼                  ▼
eye-check verdicts (TSV) ──review──>          score  <────── predictions (adskip.db, runs/)
```

## Usage

Requirements: Go 1.27+, [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) on `PATH`, or point to it with
`-yt-dlp <path>` / `YT_DLP=<path>` (e.g. a venv: `python3 -m venv ~/Web/tools/ytv && ~/Web/tools/ytv/bin/pip install yt-dlp`).

```sh
go build -o adskip ./cmd/adskip

# Fetch and collapse subtitles for every id in an eval set. Cached videos are skipped;
# rerunning after a partial failure retries only what is missing.
YT_DLP=~/Web/tools/ytv/bin/yt-dlp ./adskip fetch -set c2-spike

# Same, from an explicit list or ids on the command line.
./adskip fetch -provider main -ids /path/ids.txt
./adskip fetch c1_1001

# Collapse an already downloaded VTT without touching the network.
./adskip collapse -id c1_1001

# Load the anonymised gold export into the eval store. Idempotent: a second run inserts nothing.
./adskip import -file ~/Web/data/gold-v1.jsonl

# Eval-set files from the store (deterministic, no seed file).
./adskip evalset heldout   # 20% of assessor videos by hash bucket; never looked at during prompt work
./adskip evalset v0        # 10 assessor + 10 multi-contributor videos, not held out, not in c2-spike

# Run the detector over an eval set (paid model calls; `ant auth login` or ANTHROPIC_API_KEY).
./adskip detect -set v0 -dry-run                       # sizes and a cost estimate, no calls
./adskip detect -set v0 -model claude-opus-5-5 -effort medium

# Other transports and request formats (see the Update section).
./adskip detect -set v0 -model claude-sonnet-5-5 -transport batch -prompt prompts/v2.1.md -no-times
./adskip detect -set v0 -model claude-sonnet-5-5 -transport cli   -prompt prompts/v2.1.md -no-times [-thinking -effort high]

# One number per run, and a comparison of variants (repeats give mean and min-max).
./adskip score -run RUN -iou 0.3 -gold reviewed            # headline; add -detail for the full table
./adskip rank -exclude-owner OWNER "label=RUN1,RUN2" "other=RUN3"
./adskip compare -a RUN1 -b RUN2                           # agreement between two runs, no gold needed

# Score a run against the human labels (offline). Thresholds are flags; predictions are stored raw.
./adskip score -run 2026-10-11-claude-opus-5-5-v1 -iou 0.3 -min-seconds 5
./adskip score -run 2026-10-11-claude-opus-5-5-v1 -iou 0.3 -gold reviewed -min-confidence high -list

# Load an eye-check sheet's verdicts (private TSV) so `-gold reviewed` can apply them.
./adskip review -file ~/Web/data/review/2026-10-11-opus-v1.tsv -run 2026-10-11-claude-opus-5-5-v1 -date 2026-10-12
```

### Scorer

Human labels of one video and category are merged across contributors (union of overlapping
intervals). Predictions are filtered (`-min-seconds`, `-min-confidence`), then matched one-to-one
with merged gold of the same category, greedily by intersection-over-union, a hit at
`IoU ≥ -iou`. Precision, recall and F1 per category and for `any_ad` (categories collapsed,
adjacent segments merged on both sides), precision per confidence level, and the boundary error of
matched pairs (prediction minus gold, median and p50/p90 of the absolute value, share within
2 / 5 / 10 s). Aggregates go to `runs/<run>/score-<settings>.json`; `-list` prints the false
positives and misses with video ids to stdout only.

IoU 0.3 is deliberately loose: a text-based boundary is always inside the audio-based human one
(the jingle before the ad has no subtitles), so a stricter IoU would punish a correct detection
for a boundary offset. Boundary quality is reported separately instead of folded into the hit rule.

`-gold reviewed` applies eye-check verdicts stored in `gold_reviews` on top of the raw labels,
which are never edited: `gold_missing` adds an interval, `gold_wrong` removes the overlapping
labels, `definition_unclear` excludes the region from both sides, `model_wrong` and `gold_valid`
change nothing. Each verdict records the reviewer, the date and the run the sheet was built from.

### Detector

One model call per video: the prompt (`prompts/v1.md`) as the system text, the transcript as
numbered cues (`[12] 549.4-551.4: text`) in the user message, and a JSON schema as the response
format. The model returns **cue indices, not timestamps** (`first_cue`, `last_cue`, `category`,
`confidence`, `quote`); the code maps indices to seconds and rejects outputs with an index
outside the transcript, a reversed or an overlapping segment. Two consequences, both on purpose:
a segment can only start and end where the transcript has text (boundaries land inside the
human ones, which the scorer measures), and the model cannot invent a number.

`confidence` is `high | medium | low`, a ranking signal and not a probability; the scorer reports
precision per level so a threshold can be chosen from data. v0 detects `sponsor` and
`self_promotion` only; `intro` and `credits` have no transcript signal.

All private data lives under the **data root** (`-data DIR`, `ADSKIP_DATA`, default `~/Web/data`),
never inside the repository:

| Path | Content |
|---|---|
| `evalsets/<name>.txt` | one `native_id` per line, `#` comments allowed; `-set <name>`. `heldout` and `v0` are generated by `evalset`, `c2-spike` is the hand-picked smoke set |
| `transcripts/<provider>/<id>.info.json` | raw `yt-dlp -j` metadata (duration, subtitle tracks) |
| `transcripts/<provider>/<id>.<lang>.vtt` | raw subtitle track as served by the platform |
| `transcripts/<provider>/<id>.jsonl` | collapsed transcript: one `meta` line, then one `cue` line per phrase |
| `adskip.db` | SQLite eval store: `videos`, `contributors`, `labels`, `runs`, `calls`, `predictions` (schema in `internal/store/schema.sql`) |

### Gold labels

The product exports its human labels as JSONL, one object per interval exactly as a human made it
(no merging): `provider, native_id, duration, category, start, end, contributor, is_assessor,
contributor_rating, contributor_banned, created_at`. Contributor ids are salted hashes.
`import` validates every line (known category, `start < end`), reports the rejects, and skips an
interval that is already stored, so the same human re-submitting the same interval counts once.
**Held-out rule.** 20% of assessor-labelled videos (`sha256(provider:id) mod 5 == 0`) are frozen
before the first prompt run and never inspected while iterating on prompts; they are scored once
per milestone. A hash bucket, not a random seed, so the list is reproducible from the data and a
video never changes side when new labels arrive. The store is for offline evaluation only: the detector never depends on it, so the same detector
can run inside the product against the product's own database.

### Network guard

`fetch` resolves the external IP before the first network call and **refuses to run** if the IP
is listed in `~/.config/ad-skip-segment-eval/home_ips` (one IP per line), or if that file is
missing. Data collection is meant to run behind a VPN or `yt-dlp --proxy`; the guard makes the
safe path the only path. Cached videos never trigger the guard.

### Transcript format

```jsonl
{"kind":"meta","native_id":"c1_1001","provider":"main","duration":2311.0,"lang":"ru","track_type":"auto","track_name":"Русский (Автоматические)","cues":1480,"words":15012,"fetched_at":"2026-10-12T10:00:00Z"}
{"kind":"cue","start":46.86,"end":48.27,"text":"всем привет, всем, привет,"}
```

The platform serves rolling captions: each phrase is repeated in two or three consecutive
cues with word-level timing tags. `collapse` strips the tags, drops the repeated prefix and
near-zero-length cues, and keeps one cue per new phrase. For a 44-minute video that is ~1,500
cues / ~15k words ≈ 25–30k tokens of model input.

Videos without a subtitle track get a `meta` line with `"track_type":"none"` and no cues. On the
10-video spike set, 2 of 10 had no track; coverage is itself a number for the write-up.

`track_type` comes from the player's own label, which yt-dlp exposes as the `ext` of the
non-manifest subtitle entry: `Ru AUTO` → `auto`. A plain `vtt` label is *not* evidence of an
uploaded track: a video the player marks as auto-generated came through as `vtt` as well, so
those stay `unknown`. On the spike set: 1 auto, 7 unknown, 2 none. Recorded so the eval can be
sliced by track type once a reliable manual/auto signal exists.

## Providers

A provider is the only platform-specific piece: a URL scheme plus the rule for reading the
platform's subtitle listing out of `yt-dlp -j` (`internal/provider`). Every transcript, label and
prediction is keyed by `(provider, native_id)`. Implemented: `main`. Planned: `youtube`, `rutube`,
once there are human labels for them; the product's backend already has their tables.

## Data policy

**Nothing that identifies a video or a contributor enters this repository**: no video ids, no
labels, no transcripts, no per-video predictions. They live under the data root (files for
subtitles and transcripts, SQLite for labels and runs). What is committed: code, prompts, eval
configs and aggregate metrics per run. A test (`internal/policy`) scans every tracked file for
video ids and fails the build on a hit; `.gitignore` is only the second line of defence.
Raw subtitles are third-party content. Labels are exported anonymised (hashed contributor ids,
no account ids). No platform account or cookies are used.

## Roadmap

| Step | What | State |
|---|---|---|
| 1 | `fetch`: subtitles → transcripts, reproducible from one command | done |
| 2 | Gold labels v1, anonymised, in the eval store; held-out list; eval set v0 of 20 videos | done |
| 3 | Detector v0: one prompt, structured output, cost/latency per call | done |
| 4 | Scorer: IoU matching, precision/recall per category; first number in this README | done |
| 5 | Harness v1: human–human ceiling, LLM judge on disagreements, regression test, second model | |

## License

MIT.
