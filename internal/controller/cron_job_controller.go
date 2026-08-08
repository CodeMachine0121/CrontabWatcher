package controller

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/james-hsueh/crontab-watcher/internal/application"
)

// CronJobUpsertRequest 是新增與更新 job 的 JSON body。
type CronJobUpsertRequest struct {
	ScheduleExpression string `json:"scheduleExpression"`
	Command            string `json:"command"`
	Description        string `json:"description"`
	// Enabled 用指標以區分「沒給」與「明確給 false」。沒給時預設啟用。
	Enabled *bool `json:"enabled"`
}

func (request CronJobUpsertRequest) enabledOrDefault() bool {
	if request.Enabled == nil {
		return true
	}

	return *request.Enabled
}

// CronJobController 負責排程條目的 HTTP 端點。
type CronJobController struct {
	cronJobApplication       *application.CronJobApplication
	jobRunApplication        *application.JobRunApplication
	manualTriggerApplication *application.ManualTriggerApplication
	crontabEditApplication   *application.CrontabEditApplication
}

// NewCronJobController 建立 controller。
func NewCronJobController(
	cronJobApplication *application.CronJobApplication,
	jobRunApplication *application.JobRunApplication,
	manualTriggerApplication *application.ManualTriggerApplication,
	crontabEditApplication *application.CrontabEditApplication,
) *CronJobController {
	return &CronJobController{
		cronJobApplication:       cronJobApplication,
		jobRunApplication:        jobRunApplication,
		manualTriggerApplication: manualTriggerApplication,
		crontabEditApplication:   crontabEditApplication,
	}
}

// ListCronJobs 處理 GET /jobs。
func (controller *CronJobController) ListCronJobs(context *gin.Context) {
	jobs, err := controller.cronJobApplication.ListCronJobs()
	if err != nil {
		respondWithError(context, err)
		return
	}

	context.JSON(http.StatusOK, jobs)
}

// GetCronJob 處理 GET /jobs/:jobId。
func (controller *CronJobController) GetCronJob(context *gin.Context) {
	job, err := controller.cronJobApplication.GetCronJob(context.Param("jobId"))
	if err != nil {
		respondWithError(context, err)
		return
	}

	context.JSON(http.StatusOK, job)
}

// ListJobRuns 處理 GET /jobs/:jobId/runs。
func (controller *CronJobController) ListJobRuns(context *gin.Context) {
	limit, err := parseOptionalPositiveInteger(context, "limit")
	if err != nil {
		context.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	runList, err := controller.jobRunApplication.ListJobRuns(context.Param("jobId"), limit)
	if err != nil {
		respondWithError(context, err)
		return
	}

	context.JSON(http.StatusOK, runList)
}

// TailJobLog 處理 GET /jobs/:jobId/log。
func (controller *CronJobController) TailJobLog(context *gin.Context) {
	lines, err := parseOptionalPositiveInteger(context, "lines")
	if err != nil {
		context.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	logDto, err := controller.jobRunApplication.TailJobLog(context.Param("jobId"), lines)
	if err != nil {
		respondWithError(context, err)
		return
	}

	context.JSON(http.StatusOK, logDto)
}

// TriggerJobRun 處理 POST /jobs/:jobId/run。
func (controller *CronJobController) TriggerJobRun(context *gin.Context) {
	runDto, err := controller.manualTriggerApplication.TriggerJobRun(
		context.Request.Context(), context.Param("jobId"))
	if err != nil {
		respondWithError(context, err)
		return
	}

	context.JSON(http.StatusOK, runDto)
}

// CreateCronJob 處理 POST /jobs。
func (controller *CronJobController) CreateCronJob(context *gin.Context) {
	var request CronJobUpsertRequest
	if err := context.ShouldBindJSON(&request); err != nil {
		context.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	job, err := controller.crontabEditApplication.CreateCronJob(
		request.ScheduleExpression, request.Command, request.Description, request.enabledOrDefault())
	if err != nil {
		respondWithError(context, err)
		return
	}

	context.JSON(http.StatusCreated, job)
}

// UpdateCronJob 處理 PUT /jobs/:jobId。
func (controller *CronJobController) UpdateCronJob(context *gin.Context) {
	var request CronJobUpsertRequest
	if err := context.ShouldBindJSON(&request); err != nil {
		context.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	job, err := controller.crontabEditApplication.UpdateCronJob(
		context.Param("jobId"), request.ScheduleExpression, request.Command,
		request.Description, request.enabledOrDefault())
	if err != nil {
		respondWithError(context, err)
		return
	}

	context.JSON(http.StatusOK, job)
}

// DeleteCronJob 處理 DELETE /jobs/:jobId。
func (controller *CronJobController) DeleteCronJob(context *gin.Context) {
	if err := controller.crontabEditApplication.DeleteCronJob(context.Param("jobId")); err != nil {
		respondWithError(context, err)
		return
	}

	context.Status(http.StatusNoContent)
}

// EnableCronJob 處理 POST /jobs/:jobId/enable。
func (controller *CronJobController) EnableCronJob(context *gin.Context) {
	controller.setEnabled(context, true)
}

// DisableCronJob 處理 POST /jobs/:jobId/disable。
func (controller *CronJobController) DisableCronJob(context *gin.Context) {
	controller.setEnabled(context, false)
}

func (controller *CronJobController) setEnabled(context *gin.Context, enabled bool) {
	job, err := controller.crontabEditApplication.SetCronJobEnabled(context.Param("jobId"), enabled)
	if err != nil {
		respondWithError(context, err)
		return
	}

	context.JSON(http.StatusOK, job)
}

// AdoptCronJob 處理 POST /jobs/:jobId/adopt。
func (controller *CronJobController) AdoptCronJob(context *gin.Context) {
	job, err := controller.crontabEditApplication.AdoptCronJob(context.Param("jobId"))
	if err != nil {
		respondWithError(context, err)
		return
	}

	context.JSON(http.StatusOK, job)
}

// GetCrontabContent 處理 GET /crontab，回傳檔案原文供對照。
func (controller *CronJobController) GetCrontabContent(context *gin.Context) {
	content, err := controller.crontabEditApplication.GetCrontabContent()
	if err != nil {
		respondWithError(context, err)
		return
	}

	context.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(content))
}

// Health 處理 GET /health。
func (controller *CronJobController) Health(context *gin.Context) {
	context.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// parseOptionalPositiveInteger 解析選填的正整數 query 參數。
//
// 沒給回 0（由 application 決定預設值）；給了但不是正整數就回 400 —— 悄悄忽略
// 打錯的參數，會讓使用者以為自己的設定生效了。
func parseOptionalPositiveInteger(context *gin.Context, parameterName string) (int, error) {
	rawValue := context.Query(parameterName)
	if rawValue == "" {
		return 0, nil
	}

	value, err := strconv.Atoi(rawValue)
	if err != nil || value < 1 {
		return 0, &invalidQueryParameterError{parameterName: parameterName, rawValue: rawValue}
	}

	return value, nil
}

type invalidQueryParameterError struct {
	parameterName string
	rawValue      string
}

func (err *invalidQueryParameterError) Error() string {
	return "query parameter " + err.parameterName + " must be a positive integer, got " + strconv.Quote(err.rawValue)
}
