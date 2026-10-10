# ad-skip-segment-eval

Can an LLM find sponsor segments in long Russian-language videos from the auto-generated
subtitles alone, and how close does it get to crowdsourced human labels? One platform first; the
fetch layer is per provider, everything after it is not.

This repository is the public, measurable half of a private product: a browser extension and API
where viewers mark ad segments (sponsor, intro, self-promotion, credits) and others skip them.
Here lives the **detector**, the **eval harness** that scores it against human labels, the
prompts, the aggregate numbers, and the write-up. Written in Go. The dataset itself (video ids,
labels, transcripts, per-video predictions) stays private; see Data policy.

**Status (2026-10-10):** step 1 of 6 done — the 10-video spike set fetches and collapses from one command (8 transcripts, 2 videos without subtitles); provider layer and data policy in place. No model calls yet, no numbers yet.

## Why this exists

The product has ~12.5k human markups on ~6.3k videos, 92% of them from a single contributor per
video. The open questions are engineering questions, not research ones:

1. How good is an LLM at this task, measured by precision/recall against humans, per category?
2. What is the human–human agreement ceiling on the videos that have several labellers?
3. What does it cost per video, and how much quality is lost with a cheaper model?

Every change to the prompt or the model gets a before/after number in `runs/`.

## Pipeline

```
native_id list ──fetch──> VTT + info.json (cache) ──collapse──> transcript.jsonl
                                                                    │
gold export (JSONL) ──import──> adskip.db ───────┐                  │ detect (step 3)
                                                 ▼                  ▼
                                              score  <────── predictions (runs/)
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
```

All private data lives under the **data root** (`-data DIR`, `ADSKIP_DATA`, default `~/Web/data`),
never inside the repository:

| Path | Content |
|---|---|
| `evalsets/<name>.txt` | one `native_id` per line, `#` comments allowed; `-set <name>` |
| `transcripts/<provider>/<id>.info.json` | raw `yt-dlp -j` metadata (duration, subtitle tracks) |
| `transcripts/<provider>/<id>.<lang>.vtt` | raw subtitle track as served by the platform |
| `transcripts/<provider>/<id>.jsonl` | collapsed transcript: one `meta` line, then one `cue` line per phrase |
| `adskip.db` | SQLite eval store: `videos`, `contributors`, `labels` (schema in `internal/store/schema.sql`); runs and predictions later |

### Gold labels

The product exports its human labels as JSONL, one object per interval exactly as a human made it
(no merging): `provider, native_id, duration, category, start, end, contributor, is_assessor,
contributor_rating, contributor_banned, created_at`. Contributor ids are salted hashes.
`import` validates every line (known category, `start < end`), reports the rejects, and skips an
interval that is already stored, so the same human re-submitting the same interval counts once.
The store is for offline evaluation only: the detector never depends on it, so the same detector
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
| 2 | Gold labels v1, anonymised, in the eval store; held-out list; eval set v0 of 20 videos | import done |
| 3 | Detector v0: one prompt, structured output, cost/latency per call | |
| 4 | Scorer: IoU matching, precision/recall per category; first number in this README | |
| 5 | Harness v1: human–human ceiling, LLM judge on disagreements, regression test, second model | |

## License

MIT.
