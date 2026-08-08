# PRD · job listing

## User Stories

- **US-1** 作為使用者，我打開瀏覽器就能看到這台機器上所有的 cronjob，含排程、下次執行時間、是否啟用。
- **US-2** 作為使用者，我要一眼看出哪些 job 的執行結果是完整可信的（managed）、哪些只能看原始 log（redirect）、哪些根本沒有輸出可看（none）。
- **US-3** 作為使用者，我要看到每個 job 最近一次執行的結果與時間，好判斷它有沒有在正常運作。
- **US-4** 作為使用者，當 crontab 檔案不存在或讀不到時，我要看到明確的錯誤與檔案路徑，而不是空白的列表。

## Business Flow

```
GET /jobs（或 GET /）
  → CronJobApplication.ListCronJobs
      now = clock.Now().In(location)
      → CronJobService.ListCronJobs(now)
          crontab repo.Load()                     ← 讀不到即 502
          document.Jobs()
          job run repo.LatestByJobIDs(所有 jobID)  ← 讀不到即 502
          逐 job：NextRunAt(now)、LogSource()、附上最近一次 JobRun
          → []CronJobDto
  → JSON 或 HTML 渲染
```

## 規格細節

### `CronJobDto`

| 欄位 | 型別 | 說明 |
|:---|:---|:---|
| `jobId` | string | managed 為 uuid、foreign 為 12 hex |
| `origin` | string | `managed`／`foreign` |
| `enabled` | bool | 條目是否被註解掉 |
| `scheduleExpression` | string | 展開後的表達式 |
| `scheduleOriginalExpression` | string | 原文（`@daily`） |
| `scheduleDescription` | string | 人類可讀描述 |
| `command` | string | managed job 回**內層原指令**（不含 wrapper），另附 `rawCommand` 為 crontab 上的原文 |
| `rawCommand` | string | crontab 條目上的完整指令原文 |
| `nextRunAt` | *string (RFC3339) | `@reboot` 或已停用時為 `null` |
| `nextRunPredictable` | bool | |
| `logSource` | string | `managed`／`redirect`／`none` |
| `logFilePath` | string | `none` 時為空字串 |
| `latestRun` | *JobRunDto | 無紀錄時為 `null` |
| `lineNumber` | int | 在 crontab 檔案中的行號（1-based），供對照 |

**已停用的 job 不計算 `nextRunAt`**（回 `null`）—— 它不會跑，給出時間是誤導。

### 頁面

`GET /` 渲染 job 表格，每列顯示：狀態燈（依 `latestRun.runStatus`）、排程描述、指令、下次執行、log 來源標籤、最近執行時間。表格本體以 `GET /fragments/jobs` 每 15 秒輪詢刷新。

狀態燈規則：

| `latestRun` | 燈 |
|:---|:---|
| `succeeded` | 綠 |
| `failed`／`timedOut` | 紅 |
| `running` | 藍（動態） |
| `unknown` 或 `null` | 灰 |

## Test Cases

### `CronJobService.ListCronJobs`（mock repo）

| ID | 情境 | 期望 |
|:---|:---|:---|
| JL-01 | crontab 有 2 個 foreign、1 個 managed | 回 3 筆 DTO，`origin` 正確 |
| JL-02 | crontab 為空 | 回空 slice、**非 nil**、無錯誤 |
| JL-03 | crontab repo.Load 失敗 | 錯誤往上拋 |
| JL-04 | job run repo 有該 job 的紀錄 | `latestRun` 帶入最新一筆 |
| JL-05 | job run repo 無該 job 紀錄 | `latestRun` 為 nil |
| JL-06 | job run repo 讀取失敗 | 錯誤往上拋（**不**降級成 nil，避免謊報「沒跑過」） |
| JL-07 | 已停用的 job | `enabled=false`、`nextRunAt` 為 nil |
| JL-08 | `@reboot` job | `nextRunPredictable=false`、`nextRunAt` 為 nil |
| JL-09 | managed job | `command` 為內層指令、`rawCommand` 為 wrapper 全文 |
| JL-10 | 三種 `logSource` 混合 | 各自的 `logFilePath` 正確 |
| JL-11 | `lineNumber` | 對應該條目在檔案中的實際行號 |

### `CronJobService.GetCronJob`

| ID | 情境 | 期望 |
|:---|:---|:---|
| JL-12 | 存在的 jobID | 回該筆 DTO |
| JL-13 | 不存在的 jobID | 回 `ErrCronJobNotFound` |

### `CronJobApplication`（真實 service + mock 最外層介面）

| ID | 情境 | 期望 |
|:---|:---|:---|
| JL-14 | 完整 crontab 文字經 repo 回傳 | DTO 內容正確（連帶驗證 parse → service → DTO 全鏈） |
| JL-15 | 時區為 `Asia/Taipei` | `nextRunAt` 的偏移為 `+08:00` |
| JL-16 | 時區為 `UTC` | 偏移為 `Z` |

### Controller 狀態碼

| ID | 情境 | 期望 |
|:---|:---|:---|
| JL-17 | 正常 | 200 + JSON 陣列 |
| JL-18 | repo 失敗 | 502 + 錯誤訊息含檔案路徑 |
| JL-19 | `GET /jobs/:jobId` 不存在 | 404 |

## 驗收標準

- JL-01..JL-19 綠燈
- `curl localhost:8080/jobs` 對一份真實 crontab 回出正確 JSON
- 瀏覽器打開 `/` 看得到表格且 15 秒自動刷新
