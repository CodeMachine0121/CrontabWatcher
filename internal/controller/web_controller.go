package controller

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/james-hsueh/crontab-watcher/internal/application"
	"github.com/james-hsueh/crontab-watcher/internal/domain/dto"
	"github.com/james-hsueh/crontab-watcher/internal/domain/entity"
	"github.com/james-hsueh/crontab-watcher/internal/web"
)

// displayTimeLayout 是頁面上顯示時間的格式。刻意不含時區偏移 —— header 已經標出
// 時區，每一格再重複一次只是噪音。
const displayTimeLayout = "01/02 15:04:05"

// pageViewModel 是 template 的資料形狀。它只服務 HTML 渲染，不外流到 API。
type pageViewModel struct {
	PageTitle string
	// CrontabSourceLabel 是這份 crontab 的來源說明：檔案模式是路徑，命令模式是
	// `crontab -l`。刻意不叫 CrontabFilePath —— 命令模式下它並不是一個路徑。
	CrontabSourceLabel string
	TimeZoneName       string

	// WatchingUserCrontab 為 true 表示正在看使用者真正的 crontab（命令模式）。
	// 空清單的說明文字需要它才能給出對的建議。
	WatchingUserCrontab bool

	WriteEnabled         bool
	ManualTriggerEnabled bool

	Jobs []dto.CronJobDto

	Job     dto.CronJobDto
	RunList dto.JobRunListDto
	Log     dto.JobLogDto
	// LogUnavailableReason 在這個 job 根本沒有 log 可讀時說明原因。
	LogUnavailableReason string
}

// WebController 負責 HTML 頁面與供輪詢的 fragment。
//
// 頁面與 fragment 呼叫的是同一組 application 方法，只有渲染方式不同。
type WebController struct {
	cronJobApplication     *application.CronJobApplication
	jobRunApplication      *application.JobRunApplication
	crontabEditApplication *application.CrontabEditApplication

	templates map[string]*template.Template

	crontabSourceLabel   string
	timeZoneName         string
	watchingUserCrontab  bool
	manualTriggerEnabled bool
}

// NewWebController 建立 controller 並在啟動時就解析完 template ——
// template 有錯要在啟動時就炸掉，而不是等到使用者按下某個頁面才發現。
func NewWebController(
	cronJobApplication *application.CronJobApplication,
	jobRunApplication *application.JobRunApplication,
	crontabEditApplication *application.CrontabEditApplication,
	crontabSourceLabel string,
	timeZoneName string,
	watchingUserCrontab bool,
	manualTriggerEnabled bool,
) (*WebController, error) {
	templates, err := parseTemplates()
	if err != nil {
		return nil, err
	}

	return &WebController{
		cronJobApplication:     cronJobApplication,
		jobRunApplication:      jobRunApplication,
		crontabEditApplication: crontabEditApplication,
		templates:              templates,
		crontabSourceLabel:     crontabSourceLabel,
		timeZoneName:           timeZoneName,
		watchingUserCrontab:    watchingUserCrontab,
		manualTriggerEnabled:   manualTriggerEnabled,
	}, nil
}

// templateDefinitions 列出每個可渲染的 template 由哪些檔案組成。
//
// 每個頁面各自 parse 一套，而不是全部併成一棵樹：頁面之間都定義了同名的
// "content" 區塊，併在一起會互相覆蓋。
var templateDefinitions = map[string][]string{
	"jobListPage": {
		"templates/layout.gohtml",
		"templates/job_list_page.gohtml",
		"templates/job_table_fragment.gohtml",
	},
	"jobDetailPage": {
		"templates/layout.gohtml",
		"templates/job_detail_page.gohtml",
		"templates/run_table_fragment.gohtml",
		"templates/log_fragment.gohtml",
	},
	"jobTable": {"templates/job_table_fragment.gohtml"},
	"runTable": {"templates/run_table_fragment.gohtml"},
	"logView":  {"templates/log_fragment.gohtml"},
}

func parseTemplates() (map[string]*template.Template, error) {
	templates := make(map[string]*template.Template, len(templateDefinitions))

	for name, files := range templateDefinitions {
		parsed, err := template.New(name).Funcs(templateFunctions()).ParseFS(web.TemplateFileSystem, files...)
		if err != nil {
			return nil, fmt.Errorf("parsing templates for %s: %w", name, err)
		}

		templates[name] = parsed
	}

	return templates, nil
}

func templateFunctions() template.FuncMap {
	return template.FuncMap{
		"formatTime":     formatTime,
		"formatDuration": formatDuration,
		"formatExitCode": formatExitCode,
		"runStatusClass": runStatusClass,
		"runStatusTitle": runStatusTitle,
		"logSourceLabel": logSourceLabel,
		"logSourceTitle": logSourceTitle,
		"originLabel":    originLabel,
		"originTitle":    originTitle,
	}
}

// ShowJobListPage 處理 GET /。
func (controller *WebController) ShowJobListPage(context *gin.Context) {
	jobs, err := controller.cronJobApplication.ListCronJobs()
	if err != nil {
		controller.renderFailure(context, err)
		return
	}

	viewModel := controller.newViewModel("cronjobs")
	viewModel.Jobs = jobs

	controller.render(context, "jobListPage", "layout", viewModel)
}

// ShowJobTableFragment 處理 GET /fragments/jobs。
func (controller *WebController) ShowJobTableFragment(context *gin.Context) {
	jobs, err := controller.cronJobApplication.ListCronJobs()
	if err != nil {
		controller.renderFailure(context, err)
		return
	}

	viewModel := controller.newViewModel("cronjobs")
	viewModel.Jobs = jobs

	controller.render(context, "jobTable", "jobTable", viewModel)
}

// ShowJobDetailPage 處理 GET /jobs/:jobId/detail。
func (controller *WebController) ShowJobDetailPage(context *gin.Context) {
	viewModel, err := controller.buildDetailViewModel(context.Param("jobId"))
	if err != nil {
		controller.renderFailure(context, err)
		return
	}

	controller.render(context, "jobDetailPage", "layout", viewModel)
}

// ShowRunTableFragment 處理 GET /fragments/jobs/:jobId/runs。
func (controller *WebController) ShowRunTableFragment(context *gin.Context) {
	jobID := context.Param("jobId")

	runList, err := controller.jobRunApplication.ListJobRuns(jobID, 0)
	if err != nil {
		controller.renderFailure(context, err)
		return
	}

	viewModel := controller.newViewModel("runs")
	viewModel.RunList = runList

	controller.render(context, "runTable", "runTable", viewModel)
}

// ShowLogFragment 處理 GET /fragments/jobs/:jobId/log。
func (controller *WebController) ShowLogFragment(context *gin.Context) {
	viewModel := controller.newViewModel("log")

	logDto, err := controller.jobRunApplication.TailJobLog(context.Param("jobId"), 0)
	switch {
	case errors.Is(err, entity.ErrJobLogUnavailable):
		// 這不是故障，是這個 job 的性質。用說明取代錯誤畫面。
		viewModel.LogUnavailableReason = hintForError(err)
	case err != nil:
		controller.renderFailure(context, err)
		return
	default:
		viewModel.Log = logDto
	}

	controller.render(context, "logView", "logView", viewModel)
}

func (controller *WebController) buildDetailViewModel(jobID string) (pageViewModel, error) {
	job, err := controller.cronJobApplication.GetCronJob(jobID)
	if err != nil {
		return pageViewModel{}, err
	}

	runList, err := controller.jobRunApplication.ListJobRuns(jobID, 0)
	if err != nil {
		return pageViewModel{}, err
	}

	viewModel := controller.newViewModel(job.ScheduleDescription)
	viewModel.Job = job
	viewModel.RunList = runList

	logDto, err := controller.jobRunApplication.TailJobLog(jobID, 0)
	switch {
	case errors.Is(err, entity.ErrJobLogUnavailable):
		viewModel.LogUnavailableReason = hintForError(err)
	case err != nil:
		return pageViewModel{}, err
	default:
		viewModel.Log = logDto
	}

	return viewModel, nil
}

func (controller *WebController) newViewModel(pageTitle string) pageViewModel {
	return pageViewModel{
		PageTitle:            pageTitle,
		CrontabSourceLabel:   controller.crontabSourceLabel,
		TimeZoneName:         controller.timeZoneName,
		WatchingUserCrontab:  controller.watchingUserCrontab,
		WriteEnabled:         controller.crontabEditApplication.WriteEnabled(),
		ManualTriggerEnabled: controller.manualTriggerEnabled,
	}
}

func (controller *WebController) render(
	context *gin.Context,
	templateName string,
	definitionName string,
	viewModel pageViewModel,
) {
	parsed, found := controller.templates[templateName]
	if !found {
		controller.renderFailure(context, fmt.Errorf("template %s is not registered", templateName))
		return
	}

	context.Header("Content-Type", "text/html; charset=utf-8")
	context.Status(http.StatusOK)

	if err := parsed.ExecuteTemplate(context.Writer, definitionName, viewModel); err != nil {
		// 標頭已經送出，無法改狀態碼。至少讓錯誤出現在畫面上而不是留下截斷的頁面。
		_, _ = context.Writer.WriteString("<p>render failed: " + template.HTMLEscapeString(err.Error()) + "</p>")
	}
}

// renderFailure 以純文字回報頁面層級的失敗。
//
// 刻意不做華麗的錯誤頁：出錯時最有用的是狀態碼與原文訊息，而一個需要載入資源的
// 錯誤頁在「檔案系統壞了」這種情境下自己也可能顯示不出來。
func (controller *WebController) renderFailure(context *gin.Context, err error) {
	context.Data(statusCodeForError(err), "text/plain; charset=utf-8",
		[]byte(err.Error()+"\n\n"+hintForError(err)))
}

func formatTime(value any) string {
	switch typedValue := value.(type) {
	case time.Time:
		if typedValue.IsZero() {
			return "—"
		}
		return typedValue.Format(displayTimeLayout)
	case *time.Time:
		if typedValue == nil || typedValue.IsZero() {
			return "—"
		}
		return typedValue.Format(displayTimeLayout)
	default:
		return "—"
	}
}

func formatDuration(milliseconds *int64) string {
	if milliseconds == nil {
		return "—"
	}

	return (time.Duration(*milliseconds) * time.Millisecond).String()
}

// formatExitCode 把未知的 exit code 顯示為破折號，絕不顯示 0。
//
// 顯示 0 會被讀成「成功」，那是我們並不知道的事。
func formatExitCode(exitCode *int) string {
	if exitCode == nil {
		return "—"
	}

	return fmt.Sprintf("%d", *exitCode)
}

func runStatusClass(latestRun *dto.JobRunDto) string {
	if latestRun == nil {
		return "none"
	}

	return latestRun.RunStatus
}

func runStatusTitle(latestRun *dto.JobRunDto) string {
	if latestRun == nil {
		return "沒有執行紀錄"
	}

	switch entity.NewRunStatus(latestRun.RunStatus) {
	case entity.RunStatusSucceeded:
		return "最近一次執行成功"
	case entity.RunStatusFailed:
		return "最近一次執行失敗"
	case entity.RunStatusTimedOut:
		return "最近一次執行逾時被中止"
	case entity.RunStatusRunning:
		return "正在執行"
	default:
		return "最近一次執行結果無法判定"
	}
}

func logSourceLabel(logSource string) string {
	switch entity.NewLogSource(logSource) {
	case entity.LogSourceManaged:
		return "納管"
	case entity.LogSourceRedirect:
		return "redirect"
	default:
		return "無"
	}
}

func logSourceTitle(logSource string) string {
	switch entity.NewLogSource(logSource) {
	case entity.LogSourceManaged:
		return "輸出由 crontab-watcher 的 wrapper 記錄，含 exit code 與逐次執行紀錄"
	case entity.LogSourceRedirect:
		return "輸出由指令自己的 redirect 導向某個檔案；只能看到原始內容，沒有 exit code"
	default:
		return "輸出沒有被導向任何檔案，或被丟到 /dev/null —— 無從得知這個 job 的結果"
	}
}

func originLabel(origin string) string {
	if entity.NewJobOrigin(origin) == entity.JobOriginManaged {
		return "納管"
	}

	return "手寫"
}

func originTitle(origin string) string {
	if entity.NewJobOrigin(origin) == entity.JobOriginManaged {
		return "由 crontab-watcher 管理：執行經過 wrapper，有完整紀錄"
	}

	return "使用者手寫的條目：cron 觸發時不經過 wrapper，沒有 exit code"
}
