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
	URL       string         `json:"url"`
	Title     string         `json:"title"`
	Markdown  string         `json:"markdown"`
	HTML      string         `json:"html"`
	Comments  []ZhihuComment `json:"comments,omitempty"`
	SavedPath string         `json:"saved_path,omitempty"`
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

	logrus.Infof("页面抓取完成: %s (Markdown %d 字符)", title, len(markdown))

	return &PageContent{
		URL:      url,
		Title:    title,
		Markdown: markdown,
		HTML:     contentHTML,
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

	// 组装完整 Markdown：标题 + 来源 + 正文（评论插入到第一个回答后面）
	fullContent := fmt.Sprintf("# %s\n\n**来源**: %s\n\n---\n\n", p.Title, p.URL)

	if len(p.Comments) > 0 {
		commentBlock := formatCommentsMarkdown(p.Comments)
		fullContent += insertCommentsAfterFirstAnswer(content, commentBlock)
	} else {
		fullContent += content
	}
	fullContent += "\n"

	if err := os.WriteFile(filePath, []byte(fullContent), 0644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}

	logrus.Infof("Markdown 已保存到: %s", filePath)
	p.SavedPath = filePath
	return filePath, nil
}

// formatCommentsMarkdown 将评论列表格式化为 Markdown，包含回复嵌套关系。
func formatCommentsMarkdown(comments []ZhihuComment) string {
	totalCount := 0
	for _, c := range comments {
		totalCount += 1 + len(c.Replies)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n### 💬 评论区 (%d 条，按点赞排序)\n\n", totalCount))

	for i, c := range comments {
		sb.WriteString(formatSingleComment(i+1, c, false))

		for _, r := range c.Replies {
			sb.WriteString(formatSingleComment(0, r, true))
		}
	}

	return sb.String()
}

// formatSingleComment 格式化单条评论。isReply=true 时用引用格式表示子回复。
func formatSingleComment(idx int, c ZhihuComment, isReply bool) string {
	author := c.Author
	if author == "" {
		author = "(匿名)"
	}

	meta := fmt.Sprintf("**%s** (👍 %d", author, c.Likes)
	if c.Time != "" {
		meta += fmt.Sprintf(" · %s", c.Time)
	}
	if c.IPLocation != "" {
		meta += fmt.Sprintf(" · %s", c.IPLocation)
	}
	meta += ")"

	if isReply {
		return fmt.Sprintf("   > ↳ %s\n   > %s\n\n", meta, c.Content)
	}
	return fmt.Sprintf("%d. %s\n   %s\n\n", idx, meta, c.Content)
}

// insertCommentsAfterFirstAnswer 将评论块插入到第一个回答后面（"## 回答 2"之前）。
// 如果找不到第二个回答标记，则追加到正文末尾。
func insertCommentsAfterFirstAnswer(content, commentBlock string) string {
	// 查找 "## 回答 2" 的位置
	markers := []string{"## 回答 2", "## 回答 2 "}
	for _, marker := range markers {
		idx := strings.Index(content, marker)
		if idx > 0 {
			return content[:idx] + commentBlock + "\n---\n\n" + content[idx:]
		}
	}
	// 没有第二个回答，追加到末尾
	return content + "\n" + commentBlock
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

			header := fmt.Sprintf("<h2>回答 %d", i+1)
			if authorName != "" {
				header += fmt.Sprintf(" - %s", authorName)
			}
			header += "</h2>"

			if contentEl, err := answer.Element(`.RichContent-inner`); err == nil {
				if html, err := contentEl.HTML(); err == nil {
					parts = append(parts, header+html)
				}
			}
		}
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, "\n")
}
