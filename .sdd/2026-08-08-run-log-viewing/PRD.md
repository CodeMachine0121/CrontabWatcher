# PRD · run log viewing

## User Stories

- **US-1** 作為使用者，我要點進某個 job 就看到它最近的輸出內容，不必 ssh 進機器 `tail -f`。
- **US-2** 作為使用者，對於 managed job 我要看到逐次執行的清單（時間、耗時、exit code、觸發來源），並能展開看該次的輸出摘要。
- **US-3** 作為使用者，當某個 job 沒有 log 可看時，我要被明確告知**為什麼**（輸出被丟到 `/dev/null`／沒有 redirect／未納管），以及該怎麼做才能看到。
- **US-4** 作為使用者，log 檔很大時服務不能把記憶體吃光或卡住。

## Business Flow

```
GET /jobs/:jobId/log?lines=N
  → JobRunApplication.TailJobLog(jobID, lines)
      lines 未給 → LOG_TAIL_LINES；clamp 到 1..5000
      → JobRunService.TailJobLog
          crontab repo.Load() → FindJob         ← 無 → ErrCronJobNotFound(404)
          job.LogSource() == none               → ErrJobLogUnavailable(409)
          filePath = job.ResolveLogFilePath(dir)
          job log repo.Tail(filePath, lines)    ← 檔案不存在 → 空內容 + exists=false
          → JobLogDto{filePath, logSource, exists, truncated, content}

GET /jobs/:jobId/runs?limit=N
  → JobRunApplication.ListJobRuns
      → JobRunService.ListJobRuns
          crontab repo.Load() → FindJob         ← 無 → 404
          非 managed → 回空陣列 + unavailableReason
          job run repo.ListByJobID(jobID, limit) → 新到舊
          → []JobRunDto
```

## 規格細節

### `JobLogDto`

| 欄位 | 說明 |
|:---|:---|
| `jobId` | |
| `logSource` | `managed`／`redirect` |
| `filePath` | 實際讀的檔案路徑 |
| `exists` | log 檔是否存在 |
| `truncated` | 是否因 bytes 上限被截斷（內容為檔案尾端） |
| `lineCount` | 實際回傳的行數 |
| `content` | log 內容 |

### `JobRunDto`

| 欄位 | 說明 |
|:---|:---|
| `runId`／`jobId` | |
| `triggerSource` | `schedule`／`manual` |
| `runStatus` | `running`／`succeeded`／`failed`／`timedOut`／`unknown` |
| `startedAt`／`finishedAt` | RFC3339；`finishedAt` 未完成時為 `null` |
| `durationMilliseconds` | 未完成時為 `null` |
| `exitCode` | 未完成時為 `null` |
| `outputExcerpt` | 上限 8 KiB 的輸出摘要 |
| `outputTruncated` | 摘要是否被截斷 |

### `JobRunListDto`

```json
{ "jobId": "...", "runs": [...], "unavailableReason": "job is not managed by crontab-watcher; adopt it to record runs" }
```

`unavailableReason` 僅在 `runs` 為空且原因是「非 managed」時出現，其餘為空字串。

### Tail 演算法

從檔尾以固定 chunk（64 KiB）往前讀，累積到 `lines` 個換行或觸及 1 MiB 上限即停。**不整檔載入**。回傳時去掉第一行可能被切斷的殘片，並在觸及上限時標 `truncated=true`。

### 頁面

`GET /jobs/:jobId/detail` 顯示三塊：job 基本資訊、執行歷史表格（`GET /fragments/jobs/:jobId/runs` 每 10 秒輪詢）、log 內容 `<pre>`（`GET /fragments/jobs/:jobId/log` 每 10 秒輪詢）。`logSource=none` 時 log 區塊改顯示說明卡片與「轉為納管」按鈕。

## Test Cases

### `JobLogRepository.Tail`（真實檔案系統，`t.TempDir()`）

| ID | 情境 | 期望 |
|:---|:---|:---|
| RL-01 | 10 行檔案、要 5 行 | 回最後 5 行 |
| RL-02 | 3 行檔案、要 10 行 | 回全部 3 行、不報錯 |
| RL-03 | 檔案不存在 | 回空字串 + `os.IsNotExist` 可辨識的錯誤或空內容旗標 |
| RL-04 | 空檔案 | 回空字串 |
| RL-05 | 尾端無換行的檔案 | 最後一行完整回傳 |
| RL-06 | 超過 1 MiB 的檔案、要 200 行 | 只讀尾端、`truncated` 正確、耗時不隨檔案大小線性成長 |
| RL-07 | 單行超長（> 64 KiB chunk）的檔案 | 不無限迴圈、正確回傳 |
| RL-08 | `lines` 為 0 或負數 | 回錯誤或 clamp 到 1（實作選 clamp，需有測試釘住） |
| RL-09 | 含非 UTF-8 位元組的檔案 | 不 panic，以 replacement char 呈現 |

### `JobRunRepository` 讀取

| ID | 情境 | 期望 |
|:---|:---|:---|
| RL-10 | `runs.jsonl` 有 5 筆同 job | `ListByJobID` 回新到舊 |
| RL-11 | `limit=2` | 只回最新 2 筆 |
| RL-12 | 混雜多個 job 的紀錄 | 只回指定 jobID 的 |
| RL-13 | 檔案不存在 | 回空 slice、無錯誤 |
| RL-14 | 檔案中有一行壞掉的 JSON | **略過該行**、其餘正常回傳、不整批失敗 |
| RL-15 | `LatestByJobIDs` 多個 jobID | 回 map，每個 key 對到該 job 最新一筆 |

### `JobRunService`

| ID | 情境 | 期望 |
|:---|:---|:---|
| RL-16 | `TailJobLog` on managed job | 讀 `<dir>/<jobId>.log` |
| RL-17 | `TailJobLog` on redirect job | 讀 redirect 的 target |
| RL-18 | `TailJobLog` on `logSource=none` | 回 `ErrJobLogUnavailable` |
| RL-19 | `TailJobLog` on 不存在的 jobID | 回 `ErrCronJobNotFound` |
| RL-20 | `ListJobRuns` on foreign job | 空陣列 + `unavailableReason` 非空 |
| RL-21 | `ListJobRuns` on managed job | 回紀錄 |

### Application 與 Controller

| ID | 情境 | 期望 |
|:---|:---|:---|
| RL-22 | `?lines=10000` | clamp 到 5000 |
| RL-23 | `?lines=abc` | 400 |
| RL-24 | `?lines` 未給 | 用 `LOG_TAIL_LINES` |
| RL-25 | `logSource=none` 的 log 端點 | 409 + 訊息含建議動作 |
| RL-26 | log 檔不存在 | 200 + `exists=false` + 空 content |

## 驗收標準

- RL-01..RL-26 綠燈
- 對一個 100 MB 的 log 檔 tail 200 行，回應時間 < 100ms（驗證 tail 不是整檔載入）
- 瀏覽器詳情頁三個區塊都會自動刷新
