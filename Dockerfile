FROM golang:1.25-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/recall ./cmd/recall

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates ffmpeg python3 python3-venv \
    && python3 -m venv /opt/yt-dlp \
    && /opt/yt-dlp/bin/pip install --no-cache-dir --upgrade pip yt-dlp \
    && ln -s /opt/yt-dlp/bin/yt-dlp /usr/local/bin/yt-dlp \
    && yt-dlp --version \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --system --uid 10001 --create-home --home-dir /nonexistent recall \
    && mkdir -p /data /vault \
    && chown -R recall:recall /data /vault

COPY --from=build /out/recall /usr/local/bin/recall

USER recall
WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/recall"]
