package main

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/james-hsueh/crontab-watcher/internal/application"
	"github.com/james-hsueh/crontab-watcher/internal/controller"
	interfaces "github.com/james-hsueh/crontab-watcher/internal/domain/interface"
	"github.com/james-hsueh/crontab-watcher/internal/domain/service"
	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/crontab"
	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/runlog"
	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/shell"
	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/system"
	"github.com/james-hsueh/crontab-watcher/internal/web"
)

// applicationSet 是組好的用例集合。手動 DI，沒有容器 —— 依賴關係看得見比較重要。
type applicationSet struct {
	cronJobApplication       *application.CronJobApplication
	jobRunApplication        *application.JobRunApplication
	manualTriggerApplication *application.ManualTriggerApplication
	crontabEditApplication   *application.CrontabEditApplication
	jobRunRepository         *runlog.JobRunRepository
}

// buildCrontabDocumentRepository 依組態挑一個 crontab 存取實作。
//
// 這是整個介面設計的回報：service 與 domain 完全不知道 crontab 是從檔案來的還是從
// crontab 命令來的。
func buildCrontabDocumentRepository(configuration ServerConfiguration) interfaces.ICrontabDocumentRepository {
	if configuration.CrontabSource == CrontabSourceCommand {
		return crontab.NewCrontabCommandRepository(
			configuration.CrontabCommandPath, configuration.CrontabBackupDirectory)
	}

	return crontab.NewCrontabDocumentRepository(
		configuration.CrontabFilePath, configuration.CrontabBackupDirectory)
}

// buildApplicationSet 由組態組出全部依賴。
func buildApplicationSet(configuration ServerConfiguration) applicationSet {
	crontabDocumentRepository := buildCrontabDocumentRepository(configuration)
	jobRunRepository := runlog.NewJobRunRepository(
		configuration.RunRecordFilePath, configuration.RunRecordRetentionCount)
	jobLogRepository := runlog.NewJobLogRepository()
	commandExecutionProxy := shell.NewCommandExecutionProxy(configuration.ShellPath)
	identifierGenerator := system.NewIdentifierGenerator()
	clock := system.NewClock(configuration.Location)

	cronJobService := service.NewCronJobService(
		crontabDocumentRepository, jobRunRepository, configuration.RunLogDirectory)
	jobRunService := service.NewJobRunService(
		crontabDocumentRepository, jobRunRepository, jobLogRepository, configuration.RunLogDirectory)
	jobExecutionService := service.NewJobExecutionService(
		crontabDocumentRepository, jobRunRepository, jobLogRepository,
		commandExecutionProxy, identifierGenerator, clock, configuration.RunLogDirectory)
	crontabEditService := service.NewCrontabEditService(
		crontabDocumentRepository, identifierGenerator,
		configuration.WrapperBinaryPath, configuration.RunLogDirectory)

	return applicationSet{
		cronJobApplication: application.NewCronJobApplication(cronJobService, clock),
		jobRunApplication:  application.NewJobRunApplication(jobRunService, configuration.LogTailLines),
		manualTriggerApplication: application.NewManualTriggerApplication(
			jobExecutionService, configuration.ManualTriggerEnabled, configuration.ManualTriggerTimeout),
		crontabEditApplication: application.NewCrontabEditApplication(
			crontabEditService, clock, configuration.CrontabWriteEnabled),
		jobRunRepository: jobRunRepository,
	}
}

// buildRouter 組出 gin engine 並註冊路由。
func buildRouter(configuration ServerConfiguration, applications applicationSet) (*gin.Engine, error) {
	cronJobController := controller.NewCronJobController(
		applications.cronJobApplication,
		applications.jobRunApplication,
		applications.manualTriggerApplication,
		applications.crontabEditApplication,
	)

	webController, err := controller.NewWebController(
		applications.cronJobApplication,
		applications.jobRunApplication,
		applications.crontabEditApplication,
		configuration.CrontabSourceDescription(),
		configuration.Location.String(),
		configuration.ManualTriggerEnabled,
	)
	if err != nil {
		return nil, err
	}

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	registerRoutes(router, cronJobController, webController)

	return router, nil
}

func registerRoutes(
	router *gin.Engine,
	cronJobController *controller.CronJobController,
	webController *controller.WebController,
) {
	router.GET("/health", cronJobController.Health)

	// 頁面與供輪詢的 fragment
	router.GET("/", webController.ShowJobListPage)
	router.GET("/jobs/:jobId/detail", webController.ShowJobDetailPage)
	router.GET("/fragments/jobs", webController.ShowJobTableFragment)
	router.GET("/fragments/jobs/:jobId/runs", webController.ShowRunTableFragment)
	router.GET("/fragments/jobs/:jobId/log", webController.ShowLogFragment)

	staticFileSystem, err := fs.Sub(web.StaticFileSystem, "static")
	if err != nil {
		// embed 的內容在編譯期就固定了，這裡出錯代表程式本身壞了。
		panic(fmt.Sprintf("static assets are not embedded correctly: %v", err))
	}
	router.StaticFS("/static", http.FS(staticFileSystem))

	// JSON API
	router.GET("/jobs", cronJobController.ListCronJobs)
	router.GET("/jobs/:jobId", cronJobController.GetCronJob)
	router.GET("/jobs/:jobId/runs", cronJobController.ListJobRuns)
	router.GET("/jobs/:jobId/log", cronJobController.TailJobLog)
	router.POST("/jobs/:jobId/run", cronJobController.TriggerJobRun)

	router.POST("/jobs", cronJobController.CreateCronJob)
	router.PUT("/jobs/:jobId", cronJobController.UpdateCronJob)
	router.DELETE("/jobs/:jobId", cronJobController.DeleteCronJob)
	router.POST("/jobs/:jobId/enable", cronJobController.EnableCronJob)
	router.POST("/jobs/:jobId/disable", cronJobController.DisableCronJob)
	router.POST("/jobs/:jobId/adopt", cronJobController.AdoptCronJob)

	router.GET("/crontab", cronJobController.GetCrontabContent)
}

// reconcileInterruptedRuns 把上次程序被砍時殘留的 running 紀錄掃成無法判定。
//
// 留著它們假裝還在跑，會讓「這個 job 卡住了嗎」永遠問不出答案，也會讓並發檢查
// 永久擋住該 job 的手動觸發。
func reconcileInterruptedRuns(applications applicationSet, now time.Time) {
	interruptedCount, err := applications.jobRunRepository.MarkRunningRunsAsInterrupted(now)
	if err != nil {
		log.Printf("could not reconcile interrupted runs: %v", err)
		return
	}

	if interruptedCount > 0 {
		log.Printf("marked %d run(s) left behind by a previous shutdown as unknown", interruptedCount)
	}
}
