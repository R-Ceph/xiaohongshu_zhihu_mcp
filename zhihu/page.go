package zhihu

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/go-rod/rod"
	"github.com/sirupsen/logrus"
)

// 默认下载目录
const defaultDownloadDir = "download_files"

// PageContent 知乎页面抓取结果
type PageContent struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Markdown    string `json:"markdown"`
	HTML        string `json:"html"`
	SavedPath   string `json:"saved_path,omitempty"`
	CreatedTime string `json:"created_time,omitempty"` // 发布/编辑时间
	VoteCount   string `json:"vote_count,omitempty"`   // 赞同数
}

// FetchPageAction 知乎页面抓取操作
type FetchPageAction struct {
	page *rod.Page
}

// NewFetchPageAction 创建页面抓取 action。
//
// Args:
//
//	page: rod 浏览器页面实例
//
// Returns:
//
//	FetchPageAction 实例
func NewFetchPageAction(page *rod.Page) *FetchPageAction {
	return &FetchPageAction{page: page}
}

// FetchPage 打开知乎 URL 并提取页面正文内容，转为 Markdown。
//
// Args:
//
//	ctx: 上下文
//	url: 知乎页面 URL
//
// Returns:
//
//	*PageContent: 页面内容（含标题、Markdown、原始 HTML）
//	error: 错误信息
func (a *FetchPageAction) FetchPage(ctx context.Context, url string) (*PageContent, error) {
	if !strings.Contains(url, "zhihu.com") {
		return nil, fmt.Errorf("仅支持知乎链接，当前: %s", url)
	}

	pp := a.page.Context(ctx)

	logrus.Infof("打开知乎页面: %s", url)
	pp.MustNavigate(url).MustWaitDOMStable()
	time.Sleep(2 * time.Second)

	title := pp.MustEval(`() => document.title`).String()

	// 根据 URL 类型选择不同的正文选择器
	contentHTML := a.extractContent(pp, url)
	if contentHTML == "" {
		return nil, fmt.Errorf("未能提取到页面正文内容")
	}

	markdown, err := htmltomarkdown.ConvertString(contentHTML)
	if err != nil {
		logrus.Warnf("HTML 转 Markdown 失败，返回原始 HTML: %v", err)
		return &PageContent{
			URL:   url,
			Title: title,
			HTML:  contentHTML,
		}, nil
	}

	// 提取发布时间和赞同数
	createdTime := a.extractPageTime(pp, url)
	voteCount := a.extractPageVoteCount(pp, url)

	logrus.Infof("页面抓取完成: %s (Markdown %d 字符, 时间: %s, 赞同: %s)", title, len(markdown), createdTime, voteCount)

	return &PageContent{
		URL:         url,
		Title:       title,
		Markdown:    markdown,
		HTML:        contentHTML,
		CreatedTime: createdTime,
		VoteCount:   voteCount,
	}, nil
}

// extractContent 根据页面类型提取正文 HTML。
func (a *FetchPageAction) extractContent(pp *rod.Page, url string) string {
	// 知乎不同页面类型对应不同的正文容器
	selectors := []string{
		// 问答页面 - 回答内容
		`.AnswerItem .RichContent-inner`,
		// 文章页面
		`.Post-RichTextContainer`,
		// 专栏文章
		`.RichText.ztext.Post-RichText`,
		// 问题描述
		`.QuestionRichText--collapsed, .QuestionRichText--expanded`,
		// 通用 RichText
		`.RichText.ztext`,
	}

	// 问答页面特殊处理：收集所有回答
	if strings.Contains(url, "/question/") {
		return a.extractQuestionPage(pp)
	}

	// 其他页面：按优先级尝试选择器
	for _, sel := range selectors {
		el, err := pp.Timeout(3 * time.Second).Element(sel)
		if err != nil {
			continue
		}
		html, err := el.HTML()
		if err != nil {
			continue
		}
		if strings.TrimSpace(html) != "" {
			return html
		}
	}

	// 兜底：取 body 中主要内容区域
	el, err := pp.Timeout(3 * time.Second).Element(`#root`)
	if err == nil {
		html, _ := el.HTML()
		return html
	}

	return ""
}

// SaveToFile 将 Markdown 内容保存到文件。
// 路径格式: {baseDir}/download_files/{YY-M-DD}/{HH-MM-SS}.md
//
// Args:
//
//	baseDir: 项目根目录
//
// Returns:
//
//	string: 保存的文件完整路径
//	error: 错误信息
func (p *PageContent) SaveToFile(baseDir string) (string, error) {
	content := p.Markdown
	if content == "" {
		content = p.HTML
	}
	if content == "" {
		return "", fmt.Errorf("没有可保存的内容")
	}

	now := time.Now()
	dateDir := fmt.Sprintf("%02d-%d-%02d", now.Year()%100, int(now.Month()), now.Day())
	fileName := fmt.Sprintf("%02d-%02d-%02d.md", now.Hour(), now.Minute(), now.Second())

	dirPath := filepath.Join(baseDir, defaultDownloadDir, dateDir)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return "", fmt.Errorf("创建目录失败: %w", err)
	}

	filePath := filepath.Join(dirPath, fileName)

	// 组装完整 Markdown：标题 + 来源 + 元信息 + 正文
	var metaInfo string
	if p.CreatedTime != "" {
		metaInfo += fmt.Sprintf("**发布时间**: %s\n", p.CreatedTime)
	}
	if p.VoteCount != "" {
		metaInfo += fmt.Sprintf("**赞同数**: %s\n", p.VoteCount)
	}
	if metaInfo != "" {
		metaInfo += "\n"
	}
	fullContent := fmt.Sprintf("# %s\n\n**来源**: %s\n%s\n---\n\n%s\n", p.Title, p.URL, metaInfo, content)

	if err := os.WriteFile(filePath, []byte(fullContent), 0644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}

	logrus.Infof("Markdown 已保存到: %s", filePath)
	p.SavedPath = filePath
	return filePath, nil
}

// extractPageTime 提取页面的发布/编辑时间（针对单篇文章或单个回答页面）。
func (a *FetchPageAction) extractPageTime(pp *rod.Page, url string) string {
	// 问答页面包含多个回答，时间在 extractQuestionPage 中单独提取
	if strings.Contains(url, "/question/") && !strings.Contains(url, "/answer/") {
		return ""
	}

	// 尝试多种选择器获取发布时间
	timeSelectors := []string{
		// 回答页面的时间信息
		`.ContentItem-time`,
		// 文章页面的发布时间
		`.Post-Time`,
		`.ContentItem-time span`,
		// 文章页面备用
		`.Article-Time`,
	}

	for _, sel := range timeSelectors {
		if el, err := pp.Timeout(2 * time.Second).Element(sel); err == nil {
			if text, err := el.Text(); err == nil && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}

	// 尝试从 meta 标签获取时间
	if el, err := pp.Timeout(1 * time.Second).Element(`meta[itemprop="dateCreated"]`); err == nil {
		if content, err := el.Attribute("content"); err == nil && content != nil {
			return *content
		}
	}
	if el, err := pp.Timeout(1 * time.Second).Element(`meta[itemprop="dateModified"]`); err == nil {
		if content, err := el.Attribute("content"); err == nil && content != nil {
			return *content
		}
	}

	// 尝试通过 JavaScript 提取时间信息（data-zop 或页面内的 JSON 数据）
	result := pp.MustEval(`() => {
		// 尝试从 time 标签获取
		let timeEl = document.querySelector('.ContentItem-time time');
		if (timeEl) return timeEl.getAttribute('datetime') || timeEl.textContent.trim();

		// 尝试从 .ContentItem-time 的文本获取
		let timeSpan = document.querySelector('.ContentItem-time');
		if (timeSpan) return timeSpan.textContent.trim();

		// 尝试从文章页面的时间元素获取
		let postTime = document.querySelector('.Post-Header .ContentItem-time');
		if (postTime) return postTime.textContent.trim();

		return '';
	}`)
	if result.String() != "" {
		return result.String()
	}

	return ""
}

// extractPageVoteCount 提取页面的赞同数（针对单篇文章或单个回答页面）。
func (a *FetchPageAction) extractPageVoteCount(pp *rod.Page, url string) string {
	// 问答页面包含多个回答，赞同数在 extractQuestionPage 中单独提取
	if strings.Contains(url, "/question/") && !strings.Contains(url, "/answer/") {
		return ""
	}

	// 通过 JavaScript 提取赞同数，更可靠
	result := pp.MustEval(`() => {
		// 方式1: 从 VoteButton 获取
		let voteBtn = document.querySelector('.VoteButton--up');
		if (voteBtn) {
			let text = voteBtn.textContent.trim();
			if (text && text !== '赞同') return text.replace('赞同 ', '').replace('赞同', '').trim();
		}

		// 方式2: 从带有 aria-label 的按钮获取
		let buttons = document.querySelectorAll('button');
		for (let btn of buttons) {
			let label = btn.getAttribute('aria-label') || '';
			if (label.includes('赞同')) {
				// aria-label 格式通常为 "赞同 123"
				let match = label.match(/(\d[\d,]*)/)
				if (match) return match[1];
			}
		}

		// 方式3: 从文章页面的点赞按钮获取
		let likeBtn = document.querySelector('.Post-SocialButton .like');
		if (likeBtn) return likeBtn.textContent.trim();

		return '';
	}`)
	if result.String() != "" {
		return result.String()
	}

	return ""
}

// extractQuestionPage 提取知乎问答页面（问题 + 多个回答）。
func (a *FetchPageAction) extractQuestionPage(pp *rod.Page) string {
	var parts []string

	// 提取问题标题
	if el, err := pp.Timeout(3 * time.Second).Element(`.QuestionHeader-title`); err == nil {
		if text, err := el.Text(); err == nil {
			parts = append(parts, fmt.Sprintf("<h1>%s</h1>", text))
		}
	}

	// 提取问题描述
	if el, err := pp.Timeout(2 * time.Second).Element(`.QuestionRichText--collapsed, .QuestionRichText--expanded`); err == nil {
		if html, err := el.HTML(); err == nil && strings.TrimSpace(html) != "" {
			parts = append(parts, "<h2>问题描述</h2>"+html)
		}
	}

	// 提取回答列表
	answers, err := pp.Timeout(3 * time.Second).Elements(`.AnswerItem`)
	if err == nil {
		for i, answer := range answers {
			if i >= 10 {
				break
			}

			var authorName string
			if authorEl, err := answer.Element(`.AuthorInfo-name`); err == nil {
				authorName, _ = authorEl.Text()
			}

			// 提取赞同数
			voteCount := a.extractAnswerVoteCount(answer)

			// 提取回答时间
			answerTime := a.extractAnswerTime(answer)

			header := fmt.Sprintf("<h2>回答 %d", i+1)
			if authorName != "" {
				header += fmt.Sprintf(" - %s", authorName)
			}
			header += "</h2>"

			// 添加元信息
			meta := ""
			if voteCount != "" || answerTime != "" {
				meta = "<p>"
				if voteCount != "" {
					meta += fmt.Sprintf("<strong>赞同数: %s</strong>", voteCount)
				}
				if answerTime != "" {
					if voteCount != "" {
						meta += " | "
					}
					meta += fmt.Sprintf("<strong>发布时间: %s</strong>", answerTime)
				}
				meta += "</p>"
			}

			if contentEl, err := answer.Element(`.RichContent-inner`); err == nil {
				if html, err := contentEl.HTML(); err == nil {
					parts = append(parts, header+meta+html)
				}
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, "\n")
}

// extractAnswerVoteCount 从单个回答元素中提取赞同数。
func (a *FetchPageAction) extractAnswerVoteCount(answer *rod.Element) string {
	// 尝试从 VoteButton 获取
	if el, err := answer.Element(`.VoteButton--up`); err == nil {
		if text, err := el.Text(); err == nil {
			text = strings.TrimSpace(text)
			text = strings.ReplaceAll(text, "赞同 ", "")
			text = strings.ReplaceAll(text, "赞同", "")
			if text != "" {
				return text
			}
		}
	}

	// 尝试从 aria-label 获取
	if el, err := answer.Element(`button[aria-label*="赞同"]`); err == nil {
		if label, err := el.Attribute("aria-label"); err == nil && label != nil {
			// 格式: "赞同 123"
			text := strings.ReplaceAll(*label, "赞同 ", "")
			text = strings.ReplaceAll(text, "赞同", "")
			text = strings.TrimSpace(text)
			if text != "" {
				return text
			}
		}
	}

	return ""
}

// extractAnswerTime 从单个回答元素中提取发布时间。
func (a *FetchPageAction) extractAnswerTime(answer *rod.Element) string {
	// 尝试从 ContentItem-time 获取
	if el, err := answer.Element(`.ContentItem-time`); err == nil {
		if text, err := el.Text(); err == nil && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}

	// 尝试从 time 标签获取
	if el, err := answer.Element(`time`); err == nil {
		if dt, err := el.Attribute("datetime"); err == nil && dt != nil {
			return *dt
		}
		if text, err := el.Text(); err == nil && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}

	// 尝试从 meta 标签获取
	if el, err := answer.Element(`meta[itemprop="dateCreated"]`); err == nil {
		if content, err := el.Attribute("content"); err == nil && content != nil {
			return *content
		}
	}

	return ""
}
