.PHONY: start start-host build test vet check docker-build docker-up docker-down docker-logs mocks

BINARY := bin/cronwatch
PACKAGE := ./cmd/cronwatch

# start-host 的狀態存放位置：crontab 備份、執行紀錄、納管 job 的 log。
HOST_STATE_DIRECTORY ?= $(HOME)/.local/state/crontab-watcher

start:
	go run $(PACKAGE) serve

# start-host 直接在這台機器上跑，看的是 `crontab -l` 列出的**你真正在用的那份
# crontab**，而不是容器內自管的副本。
#
# 為什麼走 crontab 命令而不是直接讀檔：spool 目錄的權限是 drwx------ root:wheel，
# 直接讀必然 permission denied。crontab 是 setuid 執行檔，所以它是唯一不需要 root
# 就能存取使用者 crontab 的途徑 —— 替代方案是用 sudo 跑一個能執行任意指令的 web
# 服務，不值得。
#
# 為什麼先 build 而不是 go run：go run 的執行檔在暫存目錄裡，而這個路徑會被寫進
# crontab 條目，等暫存檔被清掉，那些 job 就再也跑不起來。
#
# 寫入預設開啟（改動會經由 `crontab <file>` 取代你整份 crontab，每次改動前的版本都
# 備份在 $(HOST_STATE_DIRECTORY)/backups）。只想瀏覽不想改：
#
#     CRONTAB_WRITE_ENABLED=false make start-host
#
start-host: build
	@mkdir -p "$(HOST_STATE_DIRECTORY)/logs" "$(HOST_STATE_DIRECTORY)/backups"
	CRONTAB_SOURCE=crontabCommand \
	WRAPPER_BINARY_PATH="$(CURDIR)/$(BINARY)" \
	RUN_LOG_DIRECTORY="$(HOST_STATE_DIRECTORY)/logs" \
	RUN_RECORD_FILE_PATH="$(HOST_STATE_DIRECTORY)/runs.jsonl" \
	CRONTAB_BACKUP_DIRECTORY="$(HOST_STATE_DIRECTORY)/backups" \
	./$(BINARY) serve

build:
	go build -o $(BINARY) $(PACKAGE)

test:
	go test ./...

vet:
	go vet ./...

# commit 前跑這個（沒有 pre-commit hook，靠人工）
check: build vet test

docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

# 需先安裝 mockery CLI，設定讀 .mockery.yaml
mocks:
	mockery
