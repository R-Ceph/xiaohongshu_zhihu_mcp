package browser

import (
	"github.com/sirupsen/logrus"
	"github.com/xpzouying/headless_browser"
	"github.com/xpzouying/xiaohongshu-mcp/cookies"
)

type browserConfig struct {
	binPath    string
	cookiePath string
}

type Option func(*browserConfig)

func WithBinPath(binPath string) Option {
	return func(c *browserConfig) {
		c.binPath = binPath
	}
}

// WithCookiePath 指定 cookie 文件路径，覆盖默认的小红书路径。
func WithCookiePath(path string) Option {
	return func(c *browserConfig) {
		c.cookiePath = path
	}
}

// NewBrowser 创建浏览器实例并注入 cookie。
//
// Args:
//
//	headless: 是否无头模式
//	options: 可选配置（WithBinPath, WithCookiePath 等）
//
// Returns:
//
//	初始化好的浏览器实例
func NewBrowser(headless bool, options ...Option) *headless_browser.Browser {
	cfg := &browserConfig{}
	for _, opt := range options {
		opt(cfg)
	}

	opts := []headless_browser.Option{
		headless_browser.WithHeadless(headless),
	}
	if cfg.binPath != "" {
		opts = append(opts, headless_browser.WithChromeBinPath(cfg.binPath))
	}

	// 加载 cookies：优先使用指定路径，否则用默认小红书路径
	cookiePath := cfg.cookiePath
	if cookiePath == "" {
		cookiePath = cookies.GetCookiesFilePath()
	}
	cookieLoader := cookies.NewLoadCookie(cookiePath)

	if data, err := cookieLoader.LoadCookies(); err == nil {
		opts = append(opts, headless_browser.WithCookies(string(data)))
		logrus.Debugf("loaded cookies from %s", cookiePath)
	} else {
		logrus.Warnf("failed to load cookies from %s: %v", cookiePath, err)
	}

	return headless_browser.New(opts...)
}
