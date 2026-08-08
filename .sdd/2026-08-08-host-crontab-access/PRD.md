# PRD · host crontab access

## User Stories

- **US-1** 作為使用者，我要在自己的 Mac 上直接跑這個服務、看到 `crontab -l` 列出的那些 job，不必先把它們搬進容器。
- **US-2** 作為使用者，我不要為了讓它讀得到 crontab 而用 `sudo` 跑一個能執行任意指令的 web 服務。
- **US-3** 作為使用者，服務改我的 crontab 之前要有備份 —— `crontab <file>` 是整份取代，出錯的代價是全部 job 消失。
- **US-4** 作為使用者，我剛好同時用 `crontab -e` 改了東西時，服務要拒絕覆寫。
- **US-5** 作為使用者，我要一個指令就能啟動，不必記住六個環境變數。

## Business Flow

```
CRONTAB_SOURCE=crontabCommand
  → dependencies 組 CrontabCommandRepository 而非 CrontabDocumentRepository
      Load()
        執行 `crontab -l`
        ├─ exit 0            → 內容即為 crontab 全文
        ├─ stderr 提到 no crontab → 視為空 crontab（正常狀態）
        └─ 其他失敗          → 錯誤往上拋（502）
        指紋 = sha256(內容)      ← stat 不到檔案，沒有 mtime 可用

      Save(document, expectedFingerprint)
        重新執行 `crontab -l` → 比對指紋
        ├─ 不符 → ErrCrontabChangedExternally（409）
        └─ 相符 ↓
        自我檢查渲染結果的條目數
        備份當下內容 → <backupDir>/crontab.<timestamp>.bak
        寫暫存檔（0600，位於 backupDir 內）
        執行 `crontab <暫存檔>`
        ├─ 失敗 → 錯誤往上拋，crontab 未變（crontab 命令自己會驗語法）
        └─ 成功 → 刪除暫存檔
```

## 規格細節

### 新增環境變數

| 變數 | 預設值 | 用途 |
|:---|:---|:---|
| `CRONTAB_SOURCE` | `file` | `file`＝直接讀寫 `CRONTAB_FILE_PATH`（容器自管模式）；`crontabCommand`＝走 `crontab -l` / `crontab <file>`（host 模式）。無法辨識的值退回 `file` 並警告 |
| `CRONTAB_COMMAND_PATH` | `crontab` | `crontab` 執行檔位置。可覆寫，也讓測試能指向一個假的 |

`CRONTAB_FILE_PATH` 在命令模式下**不用於讀寫**，只出現在 UI 的來源標示上。

### 指紋

`sha256(內容)` 的 hex 前 16 字元。內容雜湊而非 mtime+size，因為命令模式 stat 不到那個檔案。

代價：偵測不到「內容相同但被重寫過」——那本來就不需要偵測。

### 「沒有 crontab」

`crontab -l` 在使用者沒有 crontab 時回非 0 並在 stderr 印 `no crontab for <user>`。這是
**正常狀態**，視為空 crontab，不是錯誤 —— 回錯誤會讓還沒有任何 job 的人連頁面都打不開。

### 備份

命令模式的備份時間戳來自時鐘無從取得的檔案 mtime，因此改用「備份寫入當下」的時間，
格式與檔案模式一致（`crontab.<20060102T150405.000000000>.bak`）。

### `make start-host`

```
make start-host
```

- 先 `make build`，用 `bin/cronwatch` 而非 `go run` —— `go run` 的執行檔在暫存目錄，
  寫進 crontab 的條目會在它被清掉之後失效。
- 狀態放 `$(HOME)/.local/state/crontab-watcher/`（可用 `HOST_STATE_DIRECTORY` 覆寫）。
- 綁 `127.0.0.1:8080`。
- 寫入預設開啟。啟動時額外印一行講清楚「這會取代你真正的 crontab、備份在哪」。
  想只看不改就 `CRONTAB_WRITE_ENABLED=false make start-host`。

## Test Cases

測試以 `t.TempDir()` 裡的一個**假 crontab 腳本**進行，該腳本以一個檔案模擬
`crontab -l` 與 `crontab <file>`。**絕不碰測試執行者真正的 crontab。**

| ID | 情境 | 期望 |
|:---|:---|:---|
| HC-01 | `Load` 有內容的 crontab | 內容與指紋正確 |
| HC-02 | `Load` 使用者沒有 crontab（exit 1 + `no crontab for`） | 空文件、無錯誤 |
| HC-03 | `Load` 命令不存在 | 回錯誤（不是靜靜地當成空的） |
| HC-04 | `Load` 命令以其他原因失敗 | 錯誤含 stderr 內容 |
| HC-05 | 同內容兩次 `Load` | 指紋相同 |
| HC-06 | 不同內容 | 指紋不同 |
| HC-07 | `Save` 正常 | 假 crontab 收到新內容、備份產生 |
| HC-08 | `Save` 指紋不符 | `ErrCrontabChangedExternally`、**內容未變** |
| HC-09 | `Save` 後 `crontab -l` | 讀回剛寫入的內容 |
| HC-10 | `Save` 時 `crontab` 命令回非 0（模擬語法錯誤） | 回錯誤含 stderr、**無殘留暫存檔** |
| HC-11 | `Save` 到沒有 crontab 的使用者 | 成功、不需要備份 |
| HC-12 | 並發 `Save` | mutex 序列化、內容仍是合法 crontab |
| HC-13 | 暫存檔權限 | 0600（crontab 含指令） |
| HC-14 | 整份 CRUD 走命令模式 | 除預期行外 byte-for-byte 相同 |
| HC-15 | `CRONTAB_SOURCE` 無法辨識 | 退回 `file` 並產生警告 |
| HC-16 | `CRONTAB_SOURCE=crontabCommand` | 組出命令模式的 repository |

## 驗收標準

- HC-01..HC-16 綠燈
- **實機驗證**：`make start-host` 之後 `GET /jobs` 列出 `crontab -l` 真正的那些 job
- 實機驗證不得改動執行者的 crontab（只讀）
