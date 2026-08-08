# PRD · crontab parsing

實作的單一真實來源。詞彙見 `.sdd/UL-MAP.md`，設計裁定見 `.sdd/DECISIONS.md`。

## User Stories

- **US-1** 作為使用者，我要服務能讀懂我現有的 crontab，包含我寫的註解、環境變數行與被我註解掉的舊 job，而不是只認得標準的五欄條目。
- **US-2** 作為使用者，我要確信這個服務改我的 crontab 時**只動我要它動的那一行**，其餘一字不差。
- **US-3** 作為使用者，我要知道每個 job 下次什麼時候跑，且時間是我所在時區的時間。
- **US-4** 作為使用者，我要服務能看出我的 job 把輸出導去哪個檔案，這樣它才能給我看 log。

## Business Flow

```
crontab 文字
   │
   ├─ 逐行分類 → CrontabLine{RawText, Kind}
   │     blank / comment / marker / environment / jobEntry / disabledJobEntry
   │
   ├─ jobEntry 與 disabledJobEntry → 解析 schedule + command
   │     │
   │     ├─ NewCronSchedule(expression)  ← gronx 驗證與展開 alias
   │     ├─ ParseCommandRedirect(command) → bareCommand + CommandRedirect
   │     └─ 上一行是 marker？ → JobOrigin=managed, JobID 取自 marker
   │        否則              → JobOrigin=foreign, JobID = sha256(...)[:12]
   │
   └─ CrontabDocument{lines, jobs}
         └─ Render() → 逐行 RawText join，無損還原
```

## 規格細節

### 行分類規則（依序判斷，第一個命中者勝）

1. 去掉前後空白後為空 → `blank`
2. 符合 `^#\s*cronwatch:(id|strippedRedirect)=` → `marker`
3. 以 `#` 開頭 → 移除開頭 `#` 與空白後嘗試 parse 成 job 條目；成功 → `disabledJobEntry`，失敗 → `comment`
4. 符合 `^[A-Za-z_][A-Za-z0-9_]*=` → `environment`
5. 嘗試 parse 成 job 條目；成功 → `jobEntry`，失敗 → `comment`

### job 條目 parse 規則

- 以 `@` 開頭：第一個 token 是 alias，其餘為 command
- 否則：前 5 個以空白分隔的 token 是 schedule，第 6 個 token 起（含其後所有空白）為 command
- schedule 欄位數為 6 且第 6 欄不像指令開頭 → 仍以「前 5 欄為 schedule」處理（6 欄語意不支援，見 D-07）
- command 為空 → 不是合法 job 條目

### redirect 解析支援的形態

| 形態 | 目標 | stderr 併入 |
|:---|:---|:---|
| `>> /path/log` | `/path/log` | 否 |
| `> /path/log` | `/path/log` | 否 |
| `>> /path/log 2>&1` | `/path/log` | 是 |
| `> /path/log 2>&1` | `/path/log` | 是 |
| `&>> /path/log`／`&> /path/log` | `/path/log` | 是 |
| `> /dev/null 2>&1` | `/dev/null` | 是（但 `LogSource` 為 `none`，見下） |
| 無 redirect | — | — |

**`/dev/null` 視為無 log**：輸出被明確丟棄，`LogSource()` 回 `none`。

### `LogSource()` 判定

| 條件 | 結果 |
|:---|:---|
| `IsManaged()` | `managed` |
| 有 redirect 且目標不是 `/dev/null` | `redirect` |
| 其餘 | `none` |

### `NextRunAt` 語意

- `@reboot` → `IsPredictable()` false、`NextRunAt` 回 `(zero, false)`
- 其餘合法表達式 → 回 `(下次執行時刻, true)`，時區沿用 `from` 的 location
- 表達式不合法 → `NewCronSchedule` 直接回 `ErrInvalidCronExpression`，不會有不合法的 `CronSchedule` 存在

## Test Cases

### `CronSchedule`

| ID | 情境 | 期望 |
|:---|:---|:---|
| CP-01 | `"0 3 * * *"`、from 2026-08-08 01:00 +08 | next = 2026-08-08 03:00 +08、predictable |
| CP-02 | `"0 3 * * *"`、from 2026-08-08 05:00 +08 | next = 2026-08-09 03:00 +08（跨日） |
| CP-03 | `"*/15 * * * *"`、from 2026-08-08 01:07 +08 | next = 01:15 |
| CP-04 | `"0 0 1 1 *"`、from 2026-08-08 | next = 2027-01-01 00:00（跨年） |
| CP-05 | `"@daily"` | 展開為 `0 0 * * *`、predictable |
| CP-06 | `"@hourly"`／`"@weekly"`／`"@monthly"`／`"@yearly"`／`"@annually"`／`"@midnight"` | 各自正確展開 |
| CP-07 | `"@reboot"` | 建構成功、`IsPredictable()` false、`NextRunAt` 回 false |
| CP-08 | `"not a cron"`／空字串／`"0 3 * *"`（4 欄） | 回 `ErrInvalidCronExpression` |
| CP-09 | `"0 0 * * 1-5"`、from 週六 | next = 下週一 00:00 |
| CP-10 | `"0 3 * * *"`、from 帶 `Asia/Taipei` location | 回傳時刻的 location 為 `Asia/Taipei` |
| CP-11 | `Expression()` | 回傳**展開後**的表達式；`OriginalExpression()` 回原文（`@daily`） |

### `CommandRedirect`

| ID | 情境 | 期望 |
|:---|:---|:---|
| CP-12 | `"/bin/backup.sh >> /var/log/b.log 2>&1"` | bare=`/bin/backup.sh`、target=`/var/log/b.log`、includesStderr=true |
| CP-13 | `"/bin/backup.sh > /var/log/b.log"` | append=false、includesStderr=false |
| CP-14 | `"/bin/backup.sh &>> /var/log/b.log"` | includesStderr=true、append=true |
| CP-15 | `"/bin/backup.sh > /dev/null 2>&1"` | target=`/dev/null` |
| CP-16 | `"/bin/backup.sh"` | redirect 為 nil、bare 等於原字串 |
| CP-17 | `"echo 'a > b'"` | **不**誤判引號內的 `>` 為 redirect |
| CP-18 | `"/bin/x 2>&1 >> /var/log/x.log"` | 兩種順序都要正確解析 |
| CP-19 | `RawFragment()` | 回傳被剝離的原始片段（含前導空白），供 adopt 記錄與還原 |

### `CrontabLine` 分類

| ID | 情境 | 期望 |
|:---|:---|:---|
| CP-20 | `""`／`"   "` | `blank` |
| CP-21 | `"# 這是說明"` | `comment` |
| CP-22 | `"# cronwatch:id=abc"` | `marker` |
| CP-23 | `"PATH=/usr/bin:/bin"`／`"MAILTO="` | `environment` |
| CP-24 | `"0 3 * * * /bin/x"` | `jobEntry` |
| CP-25 | `"#0 3 * * * /bin/x"`／`"# 0 3 * * * /bin/x"` | `disabledJobEntry` |
| CP-26 | `"# 0 3 * * * 這裡以前有備份"` | `disabledJobEntry`（已知誤判，見 D-06） |
| CP-27 | `"@daily /bin/x"` | `jobEntry` |
| CP-28 | `"0 3 * * *"`（無指令） | `comment`（不是合法 job） |

### `CrontabDocument` 無損 round-trip

| ID | 情境 | 期望 |
|:---|:---|:---|
| CP-29 | 完整真實 crontab fixture（含 shebang 註解、環境變數、空行、CRLF 混用、尾端無換行） | `Render()` 與輸入 **byte-for-byte 相同** |
| CP-30 | 尾端**有**換行的輸入 | round-trip 保留尾端換行 |
| CP-31 | 尾端**無**換行的輸入 | round-trip 不多加換行 |
| CP-32 | 空字串輸入 | `Render()` 回空字串、`Jobs()` 為空 |
| CP-33 | 只有註解的檔案 | `Jobs()` 為空、round-trip 相同 |
| CP-34 | 含 Windows CRLF 行尾 | round-trip 保留 CRLF |

### `CronJob` 與 `CrontabDocument.Jobs()`

| ID | 情境 | 期望 |
|:---|:---|:---|
| CP-35 | marker + 條目 | `JobOrigin=managed`、`JobID` 取自 marker |
| CP-36 | 無 marker 的條目 | `JobOrigin=foreign`、`JobID` 為 12 hex 字元 |
| CP-37 | 兩個內容完全相同的 foreign 條目 | `JobID` 相同（**已知限制**，第二筆以 `-2` 後綴去重） |
| CP-38 | 相同 schedule 不同 command | `JobID` 不同 |
| CP-39 | managed 條目（wrapper 形狀） | `InnerCommand()` 回剝掉 wrapper 後的原指令 |
| CP-40 | `disabledJobEntry` | 出現在 `Jobs()` 中且 `Enabled()` 為 false |
| CP-41 | managed job + `strippedRedirect` marker | 該值被讀進 `CronJob` |
| CP-42 | `LogSource()` 三種情境 | 依上表判定 |
| CP-43 | `ResolveLogFilePath` for managed | `<dir>/<jobID>.log` |
| CP-44 | `ResolveLogFilePath` for redirect | redirect 的 target 路徑 |
| CP-45 | `ResolveLogFilePath` for none | 空字串 |
| CP-46 | `FindJob(不存在的 id)` | 回 `(nil, false)` |

## 驗收標準

- 全部 CP-01..CP-46 綠燈
- `internal/domain/entity/tests/testdata/` 內至少三份真實形態的 crontab fixture，皆通過無損 round-trip
- domain 層不 import 任何 infra／HTTP／檔案系統 package（`os` 除外亦不需要）
