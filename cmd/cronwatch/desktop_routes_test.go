package main

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 桌面模式的完整視窗載入的就是既有那套介面，所以「能做的事一樣多」是靠**同一個
// router** 成立的，而不是靠另寫一份。這個測試把那個結構事實釘住：有人日後拿掉
// 或改名任何一條路由，桌面模式就會少一項能力，而這裡會擋下來。
func TestDesktopModeServesEveryOperationTheWebInterfaceHas(t *testing.T) {
	gin.SetMode(gin.TestMode)

	configuration := applyDesktopDefaults(loadServerConfiguration())

	router, err := buildRouter(configuration, buildApplicationSet(configuration))
	require.NoError(t, err)

	registeredRoutes := map[string]bool{}
	for _, route := range router.Routes() {
		registeredRoutes[route.Method+" "+route.Path] = true
	}

	expectedRoutes := []string{
		"GET /",                     // 清單
		"GET /jobs/:jobId/detail",   // 詳情
		"GET /jobs/:jobId/runs",     // 執行歷史
		"GET /jobs/:jobId/log",      // 輸出
		"POST /jobs/:jobId/run",     // 手動觸發
		"POST /jobs",                // 新增
		"PUT /jobs/:jobId",          // 編輯
		"DELETE /jobs/:jobId",       // 刪除
		"POST /jobs/:jobId/enable",  // 啟用
		"POST /jobs/:jobId/disable", // 停用
		"POST /jobs/:jobId/adopt",   // 轉為納管
	}

	for _, expectedRoute := range expectedRoutes {
		assert.True(t, registeredRoutes[expectedRoute],
			"the desktop window loses an ability if %q is gone", expectedRoute)
	}
}
