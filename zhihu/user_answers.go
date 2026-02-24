package zhihu

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/sirupsen/logrus"
)

// dataZop data-zop 属性的 JSON 结构
type dataZop struct {
	ItemID     json.Number `json:"itemId"`
	Type       string      `json:"type"`
	AuthorID   string      `json:"authorId,omitempty"`
	QuestionID json.Number `json:"questionId,omitempty"`
}

// AnswerLink 单条回答信息
type AnswerLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// UserAnswersResult 用户回答列表抓取结果
type UserAnswersResult struct {
	UserURL   string       `json:"user_url"`
	UserName  string       `json:"user_name"`
	Answers   []AnswerLink `json:"answers"`
	Total     int          `json:"total"`
	SavedPath string       `json:"saved_path,omitempty"`
}

// FetchUserAnswersAction 知乎用户回答列表抓取操作
type FetchUserAnswersAction struct {
	page *rod.Page
}

// NewFetchUserAnswersAction 创建用户回答列表抓取 action。
//
// Args:
//
//	page: rod 浏览器页面实例
//
// Returns:
//
//	FetchUserAnswersAction 实例
func NewFetchUserAnswersAction(page *rod.Page) *FetchUserAnswersAction {
	return &FetchUserAnswersAction{page: page}
}

// FetchUserAnswers 抓取知乎用户的回答列表，通过滚动加载获取最多 limit 条回答。
//
// Args:
//
//	ctx: 上下文
//	userURL: 用户回答页面 URL，如 https://www.zhihu.com/people/xxx/answers
//	limit: 最多获取的回答数量
//
// Returns:
//
//	*UserAnswersResult: 回答列表结果
//	error: 错误信息
func (a *FetchUserAnswersAction) FetchUserAnswers(ctx context.Context, userURL string, limit int) (*UserAnswersResult, error) {
	if !strings.Contains(userURL, "zhihu.com/people/") {
		return nil, fmt.Errorf("需要知乎个人主页链接，当前: %s", userURL)
	}

	// 确保 URL 以 /answers 结尾
	if !strings.HasSuffix(userURL, "/answers") {
		userURL = strings.TrimRight(userURL, "/") + "/answers"
	}

	pp := a.page.Context(ctx)

	logrus.Infof("打开知乎用户回答页: %s", userURL)
	pp.MustNavigate(userURL).MustWaitDOMStable()
	time.Sleep(2 * time.Second)

	// 获取用户名
	userName := ""
	if el, err := pp.Timeout(3 * time.Second).Element(`.ProfileHeader-title .ProfileHeader-name`); err == nil {
		userName, _ = el.Text()
	}
	if userName == "" {
		if el, err := pp.Timeout(2 * time.Second).Element(`h1.ProfileHeader-title`); err == nil {
			userName, _ = el.Text()
		}
	}
	userName = cleanText(userName)

	logrus.Infof("用户: %s, 开始滚动加载回答...", userName)

	// 滚动加载，收集回答链接
	answers := a.scrollAndCollect(pp, limit)

	logrus.Infof("共获取 %d 条回答", len(answers))

	return &UserAnswersResult{
		UserURL:  userURL,
		UserName: userName,
		Answers:  answers,
		Total:    len(answers),
	}, nil
}

// scrollAndCollect 滚动页面并收集回答链接，直到达到 limit 或没有更多内容。
func (a *FetchUserAnswersAction) scrollAndCollect(pp *rod.Page, limit int) []AnswerLink {
	seen := make(map[string]bool)
	var answers []AnswerLink

	maxNoNewRounds := 5
	noNewCount := 0

	for len(answers) < limit && noNewCount < maxNoNewRounds {
		newItems := a.extractAnswersFromPage(pp, seen)

		if len(newItems) == 0 {
			noNewCount++
		} else {
			noNewCount = 0
			for _, item := range newItems {
				if len(answers) >= limit {
					break
				}
				answers = append(answers, item)
			}
		}

		if len(answers) >= limit {
			break
		}

		// 滚动到页面底部
		pp.MustEval(`() => window.scrollTo(0, document.body.scrollHeight)`)
		time.Sleep(1500 * time.Millisecond)
	}

	return answers
}

// extractAnswersFromPage 从当前页面 DOM 中提取回答链接。
func (a *FetchUserAnswersAction) extractAnswersFromPage(pp *rod.Page, seen map[string]bool) []AnswerLink {
	var newItems []AnswerLink

	// 知乎个人主页回答页面，每个回答卡片是 .List-item
	// 其中包含 .ContentItem 和链接
	items, err := pp.Elements(`.List-item`)
	if err != nil || len(items) == 0 {
		// 备用选择器
		items, err = pp.Elements(`.ContentItem`)
		if err != nil {
			return nil
		}
	}

	for _, item := range items {
		link, title := a.extractAnswerInfo(item)
		if link == "" || seen[link] {
			continue
		}
		seen[link] = true
		newItems = append(newItems, AnswerLink{
			Title: title,
			URL:   link,
		})
	}

	return newItems
}

// extractAnswerInfo 从单个回答卡片中提取完整回答链接和标题。
// 优先通过 data-zop 拼出 /question/{qid}/answer/{aid} 格式。
func (a *FetchUserAnswersAction) extractAnswerInfo(item *rod.Element) (string, string) {
	title := a.extractTitle(item)

	// 方式1: 通过 a 标签直接找含 /answer/ 的完整链接（最准确）
	if links, err := item.Elements(`a[href]`); err == nil {
		for _, link := range links {
			href, err := link.Attribute("href")
			if err != nil || href == nil {
				continue
			}
			if strings.Contains(*href, "/question/") && strings.Contains(*href, "/answer/") {
				return normalizeZhihuURL(*href), title
			}
		}
	}

	// 方式2: 通过 data-zop 解析 questionId + itemId 拼完整回答链接
	if contentItem, err := item.Element(`[data-zop]`); err == nil {
		if attr, err := contentItem.Attribute("data-zop"); err == nil && attr != nil {
			var zop dataZop
			if json.Unmarshal([]byte(*attr), &zop) == nil && zop.ItemID.String() != "" {
				// 先拿 questionId，如果 data-zop 没有就从 meta 里取
				qid := zop.QuestionID.String()
				if qid == "" || qid == "0" {
					qid = a.extractQuestionID(item)
				}
				if qid != "" {
					url := fmt.Sprintf("https://www.zhihu.com/question/%s/answer/%s", qid, zop.ItemID.String())
					return url, title
				}
			}
		}
	}

	// 方式3: 兜底 - 只有问题链接，拼不出 answerId
	if meta, err := item.Element(`meta[itemprop="url"]`); err == nil {
		if content, err := meta.Attribute("content"); err == nil && content != nil {
			url := normalizeZhihuURL(*content)
			if url != "" {
				return url, title
			}
		}
	}

	return "", ""
}

// extractQuestionID 从回答卡片中提取问题 ID。
func (a *FetchUserAnswersAction) extractQuestionID(item *rod.Element) string {
	// 从标题链接 href 中提取 questionId
	if el, err := item.Element(`h2.ContentItem-title a[href]`); err == nil {
		if href, err := el.Attribute("href"); err == nil && href != nil {
			h := *href
			// 链接格式: /question/12345 或 https://www.zhihu.com/question/12345
			if idx := strings.Index(h, "/question/"); idx >= 0 {
				rest := h[idx+len("/question/"):]
				// 取到下一个 / 或结尾
				if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
					return rest[:slashIdx]
				}
				// 去除查询参数
				if qIdx := strings.Index(rest, "?"); qIdx >= 0 {
					return rest[:qIdx]
				}
				return rest
			}
		}
	}

	// 从 meta[itemprop="url"] 中提取
	if meta, err := item.Element(`meta[itemprop="url"]`); err == nil {
		if content, err := meta.Attribute("content"); err == nil && content != nil {
			url := *content
			if idx := strings.Index(url, "/question/"); idx >= 0 {
				rest := url[idx+len("/question/"):]
				if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
					return rest[:slashIdx]
				}
				return rest
			}
		}
	}

	return ""
}

// extractTitle 从回答卡片中提取问题标题。
func (a *FetchUserAnswersAction) extractTitle(item *rod.Element) string {
	// 尝试多种选择器获取标题
	selectors := []string{
		`h2.ContentItem-title a`,
		`h2 a`,
		`.ContentItem-title`,
	}
	for _, sel := range selectors {
		if el, err := item.Element(sel); err == nil {
			if text, err := el.Text(); err == nil && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

// cleanText 去除文本中的零宽字符、多余换行和空白。
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\u200b", "")
	s = strings.ReplaceAll(s, "\u200c", "")
	s = strings.ReplaceAll(s, "\u200d", "")
	s = strings.ReplaceAll(s, "\ufeff", "")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return strings.TrimSpace(s)
}

// normalizeZhihuURL 将知乎相对链接转换为绝对链接。
func normalizeZhihuURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "/") {
		return "https://www.zhihu.com" + raw
	}
	return raw
}

// SaveToFile 将回答列表保存为 Markdown 文件。
//
// Args:
//
//	baseDir: 项目根目录
//
// Returns:
//
//	string: 保存的文件路径
//	error: 错误信息
func (r *UserAnswersResult) SaveToFile(baseDir string) (string, error) {
	if len(r.Answers) == 0 {
		return "", fmt.Errorf("没有可保存的回答")
	}

	now := time.Now()
	dateDir := fmt.Sprintf("%02d-%d-%02d", now.Year()%100, int(now.Month()), now.Day())
	fileName := fmt.Sprintf("%02d-%02d-%02d.md", now.Hour(), now.Minute(), now.Second())

	dirPath := filepath.Join(baseDir, defaultDownloadDir, dateDir)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return "", fmt.Errorf("创建目录失败: %w", err)
	}

	filePath := filepath.Join(dirPath, fileName)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s 的知乎回答\n\n", r.UserName))
	sb.WriteString(fmt.Sprintf("**主页**: %s\n", r.UserURL))
	sb.WriteString(fmt.Sprintf("**收录数量**: %d\n\n", r.Total))
	sb.WriteString("---\n\n")

	for i, ans := range r.Answers {
		sb.WriteString(fmt.Sprintf("%d. [%s](%s)\n", i+1, ans.Title, ans.URL))
	}

	if err := os.WriteFile(filePath, []byte(sb.String()), 0644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}

	logrus.Infof("回答列表已保存到: %s", filePath)
	r.SavedPath = filePath
	return filePath, nil
}
