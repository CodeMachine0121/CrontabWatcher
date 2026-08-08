# 架構設計（Architecture）

跨切片的技術設計。分層規範見 `CLAUDE.md`；本文件定義**具體的型別、介面簽章與資料流**，是實作的藍圖。

---

## 1. 依賴圖

```
cmd/cronwatch (組裝根 + wrapper subcommand)
      │
      ├──▶ controller ──▶ application ──▶ domain/service ──▶ domain/interface
      │                                        │                    ▲
      │                                        ▼                    │
      │                                  domain/entity, vo, dto     │
      └──────────────────────────────────────────────────── infrastructure (實作介面)
```

## 2. 對外介面（`internal/domain/interface/`，一檔一介面）

```go
// i_crontab_document_repository.go
type ICrontabDocumentRepository interface {
    // Load 讀取 crontab 檔並回傳文件 + 讀取當下的版本指紋（mtime+size）
    Load() (*entity.CrontabDocument, string, error)
    // Save 以 expectedFingerprint 做樂觀鎖；不符即回 ErrCrontabChangedExternally
    Save(document *entity.CrontabDocument, expectedFingerprint string) error
}

// i_job_run_repository.go
type IJobRunRepository interface {
    Append(run *entity.JobRun) error
    Update(run *entity.JobRun) error          // 以 RunID 覆寫既有紀錄（running → 完成）
    ListByJobID(jobID string, limit int) ([]*entity.JobRun, error)
    LatestByJobIDs(jobIDs []string) (map[string]*entity.JobRun, error)
    HasRunningRun(jobID string) (bool, error)
}

// i_job_log_repository.go
type IJobLogRepository interface {
    Tail(filePath string, lines int) (string, error)
    Append(filePath string, content string) error
}

// i_command_execution_proxy.go
type ICommandExecutionProxy interface {
    // Execute 以 shell 執行 command，回傳合併後的輸出、exit code。
    // timeout 為 0 表示不套逾時。timedOut 回報是否因逾時被 kill。
    Execute(ctx context.Context, command string, timeout time.Duration) (output string, exitCode int, timedOut bool, err error)
}

// i_crond_reload_proxy.go
type ICrondReloadProxy interface {
    Reload(crontabFilePath string) error
}

// i_identifier_generator.go
type IIdentifierGenerator interface {
    NewIdentifier() string
}

// i_clock.go
type IClock interface {
    Now() time.Time
}
```

**Repository 都不吃 `context`** —— 全是本機檔案 I/O，沒有可取消的網路等待，加 `ctx` 只是雜訊。`ICommandExecutionProxy` 吃 `ctx` 因為逾時取消是它的核心語意。

## 3. Domain entity 關鍵簽章

```go
// CronSchedule ── gronx 只在此處被使用
func NewCronSchedule(expression string) (*CronSchedule, error)
func (schedule *CronSchedule) Expression() string
func (schedule *CronSchedule) IsPredictable() bool            // @reboot → false
func (schedule *CronSchedule) NextRunAt(from time.Time) (time.Time, bool)
func (schedule *CronSchedule) Describe() string               // "每天 03:00"

// CommandRedirect
func ParseCommandRedirect(command string) (bareCommand string, redirect *CommandRedirect)
func (redirect *CommandRedirect) TargetFilePath() string
func (redirect *CommandRedirect) IncludesStandardError() bool
func (redirect *CommandRedirect) RawFragment() string

// CronJob
func NewCronJob(jobID string, schedule *CronSchedule, command string,
                origin JobOrigin, enabled bool, description string,
                strippedRedirect string) *CronJob
func (job *CronJob) NextRunAt(from time.Time) (time.Time, bool)
func (job *CronJob) LogSource() LogSource
func (job *CronJob) ResolveLogFilePath(managedLogDirectory string) string
func (job *CronJob) IsManaged() bool
func (job *CronJob) InnerCommand() string   // managed job 剝掉 wrapper 後的原指令

// CrontabDocument
func NewCrontabDocument(lines []vo.CrontabLine) *CrontabDocument
func ParseCrontabDocument(content string) *CrontabDocument   // 永不失敗：無法解析的行歸為 comment
func (document *CrontabDocument) Render() string             // 無損
func (document *CrontabDocument) Jobs() []*CronJob
func (document *CrontabDocument) FindJob(jobID string) (*CronJob, bool)
func (document *CrontabDocument) SetJobEnabled(jobID string, enabled bool) error
func (document *CrontabDocument) RemoveJob(jobID string) error
func (document *CrontabDocument) AppendJob(job *CronJob, wrapperCommand string) error
func (document *CrontabDocument) ReplaceJob(jobID string, job *CronJob, wrapperCommand string) error

// JobRun
func NewJobRun(runID, jobID string, triggerSource TriggerSource, startedAt time.Time) *JobRun
func (run *JobRun) Finish(finishedAt time.Time, exitCode int, timedOut bool, output string)
func (run *JobRun) Duration() time.Duration
func (run *JobRun) Succeeded() bool
```

> **`parse_crontab_line.go` 放 `entity/`，不放 `infrastructure/crontab/`。** crontab 文字的
> 解析是領域知識（哪一行是條目、什麼算合法排程），不是 I/O 細節；而且它需要
> `CronSchedule` 做合法性判斷。infra 的 `crontab` package 只負責讀寫檔案。
>
> **哨兵錯誤集中在 `entity/domain_error.go`。** 放在最內層讓 service 直接回傳、
> controller 直接以 `errors.Is` 對映狀態碼，不必每層重新宣告或包裝。

### 無損 render 的實作策略

`CrontabDocument` 內部是 `[]vo.CrontabLine`，每行都保留 `RawText`。`Render()` 就是 `strings.Join(rawTexts, "\n")` 加上原檔尾端是否有換行的旗標。**任何修改操作只替換／插入／刪除特定的 `CrontabLine`，其餘元素的 `RawText` 原封不動。** 這讓無損性質成為結構上的保證，而不是靠小心維護。

## 4. Domain Service

| Service | 公開 method | 依賴 |
|:---|:---|:---|
| `CronJobService` | `ListCronJobs(now)`、`GetCronJob(jobID, now)` | crontab repo、job run repo、clock（由 application 傳 now） |
| `JobRunService` | `ListJobRuns(jobID, limit)`、`TailJobLog(jobID, lines)` | crontab repo、job run repo、job log repo |
| `JobExecutionService` | `TriggerJobRun(ctx, jobID, source, timeout)`、`RecordWrapperRun(...)` | crontab repo、job run repo、job log repo、command proxy、id gen、clock |
| `CrontabEditService` | `CreateCronJob`、`UpdateCronJob`、`DeleteCronJob`、`SetCronJobEnabled`、`AdoptCronJob`、`GetCrontabContent` | crontab repo、crond reload proxy、id gen |

哨兵錯誤（controller 可 import 做狀態碼對映）：

```go
ErrCronJobNotFound          → 404
ErrInvalidCronExpression    → 400
ErrCrontabChangedExternally → 409
ErrJobRunAlreadyRunning     → 409
ErrJobLogUnavailable        → 409
ErrCronJobAlreadyManaged    → 409
```

`CRONTAB_WRITE_ENABLED` / `MANUAL_TRIGGER_ENABLED` 的關閉判斷放 **application 層**（它是組態驅動的用例前置條件，不是領域規則），以 `ErrCrontabWriteDisabled`／`ErrManualTriggerDisabled` 表達 → 403。

## 5. Application

| Application | 用例 |
|:---|:---|
| `CronJobApplication` | 列表、單筆查詢 |
| `JobRunApplication` | 執行歷史、log tail |
| `ManualTriggerApplication` | 手動觸發（含開關檢查、並發檢查） |
| `CrontabEditApplication` | CRUD、啟用停用、adopt、原文檢視（含寫入開關檢查） |

Application 持有組態（`ManualTriggerSettings`／`CrontabWriteSettings`）與 `*time.Location`，負責把「現在時間」傳進 domain。

## 6. 資料流：手動觸發

```
POST /jobs/:jobId/run
  → ManualTriggerApplication.TriggerJobRun
      檢查 MANUAL_TRIGGER_ENABLED（false → 403）
      → JobExecutionService.TriggerJobRun
          crontab repo.Load() → FindJob(jobID)（無 → 404）
          job run repo.HasRunningRun(jobID)（true → 409）
          id gen.NewIdentifier() → RunID
          clock.Now() → startedAt
          job run repo.Append(running 的 JobRun)
          job log repo.Append(log 檔, run 標頭)
          command proxy.Execute(ctx, job.InnerCommand(), timeout)
          job log repo.Append(log 檔, 完整輸出)
          run.Finish(...) → job run repo.Update(run)
          → 轉 JobRunDto
```

**先寫 running 紀錄再執行**：這樣 server 中途被砍時，`runs.jsonl` 裡會留下一筆 `running` 的孤兒紀錄，啟動時掃成 `unknown` 並標 `interrupted by restart`（比照 go-stock 的 pipeline run 處理方式）。

## 7. 資料流：wrapper（`cronwatch run`）

wrapper **不經過 HTTP、不需要 server 在跑**，直接組裝最小依賴（job run repo、job log repo、command proxy、clock）：

```
cronwatch run --job=<id> -- <command...>
  → 組 minimal deps（不啟動 gin、不讀 crontab）
  → RecordWrapperRun：append running → 執行 → append 輸出 → update 完成
  → os.Exit(childExitCode)   ← 讓 cron 的錯誤語意不失真
```

wrapper 內任何**自身**的錯誤（寫不進 `runs.jsonl` 等）一律寫 stderr 但**不改變 exit code**——監控工具壞掉不該讓使用者的 job 看起來失敗。

## 8. Controller 與 template

- JSON 與 HTML fragment 共用同一組 application 呼叫，只有 render 方式不同。
- template 以 `go:embed` 內嵌 `internal/web/templates/*.gohtml`；static CSS 內嵌 `internal/web/static/`。
- htmx 由 `internal/web/static/htmx.min.js` **內嵌供應**（不用 CDN，離線可用）。
- fragment 端點回傳的是 `<tbody>`／`<pre>` 等片段，由 `hx-get` + `hx-swap="outerHTML"` 替換。

## 9. 組態

`cmd/cronwatch/config.go` 的 `ServerConfiguration` 一次讀完所有環境變數（皆有預設值），並在 `loadServerConfiguration` 內做正規化：`LOG_TAIL_LINES` clamp 到 1..5000、`MANUAL_TRIGGER_TIMEOUT_SECONDS` 下限 1、`TZ` 無法解析時 fallback 到 `UTC` 並記 warning。

## 10. 測試邊界

| 層 | 怎麼測 | mock 什麼 |
|:---|:---|:---|
| entity / vo | 直接 table-driven | 無 |
| infrastructure | `t.TempDir()` 對真實檔案系統；`CommandExecutionProxy` 跑真實 shell（`echo`／`exit 3`／`sleep`） | 無 |
| domain service | 具體實例 + mockery mock 的 repository/proxy | 全部介面 |
| application | 真實 domain service + 真實 entity | 只 mock 最外層介面 |
| controller | `httptest` + mock application？**不**——controller 極薄，由 application 測試覆蓋，只針對狀態碼對映寫少量測試 | — |
