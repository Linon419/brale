package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var assets embed.FS

// StaticFS 返回静态资源的 http.FileSystem。
func StaticFS() (http.FileSystem, error) {
	sub, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	return http.FS(sub), nil
}

// MustStaticFS 返回静态文件系统（panic on error）。
func MustStaticFS() http.FileSystem {
	fs, err := StaticFS()
	if err != nil {
		panic(err)
	}
	return fs
}

// Index 返回嵌入的首页内容。
func Index() ([]byte, error) {
	return assets.ReadFile("static/index.html")
}
