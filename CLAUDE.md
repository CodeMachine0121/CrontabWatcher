# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

**crontab-watcher** — 在瀏覽器上檢視與操作 cronjob 的 **Go 單一 binary 服務**（個人側項目，僅 James Hsueh 本人使用，非公開對外服務、無鑑權）。核心價值：把散落在 crontab 檔案裡的排程條目、以及各自被 redirect 到不同位置的 log，聚合成一個網頁介面——**看得到有哪些 job、下次何時跑、上次跑成功還是失敗、輸出是什麼**，並可從瀏覽器手動觸發一次執行。

> **目前狀態：五個切片都已實作完成並通過驗收。** module path 為
> `github.com/james-hsueh/crontab-watcher`。`make check`（build + vet + test）全綠，
> `postman/crontab-watcher.postman_collection.json` 以 newman 實跑 28 個 request、
> 75 個斷言全過。

### 領域的兩個核心事實（設計的地基）

1. **系統 cron 跑完的 job，事後無法回溯 stdout 與 exit code。** 因此執行結果有兩條取得路徑，job 依此分兩類：

   | 類別 | 定義 | 結果來源 | 能拿到什麼 |
   |:---|:---|:---|:---|
   | **foreign job** | 使用者手寫的 crontab 條目，未經本服務包裝 | 解析指令尾端的 **redirect 目標**（`>> /path/foo.log 2>&1`）得出 log 路徑，直接 tail 該檔 | 只有原始輸出文字；**無 exit code、無法切分逐次執行** → `RunStatus` 為 `unknown` |
   | **managed job** | 本服務建立或改寫過的條目，指令被包成 `cronwatch run --job=<jobId> -- <原指令>` | wrapper 自己記錄 | 完整：`startedAt`／`finishedAt`／`exitCode`／`triggerSource`／輸出 |

   無 redirect 又非 managed 的 foreign job → UI 明確標示「無 log 可讀」，並提供「轉為 managed」的動作（改寫該條目、補上 wrapper 與 log 路徑）。**不猜、不假裝有資料。**

2. **容器看不到 host 的 crontab。** 於是 crontab 檔案路徑一律可配置（`CRONTAB_FILE_PATH`），同一套領域模型涵蓋兩種部署形態：

   | 形態 | 說明 | 寫入 | 手動觸發 |
   |:---|:---|:---|:---|
   | **自管模式**（Docker 主推） | crontab 檔與 log 目錄掛在 volume 上，容器內同時是排程器（busybox `crond`）與執行環境 | ✅ | ✅ 結果可信 |
   | **唯讀模式** | Linux host 上把 host 的 crontab 掛進容器（`CRONTAB_WRITE_ENABLED=false`） | ❌ 回 403 | ⚠️ 容器內環境 ≠ host 環境，預設關閉 |
   | **host 模式**（`make start-host`） | 不用容器，直接在機器上跑，看 `crontab -l` 列出的**使用者真正那份 crontab**（`CRONTAB_SOURCE=crontabCommand`） | ✅ 經由 `crontab <file>` | ✅ 環境就是 host 環境，結果可信 |
   | **桌面模式**（`make start-desktop`，僅 macOS） | host 模式的另一種**呈現方式**：進駐選單列，不必開終端機也不必開瀏覽器分頁；資料來源與 host 模式完全相同 | ✅ 同 host 模式 | ✅ 同 host 模式 |
   | **App 模式**（`make dmg` → 拖進「應用程式」，僅 macOS） | 桌面模式的**交付方式**，行為完全相同。**長期使用請用這個**：`/Applications/CrontabWatcher.app/Contents/MacOS/cronwatch` 是唯一不會因為重新編譯或清掉 `bin/` 而消失的執行檔路徑，而納管排程的條目指的就是它 | ✅ | ✅ |

   **macOS host 無法用唯讀模式**——Docker Desktop 跑在 Linux VM 內，掛不到 `/var/at/tabs/`、也叫不到 host 的 `crontab` 命令。在 Mac 上想看自己真正的 crontab，用 **host 模式**（`make start-host`）。

   **為什麼 host 模式必須走 crontab 命令**：spool 目錄的權限是 `drwx------ root:wheel`，直接讀檔必然 permission denied。`crontab` 是 setuid 執行檔，因此**它是唯一不需要 root 就能存取使用者 crontab 的途徑**。替代方案是用 `sudo` 跑一個能執行任意指令的 web 服務，不值得。故 `ICrontabDocumentRepository` 有兩個實作（`CrontabDocumentRepository` 讀檔／`CrontabCommandRepository` 走命令），domain 與 service 完全不知道差別。

此專案採 **Spec-Driven Development (SDD)**。動工前務必先讀以下權威文件：

- `.sdd/UL-MAP.md` — 通用語言地圖。**所有實體 / 動作 / 識別字命名以此為準**，不得自創同義詞。下方「通用語言（UL-MAP 種子）」是它的初始內容來源。
- `.sdd/{YYYY-MM-DD}-{feature-slug}/BRIEF.md` — 該功能切片的需求共識。
- `.sdd/{YYYY-MM-DD}-{feature-slug}/PRD.md` — 該功能切片的需求、User Stories、Business Flow，為實作的單一真實來源。
- `.sdd/{YYYY-MM-DD}-{feature-slug}/ARCH.md` — 該切片的技術設計（哪些元件、為什麼這樣切、擴充點在哪）。PRD 只談業務，ARCH 才談實作。

> 修改業務邏輯或新增功能時，對應的 SDD 文件（尤其 UL-MAP 與該切片的 PRD）需同步更新，文件與程式碼不可漂移。**新增功能請開新的 `.sdd/{date}-{feature-slug}/` 資料夾**，不要回頭改舊切片的 PRD。

### 切片（皆已完成）

依賴由低到高實作：

1. `crontab-parsing` — crontab 檔案 parse（含註解／空行／環境變數行／`@daily` alias／停用條目）、`CronSchedule.NextRunAt()`、redirect log 路徑解析
2. `job-listing` — `GET /jobs` 與網頁 job 清單（排程、下次執行、log 來源分類）
3. `run-log-viewing` — 讀 log 檔 tail、foreign job 的原始輸出檢視
4. `manual-trigger` — `POST /jobs/:jobId/run`，wrapper 執行 + `runs.jsonl` 落地 + 執行歷史
5. `crontab-writing` — CRUD 寫回 crontab（原子寫入 + 備份 + `crontab <file>` 驗證 + crond reload）、job 啟用／停用、foreign → managed 轉換
6. `host-crontab-access` — 走 `crontab -l` / `crontab <file>` 存取使用者真正那份 crontab（host 模式）
7. `desktop-menu-bar-app` — macOS 選單列常駐應用：狀態指示、摘要、失敗通知、獨立視窗（僅呈現方式，不引入新資料來源）
8. `macos-app-packaging` — 包成 `.app` 與 DMG，拖進「應用程式」即安裝（僅交付方式，不引入新行為）

## Tech Stack

| 層面 | 選型 | 角色 / 備註 |
|:---|:---|:---|
| 語言 | **Go 1.26** | 全專案；編譯成單一 static binary，無 runtime 依賴。禁用空介面 `any`（`interface{}`）與反射濫用（例外見 Conventions） |
| Web 框架 | **Gin** | REST API + HTML 渲染（Controller 層）。handler 只做 HTTP 請求／回應轉換 |
| 桌面外殼 | **`energye/systray`（選單列）+ `webview/webview_go`（獨立視窗）** | **`systray.CreateMenu()` 是必須的，少了它圖示會出現但點下去毫無反應** —— 這個 fork 刻意把 `[statusItem setMenu:menu]` 註解掉了（見其 `systray_darwin.m:78`），改由呼叫方決定要下拉選單還是要滑鼠事件回呼。我們要的是選單。**僅 macOS，且只出現在 `_darwin.go` 檔案中**。選它們而不是更知名的 `getlantern/systray`：後者拖進 17 個模組（含 Windows GUI 的 `lxn/walk`），前者只多一個 Linux 專用的 `godbus`。兩者都需要 CGO，因此 `GOOS=linux CGO_ENABLED=0` 的容器建置**不會編到它們**——不支援的平台在編譯期就分流（`menu_bar_controller_other.go`）。**`cmd/cronwatch` 在 darwin 上有一個 `runtime.LockOSThread()` 的 init**：Go 只保證 main 從主執行緒開始，而 NSApplication 只肯跑在主執行緒上 |
| 圖形資產 | **純 Go 幾何運算（`image/png`），不放二進位圖檔** | 形狀以「帶號距離 → 覆蓋率」描述，因此不需要繪圖函式庫也能有平滑邊緣。App 圖示由 `tools/appicon` 在打包時產生；選單列圖示在執行期畫成 **template 圖示**（只有 alpha 有意義，macOS 依淺色／深色自行上色——寫死顏色的選單列圖示一換佈景就看不見）。兩者共用 `internal/glyph` 的同一份形狀 |
| 前端 | **`html/template` + `go:embed` + 原生 JS**（無 Node、無 build step、無前端框架） | server 回傳完整頁面或 HTML fragment；約 40 行原生 JS 負責定時抓 fragment 換掉 `innerHTML`，以及按鈕發請求後刷新。**刻意不用 htmx**——它得 vendoring 一份 min.js，而 CDN 依賴被禁（這服務常跑在沒有對外網路的機器上），而我們只需要「輪詢換內容」與「發 POST」兩件事。CSS 為單一手寫檔。templates 與 static 資產以 `go:embed` 內嵌進 binary |
| 排程解析 | **`adhocore/gronx`** | **只當 parser 用**：驗證 cron 表達式、算 `NextTick()`／`PrevTick()`。零依賴純計算，故允許被 domain entity 直接使用（比照 go-stock 讓 domain 依賴 `decimal` 的先例）。**不使用其 tasker／daemon 功能** |
| 排程執行 | **容器內 busybox `crond`**（自管模式）或 **host 的 cron**（唯讀模式） | 本服務**不是排程器**、不在程式內跑 `time.Ticker` 排程 job。cron 由系統負責，本服務只讀寫 crontab 檔並提供 wrapper |
| 持久化 | **檔案系統（無資料庫）** | **不使用任何 SQL／ORM。** crontab 檔＝job 的真實來源；`runs.jsonl`（append-only JSON Lines）＝執行紀錄；log 目錄＝輸出內容。全部即時讀取。此規模不需要 DB，也讓 volume 掛載與備份等於 `cp` |
| 指令執行 | **`os/exec`** | wrapper（`cronwatch run`）與手動觸發共用同一條執行路徑：`$SHELL_PATH -c <command>`，捕捉 stdout／stderr／exit code／耗時，逾時以 `context.WithTimeout` + process group kill 收掉 |
| 數值處理 | **標準庫** | 本專案**無金額欄位**，不需要 `shopspring/decimal`。耗時用 `time.Duration`、exit code 用 `int` |
| 測試 | **`testing` + `stretchr/testify`** | table-driven 單元測試；mock 由 `mockery`（`.mockery.yaml`）依介面產生於 `internal/domain/interface/mocks/` |
| 程式碼品質 | **（尚未建置自動化）** | **沒有** `.golangci.yml`／lefthook／pre-commit hook；規範靠人工遵守。新增自動化前勿假設它存在 |
| 部署 | **Dockerfile（multi-stage）+ docker-compose** | 見下方「部署」。alpine base，內含 busybox `crond` 與本服務 binary |
| 對外交付 | **REST（JSON）+ HTML（可輪詢的 fragment）** | 無 SSE／WebSocket、無鑑權機制。**服務只綁 `127.0.0.1` 或內網**——它能執行任意指令，等同 remote shell，絕不可暴露到公網 |

## Architecture — Clean / Onion Architecture

**依賴方向一律指向 Domain（核心）；Domain 不依賴任何人。**

```
   Controller ───▶ Application ───▶ Domain ◀─── Infrastructure
   (HTTP/Gin/HTML)  (use cases)      (核心)      (Repository/Proxy 實作)
                                       ▲
                     internal/domain/interface/ 放所有對外介面（一介面一檔）
```

- **Domain（核心）**：entity、value object、領域計算邏輯、**Domain Service**，以及**所有對外介面**（集中在 `internal/domain/interface/`，一介面一檔）。**Domain 不 import 任何其他層**（不認識 HTTP、`os/exec`、檔案系統路徑慣例）。
- **Application** 依賴 Domain：呼叫 **Domain Service** 編排用例，拿回 **DTO**（不碰 entity）。
- **Controller** 依賴 Application：只負責 HTTP 請求／回應轉換、query／body binding、以及 HTML template 渲染。既有例外慣例：controller 可 import domain service 的**哨兵錯誤**做狀態碼對映（如 `errors.Is(err, service.ErrCrontabWriteDisabled)` → 403、`ErrCronJobNotFound` → 404、`ErrInvalidCronExpression` → 400）。
- **Infrastructure**（Repository／Proxy 的**實作**）依賴 Domain：實作 `domain/interface/` 的介面（DIP）。**Repository**＝持久化（crontab 檔、log 檔、`runs.jsonl`）；**Proxy**＝呼叫外部世界（`os/exec` 執行指令、呼叫 `crontab` 命令 reload）。
- 具體實作在組裝根（`cmd/cronwatch/dependencies.go`）以手動 DI 注入（建構子 `NewXxx(...)`）。

### Domain 內部結構

domain **只依「種類」分五個資料夾**：

- `entity/` — 充血實體（**`struct` + method**，含行為；用建構子 `NewXxx(...)` 建立，建構子內做正規化／驗證，如 `NewCronSchedule` 驗證表達式並展開 `@daily` 等 alias、`NewJobRun` 把非法 `RunStatus` 正規化為 `unknown`）。**entity 絕不直接回傳給 application。**
- `vo/` — value object（不可變純資料、無行為，**`struct`**）：如 parse crontab 檔案時的原始行 `CrontabLine`、指令 redirect 解析結果 `CommandRedirect`。
- `dto/` — Domain DTO：domain 對 application 的**唯一回傳形狀**（不外漏 entity）。
- `service/` — **Domain Service**：application 的唯一呼叫入口。透過 `interface/` 的 repository／proxy 介面取得 entity、執行計算，再把 entity **轉成 DTO** 回傳。命名 `XxxService`，**一檔一個 service struct**（規劃：`CronJobService`、`JobRunService`、`CrontabEditService`）。
- `interface/` — repository／proxy 介面（一介面一檔，`mocks/` 子資料夾放 mockery 產生的 mock）。
- **計算行為掛在 entity 的 method 上**（Rich Domain Model）：如 `CronSchedule.NextRunAt()`、`CronJob.ResolveLogFilePath()`、`CronJob.IsManaged()`、`CrontabDocument.Render()`、`JobRun.Duration()`。**禁止散落的 package-level 計算函式當「靜態工具」（壞味道）。** 跨多個 entity 的併發編排（如同時算全部 job 的下次執行時間、同時 tail 多個 log 檔）才放 Domain Service，用 `golang.org/x/sync/errgroup`。
- **同一個 Domain Service 內的公開 use-case method 互不呼叫**。需要跨方法編排時由 Application 層負責。私有 helper method 不在此限。

**呼叫鏈**：`Application → DomainService → repository/proxy 介面（impl 在 infra）→ entity → service 轉 DTO → 回傳 DTO`。Application 全程不碰 entity。

> **不使用「port」一詞或資料夾**——對外介面一律稱「介面」，集中在 `internal/domain/interface/`。

### crontab 檔案讀寫的架構要求

Go 生態**沒有** `python-crontab` 的等價物（`robfig/cron`、`mileusna/crontab`、`gronx` 全都是排程器或表達式 parser，不做檔案管理），因此 parse／render 自己寫，並遵守：

- **`CrontabDocument` 必須無損保留原始檔案**：註解行、空行、環境變數行（`SHELL=`／`PATH=`／`MAILTO=`）、行順序、原始間距一律留著。**只改動被要求改動的那一行**，其餘 byte-for-byte 還原。此性質必須有專屬單元測試（讀入→未修改→render 出來與原檔完全相同）。
- **job 身分（`JobID`）以緊鄰上一行的 marker 註解承載**：`# cronwatch:id=<uuid>`。managed job 一定有；foreign job 沒有 marker，其 `JobID` 由「schedule + command」的雜湊推導（穩定但會隨編輯改變，UI 需說明此限制）。
- **寫入必須是原子的**：寫 temp 檔 → `crontab <tempfile>` 驗證語法（唯讀模式或無 `crontab` 命令時退回自行驗證）→ 備份現行檔到 `<crontab>.bak.<timestamp>` → `os.Rename` 就位 → 通知 cron reload。任一步失敗即整體放棄、原檔不動。
- **併發寫入以行程內 mutex 序列化**，並在寫入前重讀檔案比對 mtime，偵測到外部（`crontab -e`）改動即中止並回 409，不覆蓋使用者的手改。

## Engineering Conventions

- **命名一律全名，避免縮寫**：如 `CronJobRepository`、`ManualTriggerApplication`、`ResolveLogFilePath`。程式識別字用 Go 慣例大小寫全名（匯出 `PascalCase`、未匯出 `camelCase`）；JSON 欄位用 `camelCase`。
- **各層角色用固定後綴命名**：

  | 角色 | 後綴 | 層 |
  |:---|:---|:---|
  | Domain service | `Service` | domain |
  | Application | `Application` | application |
  | Controller | `Controller` | controller |
  | Repository | `Repository` | infrastructure |
  | Proxy | `Proxy` | infrastructure |

  `Service` 後綴僅用於跨 entity 的編排；單一 entity 的計算放 entity 自己的 method。
- **一個 entity 對應一個 repository**：`CrontabDocument` → `CrontabDocumentRepository`、`JobRun` → `JobRunRepository`，讀寫都在同一個 repository，不另立切片型別。
- **資料物件命名**（domain model 只有 entity 與 dto 兩種）：

  | 類別 | 規則 |
  |:---|:---|
  | Entity（**`struct` + method**，含資料與行為） | 以領域語彙命名，**不加 `Dto`**；放 `domain/entity/` |
  | Domain DTO（service 回傳給 application 的形狀，純資料 `struct`） | `Dto` 後綴；放 `domain/dto/`（如 `CronJobDto`、`JobRunDto`） |
  | Endpoint 接收的 JSON body | `Request` 後綴 struct；放 controller 同檔（如 `cron_job_controller.go` 內的 `CronJobUpsertRequest`）。querystring 參數不立 struct，直接在 handler 內以 `context.Query(...)` 逐一解析 |

  JSON 回應直接回傳 DTO、不另立 `Response` 型別。HTML 頁面的 template data 是例外，可在 controller 層定義 `XxxViewModel`（僅供 template 使用、不外流）。
- **`interface` 只用於「行為契約」**：一律 `I` 前綴，**集中放在 `internal/domain/interface/`，一介面一檔**（如 `i_crontab_document_repository.go` 定義 `ICrontabDocumentRepository`）。**實例檔內不得宣告任何 `interface`。不使用「port」一詞或資料夾。**
  - **資料夾叫 `interface`，但 package 宣告為 `interfaces`** —— `interface` 是 Go 保留字，不能當 package 名。因為兩者不一致，**import 時一律加明確別名**：`import interfaces "github.com/james-hsueh/crontab-watcher/internal/domain/interface"`。
- **具體實作用「純角色名」**：不帶 `I`、也不帶技術前綴。例：`ICommandExecutionProxy` 的實作是 `CommandExecutionProxy`（**不是** `ShellCommandExecutionProxy`）；`IJobRunRepository` 的實作是 `JobRunRepository`（**不是** `JsonlJobRunRepository`）。
- **檔名必須對齊其主要型別／符號**（一律小寫 snake_case）：介面檔 `i_` 前綴一檔一介面；實作檔用純角色名；mock 檔 `mock_i_xxx.go`（mockery 產生）。
- **禁用空介面 `any`（`interface{}`）作為資料多型手段。** 需要多型時用具名 `interface`。允許的例外：`html/template` 的 template data 參數、JSON marshal 邊界。
- **禁止「先宣告後賦值」。** 變數宣告當下即賦值（`:=` 或 `var x = ...`）。例外：json binding decode 目標（`var request CronJobUpsertRequest` + `ShouldBindJSON`）。
- **計算行為掛在 struct method 上，避免散落的 package-level 計算函式。** 例外：無狀態的純轉換模組（如 `parse_crontab_line.go`）比照 go-stock 的 `parse_rss_xml.go` 慣例。
- **禁止手寫 SQL、禁止引入資料庫。** 需要新的持久化欄位時，加到 `runs.jsonl` 的 record 或 crontab marker 註解裡；若哪天真的需要 DB，先開 SDD 切片討論，不要偷偷加。
- **所有時間一律帶時區、內部統一存 UTC**、對外輸出 RFC3339。cron 排程的「下次執行時間」以 `TZ` 環境變數指定的時區計算——**這是 cron 語意的一部分，不可用 UTC 硬算**。
- **執行外部指令的安全紀律**：指令字串一律**原樣**交給 `$SHELL_PATH -c`，**絕不做字串拼接組合使用者輸入到指令中**（crontab 條目本身就是使用者提供的完整指令，這是設計意圖；但 API 參數如 `jobId` 必須驗證格式、絕不進到指令字串）。手動觸發一律套 `MANUAL_TRIGGER_TIMEOUT_SECONDS` 逾時並以 process group 收掉子孫程序。
- 詞彙定義見 `.sdd/UL-MAP.md`。新增業務詞彙時同步補進 UL-MAP，不得自創同義詞。

### 通用語言（UL-MAP 種子）

建立 `.sdd/UL-MAP.md` 時以此為初始內容：

| 詞彙 | 型別 | 定義 |
|:---|:---|:---|
| `CrontabDocument` | entity | 一整份 crontab 檔案：有序的 `CrontabLine` 集合 + 由其解析出的 `CronJob` 清單。負責無損 render 回檔案 |
| `CrontabLine` | vo | crontab 檔案的單一行 + 其分類（`comment`／`blank`／`environment`／`jobEntry`／`marker`） |
| `CronJob` | entity | 一筆排程條目：`JobID`／`Schedule`／`Command`／`LogSource`／`Enabled`／`Origin`／`Description` |
| `CronSchedule` | entity | cron 表達式（含 `@daily` 等 alias）；method 有 `NextRunAt(time.Time)`、`Describe()` |
| `CommandRedirect` | vo | 從指令尾端解析出的輸出重導向（目標路徑、是否 append、stderr 是否併入） |
| `JobRun` | entity | 一次執行：`RunID`／`JobID`／`TriggerSource`／`StartedAt`／`FinishedAt`／`ExitCode`／`RunStatus`／`OutputExcerpt` |
| `JobOrigin` | enum | `managed`（本服務包裝過、有 marker）／`foreign`（使用者手寫） |
| `LogSource` | enum | `managed`（本服務指定的 log 檔）／`redirect`（從指令 redirect 解析出）／`none`（無 log 可讀） |
| `TriggerSource` | enum | `schedule`（由 cron 觸發）／`manual`（從瀏覽器手動觸發） |
| `RunStatus` | enum | `running`／`succeeded`（exit code 0）／`failed`（非 0）／`timedOut`／`unknown`（foreign job 無法判定，**非法值正規化為此**） |
| 停用（disable） | 動作 | 在條目行前加 `#` 註解掉，**不刪除**——保留原文以便還原 |
| 轉為 managed（adopt） | 動作 | 改寫 foreign job：補 marker 註解、把指令包成 `cronwatch run --job=<id> -- <原指令>` |

## Commit Workflow

- 目前**沒有** lefthook／pre-commit hook，也沒有 `.golangci.yml`。commit 前請自行執行 `go build ./...`、`go vet ./...`、`go test ./...` 並確認全過。
- **直接在 `main` 上開發**，不開 feature branch。
- **commit 訊息一律遵循 Conventional Commits、一律英文**（`feat:`／`fix:`／`test:`／`refactor:`／`chore:`／`docs:`）。
- **commit 訊息內禁止出現 SDD 的編號／代號**（TC 編號、切片代號等）。編號只存在於 `.sdd/` 文件內供追溯，不外洩到 git 歷史 —— commit 訊息要靠自己的文字就說得清楚做了什麼。
- 開發節奏是 **SDD + TDD**：新功能先在 `.sdd/{date}-{feature-slug}/` 寫 `BRIEF.md`／`PRD.md`（含 test case 清單），更新 `UL-MAP.md`，再由內而外逐步提交（entity 單元測試 → repository → domain service → application → controller → `dependencies.go` 組裝路由 → template／Postman 集合更新）。**每一個 TDD cycle 或每一次 application 整合各自獨立 commit 一次。**
- **涉及 crontab 寫入的 commit 必須附「無損 round-trip」測試**（讀入→render→比對原檔），這是最容易在重構中悄悄壞掉、且壞掉代價最高的性質。

## Commands

Go modules 管理依賴；`Makefile` 為主要入口。

- `go mod download` / `go mod tidy` — 安裝 / 整理依賴
- `make start`（`go run ./cmd/cronwatch serve`）— 本機啟動 server（讀 `.env`，`godotenv.Load()`）
- `make start-desktop`（`./bin/cronwatch desktop`）— 進駐 macOS 選單列（僅 macOS；先 build，因為執行檔路徑會被寫進 crontab 也會用來開視窗子程序）
- `make build`（`go build -o bin/cronwatch ./cmd/cronwatch`）— 編譯單一 binary
- `make test`（`go test ./...`）— 跑全部單元測試
- `make vet`（`go vet ./...`）— 靜態檢查
- `make check`（build + vet + test）— **commit 前跑這個**
- `make app` / `make dmg`（`./scripts/package-macos-app.sh`）— 包成 `.app` 與 DMG，產物在 `dist/`（已 gitignore）。版本號可覆寫：`make dmg VERSION=0.2.0`
- `make docker-build` / `make docker-up` / `make docker-logs` — 容器建置與啟動
- 執行單一測試：`go test ./internal/path/to/... -run TestName`
- 重新產生 mock：`mockery`（讀取 `.mockery.yaml`，需先安裝 `mockery` CLI）

### binary 的四個 subcommand

**同一個 binary 幾種身分**，這是設計核心——wrapper 必須與 server 同一個 binary 才能保證部署一致；桌面模式的視窗子程序同理：

| 指令 | 用途 |
|:---|:---|
| `cronwatch serve` | 啟動 web server。**從終端機執行時的預設**；被包成 `.app` 時預設改為 `desktop`（見下） |
| `cronwatch desktop` | 進駐 macOS 選單列（僅 macOS）。強制 `CRONTAB_SOURCE=crontabCommand`、綁 `127.0.0.1:0`（臨時埠，其他裝置連不進來）、狀態預設放 `$HOME/.local/state/crontab-watcher/`（與 `start-host` 共用）。以檔案鎖保證單一實例 |
| `cronwatch window --url=<address>` | 獨立視窗的子程序，由 `desktop` 啟動，通常不自己跑。**必須是獨立程序**：systray 與 webview 都要佔用 macOS 主 run loop。父程序以它的 stdin 送後續網址（一行一個），stdin 關閉即收掉視窗 |
| `cronwatch run --job=<jobId> -- <command...>` | wrapper：執行指令、記錄 `JobRun` 到 `runs.jsonl`、輸出寫入該 job 的 log 檔，並以子程序的 exit code 作為自己的 exit code（讓 cron 的錯誤語意不失真） |

**沒有給子命令時要跑哪一個**，由執行檔自己的位置決定：路徑含 `.app/Contents/MacOS/` → `desktop`（雙擊一個 app 卻起了一個沒有畫面的 server，等於「什麼都沒發生」）；否則 → `serve`，既有行為不變。Finder 有時塞的 `-psn_…` 參數不是子命令，一律略過。見 `cmd/cronwatch/resolve_command.go`。

managed job 的 crontab 條目長這樣：

```cron
# cronwatch:id=8f14e45f-ea8f-4b2c-9c3d-6a1b2c3d4e5f
0 3 * * * /app/cronwatch run --job=8f14e45f-ea8f-4b2c-9c3d-6a1b2c3d4e5f -- /usr/local/bin/backup.sh
```

### 環境變數（讀取於 `cmd/cronwatch/config.go`，皆有預設值、可省略）

| 變數 | 預設值 | 用途 |
|:---|:---|:---|
| `SERVER_ADDRESS` | `127.0.0.1:8080` | 監聽位址。**預設只綁 loopback**——本服務可執行任意指令，等同 remote shell。容器內需設 `0.0.0.0:8080` 並靠 compose 的 port binding 限制在 `127.0.0.1` |
| `CRONTAB_SOURCE` | `file` | `file`＝直接讀寫 `CRONTAB_FILE_PATH`（容器自管模式）；`crontabCommand`＝走 `crontab -l` / `crontab <file>`（**host 模式必須用這個**，見下方說明）。無法辨識的值退回 `file` 並警告 |
| `CRONTAB_COMMAND_PATH` | `crontab` | `crontab` 執行檔位置，僅 `CRONTAB_SOURCE=crontabCommand` 時使用 |
| `CRONTAB_FILE_PATH` | `/data/crontabs/root` | crontab 檔案路徑，僅 `CRONTAB_SOURCE=file` 時用於讀寫（命令模式下只出現在 UI 的來源標示） |
| `RUN_LOG_DIRECTORY` | `/data/logs` | managed job 的 log 檔存放目錄（`<jobId>.log`） |
| `RUN_RECORD_FILE_PATH` | `/data/runs.jsonl` | 執行紀錄（append-only JSON Lines） |
| `CRONTAB_BACKUP_DIRECTORY` | `/data/backups` | 每次寫入 crontab 前的備份目錄 |
| `CRONTAB_WRITE_ENABLED` | `true` | 是否允許改動 crontab（CRUD／啟用停用／adopt）。`false` 時相關 endpoint 回 403 |
| `MANUAL_TRIGGER_ENABLED` | `true` | 是否允許從 UI 手動觸發。唯讀模式建議設 `false`（容器環境 ≠ host 環境，結果不可信） |
| `MANUAL_TRIGGER_TIMEOUT_SECONDS` | `900` | 手動觸發的逾時秒數，逾時以 process group kill，`RunStatus` 標 `timedOut` |
| `SHELL_PATH` | `/bin/sh` | 執行指令用的 shell |
| `LOG_TAIL_LINES` | `200` | 預設回傳的 log 尾巴行數 |
| `RUN_RECORD_RETENTION_COUNT` | `500` | 每個 job 在 `runs.jsonl` 保留的紀錄筆數，超出時壓縮重寫檔案 |
| `WRAPPER_BINARY_PATH` | 自己的執行檔絕對路徑 | 寫進 crontab 條目的執行檔路徑。**用 `make start`／`go run` 時必須明確設定**——那時執行檔在暫存目錄下，寫進 crontab 的條目會在檔案被清掉後失效（偵測到暫存路徑時會印警告） |
| `DESKTOP_REFRESH_INTERVAL_SECONDS` | `30` | 桌面模式重新確認現況的間隔，預設 `30`，下界 5 秒。選單列展開時讀的是最近一次的快照，不強制重讀——點開的瞬間卡住比看到幾秒前的資料糟得多 |
| `DESKTOP_SUMMARY_LINE_LIMIT` | `20` | 選單列摘要最多列幾筆，下界 1。超出的筆數會明確標示「另有 N 筆」，不安靜截斷 |
| `TZ` | `Asia/Taipei` | **cron 排程時區**，用於計算「下次執行時間」。必須與實際跑 cron 的環境一致，否則顯示的時間會錯 |

### API Routes（註冊於 `cmd/cronwatch/dependencies.go` 的 `registerRoutes`）

**頁面與 fragment（供輪詢）**

- `GET /` — 主頁：job 清單
- `GET /jobs/:jobId/detail` — job 詳情頁（排程、log 來源、執行歷史）
- `GET /fragments/jobs` — job 清單 fragment（每 15 秒輪詢刷新）
- `GET /fragments/jobs/:jobId/runs` — 執行歷史 fragment
- `GET /fragments/jobs/:jobId/log` — log 尾巴 fragment

**JSON API**

- `GET /health`
- `GET /jobs` — job 清單（含 `nextRunAt`／`logSource`／`origin`／最近一次 `JobRun`）
- `GET /jobs/:jobId`
- `GET /jobs/:jobId/runs?limit=` — 執行歷史（新到舊）；foreign job 回空陣列並附 `logSource` 說明為何沒有
- `GET /jobs/:jobId/log?lines=` — 直接 tail 該 job 的 log 檔；`logSource=none` 時回 409 並說明需先 adopt
- `POST /jobs/:jobId/run` — 手動觸發一次（`MANUAL_TRIGGER_ENABLED=false` → 403）
- `POST /jobs`、`PUT /jobs/:jobId`、`DELETE /jobs/:jobId` — CRUD（`CRONTAB_WRITE_ENABLED=false` → 403）
- `POST /jobs/:jobId/enable`、`POST /jobs/:jobId/disable` — 註解／取消註解該條目
- `POST /jobs/:jobId/adopt` — foreign → managed 轉換
- `GET /crontab` — 目前 crontab 原文（唯讀，供對照）

錯誤碼慣例：`400` 表達式或參數不合法、`403` 功能被設定關閉、`404` job 不存在、`409` 狀態衝突（外部改動 crontab／無 log 可讀）、`502` 底層檔案或指令操作失敗。

Postman 集合在 `postman/crontab-watcher.postman_collection.json`，新增／修改路由需同步更新。以 newman 跑：

```
newman run postman/crontab-watcher.postman_collection.json --env-var baseUrl=http://127.0.0.1:8080
```

## Layout

原始碼放 `internal/`（禁止外部 import），entrypoint 放 `cmd/cronwatch/`：

```
cmd/cronwatch/
  main.go             — subcommand 分派（serve / desktop / run / window）
  config.go           — 讀環境變數
  dependencies.go     — 手動 DI 與路由註冊
  run_command.go      — wrapper subcommand 的實作入口
  desktop_command.go  — 選單列 subcommand 的實作入口
  desktop_config.go   — 桌面模式的組態與預設值（applyDesktopDefaults）
  window_command_darwin.go / _other.go — 獨立視窗子程序（webview；非 macOS 為明確不支援）
internal/domain/
  entity/             — CronJob、CronSchedule、CrontabDocument、JobRun
                        + DesktopStatus（整體狀態與摘要的歸納）
                        + FailureNoticeLedger／FailureNotice（新失敗的判定與文字）
                        + parse_crontab_line.go（無狀態純轉換；crontab 文字的
                        解析是領域知識，不是 I/O 細節，故留在 domain）
                        + domain_error.go（所有領域哨兵錯誤集中一處）
  vo/                 — CrontabLine、CommandRedirect、JobStatusLine
  dto/                — CronJobDto、JobRunDto、DesktopStatusDto
  service/            — CronJobService、JobRunService、CrontabEditService、
                        DesktopStatusService（一檔一 struct）
  interface/          — 對外介面（一檔一介面）+ mocks/
internal/application/ — 用例編排
internal/controller/  — Gin handler + Request struct + ViewModel
                        + MenuBarController（_darwin / _other）與 MenuBarViewModel
internal/infrastructure/
  crontab/            — CrontabDocumentRepository（讀寫 crontab 檔、原子寫入、備份）
                        + CrontabCommandRepository（走 crontab -l / crontab <file>，host 模式用）
  runlog/             — JobRunRepository（runs.jsonl）、JobLogRepository（tail log 檔）
  shell/              — CommandExecutionProxy（os/exec 執行指令、逾時、process group 清理）
  system/             — IdentifierGenerator（uuid）、Clock、InstanceLock（檔案鎖）
  notification/       — NotificationProxy（osascript，文字一律走 argv）
  desktop/            — DesktopWindowProxy（獨立視窗的子程序管理）
internal/glyph/       — 服務的識別形狀（時鐘＋循環箭頭）。**app 圖示與選單列圖示
                        共用這一份定義**，兩邊各畫一份遲早會走鐘
internal/web/         — go:embed 的 templates/ 與 static/
scripts/
  package-macos-app.sh — 打包成 .app 與 DMG（圖示、Info.plist、ad-hoc 簽章、hdiutil）
tools/
  appicon/            — 純 Go 畫出 app 圖示的 iconset（不放二進位圖檔進 repo）
dist/                 — 打包產物，gitignore
```

**App 打包的三個不可商量的點**（細節見 `.sdd/2026-08-08-macos-app-packaging/PRD.md`）：

1. **App 名稱不含空白** —— 它的執行檔路徑會被寫進 crontab 條目，而 crontab 以空白分欄。建置腳本會擋。
2. **`LSUIElement=true`，但視窗子程序在執行時自行提升為前景程式** —— 一個 bundle 裡兩種活法：選單列不佔 Dock 圖示，視窗仍可聚焦、可打字、開啟時自動到最前。這也取代了原本用 `osascript` 帶視窗到最前的做法（那需要輔助使用權限）。
3. **從 Finder 啟動不繼承 shell 環境** —— 沒有 `.env`、只有最小 `PATH`。手動觸發的指令因此更接近 cron 自己的環境，依賴 `/opt/homebrew/bin` 之類路徑的指令要寫完整路徑。紀錄寫在 `$HOME/.local/state/crontab-watcher/desktop.log`，因為 stderr 在 Finder 啟動時無處可看。

**目前沒有 `internal/shared/`，也沒有各層 `README.md`**（如需要請自行補上，不要假設已存在）。

**測試集中在各層自己的 `tests/` 子資料夾**：每個有測試的 package 底下開一個 `tests/` 目錄，測試檔一律用**外部黑箱 package**（`<pkg>_test`，如 `entity_test`、`application_test`、`service_test`、`crontab_test`）放在其中，透過 import 測公開行為。crontab parse 的 fixture 檔（各種真實世界的 crontab 樣本）放 `internal/infrastructure/crontab/tests/testdata/`。entity 測試若需保留白箱寫法，以 **dot-import**（`import . ".../entity"`）處理。**例外**：`cmd/cronwatch/config_test.go` 與 `config.go` 同目錄、維持 `package main`（測未匯出的 config 解析，且 cmd 為組裝根非 internal layer）。import 一律走模組路徑（`github.com/james-hsueh/crontab-watcher/internal/...`）。

## Testing 策略

- **Entity**：直接 table-driven 單元測試。核心計算是 `CronSchedule.NextRunAt()`（跨月／跨年／DST／`@daily` alias／`*/N` step）、`CronJob.ResolveLogFilePath()`（各種 redirect 寫法：`>`／`>>`／`2>&1`／`&>`／`2>/dev/null`／無 redirect）、`CrontabDocument.Render()`（無損 round-trip）。**這三個是本專案的正確性核心，測試密度要最高。**
- **Domain Service**：**沒有介面**，以**具體實例**注入 application。有專屬單元測試，直接注入 mockery mock 的 repository／proxy。
- **Application 測試**：注入**真實的 domain service（連帶真實 entity）**，**只 mock 最外層的介面**（repository／proxy）。如此測 application 時會連帶測到 domain service 與 entity——這是刻意的「測試力度放大」。
- **Infrastructure 測試**：crontab／runlog repository 用 `t.TempDir()` 對**真實檔案系統**測（不 mock 檔案系統），驗證原子寫入、備份、mtime 衝突偵測、`runs.jsonl` 壓縮。`CommandExecutionProxy` 用真實 shell 執行無害指令（`echo`／`exit 3`／`sleep`）驗證 exit code 與逾時。
- mock 一律針對**介面**（`mockery` 產生 + `testify/mock` 的 `On(...).Return(...)`），**不手寫 fake 實作型別**。新增介面後在 `.mockery.yaml` 補一筆並重新產生 mock。
- **時間不可依賴 `time.Now()`**：需要當下時間的 entity method 一律以參數傳入（`NextRunAt(from time.Time)`），service 層才取真實時間。這讓排程計算完全可測。

## 部署

`Dockerfile`（multi-stage：`golang` build → `alpine` runtime）、`docker-entrypoint.sh`、`docker-compose.yml`、`Makefile`、`.env.example` 已備骨架，**待有 Go 程式碼（`go.mod`／`cmd/cronwatch`）後才能實際 build**。

自管模式（主推）的形狀：

- runtime image 為 alpine，內含 busybox `crond` 與 `/app/cronwatch`
- entrypoint 同時啟動 `crond`（讀 volume 上的 crontab）與 `cronwatch serve`
- 單一 volume `./data:/data` 承載 crontab 檔、log 目錄、`runs.jsonl`、備份 → 備份等於 `cp -r data`
- compose 的 port 綁 `127.0.0.1:8080:8080`——**絕不綁 `0.0.0.0`**
- job 指令所需的 runtime（python、curl、node…）需自行加進 Dockerfile 的 runtime stage，這是自管模式的代價，也是它結果可信的原因

唯讀模式（Linux host）：把 host crontab 與 log 目錄以 `:ro` 掛入，並設 `CRONTAB_WRITE_ENABLED=false`、`MANUAL_TRIGGER_ENABLED=false`。**macOS host 不支援唯讀模式**（Docker Desktop 在 Linux VM 內，看不到 `/var/at/tabs/`）。
