# BRIEF · crontab parsing

## 要解決什麼

所有功能的地基：把一份 crontab 檔案的文字，變成可查詢的 `CronJob` 清單，並且**能無損還原回原文字**。沒有這一層，後面的列表、log、觸發、CRUD 全都無從談起。

## 共識

- 輸入是 crontab 檔案的**完整文字內容**（誰去讀檔是下一層的事，本切片純粹是文字 ↔ 領域物件）。
- **解析永不失敗**。任何無法辨識的行歸類為 `comment` 並原樣保留。理由：這是別人的檔案，我們沒有資格因為看不懂某一行就拒絕整份檔案。
- **無損 round-trip 是硬需求**：`ParseCrontabDocument(text).Render() == text`，對任何輸入都成立。
- 支援範圍見 `.sdd/DECISIONS.md` D-07（5 欄 + alias，不支援 6 欄與 user 欄位）。
- 本切片**不碰檔案系統、不碰 HTTP**，純 domain。

## 不做什麼

- 不讀寫檔案（下一切片）
- 不算「最近一次執行」（需要 `runs.jsonl`）
- 不做 `Describe()` 的完整自然語言化——先做常見形態（`@daily` alias、整點、每 N 分鐘），其餘退回顯示原始表達式
