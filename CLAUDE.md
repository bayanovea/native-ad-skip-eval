# native-ad-skip-eval

LLM sponsor-segment detector for long videos (one platform at a time, others behind a provider
interface) plus the eval harness that measures it against crowdsourced human labels. Go,
standard library only unless a dependency earns its place (SQLite for labels and runs will be
the first, `modernc.org/sqlite`, no cgo).

## Layout

- `cmd/adskip` — the single CLI (`fetch`, `collapse`, `import`, `evalset`, `detect`, `score`).
- `internal/vtt` — WebVTT parsing and collapsing of rolling captions into plain cues.
- `internal/transcript` — the JSONL transcript format (meta line + cue lines).
- `internal/ytdlp` — thin wrapper over the `yt-dlp` binary with retries.
- `internal/provider` — the only platform-specific code: `Provider` interface (name, video URL,
  subtitle-track pick from yt-dlp output), registry, `platform.go`. New platform = new file + `Register`.
- `internal/store` — SQLite eval store (`adskip.db` in the data root; `modernc.org/sqlite`, no cgo),
  schema in `schema.sql`. **Offline evaluation only.** The detector must never import it: in the
  product the same detector runs against the product database. Only the CLI and the scorer use it.
- `internal/gold` — reads the product's anonymised gold export (JSONL) into store rows; validates
  every line and collects rejects instead of stopping at the first.
- `internal/detector` — the detector: prompt loading, transcript rendering (numbered cues), the
  output schema, validation (cue indices, no overlaps) and the `Client` interface. **Runs inside
  the product, so it imports only `transcript`/`vtt`.** `anthropic.go` is the Claude API
  client (Go SDK, structured output via JSON schema). The model returns cue indices, never
  timestamps; `Resolve` maps them to seconds.
- `internal/score` — the scorer: merges gold per video and category (union across contributors),
  matches predictions one-to-one by IoU, reports precision/recall per category plus a
  category-agnostic `any_ad` view (what a skip button sees), precision per confidence level and
  boundary-error quantiles. Thresholds (`-min-seconds`, `-min-confidence`, `-iou`, `-gold`) are
  flags: predictions are raw, one paid run is scored under many settings. Writes
  `runs/<id>/score-<settings>.json` (aggregates only); `-list` prints per-video detail to stdout.
- `internal/score/review.go` + `gold_reviews` table — eye-check verdicts (`gold_missing`, `gold_wrong`,
  `definition_unclear`, `model_wrong`, `gold_valid`) imported with `adskip review` from a private TSV; raw
  labels are never edited. `score -gold reviewed` applies them: missing ads added, the reviewed stretch cut out of wrong labels,
  unclear stretches cut out of both sides (only the stretch, never the whole label: a miss item is the
  uncovered rest of a label). **A review sheet built from one run's disagreements favours
  that run** (its own predictions become gold): report raw and reviewed side by side and say which run
  the sheet came from. `sheet -misses` asks the other way round (human-labelled ad stretches no run keeps); the
  product export reads every sheet as model-flagged, so load its verdicts with `review -misses`.
- `video_flags` table — per-video assessor facts from the product's review page (`adskip flags`, latest value
  wins). `rank -timing-clean` drops a video on `track_mismatch = true` or a >3 s subtitle overrun; an assessor's
  `false` never keeps an overrun video (it covers only the items they checked). Flags say nothing about verdicts.
- `prompts/vN.md` — versioned prompts; the file name is the version, the sha256 is in each run's config.
- `internal/netguard` — external-IP check; fetch refuses to run from a home IP.
- `internal/policy` — repo-wide tests; `TestNoVideoIDsInTrackedFiles` fails on any video or channel id in tracked files and on a long number in a tracked file name. `TestProductPackagesImportOnlyEachOther` keeps the five packages the product copies (`detector`, `transcript`, `vtt`, `provider`, `ytdlp`) on stdlib, each other and the Anthropic SDK.
- `runs/` — eval runs (`<date>-<model>-<prompt-vN>/`): `config.json` (model, effort, prompt sha,
  prices used), `prompt.md`, `metrics.json` (aggregates). Per-video calls and predictions go to
  `adskip.db` (`runs`, `calls`, `predictions`). Predictions are stored raw; min-duration and
  confidence thresholds are applied by the scorer, so one paid run is scored under many settings.
  A run resumes: `ok`/`no_transcript` videos are skipped, failed ones retried; `-force` restarts.

## The product's copy (since 2026-10-22)

The product (a private repo, AI-first segments) runs `detector`, `transcript`, `vtt`,
`provider`, `ytdlp` and the detector prompt as a **copy**: `internal/` is not importable from
another module and the product's CI builds from its own tree only, so
`scripts/sync-detector.sh <this checkout>` there copies the five packages
into its `ai/` (import path rewritten, `claudecli.go` and the key-file lookup dropped) and records
this repo's commit in `ai/SOURCE`. It refuses a checkout with uncommitted changes.

- **Changes land here first.** Prompt and detector changes are made, scored and committed in this
  repo, then synced; the product never edits its copy. A new prompt file needs the product's
  `AI_PROMPT` (or default) changed too; the prompt sha in its `AI-runs table` rows tells which videos
  ran under which.
- Keep the five packages free of `store`, `netguard` and anything outside stdlib, each other and the
  Anthropic SDK (`TestProductPackagesImportOnlyEachOther`). A changed SDK version in `go.mod` is
  re-vendored in the product.
- The product's judge prompt (`ai/prompts/judge-v1.md` there) is product-owned for now; it and the
  detector prompt are iterated here against the `ai-judge-*` review verdicts the product exports.

## Data policy (decided 2026-10-10)

- **No video ids, labels, transcripts or per-video predictions in the repo, ever**: they identify
  videos and contributors of the private product. Not even in examples or tests: use placeholder
  ids with a short owner part (`c1_1001`), which the policy test deliberately does not match.
- Private data root: `-data DIR` / `ADSKIP_DATA`, default `~/Web/data`. Layout:
  `evalsets/<name>.txt`, `transcripts/<provider>/<id>.{info.json,<lang>.vtt,jsonl}`,
  `adskip.db` (SQLite: gold labels now; runs, predictions, metrics later), `gold-v1.jsonl` (the export). Files for blobs, SQLite for what gets joined.
- The data root is outside the repo on purpose; `.gitignore` is a second line, the policy test is the first.
- Home IPs live in `~/.config/native-ad-skip-eval/home_ips`, outside the repo. Never hard-code an IP.
- No platform account, no cookies. Fetching goes through a VPN or `--proxy`; the tool fails closed.

## Eval sets

- `heldout` = assessor videos in hash bucket 0 of 5 (`store.Bucket`). Frozen before the first
  prompt run. Never read its transcripts or labels while iterating on prompts; score it once per
  milestone. `dev`/`v0` selection always excludes it.
- Selection is deterministic (hash order, no seed) so every set is reproducible from the store.
- `c2-spike` is hand-picked (used to debug fetch) and serves as the smoke set; it is excluded from `v0`.

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
