# PRD · crontab writing

## User Stories

- **US-1** 作為使用者，我要能在瀏覽器上新增一個 cronjob，並且它從此就是 managed（有完整執行紀錄）。
- **US-2** 作為使用者，我要能改某個 job 的排程或指令，而不必記得 crontab 的語法細節——排程寫錯時要立刻被擋下並告訴我哪裡錯。
- **US-3** 作為使用者，我要能暫時停用一個 job，之後再啟用回來，**原文一字不改**。
- **US-4** 作為使用者，我要確信服務改我的 crontab 時，我寫的註解、環境變數、其他 job 全都不會受影響。
- **US-5** 作為使用者，如果我剛好同時用 `crontab -e` 改了檔案，我要服務拒絕覆寫我的手改，而不是默默吃掉。
- **US-6** 作為使用者，我要能把既有的手寫 job 一鍵轉為 managed，並在轉之前就知道「輸出位置會改變」。
- **US-7** 作為使用者，每次改動前的版本都要有備份，讓我出事時能還原。

## Business Flow

```
POST /jobs（或 PUT/DELETE/enable/disable/adopt）
  → CrontabEditApplication.<UseCase>
      CRONTAB_WRITE_ENABLED false → ErrCrontabWriteDisabled(403)
      → CrontabEditService.<UseCase>
          crontab repo.Load() → (document, fingerprint)
          驗證輸入（schedule 走 NewCronSchedule）→ 不合法即 ErrInvalidCronExpression(400)
          document 上做結構化修改（AppendJob / ReplaceJob / RemoveJob / SetJobEnabled）
          crontab repo.Save(document, fingerprint)
              ├─ 重讀檔案比對指紋 → 不符 ErrCrontabChangedExternally(409)
              ├─ 寫 temp 檔
              ├─ 語法驗證（有 crontab 命令則 `crontab <temp>`；否則自行 parse 驗證）
              ├─ 備份現行檔 → <backupDir>/crontab.<RFC3339>.bak
              └─ os.Rename(temp, target)
          （不需要通知 cron reload —— 見下方「為什麼沒有 reload 步驟」）
          → CronJobDto
```

## 規格細節

### 新增 job 的 crontab 輸出形狀

```cron
# cronwatch:id=<新產生的 uuid>
<schedule> <binaryPath> run --job=<uuid> -- <使用者輸入的指令>
```

若使用者提供 `description`，多插一行 `# <description>` 在 marker 之前。

### `CronJobUpsertRequest`

| 欄位 | 必填 | 驗證 |
|:---|:---|:---|
| `scheduleExpression` | ✅ | 走 `NewCronSchedule`，不合法回 400 含原因 |
| `command` | ✅ | 非空白；**不得含換行**（會破壞 crontab 格式）→ 400 |
| `description` | ✖ | 不得含換行 |
| `enabled` | ✖ | 預設 true |

### 停用／啟用的行文字轉換

- 停用：`0 3 * * * /bin/x` → `#0 3 * * * /bin/x`（前綴 `#`，**不加空白**，讓還原完全可逆）
- 啟用：移除開頭第一個 `#` 與其後最多一個空白

**可逆性測試**：停用→啟用後，該行必須與原始文字完全相同。

### adopt 的行文字轉換

```
原：0 3 * * * /usr/local/bin/backup.sh >> /var/log/backup.log 2>&1

後：# cronwatch:id=<uuid>
    # cronwatch:strippedRedirect= >> /var/log/backup.log 2>&1
    0 3 * * * /app/cronwatch run --job=<uuid> -- /usr/local/bin/backup.sh
```

已是 managed 的 job 再 adopt → `ErrCronJobAlreadyManaged`(409)。

### 為什麼沒有 reload 步驟

原本規劃了 `ICrondReloadProxy` 與 `CROND_RELOAD_ENABLED`，實作時**撤掉了**。

busybox `crond`（自管模式）與 vixie cron（唯讀模式的 host）**都會自己偵測 crontab
檔案的 mtime 變化並重新載入**，最長延遲一分鐘。而唯一「主動」的做法 `crontab <file>`
其實是「安裝這份 crontab」，當 `CRONTAB_FILE_PATH` 本來就是 cron 的 spool 檔時，
這個呼叫是循環且錯誤的。

替一個不存在的問題留一個 no-op 抽象，比不留更糟：它會讓後來的人以為 reload 是必要
步驟，並在它「失敗」時去除錯一件根本不影響結果的事。UI 上改為明示「變更最長一分鐘
內生效」。

### 備份

每次成功寫入前備份到 `<CRONTAB_BACKUP_DIRECTORY>/crontab.<RFC3339 timestamp>.bak`。**不做自動清理**（個人用，檔案很小；需要時人工刪）。

### 刪除

移除該 job 的條目行、其 marker 註解行、以及緊鄰的 `strippedRedirect` marker 行。**不移除使用者自己寫的說明註解**——我們無法可靠判斷那行註解是屬於這個 job 還是下一個。

## Test Cases

### `CrontabDocument` 結構化修改（entity 層）

| ID | 情境 | 期望 |
|:---|:---|:---|
| CW-01 | `AppendJob` 到空文件 | 產生 marker + 條目兩行 |
| CW-02 | `AppendJob` 到已有內容的文件 | 附加在尾端、**既有行原封不動** |
| CW-03 | `AppendJob` 時原檔尾端無換行 | 先補換行再附加，不黏在最後一行後面 |
| CW-04 | `ReplaceJob` | 只有該條目行變、其餘 byte-for-byte 相同 |
| CW-05 | `ReplaceJob` 不存在的 jobID | 回 `ErrCronJobNotFound` |
| CW-06 | `RemoveJob` managed job | 條目行 + marker 行都移除、其他行不動 |
| CW-07 | `RemoveJob` 含 strippedRedirect marker | 三行都移除 |
| CW-08 | `RemoveJob` foreign job | 只移除條目行 |
| CW-09 | `SetJobEnabled(false)` | 該行前綴 `#`、其餘不動 |
| CW-10 | `SetJobEnabled(true)` on 已停用 | 移除前綴 |
| CW-11 | **停用再啟用** | 該行與原始文字完全相同（可逆性） |
| CW-12 | `SetJobEnabled(true)` on 已啟用 | no-op、不報錯 |
| CW-13 | 任何修改後的 `Render()` | 未受影響的行全部 byte-for-byte 相同 |
| CW-14 | `AdoptJob` | 產生 marker + strippedRedirect marker + wrapper 條目 |
| CW-15 | `AdoptJob` on 無 redirect 的 job | 產生 marker + wrapper 條目（無 strippedRedirect 行） |
| CW-16 | `AdoptJob` on 已 managed | 回 `ErrCronJobAlreadyManaged` |

### `CrontabDocumentRepository`（真實檔案系統）

| ID | 情境 | 期望 |
|:---|:---|:---|
| CW-17 | `Load` 正常檔案 | 內容與指紋正確 |
| CW-18 | `Load` 檔案不存在 | 回空文件 + 指紋，**不報錯**（首次啟動的正常狀態） |
| CW-19 | `Save` 正常 | 目標檔內容更新、備份檔產生 |
| CW-20 | `Save` 後檔案權限 | 維持 0600（crontab 含指令，不該讓別人讀） |
| CW-21 | `Save` 指紋不符（測試中改動檔案） | 回 `ErrCrontabChangedExternally`、**目標檔未被改動** |
| CW-22 | `Save` 時備份目錄不存在 | 自動建立 |
| CW-23 | `Save` 時目標目錄不可寫 | 回錯誤、**無殘留 temp 檔** |
| CW-24 | 並發 `Save` | mutex 序列化、無檔案損毀 |
| CW-25 | `Save` 產生的 temp 檔 | 與目標同目錄（確保 rename 是同一檔案系統的原子操作） |

### `CrontabEditService`

| ID | 情境 | 期望 |
|:---|:---|:---|
| CW-26 | `CreateCronJob` 合法輸入 | 回 DTO、`origin=managed`、指令為 wrapper 形狀 |
| CW-27 | `CreateCronJob` 排程不合法 | `ErrInvalidCronExpression`、**未呼叫 Save** |
| CW-28 | `CreateCronJob` 指令含換行 | 400 類錯誤、未呼叫 Save |
| CW-29 | `CreateCronJob` 指令為空白 | 錯誤 |
| CW-30 | `UpdateCronJob` | Save 被呼叫、內容正確 |
| CW-31 | `DeleteCronJob` | Save 被呼叫、job 消失 |
| CW-32 | `SetCronJobEnabled` | Save 被呼叫 |
| CW-33 | `AdoptCronJob` | Save 被呼叫、回 managed DTO |
| CW-36 | `Save` 回指紋衝突 | 錯誤原樣往上拋 |
| CW-37 | `GetCrontabContent` | 回原文 |

### Application 與 Controller

| ID | 情境 | 期望 |
|:---|:---|:---|
| CW-38 | `CRONTAB_WRITE_ENABLED=false` 下的每個寫入端點 | 403、**未呼叫 service** |
| CW-39 | `POST /jobs` 合法 | 201 + DTO |
| CW-40 | `POST /jobs` 缺 `scheduleExpression` | 400 |
| CW-41 | `PUT /jobs/:jobId` 不存在 | 404 |
| CW-42 | `DELETE /jobs/:jobId` | 204 |
| CW-43 | 指紋衝突 | 409 + 訊息說明檔案已被外部改動 |
| CW-44 | `POST /jobs/:jobId/adopt` on managed | 409 |
| CW-45 | `GET /crontab` | 200 + `text/plain` 原文 |

## 驗收標準

- CW-01..CW-45 綠燈
- **關鍵驗收**：拿一份含註解、環境變數、5 個 job 的真實 crontab，執行「新增 → 編輯 → 停用 → 啟用 → 刪除」全套操作後，用 `diff` 確認除了預期的行以外**沒有任何一個 byte 改變**
- 備份檔在每次寫入後確實產生且內容為改動前的版本
