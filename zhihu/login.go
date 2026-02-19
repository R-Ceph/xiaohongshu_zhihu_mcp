package zhihu

import (
	"context"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	zhihuHomeURL   = "https://www.zhihu.com/"
	zhihuSignInURL = "https://www.zhihu.com/signin"

	// 登录状态检测：右上角"消息"链接（已登录才会出现）
	selectorLoggedIn = `a[aria-label="消息"]`
	// 二维码图片选择器
	selectorQrcodeImg = `.Qrcode-img`
)

// LoginAction 知乎登录操作
type LoginAction struct {
	page *rod.Page
}

// NewLogin 创建知乎登录 action。
//
// Args:
//
//	page: rod 浏览器页面实例
//
// Returns:
//
//	LoginAction 实例
func NewLogin(page *rod.Page) *LoginAction {
	return &LoginAction{page: page}
}

// CheckLoginStatus 检查知乎登录状态。
//
// Args:
//
//	ctx: 上下文
//
// Returns:
//
//	bool: 是否已登录
//	error: 错误信息
func (a *LoginAction) CheckLoginStatus(ctx context.Context) (bool, error) {
	pp := a.page.Context(ctx)
	pp.MustNavigate(zhihuHomeURL).MustWaitLoad()

	time.Sleep(2 * time.Second)

	exists, _, err := pp.Has(selectorLoggedIn)
	if err != nil {
		return false, errors.Wrap(err, "检查知乎登录状态失败")
	}

	return exists, nil
}

// Login 交互式登录（非无头模式，等待用户扫码）。
// 知乎扫码成功后会跳转页面，因此通过轮询 URL 变化来检测登录完成。
//
// Args:
//
//	ctx: 上下文
//
// Returns:
//
//	error: 错误信息
func (a *LoginAction) Login(ctx context.Context) error {
	pp := a.page.Context(ctx)
	pp.MustNavigate(zhihuSignInURL).MustWaitLoad()

	time.Sleep(2 * time.Second)

	if exists, _, _ := pp.Has(selectorLoggedIn); exists {
		logrus.Info("知乎已处于登录状态")
		return nil
	}

	logrus.Info("请使用知乎 App 扫描二维码登录...")

	// 扫码成功后知乎会跳转离开 /signin，轮询检测
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return errors.New("登录超时")
		case <-ticker.C:
			info := pp.MustEval(`() => window.location.href`).String()
			if !strings.Contains(info, "/signin") {
				logrus.Infof("知乎页面已跳转到: %s，登录成功", info)
				time.Sleep(2 * time.Second)
				return nil
			}
		}
	}
}

// FetchQrcodeImage 获取知乎登录二维码图片。
//
// Args:
//
//	ctx: 上下文
//
// Returns:
//
//	string: 二维码图片 base64 或 URL
//	bool: 是否已登录
//	error: 错误信息
func (a *LoginAction) FetchQrcodeImage(ctx context.Context) (string, bool, error) {
	pp := a.page.Context(ctx)
	pp.MustNavigate(zhihuSignInURL).MustWaitLoad()

	time.Sleep(2 * time.Second)

	if exists, _, _ := pp.Has(selectorLoggedIn); exists {
		return "", true, nil
	}

	src, err := pp.MustElement(selectorQrcodeImg).Attribute("src")
	if err != nil {
		return "", false, errors.Wrap(err, "获取知乎二维码图片失败")
	}
	if src == nil || len(*src) == 0 {
		return "", false, errors.New("知乎二维码图片 src 为空")
	}

	return *src, false, nil
}

// WaitForLogin 轮询等待登录完成（通过检测页面跳转）。
//
// Args:
//
//	ctx: 带超时的上下文
//
// Returns:
//
//	bool: 是否登录成功
func (a *LoginAction) WaitForLogin(ctx context.Context) bool {
	pp := a.page.Context(ctx)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			// 方式1：检测 URL 跳转（扫码成功后离开 /signin）
			info := pp.MustEval(`() => window.location.href`).String()
			if !strings.Contains(info, "/signin") {
				time.Sleep(2 * time.Second)
				return true
			}
			// 方式2：检测已登录元素（首页直接检测）
			el, err := pp.Element(selectorLoggedIn)
			if err == nil && el != nil {
				return true
			}
		}
	}
}
