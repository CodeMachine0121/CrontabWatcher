package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/james-hsueh/crontab-watcher/internal/application"
	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
)

// ErrorResponse 是所有失敗回應的形狀。
type ErrorResponse struct {
	Error string `json:"error"`
	// Hint 在有可行下一步時給出建議（例如「先把這個 job 轉為納管」）。
	Hint string `json:"hint,omitempty"`
}

// statusCodeForError 把領域與 application 的哨兵錯誤對映到 HTTP 狀態碼。
//
// 這是 controller 唯一被允許認識下層錯誤的地方 —— 對映本身就是 HTTP 的職責。
func statusCodeForError(err error) int {
	switch {
	case errors.Is(err, entity.ErrCronJobNotFound):
		return http.StatusNotFound

	case errors.Is(err, entity.ErrInvalidCronExpression),
		errors.Is(err, entity.ErrInvalidCronCommand):
		return http.StatusBadRequest

	case errors.Is(err, entity.ErrCrontabChangedExternally),
		errors.Is(err, entity.ErrJobRunAlreadyRunning),
		errors.Is(err, entity.ErrJobLogUnavailable),
		errors.Is(err, entity.ErrCronJobAlreadyManaged):
		return http.StatusConflict

	case errors.Is(err, application.ErrCrontabWriteDisabled),
		errors.Is(err, application.ErrManualTriggerDisabled):
		return http.StatusForbidden

	default:
		// 未歸類的錯誤全是底層檔案或指令操作失敗。502 而非 500：問題出在我們
		// 依賴的東西（檔案系統、shell），不是請求本身。
		return http.StatusBadGateway
	}
}

// hintForError 在錯誤有明確下一步時給出建議。
func hintForError(err error) string {
	switch {
	case errors.Is(err, entity.ErrJobLogUnavailable):
		return "this job's output is not written to any file we can read; adopt it so crontab-watcher captures the output"

	case errors.Is(err, entity.ErrCrontabChangedExternally):
		return "the crontab file was changed outside crontab-watcher since this page loaded; reload and try again"

	case errors.Is(err, entity.ErrJobRunAlreadyRunning):
		return "wait for the current run to finish, or check the run history to see if it is stuck"

	case errors.Is(err, application.ErrCrontabWriteDisabled):
		return "set CRONTAB_WRITE_ENABLED=true to allow changes"

	case errors.Is(err, application.ErrManualTriggerDisabled):
		return "set MANUAL_TRIGGER_ENABLED=true to allow running jobs from the browser"

	default:
		return ""
	}
}

// respondWithError 以對映後的狀態碼回傳錯誤。
func respondWithError(context *gin.Context, err error) {
	context.JSON(statusCodeForError(err), ErrorResponse{
		Error: err.Error(),
		Hint:  hintForError(err),
	})
}
