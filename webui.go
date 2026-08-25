package main

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed web/*
var webFS embed.FS

// registerWebUI 注册本地调试控制台静态页面
func registerWebUI(router *gin.Engine) {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic("加载 web 静态资源失败: " + err.Error())
	}

	fileServer := http.FileServer(http.FS(sub))

	// 首页
	router.GET("/", func(c *gin.Context) {
		data, err := webFS.ReadFile("web/index.html")
		if err != nil {
			c.String(http.StatusInternalServerError, "无法加载控制台页面")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})

	// /web/* 静态资源
	router.GET("/web/*filepath", func(c *gin.Context) {
		c.Request.URL.Path = c.Param("filepath")
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}
