# PRD · macOS App 打包

**來源：** `.sdd/2026-08-08-macos-app-packaging/BRIEF.md`
**前置切片：** `.sdd/2026-08-08-desktop-menu-bar-app/`（本片不引入新行為，只改變交付方式）

## User Stories

- **US-1** 作為使用者，我要一個指令就產出一張 DMG，不必記得 `codesign`／`hdiutil`／`iconutil` 各要怎麼下。
- **US-2** 作為使用者，我打開 DMG 要看到「App ＋ 應用程式資料夾」，拖過去就安裝完成 —— 這是每個 Mac 使用者都已經會的動作。
- **US-3** 作為使用者，我從應用程式資料夾打開它就要進駐選單列，不要冒出終端機、也不要多一個沒有用的 Dock 圖示。
- **US-4** 作為使用者，我從選單列開出來的視窗要能打字 —— 那裡面有新增與編輯排程的表單。
- **US-5** 作為使用者，我用它建立的排程不能因為我之後重新編譯專案就失效。
- **US-6** 作為使用者，如果雙擊之後什麼都沒發生，我要有地方查原因。

## Business Flow

```
make dmg
  → scripts/package-macos-app.sh
      檢查平台是 macOS，且 App 名稱不含空白
      go build（含 cgo，選單列與視窗都是 Cocoa）→ Contents/MacOS/cronwatch
      go run ./tools/appicon → iconset → iconutil → Contents/Resources/AppIcon.icns
      寫 Info.plist（LSUIElement、CFBundleIconFile、版本號）→ plutil -lint 驗證
      codesign --sign -（ad-hoc）→ codesign --verify
      staging 目錄 = App + /Applications 符號連結
      hdiutil create → dist/CrontabWatcher.dmg

使用者打開 DMG → 拖 App 到「應用程式」→ 從那裡打開
  → binary 發現自己位在 .app/Contents/MacOS/ 底下
  → 預設子命令因此是 desktop（而不是 serve）
  → 進駐選單列，其餘行為與前一切片完全相同
```

## 規格細節

### App 身分

| 項目 | 值 | 理由 |
|:---|:---|:---|
| App 名稱 | `CrontabWatcher`（**不含空白**） | 執行檔的絕對路徑會被寫進 crontab 條目，而 crontab 以空白分欄。名稱裡有空格＝寫出去的排程跑不起來。建置腳本會主動擋下含空白的名稱 |
| Bundle identifier | `com.jameshsueh.crontab-watcher` | 穩定身分，讓 macOS 記得已授予的權限 |
| `LSUIElement` | `true` | 選單列 app 不該佔 Dock 圖示 |
| `LSMinimumSystemVersion` | `11.0` | 保守下限 |
| 版本號 | 建置參數，預設 `0.1.0` | `make dmg VERSION=0.2.0` |

### 從 Finder 啟動時的預設子命令

雙擊時命令列上什麼都沒有，而 `main.go` 原本的預設是 `serve` —— 那會起一個沒有任何畫面的 web server，使用者只會看到「雙擊了但什麼都沒發生」。

因此 binary 認得自己的處境：**執行檔路徑含 `.app/Contents/MacOS/` 時，預設子命令是 `desktop`**。從終端機跑時預設仍是 `serve`，既有行為不變；明確給了子命令時一律以它為準。Finder 有時會塞一個 `-psn_…` 參數，那不是子命令，一律略過。

### 前景身分：一個 bundle、兩種活法

`LSUIElement=true` 會讓**整個 bundle 裡的所有程序**都變成 accessory —— 包括視窗子程序。accessory 的視窗會躲在別人後面、拿不到鍵盤焦點，而那個視窗裡有要填的表單。

webview 只在「沒有被包成 bundle」時才自己把程序提升為前景 app，包起來之後就不管了。因此由視窗子程序在啟動時、以及每次收到新網址時，自行呼叫 `setActivationPolicy:Regular` + `activateIgnoringOtherApps:`。

**這同時取代了前一切片裡用 `osascript` 操作 System Events 來「帶到最前」的做法** —— 那需要輔助使用權限，拿不到就只能安靜地失敗。程序對自己說話不需要任何權限。`DesktopWindowProxy` 因此不再需要 `activatorPath`。

結果是：選單列程序＝accessory（無 Dock 圖示），視窗程序＝regular（可聚焦、可打字、開啟時自動到最前）。

### 簽章

`codesign --force --deep --sign -`（ad-hoc）。個人自用、無開發者憑證，**不做公證**。ad-hoc 簽章的價值不在 Gatekeeper，而在於讓 macOS 認得這是一個穩定的身分 —— 沒有簽章時每次重新編譯都會被當成不同的 app，已經授予的權限會被忘掉。

> 在**本機建置、本機安裝**的路徑上不會遇到 Gatekeeper。若把 DMG 傳到另一台機器，該檔案會被標上隔離屬性，開啟時需要在「系統設定 → 隱私權與安全性」按一次「仍要打開」。這是不做公證的代價，已知並接受。

### 從 Finder 啟動的執行環境

App 不繼承終端機的環境變數：**沒有 `.env`、沒有你 shell 裡的 `PATH`**，只有系統給的最小環境。後果：

- 所有設定都走預設值（本來就全部有預設值，因此可以正常啟動）。
- **手動觸發的指令會在最小 `PATH` 下執行**，與 cron 自己的環境更接近，而不是與你的終端機相同。指令若依賴 `/opt/homebrew/bin` 之類的路徑，要寫完整路徑 —— 這對 cron 本來就是必要的。

### 紀錄檔

從 Finder 啟動時 stderr 等於掉進黑洞。桌面模式因此把紀錄同時寫到 stderr 與
`$HOME/.local/state/crontab-watcher/desktop.log`，超過 1 MiB 就從頭寫。「打開沒反應」必須查得到原因。

### 圖示

以 `tools/appicon`（純 Go，`image/png`）畫出十個尺寸的 iconset，再交給 `iconutil`。不放二進位圖檔進 repo：這個專案的資產一向是文字，而一張圖示就是幾個幾何形狀。

## Test Cases

打包本身沒有可單元測試的領域邏輯，因此以「建置產物」與「實機」驗證為主。唯一有邏輯的部分是子命令解析，它有單元測試。

| ID | 情境 | 期望 | 方式 |
|:---|:---|:---|:---|
| AP-01 | 執行檔在 `.app/Contents/MacOS/` 下、無參數 | 子命令為 `desktop` | 單元測試 |
| AP-02 | 執行檔在一般路徑下、無參數 | 子命令為 `serve`（既有行為不變） | 單元測試 |
| AP-03 | Finder 塞了 `-psn_…` 參數 | 略過它，子命令仍為 `desktop` | 單元測試 |
| AP-04 | 明確給了子命令（bundle 內或外） | 以它為準 | 單元測試 |
| AP-05 | 子命令之後的參數 | 正確傳遞，且不含 `-psn_…` | 單元測試 |
| AP-06 | `make dmg` | 產出 `dist/CrontabWatcher.app` 與 `dist/CrontabWatcher.dmg` | 實機 |
| AP-07 | 掛載該 DMG | 內容為 App ＋ `Applications` 符號連結 | 實機 |
| AP-08 | `Info.plist` | 通過 `plutil -lint`，且 `LSUIElement` 為 true | 實機 |
| AP-09 | 簽章 | `codesign --verify --deep --strict` 通過 | 實機 |
| AP-10 | 以 Finder 的方式打開 App | 進駐選單列（走 desktop 而非 serve） | 實機 |
| AP-11 | 選單列程序的啟動策略 | `accessory`（無 Dock 圖示） | 實機 |
| AP-12 | 視窗子程序的啟動策略 | `regular`，且螢幕上有一個 layer 0 的視窗 | 實機 |
| AP-13 | 紀錄檔 | `desktop.log` 有本次啟動的內容 | 實機 |
| AP-14 | 在非 macOS 上執行建置腳本 | 明確拒絕 | 讀腳本（`uname -s` 檢查） |

## 驗收標準

- AP-01..AP-05 綠燈；`make check` 全綠。
- AP-06..AP-13 實機驗證通過。
- 既有的四種形態（容器自管、唯讀、host、本機沙箱）與桌面模式行為完全不變。
