package xiaohongshu

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/sirupsen/logrus"
)

// reShareURL 提取分享文本中的小红书链接
var reShareURL = regexp.MustCompile(`https://www\.xiaohongshu\.com/\S+`)

// ShareLinkResult 单个笔记的结果
type ShareLinkResult struct {
	FeedID   string `json:"feed_id"`
	Title    string `json:"title"`
	NoteType string `json:"note_type"` // normal(图文) / video
	ShareURL string `json:"share_url"`
	VideoURL string `json:"video_url,omitempty"` // 视频笔记的真实 mp4 链接
	Error    string `json:"error,omitempty"`     // 非空表示获取失败
}

// ShareLinkAction 负责从用户主页获取所有笔记的分享链接
type ShareLinkAction struct {
	page *rod.Page
}

// NewShareLinkAction 创建 ShareLinkAction
func NewShareLinkAction(page *rod.Page) *ShareLinkAction {
	return &ShareLinkAction{page: page}
}

// injectClipboardInterceptor 注入 clipboard 拦截器的 JS。
// 同时覆盖两种复制方式：
// 1. navigator.clipboard.writeText（现代 API）
// 2. MutationObserver 监听临时 textarea（execCommand('copy') 方式，XHS 实际使用的）
const injectClipboardInterceptor = `() => {
	window.__capturedClipboard = null;

	// 方式1：拦截 navigator.clipboard.writeText
	if (navigator.clipboard && navigator.clipboard.writeText) {
		const origWrite = navigator.clipboard.writeText.bind(navigator.clipboard);
		navigator.clipboard.writeText = function(text) {
			window.__capturedClipboard = text;
			return origWrite(text);
		};
	}

	// 方式2：拦截 execCommand('copy')，监听临时 textarea/input 的创建
	const origExec = document.execCommand.bind(document);
	document.execCommand = function(cmd, showUI, val) {
		if (cmd === 'copy' || cmd === 'cut') {
			const active = document.activeElement;
			if (active && (active.tagName === 'TEXTAREA' || active.tagName === 'INPUT')) {
				const v = active.value || active.innerText || '';
				if (v) window.__capturedClipboard = v;
			}
		}
		return origExec(cmd, showUI, val);
	};

	// 方式3：MutationObserver 监听隐藏 textarea 被添加到 body（XHS 通常的做法）
	if (window.__clipboardObserver) window.__clipboardObserver.disconnect();
	const observer = new MutationObserver(mutations => {
		for (const mut of mutations) {
			for (const node of mut.addedNodes) {
				if (!node || !node.tagName) continue;
				const tag = node.tagName;
				if (tag === 'TEXTAREA' || tag === 'INPUT') {
					const v = node.value || '';
					if (v && (v.includes('xiaohongshu') || v.includes('xhslink') || v.includes('xsec'))) {
						window.__capturedClipboard = v;
					}
				}
			}
		}
	});
	observer.observe(document.body, {childList: true, subtree: true});
	window.__clipboardObserver = observer;
}`

// GetUserAllShareLinks 获取指定用户主页上所有笔记的分享链接。
// 逐个点击笔记卡片，等待详情页加载，点击分享按钮，读取 clipboard 拦截结果。
// 单个笔记失败时记录错误但继续处理其余笔记。
func (a *ShareLinkAction) GetUserAllShareLinks(ctx context.Context, userID, xsecToken string, maxScrollCount int) ([]ShareLinkResult, error) {
	page := a.page.Context(ctx).Timeout(10 * time.Minute)

	// 1. 访问用户主页
	profileURL := makeUserProfileURL(userID, xsecToken)
	logrus.Infof("share_link: 访问用户主页 %s", userID)
	page.MustNavigate(profileURL)
	page.MustWaitDOMStable()
	time.Sleep(time.Duration(800+rand.Intn(400)) * time.Millisecond)

	// 2. 滚动加载更多笔记
	if maxScrollCount > 1 {
		scrollForNotes(page, maxScrollCount)
	}

	// 3. 收集所有笔记卡片数量
	noteCount := page.MustEval(`() => document.querySelectorAll('.note-item').length`).Int()
	logrus.Infof("share_link: 用户主页共 %d 条笔记，开始逐个获取分享链接", noteCount)

	if noteCount == 0 {
		return nil, fmt.Errorf("未找到任何笔记卡片，请确认用户主页已加载")
	}

	results := make([]ShareLinkResult, 0, noteCount)

	for i := 0; i < noteCount; i++ {
		// 重新回到用户主页（每次点击笔记后 URL 会变）
		page.MustNavigate(profileURL)
		page.MustWaitDOMStable()
		time.Sleep(time.Duration(600+rand.Intn(300)) * time.Millisecond)

		// 重新数一下（避免页面重新加载后数量变化）
		currentCount := page.MustEval(`() => document.querySelectorAll('.note-item').length`).Int()
		if i >= currentCount {
			logrus.Warnf("share_link: 第 %d 条已超出当前页面笔记数 %d，停止", i+1, currentCount)
			break
		}

		// 取第 i 个笔记的标题
		title := page.MustEval(fmt.Sprintf(`() => {
			const items = document.querySelectorAll('.note-item');
			const item = items[%d];
			if (!item) return "";
			const titleEl = item.querySelector('.title span, .title, [class*="title"]');
			return titleEl ? titleEl.innerText?.trim() : "";
		}`, i)).String()

		result := ShareLinkResult{
			Title: title,
		}

		shareURL, feedID, noteType, videoURL, err := a.getShareLinkByClickingItem(ctx, page, profileURL, i)
		result.FeedID = feedID // 无论成败都记录
		result.NoteType = noteType
		if err != nil {
			logrus.Warnf("share_link: [%d/%d] 笔记「%s」获取失败: %v", i+1, noteCount, title, err)
			result.Error = err.Error()
		} else {
			result.ShareURL = shareURL
			result.VideoURL = videoURL
		}

		results = append(results, result)

		// 每隔5条额外等待，降低反爬风险
		if (i+1)%5 == 0 {
			extra := time.Duration(1000+rand.Intn(1000)) * time.Millisecond
			logrus.Infof("share_link: 已处理 %d/%d，短暂休息 %v", i+1, noteCount, extra)
			time.Sleep(extra)
		}
	}

	successCount := 0
	for _, r := range results {
		if r.Error == "" {
			successCount++
		}
	}
	logrus.Infof("share_link: 完成，成功 %d/%d", successCount, len(results))

	return results, nil
}

// getShareLinkByClickingItem 点击用户主页第 idx 个笔记卡片，
// 等待详情页加载，获取分享链接；若为视频笔记同时提取视频真实 URL。
func (a *ShareLinkAction) getShareLinkByClickingItem(ctx context.Context, page *rod.Page, profileURL string, idx int) (shareURL, feedID, noteType, videoURL string, err error) {
	// 点击第 idx 个笔记卡片
	noteItems, err := page.Elements(".note-item")
	if err != nil || idx >= len(noteItems) {
		return "", "", "", "", fmt.Errorf("获取第 %d 个笔记卡片失败", idx)
	}

	item := noteItems[idx]
	item.MustEval(`() => { this.scrollIntoView({behavior:'smooth', block:'center'}); }`)
	time.Sleep(time.Duration(300+rand.Intn(200)) * time.Millisecond)

	if err := item.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return "", "", "", "", fmt.Errorf("点击笔记卡片失败: %w", err)
	}

	// 等待 URL 跳转到 /explore/{id}
	var noteURL string
	for j := 0; j < 30; j++ {
		time.Sleep(200 * time.Millisecond)
		curr := page.MustInfo().URL
		if isNoteDetailPage(curr) {
			noteURL = curr
			break
		}
	}
	if noteURL == "" {
		return "", "", "", "", fmt.Errorf("等待笔记详情页超时，当前 URL: %s", page.MustInfo().URL)
	}

	// 从 URL 提取 feedID
	feedID = extractNoteIDFromURL(noteURL)
	logrus.Infof("share_link: [%d] 笔记 %s 已加载", idx+1, feedID)

	// 等待页面 DOM 稳定
	page.MustWaitDOMStable()
	time.Sleep(time.Duration(500+rand.Intn(300)) * time.Millisecond)

	// 判断笔记类型并提取视频 URL
	noteType, videoURL = extractVideoURL(page)

	// 注入 clipboard 拦截器（必须在本页面注入）
	page.MustEval(injectClipboardInterceptor)

	// 找分享按钮 div.share-wrapper
	shareBtn, findErr := page.Timeout(5 * time.Second).Element("div.share-wrapper")
	if findErr != nil {
		return "", feedID, noteType, videoURL, fmt.Errorf("未找到分享按钮 div.share-wrapper: %w", findErr)
	}

	// 点击分享按钮
	shareBtn.MustEval(`() => { this.scrollIntoView({behavior:'smooth', block:'center'}); }`)
	time.Sleep(time.Duration(200+rand.Intn(200)) * time.Millisecond)
	if err := shareBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return "", feedID, noteType, videoURL, fmt.Errorf("点击分享按钮失败: %w", err)
	}

	// 等待 clipboard 被写入
	var captured string
	for j := 0; j < 20; j++ {
		time.Sleep(200 * time.Millisecond)
		val := page.MustEval(`() => window.__capturedClipboard`).String()
		if val != "" && val != "null" && val != "<nil>" {
			captured = val
			break
		}
	}

	if captured == "" {
		return "", feedID, noteType, videoURL, fmt.Errorf("clipboard 未捕获到内容，分享按钮可能未触发复制")
	}

	// 从完整复制文本中提取 URL
	shareURL = extractShareURL(captured)
	if shareURL == "" {
		return "", feedID, noteType, videoURL, fmt.Errorf("clipboard 内容中未找到有效 URL: %s", captured[:min(len(captured), 100)])
	}

	return shareURL, feedID, noteType, videoURL, nil
}

// extractVideoURL 从笔记详情页提取视频真实 URL。
// 优先读 <video> 标签的 src，其次查 performance API 里最清晰的 mp4 链接。
// 返回 (noteType, videoURL)，noteType 为 "video" 或 "normal"。
func extractVideoURL(page *rod.Page) (noteType, videoURL string) {
	result := page.MustEval(`() => {
		// 先读 <video> 标签
		const vid = document.querySelector('video');
		if (vid) {
			const src = vid.currentSrc || vid.src || vid.getAttribute('src') || '';
			if (src) return JSON.stringify({type:'video', url: src});
		}
		// 再从 performance API 找清晰度最高的 mp4（文件名中数字最大的）
		try {
			const entries = performance.getEntriesByType('resource');
			const mp4s = entries
				.map(e => e.name)
				.filter(n => n.includes('.mp4') && (n.includes('xhscdn') || n.includes('sns-')));
			if (mp4s.length > 0) {
				// 取 URL 中清晰度数字最大的（如 _259.mp4 > _115.mp4）
				mp4s.sort((a, b) => {
					const numA = parseInt(a.match(/_(\d+)\.mp4/)?.[1] || '0');
					const numB = parseInt(b.match(/_(\d+)\.mp4/)?.[1] || '0');
					return numB - numA;
				});
				return JSON.stringify({type:'video', url: mp4s[0]});
			}
		} catch(e) {}
		return JSON.stringify({type:'normal', url: ''});
	}`).String()

	var parsed struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	// 简单解析，避免 import encoding/json
	if idx := strings.Index(result, `"type":"`); idx != -1 {
		rest := result[idx+8:]
		if end := strings.Index(rest, `"`); end != -1 {
			parsed.Type = rest[:end]
		}
	}
	if idx := strings.Index(result, `"url":"`); idx != -1 {
		rest := result[idx+7:]
		if end := strings.Index(rest, `"`); end != -1 {
			parsed.URL = rest[:end]
		}
	}

	return parsed.Type, parsed.URL
}

// extractShareURL 从小红书分享复制文本中提取 URL
func extractShareURL(text string) string {
	// 优先匹配 xiaohongshu.com 链接
	if m := reShareURL.FindString(text); m != "" {
		return strings.TrimSpace(m)
	}
	// 兜底：尝试 xhslink.com 短链
	if idx := strings.Index(text, "xhslink.com/"); idx != -1 {
		url := "https://" + text[idx:]
		// 截断到第一个空格
		if end := strings.IndexAny(url, " \t\n\r"); end != -1 {
			url = url[:end]
		}
		return strings.TrimSpace(url)
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// scrollForNotes 在用户主页滚动加载更多笔记
func scrollForNotes(page *rod.Page, maxScrollCount int) {
	noNewCount := 0
	const maxNoNew = 3

	for i := 0; i < maxScrollCount-1; i++ {
		before := page.MustEval(`() => document.querySelectorAll('.note-item').length`).Int()
		page.MustEval(`() => window.scrollBy(0, window.innerHeight * 2)`)
		time.Sleep(time.Duration(800+rand.Intn(700)) * time.Millisecond)
		after := page.MustEval(`() => document.querySelectorAll('.note-item').length`).Int()

		logrus.Infof("share_link scroll [%d/%d]: %d -> %d notes", i+1, maxScrollCount-1, before, after)

		if after <= before {
			noNewCount++
			if noNewCount >= maxNoNew {
				logrus.Infof("share_link: 连续 %d 次无新笔记，停止滚动（共 %d 条）", maxNoNew, after)
				break
			}
			time.Sleep(time.Duration(1000+rand.Intn(500)) * time.Millisecond)
		} else {
			noNewCount = 0
		}
	}
}
