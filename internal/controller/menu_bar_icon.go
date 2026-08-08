package controller

import (
	"bytes"
	"image"
	"image/color"
	"image/png"

	"github.com/james-hsueh/crontab-watcher/internal/glyph"
)

// menuBarIconPixelWidth 是選單列圖示的像素寬度。
//
// macOS 會把它縮成 16 點呈現，所以 32 恰好是 Retina 上的一比一。給更大的尺寸只
// 會多一次非整數倍的縮放，反而模糊。
const menuBarIconPixelWidth = 32

// MenuBarIconPNG 畫出選單列圖示，回傳 PNG 位元組。
//
// 它是一張 **template 圖示**：只有 alpha 有意義，macOS 會自己上色——淺色選單列
// 用黑、深色用白、選單展開時反白。這是選單列圖示唯一正確的做法，寫死顏色的圖示
// 一換佈景就看不見了。
//
// 圖示在執行期畫出來而不是放一個檔案進 repo：這個專案的資產一向是文字，而這個
// 形狀與 app 圖示共用同一份定義（`internal/glyph`），兩邊不會走鐘。
//
// 畫不出來時回傳 nil。呼叫方必須據此退回文字，否則會得到一個看不見的選單列項目。
func MenuBarIconPNG() []byte {
	canvas := image.NewNRGBA(image.Rect(0, 0, menuBarIconPixelWidth, menuBarIconPixelWidth))

	glyph.DrawClockArrow(canvas, canvas.Bounds(), color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF})

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		return nil
	}

	return encoded.Bytes()
}
