# syntax=docker/dockerfile:1
#
# crontab-watcher — 自管模式（container 內同時是排程器與執行環境）
#
# 注意：runtime stage 只有 alpine 的基本工具。你的 cronjob 指令若需要
# python / curl / node / mysql-client 等，必須自行加到下方 runtime stage 的
# apk add 清單裡 —— 這是自管模式的代價，也是它「執行結果可信」的原因。

# ---------- build stage ----------
FROM golang:1.26-alpine AS builder

WORKDIR /src

# 先只複製 module 檔案，讓依賴下載這層能被 cache
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 → 完全靜態，runtime 不需要 libc 相依
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/cronwatch \
    ./cmd/cronwatch

# ---------- runtime stage ----------
FROM alpine:3.21

# busybox 已內含 crond；tzdata 供 TZ 計算排程時區（cron 語意必需）
# 若 cronjob 需要其他 runtime，加在這裡
RUN apk add --no-cache tzdata ca-certificates

COPY --from=builder /out/cronwatch /app/cronwatch
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

# crontab 檔、log、runs.jsonl、備份全部落在這個 volume
# → 備份整個服務的狀態等於 cp -r data
VOLUME ["/data"]

ENV SERVER_ADDRESS=0.0.0.0:8080 \
    CRONTAB_FILE_PATH=/data/crontabs/root \
    RUN_LOG_DIRECTORY=/data/logs \
    RUN_RECORD_FILE_PATH=/data/runs.jsonl \
    CRONTAB_BACKUP_DIRECTORY=/data/backups \
    TZ=Asia/Taipei

EXPOSE 8080

ENTRYPOINT ["/app/docker-entrypoint.sh"]
