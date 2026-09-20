# syntax=docker/dockerfile:1

# ---- compila o bot (Go puro, sem CGO) ----
FROM docker.io/library/golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
# Os caches do Go ficam fora das camadas da imagem: o disco cresce pouco a cada
# atualização e recompilar depois de mudar o código leva segundos.
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
# O SQLite em Go puro (modernc.org/libc) é um pacote enorme: sem estes limites o
# compilador passa de 768 MB e é morto em VPS pequenas. Com eles, compila com
# 512 MB, sem swap (medido); custa uns segundos a mais.
ENV GOGC=20 GOMEMLIMIT=320MiB GOFLAGS=-p=1
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/guardei ./cmd/bot

# ---- baixa o yt-dlp e confere o checksum ----
FROM docker.io/library/alpine:3 AS ytdlp
ARG YTDLP_VERSION=2026.08.19
RUN apk add --no-cache curl \
 && curl -fsSL -o /yt-dlp "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp_musllinux" \
 && curl -fsSL -o /sums "https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/SHA2-256SUMS" \
 && echo "$(grep ' yt-dlp_musllinux$' /sums | cut -d' ' -f1)  /yt-dlp" | sha256sum -c - \
 && chmod +x /yt-dlp

# ---- imagem final ----
FROM docker.io/library/alpine:3
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
