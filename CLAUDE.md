# ad-skip-segment-eval

LLM sponsor-segment detector for videos plus the eval harness that measures it against
crowdsourced human labels. Go, standard library only unless a dependency earns its place.

## Layout

- `cmd/adskip` — the single CLI (`fetch`, `collapse`; later `detect`, `score`).
- `internal/vtt` — WebVTT parsing and collapsing of rolling captions into plain cues.
- `internal/transcript` — the JSONL transcript format (meta line + cue lines).
- `internal/ytdlp` — thin wrapper over the `yt-dlp` binary with retries.
- `internal/netguard` — external-IP check; fetch refuses to run from a home IP.
- `data/evalset/*.txt` — lists of `native_id`s. `data/gold/` — anonymised labels (step 2).
- `runs/` — eval runs (`<date>-<model>-<prompt-vN>/`), committed.

## Data policy

- Raw subtitles (`*.vtt`, `*.info.json`) are third-party content: cached in `~/Web/data/transcripts`,
  never committed. Collapsed transcripts (`*.jsonl`) live there too; a demo subset may be committed later.
- Home IPs live in `~/.config/ad-skip-segment-eval/home_ips`, outside the repo. Never hard-code an IP.
- No platform account, no cookies. Fetching goes through a VPN or `--proxy`; the tool fails closed.

## Conventions

- English everywhere (code, comments, docs, commit messages).
- `go build ./... && go vet ./... && go test ./...` must pass before a commit.
- Keep commands reproducible: the same input list must produce the same files; cached inputs are reused.
