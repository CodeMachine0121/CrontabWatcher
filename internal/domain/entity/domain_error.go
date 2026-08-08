package entity

import "errors"

// 領域哨兵錯誤。集中在最內層的 entity package，讓 service 直接回傳、controller
// 直接以 errors.Is 對映 HTTP 狀態碼，不必在每一層重新包裝或重新宣告。
var (
	// ErrInvalidCronExpression 表示 cron 表達式無法解析，或使用了本服務刻意不
	// 支援的形態（六欄含秒、未知 alias）。→ 400
	ErrInvalidCronExpression = errors.New("invalid cron expression")

	// ErrInvalidCronCommand 表示指令為空白，或含有會破壞 crontab 單行格式的
	// 換行字元。→ 400
	ErrInvalidCronCommand = errors.New("invalid cron command")

	// ErrCronJobNotFound 表示 crontab 內找不到該 jobID。→ 404
	ErrCronJobNotFound = errors.New("cron job not found")

	// ErrCronJobAlreadyManaged 表示對已納管的 job 再次執行 adopt。→ 409
	ErrCronJobAlreadyManaged = errors.New("cron job is already managed")

	// ErrCrontabChangedExternally 表示自讀取以來 crontab 檔案已被外部改動
	// （例如使用者跑了 crontab -e），寫入因此中止以免覆蓋手改。→ 409
	ErrCrontabChangedExternally = errors.New("crontab file changed externally")

	// ErrJobRunAlreadyRunning 表示該 job 已有一筆執行中的紀錄。→ 409
	ErrJobRunAlreadyRunning = errors.New("job run already in progress")

	// ErrJobRunNotFound 表示要更新的執行紀錄不存在。
	ErrJobRunNotFound = errors.New("job run not found")

	// ErrJobLogUnavailable 表示該 job 無 log 可讀——輸出沒有被導向任何檔案，
	// 或被導向 /dev/null。這與「跑過但沒有輸出」是不同的事實。→ 409
	ErrJobLogUnavailable = errors.New("job log unavailable")
)
