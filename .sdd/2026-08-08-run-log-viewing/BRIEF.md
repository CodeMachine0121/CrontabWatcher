# BRIEF · run log viewing

## 要解決什麼

讓使用者在瀏覽器上看到 job 的實際輸出——這是「為什麼要有這個服務」的核心。同時誠實區分兩種資料品質：managed job 有逐次執行紀錄，foreign job 只有一坨連續的 log 原文。

## 共識

- log 一律**從檔尾往回讀**（tail），行數由 `LOG_TAIL_LINES` 控制、`?lines=` 可覆寫，硬上限 5000、單次讀取 bytes 上限 1 MiB（見 D-12）。
- `LogSource=none` 時回 **409** 而非 200 空字串（見 D-16）——「無從得知」和「跑了但沒輸出」是不同的事實。
- log 檔不存在（job 還沒跑過）→ 回 200 + 空內容 + `exists=false` 旗標，這是正常狀態不是錯誤。
- 執行歷史只對 managed job 有意義；foreign job 回空陣列 + 說明原因的 `unavailableReason`。

## 不做什麼

- 不做 log 全文搜尋、不做語法高亮
- 不做 log rotation（那是系統或使用者的事；我們只負責讀）
- 不做 SSE 串流（定時輪詢 fragment 足夠）
