# Recall

Recall is a local-first Go service for turning a video URL into a structured briefing. It downloads and extracts audio with `yt-dlp`/`ffmpeg`, sends the audio to a Whisper-compatible transcription endpoint, sends the transcript to an OpenAI-compatible chat endpoint, and stores the generated Markdown note and transcript in a database.

The web UI at `/` lets you submit a video URL, add optional tags, watch job progress, view recent jobs, open generated note/transcript artefacts inline in the browser, redo either the note or the transcript, and delete jobs with their stored artefacts.

## MVP limitations

- Jobs run in-process after `POST /briefings` returns a job ID; there is no durable queue or retry worker.
- There is no queue, retry system, or knowledge graph.
- The default database is a simple JSON file at `DB_PATH` for a dependency-free MVP. Postgres is supported for real multi-user deployments.
- The generated Markdown is previewed from the service; it is not automatically written into an Obsidian vault.
- Unit tests use fakes and do not call real `yt-dlp`, Whisper, or LLM services.

## Required services

- `yt-dlp` and `ffmpeg` in the container image.
- A transcription endpoint. Recall supports either:
  - OpenAI-compatible Whisper HTTP:
  `POST {WHISPER_BASE_URL}/v1/audio/transcriptions`
  - Wyoming protocol faster-whisper over TCP, usually on port `10300`
- An OpenAI-compatible chat completions endpoint:
  `POST {LLM_BASE_URL}/chat/completions`

## Configuration

Environment variables:

- `ADDR`, default `:8080`
- `DATA_DIR`, default `/data`
- `STORE_TYPE`, default `json`; use `postgres` for multi-user deployments
- `DB_PATH`, default `/data/recall-db.json`
- `DATABASE_URL`, required when `STORE_TYPE=postgres`
- `TRANSCRIBER_TYPE`, default `openai`; use `wyoming` for Wyoming faster-whisper
- `WHISPER_BASE_URL`, required when `TRANSCRIBER_TYPE=openai`
- `WYOMING_WHISPER_ADDR`, required when `TRANSCRIBER_TYPE=wyoming`, for example `host.docker.internal:10300`
- `WYOMING_WHISPER_MODEL`, optional
- `WYOMING_WHISPER_LANGUAGE`, optional, for example `en`
- `LLM_BASE_URL`, required
- `LLM_API_KEY`, optional
- `LLM_MODEL`, required
- `JOB_TIMEOUT_SECONDS`, default `3600`
- `YTDLP_EXTRA_ARGS`, optional extra arguments passed to `yt-dlp`
- `AUTH_MODE`, default `none`; use `oidc` to require login
- `OIDC_ISSUER_URL`, required when `AUTH_MODE=oidc`
- `OIDC_CLIENT_ID`, required when `AUTH_MODE=oidc`
- `OIDC_CLIENT_SECRET`, required when `AUTH_MODE=oidc`
- `OIDC_REDIRECT_URL`, required when `AUTH_MODE=oidc`, for example `https://recall.example/auth/callback`
- `SESSION_SECRET`, required when `AUTH_MODE=oidc`; use a random value of at least 32 characters
- `AUTH_ALLOWED_EMAILS`, optional comma-separated allow-list
- `AUTH_ALLOWED_DOMAINS`, optional comma-separated domain allow-list

Legacy note output variables are accepted but not used by the database-backed MVP:

- `NOTE_OUTPUT_DIR`
- `TRANSCRIPT_OUTPUT_DIR`

## Run With Docker Compose

The default `compose.yaml` runs `hwdsl2/whisper-server` as an OpenAI-compatible transcription service and points Recall at `http://whisper:9000`. Edit `LLM_BASE_URL`, `LLM_API_KEY`, and `LLM_MODEL` to match your LLM endpoint, then run:

```sh
docker compose up --build
```

Open:

```text
http://localhost:8088/
```

## Multi-User Mode

For multiple real users, use Postgres and OIDC. The app sets a per-request Postgres session variable and the `jobs` and `artefacts` tables enforce ownership with row-level security.

In `compose.yaml`, uncomment the Postgres/OIDC environment variables for the `recall` service, set your OIDC values, and run with the Postgres profile:

```sh
docker compose --profile postgres up --build
```

`AUTH_MODE=oidc` requires `STORE_TYPE=postgres`. The default `AUTH_MODE=none` path uses a fixed local user and keeps existing single-user JSON deployments working.

## Example API Request

```sh
curl -sS http://localhost:8088/briefings \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/video","tags":["cyber","kubernetes"]}'
```

Example response:

```json
{
  "job_id": "20260525T120000-abc123def456",
  "note_id": "20260525T120000-abc123def456-note",
  "transcript_id": "20260525T120000-abc123def456-transcript",
  "note_url": "/artefacts/20260525T120000-abc123def456-note",
  "transcript_url": "/artefacts/20260525T120000-abc123def456-transcript"
}
```

## Expected Output

Recall stores:

- job records
- generated Markdown briefing notes
- generated Markdown transcripts, lightly cleaned before summarisation and storage
- timed transcript paragraphs when the transcription backend returns word or segment timestamps
- speaker-labelled paragraphs when the transcription backend returns diarisation labels
- long-transcript summarisation in one pass by default, with chunking only for very large transcripts where it is likely to help

Work files remain under:

```text
/data/jobs/<job-id>
```

Generated artefacts are served inline from:

```text
/artefacts/<artefact-id>
```

Job status is available from:

```text
/api/jobs/<job-id>
```

Redo only the note from the stored transcript with:

```text
POST /api/jobs/<job-id>/resummarise
```

Redo the transcript and regenerate the note with:

```text
POST /api/jobs/<job-id>/retranscribe
```

Delete a job and its note/transcript artefacts with:

```text
DELETE /api/jobs/<job-id>
```

## Security Notes

- Run this on a trusted local network unless you enable `AUTH_MODE=oidc`.
- Treat submitted URLs as untrusted input.
- The container uses `read_only: true`, drops Linux capabilities, and writes only to `/data`, `/tmp`, and mounted paths.
- LLM and Whisper endpoints receive the content you submit for processing.

## YouTube Download Issues

YouTube changes frequently, and distro-packaged `yt-dlp` versions can go stale. The Docker image installs `yt-dlp` from PyPI so rebuilding the image usually picks up extractor fixes:

```sh
docker compose build --no-cache recall
docker compose up -d
```

If YouTube still returns `Precondition check failed`, `HTTP Error 400`, `HTTP Error 403`, or `nsig extraction failed`, try passing `yt-dlp` overrides through `YTDLP_EXTRA_ARGS`. Use equals-style arguments because the value is split on whitespace:

```yaml
YTDLP_EXTRA_ARGS: "--extractor-args=youtube:player_client=default,-tv"
```

Some age-restricted, private, bot-checked, or region-limited videos may require cookies:

```yaml
YTDLP_EXTRA_ARGS: "--cookies=/data/cookies.txt"
```

Mount the cookies file under `/data` if you use this option.

## Wyoming Faster-Whisper

If your faster-whisper server exposes the Wyoming protocol, set:

```yaml
TRANSCRIBER_TYPE: "wyoming"
WYOMING_WHISPER_ADDR: "host.docker.internal:10300"
WYOMING_WHISPER_LANGUAGE: "en"
```

For a server on another machine, use `host:port`, for example:

```yaml
WYOMING_WHISPER_ADDR: "192.168.1.50:10300"
```

Recall converts the downloaded audio to 16 kHz mono signed 16-bit PCM with `ffmpeg`, then sends Wyoming `transcribe`, `audio-start`, `audio-chunk`, and `audio-stop` events over TCP. Standard Wyoming transcript events usually return text only, so timed paragraphing depends on the backend exposing timestamps. The OpenAI-compatible Whisper HTTP path requests segment timestamps and uses them when available.

## docker-whisper

The included Compose file uses `hwdsl2/whisper-server`:

```yaml
TRANSCRIBER_TYPE: "openai"
WHISPER_BASE_URL: "http://whisper:9000"
```

This keeps Recall on the OpenAI-compatible HTTP path. Recall requests both segment and word timestamps, then uses word timing gaps, punctuation, transition words, speaker changes, and length limits to group the transcript into more coherent timestamped paragraphs before sending it to the LLM. If `WHISPER_DIARIZATION=true` is enabled in `hwdsl2/whisper-server`, verbose JSON adds a `speaker` field to segments; Recall renders those labels as `SPEAKER_00`, `SPEAKER_01`, and so on.

## Legal And Terms Reminder

Only download or process content that you are permitted to use. Respect copyright, platform terms, and privacy obligations.

## Development

Run tests:

```sh
go test ./...
```

Start locally:

```sh
WHISPER_BASE_URL=http://whisper.local:9000 \
TRANSCRIBER_TYPE=openai \
LLM_BASE_URL=http://llm.local:8000/v1 \
LLM_MODEL=local-model \
DATA_DIR=/tmp/recall \
go run ./cmd/recall
```
