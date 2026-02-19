package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-rod/rod"
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/headless_browser"
	"github.com/xpzouying/xiaohongshu-mcp/browser"
	"github.com/xpzouying/xiaohongshu-mcp/configs"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
	"github.com/xpzouying/xiaohongshu-mcp/zhihu"
)

// ZhihuService 知乎业务服务
type ZhihuService struct{}

// NewZhihuService 创建知乎服务实例。
//
// Returns:
//
//	ZhihuService 实例
func NewZhihuService() *ZhihuService {
	return &ZhihuService{}
}

// ZhihuLoginStatusResponse 知乎登录状态响应
type ZhihuLoginStatusResponse struct {
	IsLoggedIn bool   `json:"is_logged_in"`
	Platform   string `json:"platform"`
}

// ZhihuLoginQrcodeResponse 知乎登录二维码响应
type ZhihuLoginQrcodeResponse struct {
	Timeout    string `json:"timeout"`
	IsLoggedIn bool   `json:"is_logged_in"`
	Img        string `json:"img,omitempty"`
	Platform   string `json:"platform"`
}

// CheckLoginStatus 检查知乎登录状态。
//
// Args:
//
//	ctx: 上下文
//
// Returns:
//
//	*ZhihuLoginStatusResponse: 登录状态
//	error: 错误信息
func (s *ZhihuService) CheckLoginStatus(ctx context.Context) (*ZhihuLoginStatusResponse, error) {
	b := newZhihuBrowser()
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := zhihu.NewLogin(page)
	isLoggedIn, err := action.CheckLoginStatus(ctx)
	if err != nil {
		return nil, err
	}

	return &ZhihuLoginStatusResponse{
		IsLoggedIn: isLoggedIn,
		Platform:   "zhihu",
	}, nil
}

// GetLoginQrcode 获取知乎登录二维码。
//
// Args:
//
//	ctx: 上下文
//
// Returns:
//
//	*ZhihuLoginQrcodeResponse: 二维码响应
//	error: 错误信息
func (s *ZhihuService) GetLoginQrcode(ctx context.Context) (*ZhihuLoginQrcodeResponse, error) {
	b := newZhihuBrowser()
	page := b.NewPage()

	deferFunc := func() {
		_ = page.Close()
		b.Close()
	}

	action := zhihu.NewLogin(page)

	img, loggedIn, err := action.FetchQrcodeImage(ctx)
	if err != nil || loggedIn {
		defer deferFunc()
	}
	if err != nil {
		return nil, err
	}

	timeout := 4 * time.Minute

	if !loggedIn {
		go func() {
			ctxTimeout, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			defer deferFunc()

			if action.WaitForLogin(ctxTimeout) {
				if er := saveZhihuCookies(page); er != nil {
					logrus.Errorf("保存知乎 cookies 失败: %v", er)
				}
			}
		}()
	}

	return &ZhihuLoginQrcodeResponse{
		Timeout: func() string {
			if loggedIn {
				return "0s"
			}
			return timeout.String()
		}(),
		Img:        img,
		IsLoggedIn: loggedIn,
		Platform:   "zhihu",
	}, nil
}

// DeleteCookies 删除知乎 cookies 文件。
//
// Args:
//
//	ctx: 上下文
//
// Returns:
//
//	error: 错误信息
func (s *ZhihuService) DeleteCookies(ctx context.Context) error {
	cookiePath := cookies.GetCookiesFilePathForPlatform("zhihu")
	cookieLoader := cookies.NewLoadCookie(cookiePath)
	return cookieLoader.DeleteCookies()
}

// FetchPage 抓取知乎页面内容并转为 Markdown。
//
// Args:
//
//	ctx: 上下文
//	url: 知乎页面 URL
//
// Returns:
//
//	*zhihu.PageContent: 页面内容
//	error: 错误信息
func (s *ZhihuService) FetchPage(ctx context.Context, url string) (*zhihu.PageContent, error) {
	b := newZhihuBrowser()
	defer b.Close()

	page := b.NewPage()
	defer page.Close()

	action := zhihu.NewFetchPageAction(page)
	result, err := action.FetchPage(ctx, url)
	if err != nil {
		return nil, err
	}

	// 自动保存到本地文件
	if savedPath, saveErr := result.SaveToFile("."); saveErr != nil {
		logrus.Warnf("自动保存文件失败: %v", saveErr)
	} else {
		logrus.Infof("文件已保存: %s", savedPath)
	}

	return result, nil
}

func newZhihuBrowser() *headless_browser.Browser {
	cookiePath := cookies.GetCookiesFilePathForPlatform("zhihu")
	return browser.NewBrowser(
		configs.IsHeadless(),
		browser.WithBinPath(configs.GetBinPath()),
		browser.WithCookiePath(cookiePath),
	)
}

func saveZhihuCookies(page *rod.Page) error {
	cks, err := page.Browser().GetCookies()
	if err != nil {
		return err
	}

	data, err := json.Marshal(cks)
	if err != nil {
		return err
	}

	cookiePath := cookies.GetCookiesFilePathForPlatform("zhihu")
	cookieLoader := cookies.NewLoadCookie(cookiePath)
	return cookieLoader.SaveCookies(data)
}
