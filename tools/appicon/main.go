// Command appicon 產生 macOS app 圖示的 iconset。
//
// 為什麼自己畫而不是放一張 PNG 進 repo：這個專案刻意不帶二進位資產（templates
// 與 CSS 都是文字），而一張圖示是純幾何——一個圓角方塊加上一個時鐘。
//
// 時鐘的形狀定義在 internal/glyph，與選單列圖示共用同一份：兩邊各畫一份，遲早會
// 在某次調整後長得不一樣。
//
// 用法：go run ./tools/appicon <輸出的 .iconset 目錄>
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"github.com/james-hsueh/crontab-watcher/internal/glyph"
)

// iconSizes 是 iconutil 要求的完整尺寸清單。少一個它就會拒絕整份 iconset。
var iconSizes = []struct {
	fileName   string
	pixelWidth int
}{
	{"icon_16x16.png", 16},
	{"icon_16x16@2x.png", 32},
	{"icon_32x32.png", 32},
	{"icon_32x32@2x.png", 64},
	{"icon_128x128.png", 128},
	{"icon_128x128@2x.png", 256},
	{"icon_256x256.png", 256},
	{"icon_256x256@2x.png", 512},
	{"icon_512x512.png", 512},
	{"icon_512x512@2x.png", 1024},
}

// 配色。深靛藍底配白色時鐘，在淺色與深色的 Dock、Finder 上都看得清楚。
var (
	backgroundTopColor    = color.NRGBA{R: 0x3B, G: 0x4A, B: 0x7A, A: 0xFF}
	backgroundBottomColor = color.NRGBA{R: 0x1E, G: 0x26, B: 0x45, A: 0xFF}
	glyphColor            = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
)

const (
	// squircleScale 是圓角方塊佔整格的比例。macOS 的圖示不佔滿整格：留白讓它在
	// Dock 裡與其他 app 對齊。
	squircleScale = 0.80
	// glyphScale 是時鐘佔整格的比例，留給圓角方塊足夠的邊。
	glyphScale = 0.54
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: appicon <output.iconset>")
		os.Exit(2)
	}

	outputDirectory := os.Args[1]
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "appicon: %v\n", err)
		os.Exit(1)
	}

	for _, iconSize := range iconSizes {
		if err := writeIcon(filepath.Join(outputDirectory, iconSize.fileName), iconSize.pixelWidth); err != nil {
			fmt.Fprintf(os.Stderr, "appicon: %v\n", err)
			os.Exit(1)
		}
	}
}

func writeIcon(filePath string, pixelWidth int) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	return png.Encode(file, renderIcon(pixelWidth))
}

// renderIcon 畫出一個尺寸的圖示：圓角方塊在下，時鐘疊在上面。
func renderIcon(pixelWidth int) image.Image {
	canvas := image.NewNRGBA(image.Rect(0, 0, pixelWidth, pixelWidth))

	drawSquircle(canvas, pixelWidth)

	glyphInset := int(math.Round(float64(pixelWidth) * (1 - glyphScale) / 2))
	glyph.DrawClockArrow(canvas,
		image.Rect(glyphInset, glyphInset, pixelWidth-glyphInset, pixelWidth-glyphInset),
		glyphColor)

	return canvas
}

// drawSquircle 畫出帶漸層的圓角方塊底。
//
// 形狀以「帶號距離」（負值在形狀內、正值在形狀外）描述，再把距離換算成覆蓋率當作
// alpha。這樣就有了平滑的邊緣，不需要超取樣，也不需要繪圖函式庫。
func drawSquircle(canvas *image.NRGBA, pixelWidth int) {
	size := float64(pixelWidth)
	halfWidth := size * squircleScale / 2
	cornerRadius := halfWidth * 0.45
	centerX, centerY := size/2, size/2

	for y := 0; y < pixelWidth; y++ {
		for x := 0; x < pixelWidth; x++ {
			pointX, pointY := float64(x)+0.5, float64(y)+0.5

			coverage := coverageOf(roundedSquareDistance(
				pointX-centerX, pointY-centerY, halfWidth, cornerRadius))
			if coverage <= 0 {
				continue
			}

			pixel := blendVertically(backgroundTopColor, backgroundBottomColor, pointY/size)
			pixel.A = uint8(float64(pixel.A) * coverage)

			canvas.SetNRGBA(x, y, pixel)
		}
	}
}

// roundedSquareDistance 是圓角正方形的帶號距離。
func roundedSquareDistance(offsetX float64, offsetY float64, halfWidth float64, cornerRadius float64) float64 {
	insetX := math.Abs(offsetX) - (halfWidth - cornerRadius)
	insetY := math.Abs(offsetY) - (halfWidth - cornerRadius)

	outsideX := math.Max(insetX, 0)
	outsideY := math.Max(insetY, 0)

	return math.Hypot(outsideX, outsideY) + math.Min(math.Max(insetX, insetY), 0) - cornerRadius
}

// coverageOf 把帶號距離換算成 0..1 的覆蓋率，邊界一個像素內線性過渡。
func coverageOf(signedDistance float64) float64 {
	return math.Min(math.Max(0.5-signedDistance, 0), 1)
}

func blendVertically(topColor color.NRGBA, bottomColor color.NRGBA, ratio float64) color.NRGBA {
	return color.NRGBA{
		R: interpolate(topColor.R, bottomColor.R, ratio),
		G: interpolate(topColor.G, bottomColor.G, ratio),
		B: interpolate(topColor.B, bottomColor.B, ratio),
		A: 0xFF,
	}
}

func interpolate(from uint8, to uint8, ratio float64) uint8 {
	return uint8(float64(from) + (float64(to)-float64(from))*ratio)
}
