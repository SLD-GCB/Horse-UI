package webui

import (
	"embed"
	"io/fs"
)

// dist 为前端构建产物，由 front-UI 构建时输出到此目录。
//
//go:embed all:dist
var distFS embed.FS

// Dist 返回前端静态资源文件系统。
func Dist() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
