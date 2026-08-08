package controller_test

import (
	"bytes"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/controller"
)

// 選單列圖示是一張 template 圖示：macOS 只看 alpha，自己決定要畫黑還是白。因此
// 「有透明的地方」與「有不透明的地方」兩者都必須成立 —— 全不透明會變成一個實心
// 方塊，全透明則什麼都看不到。
func TestMenuBarIconIsATemplateImage(t *testing.T) {
	iconBytes := controller.MenuBarIconPNG()
	require.NotEmpty(t, iconBytes, "without an icon the menu bar falls back to text")

	decoded, err := png.Decode(bytes.NewReader(iconBytes))
	require.NoError(t, err, "systray is handed these bytes as a PNG")

	bounds := decoded.Bounds()
	assert.Equal(t, bounds.Dx(), bounds.Dy(), "a menu bar icon is square")

	transparent, opaque := 0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := decoded.At(x, y).RGBA()
			switch {
			case alpha == 0:
				transparent++
			case alpha == 0xFFFF:
				opaque++
			}
		}
	}

	assert.Greater(t, transparent, 0, "a fully opaque icon would render as a solid block")
	assert.Greater(t, opaque, 0, "a fully transparent icon would be invisible")
}
