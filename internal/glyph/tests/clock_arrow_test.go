package glyph_test

import (
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/james-hsueh/crontab-watcher/internal/glyph"
)

var whiteInk = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}

func drawOnTransparent(t *testing.T, pixelWidth int) *image.NRGBA {
	t.Helper()

	canvas := image.NewNRGBA(image.Rect(0, 0, pixelWidth, pixelWidth))
	glyph.DrawClockArrow(canvas, canvas.Bounds(), whiteInk)

	return canvas
}

// 畫在全透明的畫布上時，覆蓋率必須變成 alpha —— 選單列的 template 圖示只看 alpha，
// 若把形狀畫成「不透明的白」，macOS 會得到一個實心方塊。
func TestDrawClockArrowLeavesEverythingItDoesNotCoverTransparent(t *testing.T) {
	canvas := drawOnTransparent(t, 64)

	for _, corner := range []image.Point{{X: 0, Y: 0}, {X: 63, Y: 0}, {X: 0, Y: 63}, {X: 63, Y: 63}} {
		assert.Zero(t, canvas.NRGBAAt(corner.X, corner.Y).A,
			"a template icon must be transparent where the shape is not")
	}

	assert.Equal(t, uint8(0xFF), canvas.NRGBAAt(32, 32).A, "the pivot at the centre is solid")
}

// 環必須有缺口，而缺口必須是真的空的。一個閉合的圓只是時鐘；開口加箭頭才是
// 「它會再回來」。
func TestDrawClockArrowLeavesAGapInTheRing(t *testing.T) {
	const pixelWidth = 256

	canvas := drawOnTransparent(t, pixelWidth)
	center := float64(pixelWidth) / 2

	// 缺口中沒有被箭頭佔用的那一段，必須完全空白。
	for degrees := -36.0; degrees <= -22.0; degrees += 0.5 {
		angle := degrees * math.Pi / 180
		x := int(center + 0.70*center*math.Cos(angle))
		y := int(center + 0.70*center*math.Sin(angle))

		require.Zero(t, canvas.NRGBAAt(x, y).A,
			"the ring must be open at %.1f degrees", degrees)
	}

	// 環的其餘部分則必須是實的，否則那不是一個環。
	for degrees := 30.0; degrees <= 260.0; degrees += 5 {
		angle := degrees * math.Pi / 180
		x := int(center + 0.70*center*math.Cos(angle))
		y := int(center + 0.70*center*math.Sin(angle))

		require.Equal(t, uint8(0xFF), canvas.NRGBAAt(x, y).A,
			"the ring must be solid at %.1f degrees", degrees)
	}
}

// 半透明的像素應該只出現在邊緣。這條斷言擋的是幾何算錯時常見的後果：距離被低估，
// 於是形狀的延長線上留下一整片若隱若現的雜點。
func TestDrawClockArrowOnlyFeathersItsEdges(t *testing.T) {
	const pixelWidth = 256

	canvas := drawOnTransparent(t, pixelWidth)

	partiallyCovered := 0
	for y := 0; y < pixelWidth; y++ {
		for x := 0; x < pixelWidth; x++ {
			if alpha := canvas.NRGBAAt(x, y).A; alpha > 8 && alpha < 248 {
				partiallyCovered++
			}
		}
	}

	// 一個像素寬的邊緣，周長約一千五百像素。給一倍餘裕；雜點會遠遠超過。
	assert.Less(t, partiallyCovered, 3000,
		"only edges may be partially covered; more than that means stray ink")
	assert.Greater(t, partiallyCovered, 500, "edges must be antialiased at all")
}

// 疊在不透明的底色上時，要混色而不是把底色的 alpha 蓋掉 —— app 圖示就是這樣把
// 白色時鐘畫在深色圓角方塊上的。
func TestDrawClockArrowBlendsOntoAnOpaqueBackground(t *testing.T) {
	const pixelWidth = 64

	canvas := image.NewNRGBA(image.Rect(0, 0, pixelWidth, pixelWidth))
	background := color.NRGBA{R: 0x20, G: 0x28, B: 0x40, A: 0xFF}
	for y := 0; y < pixelWidth; y++ {
		for x := 0; x < pixelWidth; x++ {
			canvas.SetNRGBA(x, y, background)
		}
	}

	glyph.DrawClockArrow(canvas, canvas.Bounds(), whiteInk)

	assert.Equal(t, background, canvas.NRGBAAt(0, 0), "the background is left alone where the shape is not")
	assert.Equal(t, whiteInk, canvas.NRGBAAt(32, 32), "the centre is fully inked")
}

func TestDrawClockArrowIgnoresAnEmptyArea(t *testing.T) {
	canvas := image.NewNRGBA(image.Rect(0, 0, 8, 8))

	assert.NotPanics(t, func() {
		glyph.DrawClockArrow(canvas, image.Rect(0, 0, 0, 0), whiteInk)
	})
}
