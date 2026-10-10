# ad-skip-segment-eval

LLM sponsor-segment detector for long videos (one platform at a time, others behind a provider
interface) plus the eval harness that measures it against crowdsourced human labels. Go,
standard library only unless a dependency earns its place (SQLite for labels and runs will be
the first, `modernc.org/sqlite`, no cgo).

## Layout

- `cmd/adskip` — the single CLI (`fetch`, `collapse`; later `detect`, `score`).
- `internal/vtt` — WebVTT parsing and collapsing of rolling captions into plain cues.
- `internal/transcript` — the JSONL transcript format (meta line + cue lines).
- `internal/ytdlp` — thin wrapper over the `yt-dlp` binary with retries.
- `internal/provider` — the only platform-specific code: `Provider` interface (name, video URL,
  subtitle-track pick from yt-dlp output), registry, `platform.go`. New platform = new file + `Register`.
- `internal/netguard` — external-IP check; fetch refuses to run from a home IP.
- `internal/policy` — repo-wide tests; `TestNoVideoIDsInTrackedFiles` fails on any video id in tracked files.
- `runs/` — eval runs (`<date>-<model>-<prompt-vN>/`): config, prompt, aggregate metrics only.

## Data policy (decided 2026-10-10)

- **No video ids, labels, transcripts or per-video predictions in the repo, ever**: they identify
  videos and contributors of the private product. Not even in examples or tests: use placeholder
  ids with a short owner part (`c1_1001`), which the policy test deliberately does not match.
- Private data root: `-data DIR` / `ADSKIP_DATA`, default `~/Web/data`. Layout:
  `evalsets/<name>.txt`, `transcripts/<provider>/<id>.{info.json,<lang>.vtt,jsonl}`, later
  `adskip.db` (SQLite: gold labels, runs, predictions, metrics). Files for blobs, SQLite for what gets joined.
- The data root is outside the repo on purpose; `.gitignore` is a second line, the policy test is the first.
- Home IPs live in `~/.config/ad-skip-segment-eval/home_ips`, outside the repo. Never hard-code an IP.
- No platform account, no cookies. Fetching goes through a VPN or `--proxy`; the tool fails closed.

## Providers

- Everything is keyed by `(provider, native_id)`: transcript meta, cache paths, later DB tables
  (one table with a `provider` column, not prefixed tables as in the product backend).
- Provider-specific knowledge stays inside `internal/provider`. The prompt is not provider-aware;
  language is a transcript property (`lang`).
- Do not add `youtube`/`rutube` implementations before there are human labels for them.

## Conventions

- English everywhere (code, comments, docs, commit messages).
- `go build ./... && go vet ./... && go test ./...` must pass before a commit.
- Keep commands reproducible: the same input list must produce the same files; cached inputs are reused.
