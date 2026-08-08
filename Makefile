.PHONY: start start-host start-desktop build app dmg test vet check docker-build docker-up docker-down docker-logs mocks

BINARY := bin/cronwatch
PACKAGE := ./cmd/cronwatch

# start-host 的狀態存放位置：crontab 備份、執行紀錄、納管 job 的 log。
HOST_STATE_DIRECTORY ?= $(HOME)/.local/state/crontab-watcher

# start 用的沙箱位置（專案內，已在 .gitignore）。
LOCAL_STATE_DIRECTORY ?= ./data

# start 跑在一份**專案內的沙箱 crontab** 上，不碰你真正的 crontab。適合開發與試玩。
#
# 刻意不使用內建預設值：那些預設是容器裡的路徑（/data/...），在開發機上不存在，
# 於是頁面會顯示一份空清單，看起來像壞了。指向 ./data 之後，看到的空清單就真的是
# 一份空的沙箱 crontab —— 那是實話。
#
# 想看你自己真正在用的 crontab，用 `make start-host`。
start: build
	@mkdir -p "$(LOCAL_STATE_DIRECTORY)/crontabs" "$(LOCAL_STATE_DIRECTORY)/logs" "$(LOCAL_STATE_DIRECTORY)/backups"
	@test -f "$(LOCAL_STATE_DIRECTORY)/crontabs/root" || \
		printf '# sandbox crontab for local development — not your real crontab\n' \
		> "$(LOCAL_STATE_DIRECTORY)/crontabs/root"
	CRONTAB_FILE_PATH="$(LOCAL_STATE_DIRECTORY)/crontabs/root" \
	WRAPPER_BINARY_PATH="$(CURDIR)/$(BINARY)" \
	RUN_LOG_DIRECTORY="$(LOCAL_STATE_DIRECTORY)/logs" \
	RUN_RECORD_FILE_PATH="$(LOCAL_STATE_DIRECTORY)/runs.jsonl" \
	CRONTAB_BACKUP_DIRECTORY="$(LOCAL_STATE_DIRECTORY)/backups" \
	./$(BINARY) serve

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

# start-desktop 進駐 macOS 選單列。看的是與 start-host 同一份東西 ——「你真正在用
# 的那份 crontab」—— 差別只在怎麼呈現：不必開終端機、不必開瀏覽器分頁，出事時會
# 主動通知。
#
# 服務綁在 loopback 的臨時埠上，只給這台機器上的視窗看；狀態與 start-host 共用
# $(HOST_STATE_DIRECTORY)，兩種啟動方式因此看得到同一份執行歷史。
#
# 一樣先 build 而不是 go run：go run 的執行檔在暫存目錄，而那個路徑會被寫進
# crontab，也會被用來開啟視窗子程序。
#
# 僅 macOS。其他平台會直接說明並退出。
start-desktop: build
	./$(BINARY) desktop

# app／dmg 把服務包成一個可以拖進「應用程式」的 macOS app。
#
# 包起來之後雙擊即進駐選單列（binary 認得自己在 app bundle 裡，預設身分因此是
# desktop 而不是 serve），而納管的 crontab 條目會指向
# /Applications/CrontabWatcher.app/Contents/MacOS/cronwatch —— 一個不會因為重新
# 編譯或清掉 bin/ 而消失的穩定路徑，這是它比 make start-desktop 更適合長期使用的
# 原因。
#
# 版本號可覆寫：make dmg VERSION=0.2.0
VERSION ?= 0.1.0

app:
	./scripts/package-macos-app.sh $(VERSION)

dmg: app

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
