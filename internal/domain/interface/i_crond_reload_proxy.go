package interfaces

// ICrondReloadProxy 在 crontab 檔案被改動後通知 cron 重新載入。
type ICrondReloadProxy interface {
	// Reload 讓 cron 重新載入指定的 crontab 檔。
	// 失敗不代表寫入失敗 —— 檔案已經改好了，只是生效時間往後延。
	Reload(crontabFilePath string) error
}
