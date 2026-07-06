FROM golang:1.24-alpine AS build

WORKDIR /app/backend
COPY backend/go.mod backend/go.sum* ./
RUN go mod download
COPY backend/ ./
RUN go build -o /bin/chat-server ./cmd/server
RUN go build -o /bin/tts-worker ./cmd/tts-worker
RUN go build -o /bin/vapid-keygen ./cmd/vapid-keygen

FROM alpine:3.22

WORKDIR /app/backend
RUN apk add --no-cache ca-certificates ffmpeg
COPY --from=build /bin/chat-server /bin/chat-server
COPY --from=build /bin/tts-worker /bin/tts-worker
# VAPID鍵生成CLI。`docker compose run --rm backend /bin/vapid-keygen` で単発実行して使う。
COPY --from=build /bin/vapid-keygen /bin/vapid-keygen
COPY backend/migrations ./migrations
# アプリバージョン。config.loadVersion が実行時に /app/version.full（あれば）→ /app/version を読む。
# version.full は make version が生成（git管理外）。未生成でも version で動作する。
COPY version* /app/
CMD ["/bin/chat-server"]
