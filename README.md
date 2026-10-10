# ad-skip-segment-eval

Can an LLM find sponsor segments in long Russian-language videos from the auto-generated
subtitles alone, and how close does it get to crowdsourced human labels?

This repository is the public, measurable half of a private product: a browser extension and API
where viewers mark ad segments (sponsor, intro, self-promotion, credits) and others skip them.
Here lives the **detector**, the **eval harness** that scores it against human labels, the
**anonymised dataset**, and the write-up. Written in Go.

**Status (2026-10-10):** step 1 of 6 done — the 10-video spike set fetches and collapses from one command (8 transcripts, 2 videos without subtitles). No model calls yet, no numbers yet.

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
gold labels (data/gold) ─────────────────────────┐                  │ detect (step 3)
                                                 ▼                  ▼
                                              score  <────── predictions (runs/)
```

## Usage

Requirements: Go 1.27+, [`yt-dlp`](https://github.com/yt-dlp/yt-dlp) on `PATH`, or point to it with
`-yt-dlp <path>` / `YT_DLP=<path>` (e.g. a venv: `python3 -m venv ~/Web/tools/ytv && ~/Web/tools/ytv/bin/pip install yt-dlp`).

```sh
go build -o adskip ./cmd/adskip

# Fetch and collapse subtitles for every id in the list. Cached videos are skipped;
# rerunning after a partial failure retries only what is missing.
YT_DLP=~/Web/tools/ytv/bin/yt-dlp ./adskip fetch -ids data/evalset/c2-spike.txt

# Collapse an already downloaded VTT without touching the network.
./adskip collapse -id c1_1001
```

`fetch` writes to `~/Web/data/transcripts/` (override with `-cache`):

| File | Content |
|---|---|
| `<id>.info.json` | raw `yt-dlp -j` metadata (duration, subtitle tracks) |
| `<id>.<lang>.vtt` | raw subtitle track as served by the platform |
| `<id>.jsonl` | collapsed transcript: one `meta` line, then one `cue` line per phrase |

### Network guard

`fetch` resolves the external IP before the first network call and **refuses to run** if the IP
is listed in `~/.config/ad-skip-segment-eval/home_ips` (one IP per line), or if that file is
missing. Data collection is meant to run behind a VPN or `yt-dlp --proxy`; the guard makes the
safe path the only path. Cached videos never trigger the guard.

### Transcript format

```jsonl
{"kind":"meta","native_id":"c1_1001","source":"main","duration":2311.0,"lang":"ru","track_type":"auto","track_name":"Русский (Автоматические)","cues":1480,"words":15012,"fetched_at":"2026-10-12T10:00:00Z"}
{"kind":"cue","start":46.86,"end":48.27,"text":"всем привет, всем, привет,"}
```

The platform serves rolling captions: each phrase is repeated in two or three consecutive
cues with word-level timing tags. `collapse` strips the tags, drops the repeated prefix and
near-zero-length cues, and keeps one cue per new phrase. For a 44-minute video that is ~1,500
cues / ~15k words ≈ 25–30k tokens of model input.

Videos without a subtitle track get a `meta` line with `"track_type":"none"` and no cues. On the
10-video spike set, 2 of 10 had no track; coverage is itself a number for the write-up.

`track_type` comes from the player's own label, which yt-dlp exposes as the `ext` of the
non-manifest subtitle entry: `Ru AUTO` → `auto`, a plain `vtt` → `manual` (uploaded by the
author, as far as the label tells). On the spike set: 1 auto, 7 manual, 2 none. It is recorded so
the eval can be sliced by track type; the auto/manual split still needs a check against the player
on a few videos.

## Data policy

Raw subtitles are third-party content and stay out of the repo. Gold labels are exported
anonymised (hashed contributor ids, no account ids). No platform account or cookies are used.

## Roadmap

| Step | What | State |
|---|---|---|
| 1 | `fetch`: subtitles → transcripts, reproducible from one command | done |
| 2 | Gold labels v1, anonymised; eval set v0 of 20 videos | |
| 3 | Detector v0: one prompt, structured output, cost/latency per call | |
| 4 | Scorer: IoU matching, precision/recall per category; first number in this README | |
| 5 | Harness v1: human–human ceiling, LLM judge on disagreements, regression test, second model | |

## License

MIT.
