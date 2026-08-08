# UL-MAP · 通用語言地圖

**所有實體／動作／識別字命名以此為準，不得自創同義詞。** 新增業務詞彙時同步補進本文件。

---

## 實體（entity · `internal/domain/entity/`）

| 詞彙 | 說明 | 主要 method |
|:---|:---|:---|
| `CrontabDocument` | 一整份 crontab 檔案：有序的 `CrontabLine` 集合，以及由其解析出的 `CronJob` 清單。負責**無損** render 回檔案 | `Render()`、`Jobs()`、`FindJob(jobId)`、`UpsertJob(...)`、`RemoveJob(jobId)`、`SetJobEnabled(jobId, bool)` |
| `CronJob` | 一筆排程條目 | `NextRunAt(from)`、`ResolveLogFilePath(managedDir)`、`IsManaged()`、`LogSource()` |
| `CronSchedule` | cron 表達式（含 alias 展開） | `NextRunAt(from)`、`IsPredictable()`、`Expression()`、`Describe()` |
| `JobRun` | 一次執行的紀錄 | `Duration()`、`IsFinished()`、`Succeeded()` |

## 值物件（vo · `internal/domain/vo/`）

| 詞彙 | 說明 |
|:---|:---|
| `CrontabLine` | crontab 檔案的單一行：原始文字 + 分類（`CrontabLineKind`） |
| `CommandRedirect` | 從指令尾端解析出的輸出重導向：目標路徑、是否 append、stderr 是否併入、被剝離的原始片段 |

## 列舉（enum）

| 詞彙 | 值 | 非法值正規化 |
|:---|:---|:---|
| `CrontabLineKind` | `blank`／`comment`／`marker`／`environment`／`jobEntry`／`disabledJobEntry` | `comment` |
| `JobOrigin` | `managed`（本服務包裝過、有 marker）／`foreign`（使用者手寫） | `foreign` |
| `LogSource` | `managed`（本服務指定的 log 檔）／`redirect`（從指令 redirect 解析出）／`none`（無 log 可讀） | `none` |
| `TriggerSource` | `schedule`（cron 觸發）／`manual`（瀏覽器觸發） | `schedule` |
| `RunStatus` | `running`／`succeeded`（exit code 0）／`failed`（非 0）／`timedOut`／`unknown`（foreign job 無法判定） | `unknown` |

## 動作（use case 語彙）

| 詞彙 | 英文識別字 | 說明 |
|:---|:---|:---|
| 列出排程 | `ListCronJobs` | 讀 crontab，回傳全部 job 含下次執行時間與最近一次執行 |
| 檢視輸出 | `TailJobLog` | 從 job 的 log 檔尾端讀取指定行數 |
| 查執行歷史 | `ListJobRuns` | 從 `runs.jsonl` 讀該 job 的紀錄（新到舊） |
| 手動觸發 | `TriggerJobRun` | 立即執行一次，`TriggerSource=manual` |
| 建立／更新／刪除 | `CreateCronJob`／`UpdateCronJob`／`DeleteCronJob` | 寫回 crontab |
| 啟用／停用 | `EnableCronJob`／`DisableCronJob` | 取消註解／註解掉該條目，**不刪除** |
| 轉為 managed | `AdoptCronJob` | 補 marker、把指令包成 wrapper、剝離原 redirect |
| 記錄執行 | `RecordJobRun` | wrapper 落地一筆 `JobRun` |

## 關鍵約定

| 概念 | 約定 |
|:---|:---|
| marker 註解 | `# cronwatch:id=<uuid>`，緊鄰其 job 條目的上一行 |
| 剝離 redirect 註解 | `# cronwatch:strippedRedirect=<原始片段>` |
| wrapper 指令形狀 | `<binaryPath> run --job=<jobId> -- <原指令>` |
| foreign job 的 `JobID` | `sha256(schedule + "\x00" + command)` 取前 12 個 hex 字元 |
| managed job 的 log 檔 | `<RUN_LOG_DIRECTORY>/<jobId>.log` |
| 執行紀錄檔 | `<RUN_RECORD_FILE_PATH>`，append-only JSON Lines |

## 明確不使用的詞

| 不用 | 用這個 | 原因 |
|:---|:---|:---|
| port | 介面（`internal/domain/interface/`） | 專案約定 |
| task／schedule（當名詞指 job） | `CronJob` | `schedule` 一詞保留給 `CronSchedule`（表達式本身） |
| execution／history（當型別名） | `JobRun` | 一次執行就叫 run，與 crontab 生態一致 |
| delete（指停用） | disable | 停用是註解掉、可還原；delete 是真的移除該行 |
| DB／record（指持久化層） | 檔案系統／`runs.jsonl` | 本專案無資料庫 |
