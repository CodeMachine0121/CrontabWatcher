package main

import "strings"

// appBundleExecutableMarker 是 macOS app bundle 內執行檔的固定位置。
const appBundleExecutableMarker = ".app/Contents/MacOS/"

// processSerialNumberPrefix 是 Finder 有時會塞給 app 的參數。它不是子命令。
const processSerialNumberPrefix = "-psn_"

// resolveCommand 決定這次要跑哪一個子命令。
//
// 被包成 app 從 Finder 雙擊時，命令列上什麼都沒有，而那時使用者要的顯然是選單列
// 而不是一個沒有畫面的 web server。與其讓使用者去猜為什麼雙擊沒反應，不如讓
// binary 認得自己現在的身分：**在 app bundle 裡，預設就是桌面模式**。
//
// 從終端機跑時預設仍是 serve，維持既有行為不變。明確給了子命令時一律以它為準，
// 兩種情況都一樣。
func resolveCommand(arguments []string, executablePath string) string {
	for _, argument := range arguments {
		if strings.HasPrefix(argument, processSerialNumberPrefix) {
			continue
		}

		return argument
	}

	if strings.Contains(executablePath, appBundleExecutableMarker) {
		return "desktop"
	}

	return "serve"
}

// commandArguments 回傳子命令之後的參數，並略過 Finder 塞進來的那一個。
func commandArguments(arguments []string) []string {
	for index, argument := range arguments {
		if strings.HasPrefix(argument, processSerialNumberPrefix) {
			continue
		}

		return arguments[index+1:]
	}

	return nil
}
