# syntax=docker/dockerfile:1

# ---- compila o bot (Go puro, sem CGO) ----
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/guardei ./cmd/bot

# ---- baixa o yt-dlp e confere o checksum ----
FROM alpine:3 AS ytdlp
ARG YTDLP_VERSION=2026.08.19
RUN apk add --no-cache curl \
 && curl -fsSL -o /yt-dlp "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp_musllinux" \
 && curl -fsSL -o /sums "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/SHA2-256SUMS" \
 && echo "$(grep ' yt-dlp_musllinux$' /sums | cut -d' ' -f1)  /yt-dlp" | sha256sum -c - \
 && chmod +x /yt-dlp

# ---- imagem final ----
FROM alpine:3
# sqlite: só para o `.backup` (veja o README); ffmpeg: converte e fatia o áudio.
RUN apk add --no-cache ffmpeg ca-certificates sqlite \
 && adduser -D -u 10001 app \
 && mkdir -p /data /opt/yt-dlp \
 && chown app:app /data /opt/yt-dlp

# O yt-dlp fica gravável pelo usuário do bot: ele se atualiza toda semana (-U).
COPY --from=ytdlp --chown=app:app /yt-dlp /opt/yt-dlp/yt-dlp
COPY --from=build /out/guardei /usr/local/bin/guardei

USER app
ENV DB_PATH=/data/app.db \
    YTDLP_PATH=/opt/yt-dlp/yt-dlp
VOLUME /data
ENTRYPOINT ["guardei"]
