#!/bin/sh
# 自管模式的 entrypoint：同時跑 busybox crond（排程）與 cronwatch serve（檢視/操作）。
# 任一方掛掉就整個容器退出，交給 docker 的 restart policy 處理 —— 不容忍
# 「crond 死了但 web 還活著」這種假健康狀態。
set -eu

CRONTAB_DIRECTORY="$(dirname "${CRONTAB_FILE_PATH:-/data/crontabs/root}")"

mkdir -p \
    "${CRONTAB_DIRECTORY}" \
    "${RUN_LOG_DIRECTORY:-/data/logs}" \
    "${CRONTAB_BACKUP_DIRECTORY:-/data/backups}"

# 首次啟動時建立空的 crontab，讓 crond 不會抱怨、UI 也有東西可讀
[ -f "${CRONTAB_FILE_PATH:-/data/crontabs/root}" ] || \
    printf '# managed by crontab-watcher\n' > "${CRONTAB_FILE_PATH:-/data/crontabs/root}"

# busybox crond：-f 前景、-d 8 log level、-c 指定 crontab 目錄（檔名即使用者名）
crond -f -d 8 -c "${CRONTAB_DIRECTORY}" &
CROND_PID=$!

/app/cronwatch serve &
SERVER_PID=$!

# 收到 SIGTERM（docker stop）時把兩個子程序一起帶走
terminate() {
    kill "${CROND_PID}" "${SERVER_PID}" 2>/dev/null || true
    exit 0
}
trap terminate TERM INT

# 輪詢兩個子程序，任一死亡即收掉另一個並退出。
# 刻意不用 `wait -n` —— 那是 bash 擴充，busybox ash 不保證支援。
while kill -0 "${CROND_PID}" 2>/dev/null && kill -0 "${SERVER_PID}" 2>/dev/null; do
    sleep 5
done

echo "entrypoint: a child process exited (crond=${CROND_PID} server=${SERVER_PID}), shutting down" >&2
kill "${CROND_PID}" "${SERVER_PID}" 2>/dev/null || true
exit 1
