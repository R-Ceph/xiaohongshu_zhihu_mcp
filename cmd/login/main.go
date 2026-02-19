package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	"github.com/go-rod/rod"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/xiaohongshu-mcp/browser"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
	"github.com/xpzouying/xiaohongshu-mcp/xiaohongshu"
	"github.com/xpzouying/xiaohongshu-mcp/zhihu"
)

func main() {
	var (
		binPath  string
		platform string
	)
	flag.StringVar(&binPath, "bin", "", "浏览器二进制文件路径")
	flag.StringVar(&platform, "platform", "xhs", "登录平台: xhs(小红书) 或 zhihu(知乎)")
	flag.Parse()

	switch platform {
	case "xhs":
		loginXiaohongshu(binPath)
	case "zhihu":
		loginZhihu(binPath)
	default:
		logrus.Fatalf("不支持的平台: %s，可选值: xhs, zhihu", platform)
	}
}

func loginXiaohongshu(binPath string) {
	b := browser.NewBrowser(false, browser.WithBinPath(binPath))
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := xiaohongshu.NewLogin(page)

	status, err := action.CheckLoginStatus(context.Background())
	if err != nil {
		logrus.Fatalf("检查小红书登录状态失败: %v", err)
	}

	logrus.Infof("小红书当前登录状态: %v", status)

	if status {
		return
	}

	logrus.Info("开始小红书登录流程...")
	if err = action.Login(context.Background()); err != nil {
		logrus.Fatalf("小红书登录失败: %v", err)
	} else {
		if err := saveCookiesToPath(page, cookies.GetCookiesFilePath()); err != nil {
			logrus.Fatalf("保存小红书 cookies 失败: %v", err)
		}
	}

	status, err = action.CheckLoginStatus(context.Background())
	if err != nil {
		logrus.Fatalf("登录后检查状态失败: %v", err)
	}

	if status {
		logrus.Info("小红书登录成功！")
	} else {
		logrus.Error("小红书登录流程完成但仍未登录")
	}
}

func loginZhihu(binPath string) {
	cookiePath := cookies.GetCookiesFilePathForPlatform("zhihu")
	b := browser.NewBrowser(false, browser.WithBinPath(binPath), browser.WithCookiePath(cookiePath))
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := zhihu.NewLogin(page)

	status, err := action.CheckLoginStatus(context.Background())
	if err != nil {
		logrus.Fatalf("检查知乎登录状态失败: %v", err)
	}

	logrus.Infof("知乎当前登录状态: %v", status)

	if status {
		return
	}

	logrus.Info("开始知乎登录流程...")
	if err = action.Login(context.Background()); err != nil {
		logrus.Fatalf("知乎登录失败: %v", err)
	} else {
		if err := saveCookiesToPath(page, cookiePath); err != nil {
			logrus.Fatalf("保存知乎 cookies 失败: %v", err)
		}
	}

	status, err = action.CheckLoginStatus(context.Background())
	if err != nil {
		logrus.Fatalf("登录后检查状态失败: %v", err)
	}

	if status {
		logrus.Info("知乎登录成功！")
	} else {
		logrus.Error("知乎登录流程完成但仍未登录")
	}
}

// saveCookiesToPath 保存浏览器 cookies 到指定路径。
//
// Args:
//
//	page: rod 浏览器页面
//	cookiePath: cookie 文件保存路径
//
// Returns:
//
//	error: 错误信息
func saveCookiesToPath(page *rod.Page, cookiePath string) error {
	cks, err := page.Browser().GetCookies()
	if err != nil {
		return err
	}

	data, err := json.Marshal(cks)
	if err != nil {
		return err
	}

	fmt.Printf("cookies 将保存到: %s\n", cookiePath)
	cookieLoader := cookies.NewLoadCookie(cookiePath)
	return cookieLoader.SaveCookies(data)
}
