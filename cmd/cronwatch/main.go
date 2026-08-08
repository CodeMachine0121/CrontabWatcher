package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

const mainUsage = `usage: cronwatch <command>

commands:
  serve                                    start the web server (default)
  desktop                                  live in the macOS menu bar (macOS only)
  run --job=<jobId> -- <command...>        run a command and record the run
  window --url=<address>                   open the standalone window (started by desktop)

environment variables are documented in CLAUDE.md; all of them have defaults.`

func main() {
	// .env 不存在是完全正常的 —— 所有變數都有預設值。
	_ = godotenv.Load()

	arguments := os.Args[1:]
	command := "serve"
	if len(arguments) > 0 {
		command = arguments[0]
	}

	switch command {
	case "serve":
		os.Exit(runServeCommand())
	case "desktop":
		os.Exit(runDesktopCommand())
	case "run":
		os.Exit(runWrapperCommand(arguments[1:]))
	case "window":
		os.Exit(runWindowCommand(arguments[1:]))
	case "-h", "--help", "help":
		fmt.Println(mainUsage)
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "cronwatch: unknown command %q\n\n%s\n", command, mainUsage)
		os.Exit(usageExitCode)
	}
}

func runServeCommand() int {
	configuration := loadServerConfiguration()

	for _, warning := range configuration.Warnings {
		log.Printf("configuration: %s", warning)
	}

	applications := buildApplicationSet(configuration)
	reconcileInterruptedRuns(applications, time.Now().In(configuration.Location))

	router, err := buildRouter(configuration, applications)
	if err != nil {
		log.Printf("could not start: %v", err)
		return 1
	}

	log.Printf("watching %s", configuration.CrontabSourceDescription())
	if configuration.UsesUserCrontab() && configuration.CrontabWriteEnabled {
		// 這條路徑會用 `crontab <file>` 整份取代使用者真正的 crontab。說清楚，並
		// 指出備份在哪，以及怎麼關掉寫入。
		log.Printf("WRITES GO TO YOUR REAL CRONTAB: edits replace it via %s <file>. "+
			"Backups of every previous version are kept in %s. "+
			"Set CRONTAB_WRITE_ENABLED=false to browse without writing.",
			configuration.CrontabCommandPath, configuration.CrontabBackupDirectory)
	}
	log.Printf("run records in %s, managed logs in %s",
		configuration.RunRecordFilePath, configuration.RunLogDirectory)
	log.Printf("crontab writing %s, manual triggering %s",
		enabledLabel(configuration.CrontabWriteEnabled), enabledLabel(configuration.ManualTriggerEnabled))
	log.Printf("listening on http://%s (timezone %s)", configuration.ServerAddress, configuration.Location)

	if err := http.ListenAndServe(configuration.ServerAddress, router); err != nil &&
		!errors.Is(err, http.ErrServerClosed) {
		log.Printf("server stopped: %v", err)
		return 1
	}

	return 0
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}

	return "disabled"
}
