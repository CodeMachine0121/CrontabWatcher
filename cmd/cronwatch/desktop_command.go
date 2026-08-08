package main

import (
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/james-hsueh/crontab-watcher/internal/controller"
	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/desktop"
	"github.com/james-hsueh/crontab-watcher/internal/infrastructure/system"
)

// runDesktopCommand 啟動桌面形態：進駐選單列，並在背後跑一個只有這台機器看得到
// 的服務供完整視窗載入。
//
// 檢查的順序是刻意的：先確認平台支援、再搶單一實例鎖，最後才開埠。反過來的話，
// 第二次啟動會先佔住一個埠、開始改動狀態，然後才發現自己不該存在。
func runDesktopCommand() int {
	configuration := applyDesktopDefaults(loadServerConfiguration())

	for _, warning := range configuration.Warnings {
		log.Printf("configuration: %s", warning)
	}

	menuBarSupport := controller.NewMenuBarController(nil, nil, "", 0, 0)
	if err := menuBarSupport.Supported(); err != nil {
		log.Printf("%v", err)

		return 1
	}

	if err := prepareDesktopStateDirectories(configuration); err != nil {
		log.Printf("could not prepare the state directories: %v", err)

		return 1
	}

	instanceLock, err := system.AcquireInstanceLock(configuration.DesktopLockFilePath)
	if err != nil {
		if errors.Is(err, system.ErrInstanceAlreadyRunning) {
			log.Printf("cronwatch is already running in the menu bar; look for %q up there",
				"cw")

			return 1
		}

		log.Printf("could not take the single instance lock: %v", err)

		return 1
	}
	defer instanceLock.Release()

	applications := buildApplicationSet(configuration)
	reconcileInterruptedRuns(applications, time.Now().In(configuration.Location))

	router, err := buildRouter(configuration, applications)
	if err != nil {
		log.Printf("could not start: %v", err)

		return 1
	}

	// 綁 loopback 上的臨時埠，並問出系統實際配了哪一個 —— 組態上那個 ":0"
	// 不是真的埠號，而完整視窗要靠它才連得上。
	listener, err := net.Listen("tcp", configuration.ServerAddress)
	if err != nil {
		log.Printf("could not listen on %s: %v", configuration.ServerAddress, err)

		return 1
	}
	defer func() { _ = listener.Close() }()

	go func() {
		if err := http.Serve(listener, router); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server stopped: %v", err)
		}
	}()

	baseURL := "http://" + listener.Addr().String()

	log.Printf("watching %s", configuration.CrontabSourceDescription())
	log.Printf("run records in %s, managed logs in %s",
		configuration.RunRecordFilePath, configuration.RunLogDirectory)
	log.Printf("the window loads %s and nothing outside this machine can reach it", baseURL)
	log.Printf("refreshing every %s; closing cronwatch does not stop any job",
		configuration.DesktopRefreshInterval)

	windowProxy := desktop.NewDesktopWindowProxy(resolveOwnExecutablePath())
	defer windowProxy.Close()

	menuBarController := controller.NewMenuBarController(
		applications.desktopApplication,
		windowProxy,
		baseURL,
		configuration.DesktopRefreshInterval,
		configuration.DesktopSummaryLineLimit,
	)

	// 選單列必須從主執行緒跑：macOS 的選單列綁在主 run loop 上。
	menuBarController.Run()

	return 0
}

// prepareDesktopStateDirectories 先把要寫入的目錄建好。
//
// 桌面形態沒有 entrypoint 腳本可以代勞，而使用者是雙擊啟動的 —— 因為一個不存在
// 的目錄就啟動失敗，是最沒必要的失敗。
func prepareDesktopStateDirectories(configuration ServerConfiguration) error {
	directories := []string{
		configuration.RunLogDirectory,
		configuration.CrontabBackupDirectory,
		filepath.Dir(configuration.RunRecordFilePath),
		filepath.Dir(configuration.DesktopLockFilePath),
	}

	for _, directory := range directories {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}

	return nil
}

// resolveOwnExecutablePath 找出自己的執行檔位置，視窗子程序就是它。
func resolveOwnExecutablePath() string {
	executablePath, err := os.Executable()
	if err != nil {
		return "cronwatch"
	}

	absolutePath, err := filepath.Abs(executablePath)
	if err != nil {
		return executablePath
	}

	return absolutePath
}
