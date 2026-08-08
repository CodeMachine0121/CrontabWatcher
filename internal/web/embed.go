package web

import "embed"

// TemplateFileSystem 內嵌 HTML template。
//
// 用 go:embed 而不是執行時讀檔：整個服務因此仍是一個可以直接丟到任何機器上的
// 單一 binary，不必附帶資產目錄。
//
//go:embed templates/*.gohtml
var TemplateFileSystem embed.FS

// StaticFileSystem 內嵌 CSS 與 JS。
//
// 這裡沒有任何 CDN 依賴 —— 這個服務常常跑在沒有對外網路的機器上，而一個載不到
// 樣式表的監控頁面等於沒有。
//
//go:embed static/*
var StaticFileSystem embed.FS
