# 釐清決策紀錄（Clarify）

動工前所有懸而未決的設計問題與**已裁定的答案**。使用者授權「有問題直接選推薦選項」，故以下皆為推薦答案並已生效。任何後續實作與此衝突時，以本文件為準或先修改本文件。

日期：2026-08-08

---

## 已由使用者裁定

| # | 問題 | 裁定 |
|:--|:---|:---|
| D-01 | 執行結果從哪來？ | **解析 crontab 條目自帶的 redirect 得出 log 位置**；另補 wrapper 路徑取得完整 exit code（見 D-04） |
| D-02 | UI 要做到什麼程度？ | **完整 CRUD**（已確認 Go 無現成 library，parse/render 自己寫） |
| D-03 | 部署形態？ | **Docker**，無資料庫，狀態全落檔案系統 |

## 本文件裁定（推薦答案）

### D-04 · 兩類 job 並存

`JobOrigin` 分 `foreign`（使用者手寫）與 `managed`（本服務包裝過）。

- **foreign**：結果只能從 redirect 目標檔案 tail 原文。**無 exit code、無法切分逐次執行** → `RunStatus` 恆為 `unknown`，`GET /jobs/:jobId/runs` 回空陣列。
- **managed**：指令被包成 `cronwatch run --job=<id> -- <原指令>`，wrapper 落地完整 `JobRun`。

**理由**：不假裝有資料。UI 明確標示每個 job 屬於哪一類、能看到什麼。

### D-05 · foreign job 的身分不穩定

foreign job 沒有 marker 註解，`JobID` 由 `sha256(schedule + "\x00" + command)` 取前 12 個 hex 字元推導。

**後果**：使用者一改該條目，`JobID` 就變，舊的執行紀錄變孤兒。**接受此限制**，UI 在 foreign job 上明示「執行紀錄會在條目被編輯後失去關聯，建議轉為 managed」。

**理由**：唯一的替代方案是主動在使用者的 crontab 裡插入 marker 註解，那等於未經同意就改動檔案，比資料孤兒更糟。

### D-06 · 停用（disable）＝註解掉，不刪除

停用一個 job 是在該行前加 `#`，保留原文。反向辨識規則：**一個註解行若移除開頭的 `#` 與空白後能成功 parse 成合法 cron 條目，即視為「已停用的 job」**。

**已知誤判風險**：純解說性註解若長得像 cron 條目（例：`# 0 3 * * * this is where the backup used to be`）會被誤認為停用的 job。**接受**——因為 `Render()` 無損保留原文，誤判只影響顯示，不會破壞檔案。

### D-07 · crontab 格式支援範圍

| 支援 | 不支援 |
|:---|:---|
| 5 欄標準 schedule（`m h dom mon dow`） | **6 欄（含秒）** → 視為不合法並回明確訊息 |
| `@yearly`／`@annually`／`@monthly`／`@weekly`／`@daily`／`@midnight`／`@hourly` alias | `@reboot` 可 parse 但**無下次執行時間**（`IsPredictable()` 為 false） |
| 使用者 crontab 格式 | `/etc/crontab` 的第 6 欄 user 欄位 |
| 環境變數行（`SHELL=`／`PATH=`／`MAILTO=`／任意 `KEY=value`） | — |

**理由**：實際執行者是 busybox `crond`（自管模式）或標準 cron，兩者都只吃 5 欄。支援 6 欄只會讓 UI 顯示的「下次執行」與現實不符。

### D-08 · 排程時區

`NextRunAt` 一律以 `TZ` 環境變數指定的 `*time.Location` 計算，**不用 UTC 硬算**。entity method 收 `from time.Time` 參數，時區由呼叫方帶入 → 排程計算完全可測。

### D-09 · 手動觸發的並發控制

**同一個 job 不允許並發手動觸發** → 已有一筆 `RunStatus=running` 的紀錄時回 `409`。不同 job 之間可並發。

**理由**：cronjob 多半不是 idempotent（備份、報表、匯入），重複觸發的代價高於「使用者得等」。

### D-10 · 逾時處理

手動觸發一律套 `MANUAL_TRIGGER_TIMEOUT_SECONDS`（預設 900）。逾時以 **process group kill**（`Setpgid` + `kill(-pgid)`）收掉子孫程序，`RunStatus` 標 `timedOut`。

wrapper（`cronwatch run`）**不套逾時**——由 cron 觸發的排程執行該跑多久就跑多久，這是使用者自己排的 job。

### D-11 · 執行輸出的存放

- 完整輸出 **append 到 log 檔**（`<RUN_LOG_DIRECTORY>/<jobId>.log`），每次執行前寫一行可辨識的 run 標頭。
- `runs.jsonl` 只存 metadata + **截斷後的輸出摘要**（`OutputExcerpt`，上限 8 KiB，超出時保留尾端並標記已截斷）。

**理由**：`runs.jsonl` 要能整份載入記憶體做歷史查詢，不能讓單筆巨大輸出把它撐爆；完整內容看 log 檔。

### D-12 · log 讀取上限

tail 只從檔尾往回讀，**單次讀取上限 1 MiB**。超大 log 檔不會把記憶體吃光。行數上限由 `LOG_TAIL_LINES`（預設 200）控制，`?lines=` 可覆寫但硬上限 5000。

### D-13 · adopt（foreign → managed）如何處理原有 redirect

**剝離**指令尾端的 redirect，交由 wrapper 管理輸出，並把被剝離的內容記在 marker 註解裡以便還原：

```cron
# cronwatch:id=8f14e45f-ea8f-4b2c-9c3d-6a1b2c3d4e5f
# cronwatch:strippedRedirect=>> /var/log/backup.log 2>&1
0 3 * * * /app/cronwatch run --job=8f14e45f-... -- /usr/local/bin/backup.sh
```

**後果**：adopt 之後輸出改落在 managed log，原路徑不再更新。UI 在 adopt 確認前明確告知。

**理由**：若保留原 redirect，stdout 在到達 wrapper 之前就被導走，wrapper 只會記到空輸出——那是最糟的結果（看起來有紀錄，實際上是空的）。

### D-14 · 外部改動偵測

寫入前重讀檔案並比對 **mtime + size**，偵測到與讀取時不一致即中止並回 `409`，不覆蓋使用者用 `crontab -e` 的手改。行程內以 mutex 序列化所有寫入。

### D-15 · ID 產生與時間取得走介面

`IIdentifierGenerator`（產生 `JobID`／`RunID`）與 `IClock`（取當下時間）都是 `domain/interface/` 的介面，infra 提供實作。

**理由**：crontab 寫入與執行紀錄的測試需要可預測的 ID 與時間，否則斷言只能寫得很鬆。

### D-16 · 無 log 可讀時的行為

`LogSource=none`（foreign job 且指令無 redirect）時，`GET /jobs/:jobId/log` 回 **409** 並附說明與建議動作（adopt），**不回 200 空字串**。

**理由**：200 空字串會被讀成「跑過但沒輸出」，與「根本無從得知」是完全不同的事實。

### D-17 · 沒有「通知 cron reload」這個步驟（實作時撤回原規劃）

原本規劃了 `ICrondReloadProxy` 與 `CROND_RELOAD_ENABLED` 環境變數，實作時**移除**。

busybox `crond`（自管模式）與 vixie cron（唯讀模式的 host）**都會自己偵測 crontab 檔案的
mtime 變化並重新載入**，最長延遲一分鐘。而唯一「主動」的做法 `crontab <file>` 語意其實是
「安裝這份 crontab」——當 `CRONTAB_FILE_PATH` 本身就是 cron 的 spool 檔時，這個呼叫是循環
且錯誤的。

**理由**：替一個不存在的問題留一個 no-op 抽象，比不留更糟——它會讓後來的人以為 reload 是
必要步驟，並在它「失敗」時去除錯一件根本不影響結果的事。UI 改為明示「變更最長一分鐘內生效」。
