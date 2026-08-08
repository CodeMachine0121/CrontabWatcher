// Command appicon 產生 macOS app 圖示的 iconset。
//
// 為什麼自己畫而不是放一張 PNG 進 repo：這個專案刻意不帶二進位資產（templates
// 與 CSS 都是文字），而一張圖示是純幾何——一個圓角方塊、一個錶面、兩根指針。
// 用程式描述它，改一個顏色是改一行，而不是重新開一次繪圖軟體。
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

// 配色。深靛藍底配白色錶面，在淺色與深色的 Dock、Finder 上都看得清楚。
var (
	backgroundTopColor    = color.NRGBA{R: 0x3B, G: 0x4A, B: 0x7A, A: 0xFF}
	backgroundBottomColor = color.NRGBA{R: 0x1E, G: 0x26, B: 0x45, A: 0xFF}
	faceColor             = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
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

// renderIcon 畫出一個尺寸的圖示。
//
// 每個形狀都以「帶號距離」（負值在形狀內、正值在形狀外）描述，再把距離換算成
// 覆蓋率當作 alpha。這樣就有了平滑的邊緣，不需要超取樣，也不需要繪圖函式庫。
func renderIcon(pixelWidth int) image.Image {
	canvas := image.NewNRGBA(image.Rect(0, 0, pixelWidth, pixelWidth))

	size := float64(pixelWidth)
	// macOS 的圖示不佔滿整格：留白讓它在 Dock 裡與其他 app 對齊。
	squircleHalfWidth := size * 0.40
	cornerRadius := squircleHalfWidth * 0.45
	centerX, centerY := size/2, size/2

	faceRadius := squircleHalfWidth * 0.62
	ringHalfWidth := math.Max(size*0.018, 0.6)

	for y := 0; y < pixelWidth; y++ {
		for x := 0; x < pixelWidth; x++ {
			pointX, pointY := float64(x)+0.5, float64(y)+0.5

			squircleCoverage := coverageOf(roundedSquareDistance(
				pointX-centerX, pointY-centerY, squircleHalfWidth, cornerRadius))
			if squircleCoverage <= 0 {
				continue
			}

			pixel := blendVertically(backgroundTopColor, backgroundBottomColor, pointY/size)
			pixel.A = uint8(float64(pixel.A) * squircleCoverage)

			faceCoverage := math.Max(
				coverageOf(math.Abs(distance(pointX-centerX, pointY-centerY)-faceRadius)-ringHalfWidth),
				handsCoverage(pointX-centerX, pointY-centerY, faceRadius, ringHalfWidth))

			if faceCoverage > 0 {
				pixel = blendOver(pixel, faceColor, faceCoverage*squircleCoverage)
			}

			canvas.SetNRGBA(x, y, pixel)
		}
	}

	return canvas
}

// handsCoverage 畫時針、分針與軸心。
//
// 兩根指針刻意成直角且長短分明：對稱的擺法在 16 像素下會讀成一個勾，而不是一個
// 錶面。直角是「這是時鐘」最短的視覺提示。
func handsCoverage(offsetX float64, offsetY float64, faceRadius float64, ringHalfWidth float64) float64 {
	const (
		hourAngle    = -math.Pi / 2 // 十二點鐘方向
		minuteAngle  = 0.0          // 三點鐘方向
		hourLength   = 0.46
		minuteLength = 0.70
	)

	hourDistance := segmentDistance(offsetX, offsetY,
		faceRadius*hourLength*math.Cos(hourAngle), faceRadius*hourLength*math.Sin(hourAngle))
	minuteDistance := segmentDistance(offsetX, offsetY,
		faceRadius*minuteLength*math.Cos(minuteAngle), faceRadius*minuteLength*math.Sin(minuteAngle))

	// 軸心點讓兩根指針看起來是從同一個地方長出來的，而不是兩條剛好相交的線。
	pivotDistance := distance(offsetX, offsetY) - ringHalfWidth*1.6

	return math.Max(
		math.Max(coverageOf(hourDistance-ringHalfWidth), coverageOf(minuteDistance-ringHalfWidth)),
		coverageOf(pivotDistance))
}

// roundedSquareDistance 是圓角正方形的帶號距離。
func roundedSquareDistance(offsetX float64, offsetY float64, halfWidth float64, cornerRadius float64) float64 {
	insetX := math.Abs(offsetX) - (halfWidth - cornerRadius)
	insetY := math.Abs(offsetY) - (halfWidth - cornerRadius)

	outsideX := math.Max(insetX, 0)
	outsideY := math.Max(insetY, 0)

	return distance(outsideX, outsideY) + math.Min(math.Max(insetX, insetY), 0) - cornerRadius
}

// segmentDistance 回傳一點到「原點到 (endX, endY) 這條線段」的距離。
func segmentDistance(pointX float64, pointY float64, endX float64, endY float64) float64 {
	segmentLengthSquared := endX*endX + endY*endY
	if segmentLengthSquared == 0 {
		return distance(pointX, pointY)
	}

	projection := (pointX*endX + pointY*endY) / segmentLengthSquared
	projection = math.Min(math.Max(projection, 0), 1)

	return distance(pointX-projection*endX, pointY-projection*endY)
}

func distance(offsetX float64, offsetY float64) float64 {
	return math.Hypot(offsetX, offsetY)
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

func blendOver(baseColor color.NRGBA, overlayColor color.NRGBA, overlayAlpha float64) color.NRGBA {
	return color.NRGBA{
		R: interpolate(baseColor.R, overlayColor.R, overlayAlpha),
		G: interpolate(baseColor.G, overlayColor.G, overlayAlpha),
		B: interpolate(baseColor.B, overlayColor.B, overlayAlpha),
		A: baseColor.A,
	}
}

func interpolate(from uint8, to uint8, ratio float64) uint8 {
	return uint8(float64(from) + (float64(to)-float64(from))*ratio)
}
