# PRD · manual trigger

## User Stories

- **US-1** 作為使用者，我要能從瀏覽器立刻跑一次某個 job，並看到它的輸出與 exit code，不必等到下次排程。
- **US-2** 作為使用者，我要 managed job 由 cron 自動觸發時也留下完整紀錄，這樣我隔天早上打開頁面就知道昨晚跑得怎樣。
- **US-3** 作為使用者，我不要同一個 job 因為我多點兩下就被跑兩次。
- **US-4** 作為使用者，我要卡死的手動觸發會被自動收掉，而不是永遠掛著。
- **US-5** 作為使用者，我要 crontab-watcher 自己的故障（寫不進紀錄檔）**不會**讓我的 job 看起來失敗。

## Business Flow

### 手動觸發

```
POST /jobs/:jobId/run
  → ManualTriggerApplication.TriggerJobRun(ctx, jobID)
      MANUAL_TRIGGER_ENABLED false → ErrManualTriggerDisabled(403)
      → JobExecutionService.TriggerJobRun(ctx, jobID, manual, timeout)
          crontab repo.Load() → FindJob            ← 無 → 404
          job run repo.HasRunningRun(jobID) → true → ErrJobRunAlreadyRunning(409)
          runID = id gen.NewIdentifier()
          startedAt = clock.Now()
          job run repo.Append(JobRun{running})
          job log repo.Append(logPath, run 標頭)
          command proxy.Execute(ctx, job.InnerCommand(), timeout)
          job log repo.Append(logPath, 輸出 + run 結尾)
          run.Finish(clock.Now(), exitCode, timedOut, 輸出)
          job run repo.Update(run)
          → JobRunDto
```

### wrapper

```
cronwatch run --job=<jobID> -- <command...>
  → 組最小依賴（job run repo、job log repo、command proxy、clock、id gen）
      ※ 不讀 crontab、不啟動 gin
  → JobExecutionService.RecordWrapperRun(jobID, command, logFilePath)
      同上流程，TriggerSource=schedule，timeout=0（不套）
  → os.Exit(childExitCode)
```

## 規格細節

### log 檔的 run 標頭

每次執行在 log 檔寫入可辨識的分隔，讓人肉閱讀時能切分逐次執行：

```
===== cronwatch run 3f2a1b runId=… trigger=manual started=2026-08-08T14:03:01+08:00 =====
<輸出原文>
===== cronwatch run 3f2a1b exit=0 duration=1.204s =====
```

### 逾時與 process group

`CommandExecutionProxy.Execute` 以 `exec.CommandContext` + `SysProcAttr{Setpgid: true}` 啟動，逾時時對 `-pgid` 送 `SIGKILL`，確保子孫程序一併收掉。`timeout == 0` 時不建立逾時 context。

### exit code 語意

| 情況 | `exitCode` | `RunStatus` |
|:---|:---|:---|
| 正常結束 0 | 0 | `succeeded` |
| 正常結束非 0 | 該值 | `failed` |
| 被逾時 kill | -1 | `timedOut` |
| 指令無法啟動（shell 找不到等） | 127 | `failed` |

### 殘留 `running` 的清理

server 啟動時 `JobRunRepository` 掃全檔，把 `running` 的紀錄改為 `unknown`、`outputExcerpt` 補上 `interrupted by restart`。**這是啟動時一次性動作**，寫在 `dependencies.go` 的組裝流程裡。

### 輸出摘要截斷

`OutputExcerpt` 上限 8 KiB。超出時**保留尾端**（錯誤訊息通常在最後）並標 `outputTruncated=true`。

## Test Cases

### `CommandExecutionProxy`（真實 shell）

| ID | 情境 | 期望 |
|:---|:---|:---|
| MT-01 | `echo hello` | output 含 `hello`、exit 0、timedOut false |
| MT-02 | `exit 3` | exit 3 |
| MT-03 | `echo out; echo err >&2` | output **同時包含** stdout 與 stderr |
| MT-04 | `sleep 5`、timeout 1s | timedOut true、exit -1、耗時約 1s |
| MT-05 | `sleep 5`、timeout 0 | 不逾時（測試用較短的 sleep 驗證） |
| MT-06 | 不存在的指令 | exit 127、err 為 nil（指令失敗不是 proxy 的錯誤） |
| MT-07 | 逾時時的子孫程序 | `sh -c 'sleep 30 & wait'` 逾時後子程序也被收掉 |
| MT-08 | 大量輸出（1 MB） | 完整取得、不 deadlock |
| MT-09 | ctx 被外部取消 | 立即回傳、timedOut false（區分外部取消與逾時） |

### `JobRunRepository` 寫入

| ID | 情境 | 期望 |
|:---|:---|:---|
| MT-10 | `Append` 到不存在的檔案 | 自動建檔含父目錄 |
| MT-11 | `Append` 兩筆 | 檔案有兩行合法 JSON |
| MT-12 | `Update` 既有 runID | 該筆被覆寫、其他筆不動、行數不變 |
| MT-13 | `Update` 不存在的 runID | 回錯誤 |
| MT-14 | `HasRunningRun` 有 running 紀錄 | true |
| MT-15 | `HasRunningRun` 只有已完成紀錄 | false |
| MT-16 | 超過 `RUN_RECORD_RETENTION_COUNT` | 壓縮重寫，每 job 只留最新 N 筆 |
| MT-17 | 並發 `Append` | 以 mutex 序列化，無交錯壞行 |
| MT-18 | `MarkRunningAsInterrupted` | 所有 `running` 變 `unknown` 且帶原因 |

### `JobExecutionService`

| ID | 情境 | 期望 |
|:---|:---|:---|
| MT-19 | 手動觸發成功的 job | 兩次 repo 呼叫（append running → update succeeded）、DTO 為 succeeded |
| MT-20 | 手動觸發失敗的 job（exit 3） | `RunStatus=failed`、`exitCode=3` |
| MT-21 | 已有 running | 回 `ErrJobRunAlreadyRunning`、**不執行指令**（mock 驗證 Execute 未被呼叫） |
| MT-22 | jobID 不存在 | 回 `ErrCronJobNotFound`、不執行指令 |
| MT-23 | managed job | 執行的是 `InnerCommand()`（不是 wrapper 全文，否則無限遞迴） |
| MT-24 | 逾時 | `RunStatus=timedOut` |
| MT-25 | 輸出超過 8 KiB | `outputExcerpt` 被截斷、保留尾端、`outputTruncated=true` |
| MT-26 | log 檔寫入失敗 | **執行仍繼續**、紀錄仍落地、錯誤只被記錄（監控壞掉不該擋住 job） |
| MT-27 | `RecordWrapperRun` | `TriggerSource=schedule`、不套逾時 |
| MT-28 | `foreign` job 手動觸發 | 允許、紀錄落地、log 寫到 managed 目錄 |

### Application 與 Controller

| ID | 情境 | 期望 |
|:---|:---|:---|
| MT-29 | `MANUAL_TRIGGER_ENABLED=false` | 403、不呼叫 service |
| MT-30 | 正常觸發 | 200 + `JobRunDto` |
| MT-31 | 409 情境 | 409 + 訊息說明已在執行 |
| MT-32 | 404 情境 | 404 |

### wrapper subcommand

| ID | 情境 | 期望 |
|:---|:---|:---|
| MT-33 | `run --job=x -- echo hi` | exit 0、`runs.jsonl` 多一筆 succeeded、log 檔有 `hi` |
| MT-34 | `run --job=x -- sh -c 'exit 7'` | **wrapper 自己 exit 7** |
| MT-35 | `run` 缺 `--job` | exit 2 + usage 到 stderr |
| MT-36 | `run --job=x` 後面沒有 `--` 或無指令 | exit 2 + usage |
| MT-37 | `runs.jsonl` 不可寫（目錄唯讀） | 指令**仍然執行**、wrapper 仍以子程序 exit code 退出、錯誤寫 stderr |

## 驗收標準

- MT-01..MT-37 綠燈
- 實機驗證：`./bin/cronwatch run --job=test -- sh -c 'echo hi; exit 5'` → `echo $?` 為 5，且 `runs.jsonl`／log 檔內容正確
- MT-07（子孫程序清理）以 `ps` 或 process group 檢查確實無殘留
