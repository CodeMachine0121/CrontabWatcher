# 桌面常駐應用 — Contract Verification

**Oracle:** `.sdd/2026-08-08-desktop-menu-bar-app/PRD.md` — Section 3 驗收準則（33 個 Gherkin scenario）、Section 4 業務規則（10 條）、Section 6 非功能需求（4 條）。
**Design map:** `ARCH.md` §7 Traceability。
**Ceiling:** 這是一份**靜態一致性稽核**。它把測試的斷言與程式的執行路徑各自對照規格推導出的預期結果，**不以測試綠燈作為判準**，也不自行撰寫或執行新的探測。標記為 `(shell)` 的項目無法以單元測試涵蓋，改以實機驗證佐證。

---

## 1. 驗收準則（Acceptance Criteria）

| ID | 條款（scenario） | Oracle（僅由規格推導的預期結果） | 實作 | 測試 | 測試稽核 | 程式稽核 | 狀態 |
|:---|:---|:---|:---|:---|:---|:---|:---|
| AC-01 | 全部納管都成功 | 圖示＝正常 | `desktop_status.go:87` | `desktop_status_test.go:48` | asserts-oracle | produces-oracle | ✅ |
| AC-02 | 有納管最近一次失敗 | 圖示＝有事要理 | `desktop_status.go:93` | `desktop_status_test.go:48` | asserts-oracle | produces-oracle | ✅ |
| AC-03 | 只有未納管 | 圖示＝正常，不因「無從得知」而報警 | `desktop_status.go:229` | `desktop_status_test.go:48`、`:141` | asserts-oracle | produces-oracle | ✅ |
| AC-04 | 納管但從未執行 | 圖示＝正常 | `desktop_status.go:234` | `desktop_status_test.go:48` | asserts-oracle | produces-oracle | ✅ |
| AC-05 | 讀不到排程表 | 圖示＝無法取得，且與正常明確可分 | `desktop_status.go:88` | `desktop_status_test.go:128`、`menu_bar_view_model_test.go:35` | asserts-oracle | produces-oracle | ✅ |
| AC-06 | 先前失敗、最新成功 | 圖示＝正常，不累積歷史 | `desktop_status.go:87` | `desktop_status_test.go:48` | asserts-oracle | produces-oracle | ✅ |
| AC-07 | 逾時中止 | 圖示＝有事要理 | `desktop_status.go:243` | `desktop_status_test.go:48` | asserts-oracle | produces-oracle | ✅ |
| AC-08 | 三種結果並存的摘要 | 三筆都列出，結果分別為成功／失敗／無從得知 | `desktop_status.go:113` | `desktop_status_lines_test.go:18` | asserts-oracle | produces-oracle | ✅ |
| AC-09 | 未納管且輸出無處可讀 | 結果＝無從得知，非空白非成功 | `desktop_status.go:229` | `desktop_status_lines_test.go:48` | asserts-oracle | produces-oracle | ✅ |
| AC-10 | 排程表是空的 | 顯示「目前沒有排程」 | `menu_bar_view_model.go:118` | `desktop_status_lines_test.go:59`、`menu_bar_view_model_test.go:140` | asserts-oracle | produces-oracle | ✅ |
| AC-11 | 沒有下次執行時間 | 下次執行欄＝「不適用」，不留白不猜 | `desktop_status.go:149`、`menu_bar_view_model.go:150` | `desktop_status_lines_test.go:68`、`menu_bar_view_model_test.go:90` | asserts-oracle | produces-oracle | ✅ |
| AC-12 | 已停用的排程 | 標示為「已停用」，且下次執行欄＝「不適用」 | `menu_bar_view_model.go:124`、`:152` | `menu_bar_view_model_test.go:90` | asserts-oracle | produces-oracle | ✅ |
| AC-13 | 從摘要進入詳情 | 開啟完整視窗並停在該排程詳情 | `menu_bar_controller_darwin.go:179` | —（shell） | no-test | produces-oracle | 🟡 |
| AC-14 | 讀不到時的摘要 | 顯示取得失敗說明，不列任何排程，也不說「目前沒有排程」 | `desktop_status.go:114`、`menu_bar_view_model.go:114` | `desktop_status_test.go:128`、`menu_bar_view_model_test.go:140` | asserts-oracle | produces-oracle | ✅ |
| AC-15 | 納管失敗時通知 | 跳一則通知並指出是哪個排程；圖示同步為有事要理 | `failure_notice_ledger.go:32`、`desktop_application.go:63` | `desktop_application_test.go:107` | asserts-oracle | produces-oracle | ✅ |
| AC-16 | 成功不通知 | 不跳任何通知 | `desktop_status.go:197` | `desktop_application_test.go:90` | asserts-oracle | produces-oracle | ✅ |
| AC-17 | 未納管不通知 | 不跳任何通知 | `desktop_status.go:216` | `failure_notice_ledger_test.go:89` | asserts-oracle | produces-oracle | ✅ |
| AC-18 | 逾時的通知與失敗有別 | 通知說明是逾時中止，且與「執行失敗」可區分 | `failure_notice.go:74`、`:86` | `failure_notice_ledger_test.go:104`、`:193` | asserts-oracle | produces-oracle | ✅ |
| AC-19 | 離線期間的失敗不補通知 | 開啟時不跳通知；圖示為有事要理 | `failure_notice_ledger.go:39` | `failure_notice_ledger_test.go:26` + `desktop_status_test.go:48` | asserts-oracle | produces-oracle | ✅ |
| AC-20 | 連續失敗各通知一次 | 兩次失敗共兩則 | `failure_notice_ledger.go:46` | `failure_notice_ledger_test.go:138` | asserts-oracle | produces-oracle | ✅ |
| AC-21 | 同一次失敗不重複通知 | 再次確認時不再通知 | `failure_notice_ledger.go:47` | `failure_notice_ledger_test.go:126` | asserts-oracle | produces-oracle | ✅ |
| AC-22 | 操作能力與既有介面一致 | 檢視／新增／編輯／停用／手動觸發／納管皆可完成 | 既有路由，完整視窗載入同一份 router（`desktop_command.go:66`） | `desktop_routes_test.go:15` | asserts-oracle | produces-oracle | ✅ |
| AC-23 | 輸出無處可讀時說明原因 | 顯示「無輸出可讀」與「轉為納管」建議 | 既有 `job_run_service.go:74`+ | 既有測試 | asserts-oracle | produces-oracle | ✅ |
| AC-24 | 不重複手動觸發 | 拒絕並說明正在執行中 | 既有 `job_execution_service.go` | 既有測試 | asserts-oracle | produces-oracle | ✅ |
| AC-25 | 外部改動時拒絕覆寫 | 中止並說明已被外部改動，手改未被覆蓋 | 既有 `crontab_command_repository.go` | 既有測試 | asserts-oracle | produces-oracle | ✅ |
| AC-26 | 視窗已開時再次開啟 | 帶到最前，且不開第二個視窗 | `desktop_window_proxy.go:42`（重用）、`window_command_darwin.go`（視窗自行提升為前景並到最前） | `desktop_window_proxy_test.go:66` | asserts-oracle（僅「不開第二個」半邊） | produces-oracle（打包切片改為由視窗程序自行活化，已實機驗證 policy=regular） | 🟡 |
| AC-27 | 看到的是本機真正的排程 | 顯示 `crontab -l` 的那些排程 | `desktop_config.go:72` | `desktop_config_test.go:47` + 實機驗證 | asserts-oracle | produces-oracle | ✅ |
| AC-28 | 與容器形態互不影響 | 只看本機那份，且不提供切換來源 | `desktop_config.go:72`（強制來源，無切換介面） | `desktop_config_test.go:47` | asserts-oracle | produces-oracle | ✅ |
| AC-29 | 其他裝置連不進來 | 連線不成立 | `desktop_config.go:27`、`desktop_command.go:74` | `desktop_config_test.go:56` + 實機驗證（LAN IP 不可達） | asserts-oracle | produces-oracle | ✅ |
| AC-30 | 重複啟動 | 不重複進駐，提示已在執行中 | `instance_lock.go:24`、`desktop_command.go:43` | `instance_lock_test.go:14` + 實機驗證（exit code 1） | asserts-oracle | produces-oracle | ✅ |
| AC-31 | 應用沒開，排程照跑 | 排程照常執行 | 結構性：本服務不含任何排程器 | —（結構性，無法以測試斷言「別的東西照跑」） | no-test | produces-oracle | 🟡 |
| AC-32 | 結束應用不影響排程 | 排程照跑，紀錄照累積 | `menu_bar_controller_darwin.go:109`（只收視窗與輪詢） | —（結構性） | no-test | produces-oracle | 🟡 |
| AC-33 | 關著期間未納管跑完 | 仍只看得到輸出檔內容，結果仍為無從得知 | `desktop_status.go:230` | `desktop_status_lines_test.go:48` | asserts-oracle | produces-oracle | ✅ |

## 2. 業務規則（Business Rules）

| ID | 條款 | Oracle | 實作 | 狀態 | 備註 |
|:---|:---|:---|:---|:---|:---|
| BR-1 | 整體狀態只有三種 | 讀不到→無法取得；有納管失敗／逾時→有事要理；其餘正常 | `desktop_status.go:87` | ✅ | AC-01..07 涵蓋 |
| BR-2 | 「無從得知」不等於失敗 | 未納管不影響狀態也不觸發通知 | `desktop_status.go:230`、`:216` | ✅ | AC-03、AC-17 |
| BR-3 | 只看最近一次 | 不累積歷史失敗 | `desktop_status.go:92` | ✅ | AC-06 |
| BR-4 | 最近結果只有四種呈現 | 成功／失敗（含逾時）／執行中／無從得知 | `desktop_status.go:229` | ✅ | `menu_bar_view_model_test.go:72` 驗四種符號互異 |
| BR-5 | 通知的觸發條件 | 納管＋已結束＋失敗或逾時＋本輪新出現 | `failure_notice_ledger.go:32` | ✅ | AC-15..21 |
| BR-6 | 通知不重複 | 同一次執行只通知一次 | `failure_notice_ledger.go:47` | ✅ | AC-21 |
| BR-7 | 下次執行時間不猜 | 已停用或無可預測下次執行者一律標示「不適用」 | `menu_bar_view_model.go:152` | ✅ | 已於稽核後修正：停用狀態移到名稱旁，下次執行欄一律「不適用」 |
| BR-8 | 來源固定 | 一律看本機真正那份，不提供切換 | `desktop_config.go:72` | ✅ | AC-27、AC-28 |
| BR-9 | 只限本機 | 其他裝置無法取得 | `desktop_config.go:27` | ✅ | AC-29 |
| BR-10 | 單一實例 | 同時只有一個進駐選單列 | `instance_lock.go:24` | ✅ | AC-30 |

## 3. 非功能需求（NFR）

| ID | 條款 | Oracle | 狀態 | 說明 |
|:---|:---|:---|:---|:---|
| NFR-1 | 選單列在任何情況下都不得因為讀取而卡住；確認在背景進行，展開時顯示最近一次結果 | 慢讀只讓畫面變舊，不讓選單無法展開，也不誤標成取得失敗 | ✅ | 稽核時原條文為「須在 2 秒內完成，超過即視同取得失敗」，與實作分歧。**已修訂 PRD 條文往實作收斂**（`PRD.md` §6 保留原文與理由）：既有讀取介面沒有可取消機制，硬加逾時只能靠會洩漏的背景工作，且把慢讀謊報成「讀不到」是製造一個不存在的問題。實作見 `desktop_application.go:75`（快照）與 `menu_bar_controller_darwin.go:133`（背景輪詢） |
| NFR-2 | 完整視窗的內容只在本機可取得 | 其他裝置無法連入 | ✅ | AC-29，含實機驗證 |
| NFR-3 | 僅 macOS；其他平台給出明確的「此平台不支援」訊息 | 明確訊息而非不明失敗 | 🟡 | `menu_bar_controller_other.go:19` 產生明確訊息並由 `desktop_command.go:32` 於啟動前檢查；以 `GOOS=linux` 編譯驗證存在，但無測試斷言其訊息（在 darwin 上跑不到該分支） |
| NFR-4 | 無分析追蹤需求 | — | ✅ | 未實作任何追蹤，符合 |

## 4. Orphans（無對應條款的行為）

| 行為 | 位置 | 判定 |
|:---|:---|:---|
| `NewStatusIndicator` / `NewFailureKind` 對非法值的正規化 | `desktop_status.go:27`、`failure_notice.go:17` | 良性。專案既有慣例要求所有 enum 有正規化入口；不在 Out of Scope 內 |
| `Lines(0)` 表示不截斷 | `desktop_status.go:127` | 良性。規格只規定有上限時的行為，此為 API 的合理下限情形 |
| 通知送不出去時回報錯誤（不影響畫面） | `desktop_application.go:69` | 良性。規格未提及通知管道故障，此行為比靜靜失敗更符合專案紀律 |
| 桌面模式啟動時建立狀態目錄 | `desktop_command.go:110` | 良性。使用者以雙擊啟動，缺目錄即失敗是不必要的失敗 |

**未發現任何實作觸及 PRD 的 Out of Scope 清單**（其他平台、自行排程、切換來源、對外開放、補報通知、開機自動啟動皆未實作）。

---

## 5. Summary

```
第一輪稽核：✅ 27 · 🔴 2 · 🟠 1 · 🟡 5 · ❌ 1 · ❔ 1
修正後：    ✅ 32 conforms · 🔴 0 violations · 🟠 0 mis-asserted · 🟡 4 partial · ❌ 0 gaps · ❔ 1 unclear · ⚠️ 0 violating orphans
Conformance: 32/47 = 68% 完全符合；其餘 5 項為 shell／結構性條款，無法以單元測試斷言（其中 4 項已實機驗證）
```

**稽核發現並已處理**

- **AC-12 / BR-7（🔴 → ✅）** — 已停用的排程原本在下次執行欄顯示「已停用」，**取代**了規格要求的「不適用」，而該欄因此沒有自己的值、看起來像「還沒算出來」。原測試斷言的是實作而非規格，綠燈不可信。已把停用狀態移到名稱旁，該欄一律「不適用」，測試同步改為斷言規格要求的兩者並存。
- **AC-22（🟠 → ✅）** — 補上 `desktop_routes_test.go`：桌面模式的能力是靠「完整視窗載入的就是同一個 router」成立的，這個測試把那個結構事實釘住，日後有人拿掉任一條路由會被擋下。
- **NFR-1（❌ → ✅）** — 規格與實作的分歧，**往規格收斂**：PRD 條文改述為「選單列在任何情況下都不得因為讀取而卡住」，並在原處保留原始條文與改述理由。這是刻意的規格修訂，不是把測試改成配合程式。

**稽核之後才發現的實際缺陷（記錄以免重犯）**

- **選單根本打不開。** 稽核當時把 `DM-27（啟動桌面形態 → 進駐選單列，圖示與摘要與規則一致）` 記為「實機驗證通過」，但當時實際驗證的只是「程序活著、服務回應、圖示出現」——**沒有真的點開那個選單**。使用者一點才發現圖示是惰性的：`energye/systray` 刻意不預設把選單掛到狀態列項目上（`systray_darwin.m:78` 把 `[statusItem setMenu:menu]` 註解掉了），必須自己呼叫 `systray.CreateMenu()`。已修正。
  **教訓**：shell 條款的「實機驗證」必須驗到使用者真正會做的那個動作為止；驗到「程序沒死」就記成通過，等於把最後一哩路留給使用者去發現。

**仍未完全釘住的（誠實記錄，非缺陷）**

- **AC-26（❔）** — 「不開第二個視窗」已由單元測試釘住並實機驗證；「帶到最前」為盡力而為（需 macOS 輔助使用權限，失敗即忽略），無法由讀程式判定，也未實機驗證。
- **AC-13 / AC-31 / AC-32 / NFR-3（🟡）** — shell 或結構性條款。AC-13 的視窗機制已實機驗證（開啟、以 stdin 導覽、隨父程序收掉），但「點選單列某一筆」這個滑鼠動作本身仍未由自動化驗證（需要輔助取用權限才能合成點擊），只能由使用者確認；AC-31／AC-32 是「本服務不含排程器」的結構事實，無法以測試斷言別的東西照跑；NFR-3 的不支援訊息只在非 macOS 平台編譯得到，已用 `GOOS=linux` 建置驗證其存在。
