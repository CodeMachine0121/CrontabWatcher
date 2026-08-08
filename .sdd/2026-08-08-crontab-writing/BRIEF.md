# BRIEF · crontab writing

## 要解決什麼

從瀏覽器新增／編輯／刪除／啟用停用 cronjob，以及把 foreign job 轉為 managed（adopt）。這是風險最高的切片——寫壞使用者的 crontab 就等於弄壞他的自動化。

## 共識

- Go 沒有現成的 crontab 檔案管理 library，parse／render 自己寫（已在 crontab-parsing 切片完成）。
- **原子寫入**：temp 檔 → 語法驗證 → 備份現行檔 → `os.Rename` 就位 → 通知 cron reload。任一步失敗即整體放棄、原檔不動。
- **樂觀鎖**：以讀取時的 mtime+size 指紋比對，偵測到外部（`crontab -e`）改動即 409，絕不覆蓋（見 D-14）。
- **停用是註解掉，不是刪除**（見 D-06）。刪除才真的移除該行。
- adopt 會剝離原有 redirect 並記在 marker 註解裡以便還原（見 D-13）。
- `CRONTAB_WRITE_ENABLED=false` 時所有寫入端點回 403。

## 不做什麼

- 不做多使用者的 crontab（只管 `CRONTAB_FILE_PATH` 指的那一份）
- 不做 `/etc/crontab` 的 user 欄位格式
- 不做 undo／版本瀏覽 UI（備份檔留在磁碟上，需要時人工還原）
