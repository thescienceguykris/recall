# Recall

Recall turns a video URL into a structured briefing note. It downloads media with `yt-dlp`, extracts audio with `ffmpeg`, transcribes the audio with either an OpenAI-compatible Whisper endpoint or a Wyoming faster-whisper server, summarises the transcript with an OpenAI-compatible chat endpoint, and stores the resulting note and transcript.

> [!WARNING]
> Recall accepts arbitrary URLs and downloads them server-side. Run it only on a trusted local network unless you enable OIDC authentication and understand the network exposure of your downloader, transcription, and LLM services.

## What You Get

- A web UI at `/` for submitting URLs, adding tags, watching progress, and viewing recent jobs.
- Inline Markdown artefacts for generated notes and transcripts.
- Retry actions to regenerate only the note or rerun transcription and then regenerate the note.
- A single-user JSON store for local use.
- A Postgres store with per-user ownership checks for OIDC-backed multi-user use.

Current limitations:

- Jobs run in-process after creation; there is no durable external queue.
- Failed jobs are recorded, but there is no automatic retry worker.
- Notes and transcripts are stored by Recall; they are not automatically written to an Obsidian vault.
- Tests use fakes and do not call real downloader, Whisper, or LLM services.

## Requirements

- Docker Compose, or Go plus local `yt-dlp` and `ffmpeg`.
- A transcription backend:
  - OpenAI-compatible Whisper HTTP at `POST {WHISPER_BASE_URL}/v1/audio/transcriptions`.
  - Or a Wyoming faster-whisper TCP endpoint, commonly on port `10300`.
- An OpenAI-compatible chat completions backend at `POST {LLM_BASE_URL}/chat/completions`.

The included Docker image installs `yt-dlp` from PyPI and includes `ffmpeg`.

## Quick Start

Copy the example environment file and set at least your LLM values:

```sh
cp example.env .env
```

For the default Compose setup, set these values in `.env`:

```dotenv
LLM_BASE_URL=https://api.openai.com/v1
LLM_API_KEY=your-api-key
LLM_MODEL=your-chat-model
```

Then start Recall and the bundled Whisper server:

```sh
docker compose up --build
```

Open:

```text
http://localhost:8088/
```

By default Compose uses:

- `STORE_TYPE=json`
- `AUTH_MODE=none`
- `WHISPER_BASE_URL=http://whisper:9000`
- data stored in the `recall-data` Docker volume

## Configuration

Core settings:

| Variable | Default | Notes |
| --- | --- | --- |
| `ADDR` | `:8080` | HTTP listen address inside the container. |
| `DATA_DIR` | `/data` | Working directory for downloads and generated job files. |
| `JOB_TIMEOUT_SECONDS` | `3600` | Per-job timeout used by the HTTP client and job context. |
| `YTDLP_EXTRA_ARGS` | empty | Extra whitespace-separated arguments passed to `yt-dlp`. Use equals-style flags. |

Storage:

| Variable | Default | Notes |
| --- | --- | --- |
| `STORE_TYPE` | `json` | Use `json` for local single-user use or `postgres` for multi-user deployments. |
| `DB_PATH` | `/data/recall-db.json` | JSON store path. |
| `DATABASE_URL` | empty | Required when `STORE_TYPE=postgres`. |

Transcription:

| Variable | Default | Notes |
| --- | --- | --- |
| `TRANSCRIBER_TYPE` | `openai` | Use `openai` for Whisper-compatible HTTP or `wyoming` for Wyoming TCP. |
| `WHISPER_BASE_URL` | empty | Required for `TRANSCRIBER_TYPE=openai`. Do not include `/v1/audio/transcriptions`. |
| `WYOMING_WHISPER_ADDR` | empty | Required for `TRANSCRIBER_TYPE=wyoming`, for example `host.docker.internal:10300`. |
| `WYOMING_WHISPER_MODEL` | empty | Optional model hint sent to the Wyoming server. |
| `WYOMING_WHISPER_LANGUAGE` | empty | Optional language hint, for example `en`. |

On startup, Recall waits up to one minute for the configured transcriber to become reachable. For OpenAI-compatible Whisper, this checks that `GET {WHISPER_BASE_URL}/v1/audio/transcriptions` returns response headers. For Wyoming, this checks that a TCP connection can be opened. This does not run a sample transcription.

Summarisation:

| Variable | Default | Notes |
| --- | --- | --- |
| `LLM_BASE_URL` | empty | Required. Base URL for an OpenAI-compatible API, for example `https://api.openai.com/v1`. |
| `LLM_API_KEY` | empty | Optional for local endpoints; usually required for hosted APIs. |
| `LLM_MODEL` | empty | Required. Model name understood by your LLM endpoint. |

Authentication:

| Variable | Default | Notes |
| --- | --- | --- |
| `AUTH_MODE` | `none` | Use `oidc` to require login. |
| `OIDC_ISSUER_URL` | empty | Required for OIDC. Must be the issuer URL from the provider metadata. |
| `OIDC_CLIENT_ID` | empty | Required for OIDC. |
| `OIDC_CLIENT_SECRET` | empty | Required for OIDC. |
| `OIDC_REDIRECT_URL` | empty | Required for OIDC. Usually `https://your-host/auth/callback`. |
| `SESSION_SECRET` | empty | Required for OIDC. Must be at least 32 characters. |
| `AUTH_ALLOWED_EMAILS` | empty | Optional comma-separated allow-list of exact email addresses. |
| `AUTH_ALLOWED_DOMAINS` | empty | Optional comma-separated allow-list of email domains. |

Legacy variables `NOTE_OUTPUT_DIR` and `TRANSCRIPT_OUTPUT_DIR` are still accepted for compatibility but are not used by the current database-backed output path.

## OIDC With PKCE

Recall supports OIDC login with the authorization code flow and PKCE. In the provider, create an OIDC client with these settings:

- Client type: confidential or web application.
- Redirect URI: the exact value you will set in `OIDC_REDIRECT_URL`, for example `https://recall.example.com/auth/callback`.
- Grant type: authorization code.
- PKCE: enabled. Recall sends an S256 code challenge on login and verifies the code verifier during callback.
- Scopes: `openid`, `profile`, and `email`.

Then set:

```dotenv
STORE_TYPE=postgres
AUTH_MODE=oidc
OIDC_ISSUER_URL=https://issuer.example.com/application/o/recall/
OIDC_CLIENT_ID=your-client-id
OIDC_CLIENT_SECRET=your-client-secret
OIDC_REDIRECT_URL=https://recall.example.com/auth/callback
SESSION_SECRET=replace-with-at-least-32-random-characters
AUTH_ALLOWED_DOMAINS=example.com
```

Important behavior:

- `AUTH_MODE=oidc` requires `STORE_TYPE=postgres`.
- Recall discovers the provider from `OIDC_ISSUER_URL` using standard OIDC metadata.
- The login flow stores short-lived state and PKCE verifier cookies, then creates a signed `recall_session` cookie after callback.
- Session cookies are marked `Secure` only when `OIDC_REDIRECT_URL` uses `https`.
- If the identity provider returns an `email` claim with `email_verified=false`, login is rejected.
- If neither `AUTH_ALLOWED_EMAILS` nor `AUTH_ALLOWED_DOMAINS` is set, any valid user from the issuer can log in.

Start the Postgres-backed OIDC setup with:

```sh
docker compose --profile postgres up --build
```

For a public deployment, put Recall behind a TLS-terminating reverse proxy and make `OIDC_REDIRECT_URL` match the external HTTPS URL exactly.

## API

Create a briefing:

```sh
curl -sS http://localhost:8088/briefings \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/video","tags":["research","demo"]}'
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

Useful endpoints:

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/healthz` | Health check. |
| `GET` | `/api/me` | Current auth status and user. |
| `POST` | `/briefings` | Create a new briefing job. |
| `GET` | `/api/jobs` | List visible jobs for the current user. |
| `GET` | `/api/jobs/{job-id}` | Get one job. |
| `GET` | `/artefacts/{artefact-id}` | View a note or transcript inline. |
| `POST` | `/api/jobs/{job-id}/resummarise` | Regenerate the note from the stored transcript. |
| `POST` | `/api/jobs/{job-id}/retranscribe` | Rerun transcription and regenerate the note. |
| `DELETE` | `/api/jobs/{job-id}` | Delete a job and its artefacts. |
| `GET` | `/auth/login` | Start OIDC login when enabled. |
| `GET` | `/auth/callback` | OIDC callback. |
| `POST` | `/auth/logout` | Clear the session cookie. |

## Output

Recall stores:

- job records
- generated Markdown briefing notes
- generated Markdown transcripts
- timed transcript paragraphs when timestamps are available
- speaker labels when the transcription backend returns diarisation data

Job work files remain under:

```text
/data/jobs/<job-id>
```

Artefacts are served from:

```text
/artefacts/<artefact-id>
```

## Transcription Backends

### Bundled Whisper Server

The included Compose file uses `hwdsl2/whisper-server`:

```yaml
TRANSCRIBER_TYPE: "openai"
WHISPER_BASE_URL: "http://whisper:9000"
```

Recall requests verbose JSON with segment and word timestamps. It uses timing gaps, punctuation, transition words, speaker changes, and length limits to group the transcript into timestamped paragraphs before sending it to the LLM. If diarisation is enabled in the Whisper server and the response includes speaker labels, Recall renders them as labels such as `SPEAKER_00`.

### Wyoming Faster-Whisper

For a Wyoming endpoint, set:

```dotenv
TRANSCRIBER_TYPE=wyoming
WYOMING_WHISPER_ADDR=host.docker.internal:10300
WYOMING_WHISPER_LANGUAGE=en
```

For a server on another machine, use `host:port`, for example:

```dotenv
WYOMING_WHISPER_ADDR=192.168.1.50:10300
```

Recall converts downloaded audio to 16 kHz mono signed 16-bit PCM with `ffmpeg`, then sends Wyoming `transcribe`, `audio-start`, `audio-chunk`, and `audio-stop` events over TCP. Standard Wyoming transcript events usually return text only, so timestamped paragraphing depends on backend support.

## YouTube Download Issues

YouTube changes frequently, and old `yt-dlp` versions can break. The Docker image installs `yt-dlp` from PyPI, so rebuilding usually picks up extractor fixes:

```sh
docker compose build --no-cache recall
docker compose up -d
```

If YouTube returns errors such as `Precondition check failed`, `HTTP Error 400`, `HTTP Error 403`, or `nsig extraction failed`, try passing `yt-dlp` overrides:

```dotenv
YTDLP_EXTRA_ARGS=--extractor-args=youtube:player_client=default,-tv
```

Some age-restricted, private, bot-checked, or region-limited videos may require cookies:

```dotenv
YTDLP_EXTRA_ARGS=--cookies=/data/cookies.txt
```

Mount the cookies file under `/data` if you use this option.

## Security Notes

- Keep unauthenticated deployments on a trusted local network.
- Treat submitted URLs as untrusted input.
- The container runs read-only, drops Linux capabilities, and writes only to `/data`, `/tmp`, and mounted volumes.
- Whisper and LLM endpoints receive submitted media-derived content.
- OIDC protects Recall routes, but it does not make arbitrary URL downloading safe on an untrusted network by itself.

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

## Legal And Terms

Only download or process content that you are permitted to use. Respect copyright, platform terms, and privacy obligations.
