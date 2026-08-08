// Package glyph 畫出本服務的識別形狀：一個帶循環箭頭的時鐘。
//
// 它同時服務兩處——app 圖示（`tools/appicon`）與選單列圖示（controller）——所以
// 形狀只有一份定義。兩邊各畫一份，遲早會在某次調整後長得不一樣。
//
// 每個形狀都以「帶號距離」（負值在形狀內、正值在形狀外）描述，再把距離換算成
// 覆蓋率當作 alpha。這樣就有平滑的邊緣，不需要超取樣，也不需要繪圖函式庫。
package glyph

import (
	"image"
	"image/color"
	"math"
)

// 形狀參數。以「半徑 1 的正方形」為單位，實際尺寸由呼叫方給的邊界決定。
const (
	ringRadius     = 0.70
	ringHalfWidth  = 0.098
	gapStartAngle  = -78 * math.Pi / 180 // 缺口的上緣，箭頭就長在這裡
	gapEndAngle    = -18 * math.Pi / 180 // 缺口的右緣
	arrowTipAngle  = -44 * math.Pi / 180
	hourHandAngle  = -118 * math.Pi / 180
	hourHandLength = 0.46
	minuteAngle    = 22 * math.Pi / 180
	minuteLength   = 0.62
	handHalfWidth  = 0.062
	pivotRadius    = 0.085
)

// DrawClockArrow 把時鐘畫進 bounds 這塊方形區域。
//
// 只寫進形狀覆蓋到的像素，其餘一概不動——這讓它既能疊在 app 圖示的圓角方塊上，
// 也能單獨畫在全透明的畫布上當選單列的 template 圖示。
func DrawClockArrow(canvas *image.NRGBA, bounds image.Rectangle, inkColor color.NRGBA) {
	size := math.Min(float64(bounds.Dx()), float64(bounds.Dy()))
	if size <= 0 {
		return
	}

	unit := size / 2
	centerX := float64(bounds.Min.X) + float64(bounds.Dx())/2
	centerY := float64(bounds.Min.Y) + float64(bounds.Dy())/2

	arrow := arrowVertices()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			// 換算到「半徑 1」的座標系，距離也一併換算，覆蓋率才會與尺寸無關。
			offsetX := (float64(x) + 0.5 - centerX) / unit
			offsetY := (float64(y) + 0.5 - centerY) / unit

			coverage := coverageOf(shapeDistance(offsetX, offsetY, arrow), unit)
			if coverage <= 0 {
				continue
			}

			canvas.SetNRGBA(x, y, blendOver(canvas.NRGBAAt(x, y), inkColor, coverage))
		}
	}
}

// shapeDistance 是整個時鐘的帶號距離：缺口環、箭頭、兩根指針與軸心的聯集。
func shapeDistance(offsetX float64, offsetY float64, arrow [3][2]float64) float64 {
	hourEndX, hourEndY := polar(hourHandLength*ringRadius, hourHandAngle)
	minuteEndX, minuteEndY := polar(minuteLength*ringRadius, minuteAngle)

	distances := []float64{
		gappedRingDistance(offsetX, offsetY),
		convexPolygonDistance(offsetX, offsetY, arrow),
		segmentDistance(offsetX, offsetY, hourEndX, hourEndY) - handHalfWidth,
		segmentDistance(offsetX, offsetY, minuteEndX, minuteEndY) - handHalfWidth,
		math.Hypot(offsetX, offsetY) - pivotRadius,
	}

	nearest := distances[0]
	for _, candidate := range distances[1:] {
		nearest = math.Min(nearest, candidate)
	}

	return nearest
}

// gappedRingDistance 是環的距離，但上緣到右緣之間留一個缺口讓箭頭進來。
//
// 缺口是這個圖示的意義所在：一個閉合的圓只是時鐘，開口加箭頭才是「會再回來」。
func gappedRingDistance(offsetX float64, offsetY float64) float64 {
	ringDistance := math.Abs(math.Hypot(offsetX, offsetY)-ringRadius) - ringHalfWidth
	if ringDistance > 0 {
		return ringDistance
	}

	angle := math.Atan2(offsetY, offsetX)
	if angle >= gapStartAngle && angle <= gapEndAngle {
		// 落在缺口裡：回一個正值把它挖掉，數值取到缺口邊界的角距離換算成長度。
		toStart := (angle - gapStartAngle) * ringRadius
		toEnd := (gapEndAngle - angle) * ringRadius

		return math.Min(toStart, toEnd)
	}

	return ringDistance
}

// arrowVertices 給出箭頭那個三角形的三個頂點。
//
// 底邊沿著缺口上緣呈放射狀，尖端朝缺口內側——看起來就是環繞了一圈之後往前指。
func arrowVertices() [3][2]float64 {
	tipX, tipY := polar(ringRadius*1.04, arrowTipAngle)
	innerX, innerY := polar(ringRadius-ringHalfWidth*2.0, gapStartAngle)
	outerX, outerY := polar(ringRadius+ringHalfWidth*2.0, gapStartAngle)

	return [3][2]float64{{tipX, tipY}, {innerX, innerY}, {outerX, outerY}}
}

// convexPolygonDistance 是凸多邊形的帶號距離。
//
// 內外由「各邊半平面的最大值」判定，距離則取「到各邊線段的最小值」。**兩者不能
// 混用**：單用半平面最大值，遠離形狀的點會拿到被低估的小正值，在邊的延長線上留
// 下一排若隱若現的雜點——那正是第一版渲染出來的虛線。
func convexPolygonDistance(pointX float64, pointY float64, vertices [3][2]float64) float64 {
	farthestHalfPlane := math.Inf(-1)
	nearestEdge := math.Inf(1)

	for index := range vertices {
		current := vertices[index]
		next := vertices[(index+1)%len(vertices)]

		edgeX, edgeY := next[0]-current[0], next[1]-current[1]
		edgeLength := math.Hypot(edgeX, edgeY)
		if edgeLength == 0 {
			continue
		}

		normalX, normalY := edgeY/edgeLength, -edgeX/edgeLength
		farthestHalfPlane = math.Max(farthestHalfPlane,
			(pointX-current[0])*normalX+(pointY-current[1])*normalY)

		nearestEdge = math.Min(nearestEdge,
			segmentDistance(pointX-current[0], pointY-current[1], edgeX, edgeY))
	}

	if farthestHalfPlane <= 0 {
		return -nearestEdge
	}

	return nearestEdge
}

// segmentDistance 回傳一點到「原點到 (endX, endY) 這條線段」的距離。
func segmentDistance(pointX float64, pointY float64, endX float64, endY float64) float64 {
	segmentLengthSquared := endX*endX + endY*endY
	if segmentLengthSquared == 0 {
		return math.Hypot(pointX, pointY)
	}

	projection := (pointX*endX + pointY*endY) / segmentLengthSquared
	projection = math.Min(math.Max(projection, 0), 1)

	return math.Hypot(pointX-projection*endX, pointY-projection*endY)
}

func polar(radius float64, angle float64) (float64, float64) {
	return radius * math.Cos(angle), radius * math.Sin(angle)
}

// coverageOf 把帶號距離換算成 0..1 的覆蓋率，邊界一個像素內線性過渡。
func coverageOf(signedDistance float64, unit float64) float64 {
	return math.Min(math.Max(0.5-signedDistance*unit, 0), 1)
}

func blendOver(baseColor color.NRGBA, inkColor color.NRGBA, inkAlpha float64) color.NRGBA {
	if baseColor.A == 0 {
		// 畫在全透明的底上：混色沒有意義，直接讓覆蓋率變成 alpha。這是選單列
		// template 圖示走的路，macOS 只看 alpha。
		return color.NRGBA{R: inkColor.R, G: inkColor.G, B: inkColor.B, A: uint8(float64(inkColor.A) * inkAlpha)}
	}

	return color.NRGBA{
		R: interpolate(baseColor.R, inkColor.R, inkAlpha),
		G: interpolate(baseColor.G, inkColor.G, inkAlpha),
		B: interpolate(baseColor.B, inkColor.B, inkAlpha),
		A: baseColor.A,
	}
}

func interpolate(from uint8, to uint8, ratio float64) uint8 {
	return uint8(float64(from) + (float64(to)-float64(from))*ratio)
}
