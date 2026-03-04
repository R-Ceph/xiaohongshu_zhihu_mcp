package zhihu

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/sirupsen/logrus"
)

// ZhihuComment 知乎单条评论
type ZhihuComment struct {
	Author     string         `json:"author"`
	Content    string         `json:"content"`
	Likes      int            `json:"likes"`
	Time       string         `json:"time,omitempty"`
	IPLocation string         `json:"ip_location,omitempty"`
	Replies    []ZhihuComment `json:"replies,omitempty"`
}

// AnswerCommentsResult 回答评论抓取结果
type AnswerCommentsResult struct {
	URL      string         `json:"url"`
	Total    int            `json:"total"`
	Comments []ZhihuComment `json:"comments"`
}

// FetchCommentsAction 知乎评论抓取操作
type FetchCommentsAction struct {
	page *rod.Page
}

// NewFetchCommentsAction 创建评论抓取 action。
//
// Args:
//
//	page: rod 浏览器页面实例
//
// Returns:
//
//	FetchCommentsAction 实例
func NewFetchCommentsAction(page *rod.Page) *FetchCommentsAction {
	return &FetchCommentsAction{page: page}
}

// FetchComments 抓取知乎回答/文章的评论，按点赞数降序排列。
//
// Args:
//
//	ctx: 上下文
//	url: 知乎回答或文章 URL
//	limit: 最多获取的评论数量
//
// Returns:
//
//	*AnswerCommentsResult: 评论列表结果
//	error: 错误信息
func (a *FetchCommentsAction) FetchComments(ctx context.Context, url string, limit int) (*AnswerCommentsResult, error) {
	if !strings.Contains(url, "zhihu.com") {
		return nil, fmt.Errorf("仅支持知乎链接，当前: %s", url)
	}

	pp := a.page.Context(ctx)

	logrus.Infof("打开知乎页面: %s", url)
	pp.MustNavigate(url).MustWaitDOMStable()
	time.Sleep(3 * time.Second)

	// 点击评论按钮展开评论弹窗
	a.openCommentModal(pp)
	time.Sleep(3 * time.Second)

	// 滚动评论弹窗加载更多
	a.scrollModalComments(pp, limit)

	// 提取评论
	comments := a.extractModalComments(pp)

	// 按点赞数降序
	sort.Slice(comments, func(i, j int) bool {
		return comments[i].Likes > comments[j].Likes
	})

	if len(comments) > limit {
		comments = comments[:limit]
	}

	logrus.Infof("共获取 %d 条评论", len(comments))

	return &AnswerCommentsResult{
		URL:      url,
		Total:    len(comments),
		Comments: comments,
	}, nil
}

// FetchCommentsFromLoadedPage 从已加载的页面直接抓取评论（跳过导航）。
// 用于 FetchPage 之后复用同一页面实例提取评论。
//
// Args:
//
//	ctx: 上下文
//	limit: 最多获取的评论数量
//
// Returns:
//
//	[]ZhihuComment: 评论列表（按点赞降序）
func (a *FetchCommentsAction) FetchCommentsFromLoadedPage(ctx context.Context, limit int) []ZhihuComment {
	pp := a.page.Context(ctx)

	a.openCommentModal(pp)
	time.Sleep(3 * time.Second)

	a.scrollModalComments(pp, limit)

	comments := a.extractModalComments(pp)

	sort.Slice(comments, func(i, j int) bool {
		return comments[i].Likes > comments[j].Likes
	})

	if len(comments) > limit {
		comments = comments[:limit]
	}

	logrus.Infof("从已加载页面获取 %d 条评论", len(comments))
	return comments
}

// openCommentModal 点击评论按钮打开评论弹窗。
func (a *FetchCommentsAction) openCommentModal(pp *rod.Page) {
	clicked := pp.MustEval(`() => {
		const btns = document.querySelectorAll('button');
		for (const btn of btns) {
			const text = (btn.textContent || '').trim();
			if (/\d+\s*条评论/.test(text)) {
				btn.click();
				return text;
			}
		}
		// 备用：查找aria-label
		for (const btn of btns) {
			const aria = btn.getAttribute('aria-label') || '';
			if (aria.includes('评论')) {
				btn.click();
				return 'aria: ' + aria;
			}
		}
		return 'not_found';
	}`).String()
	logrus.Infof("点击评论按钮: %s", clicked)
}

// scrollModalComments 滚动评论弹窗加载更多评论。
func (a *FetchCommentsAction) scrollModalComments(pp *rod.Page, limit int) {
	maxRounds := 30
	lastCount := 0
	staleRounds := 0

	for i := 0; i < maxRounds; i++ {
		count := pp.MustEval(`() => {
			return document.querySelectorAll('.CommentContent').length;
		}`).Int()

		logrus.Infof("评论加载进度: %d/%d (第%d轮)", count, limit, i+1)

		if count >= limit {
			break
		}

		if count == lastCount {
			staleRounds++
			if staleRounds >= 5 {
				logrus.Info("评论数量不再增长，停止滚动")
				break
			}
		} else {
			staleRounds = 0
		}
		lastCount = count

		// 在Modal-content中找到可滚动的容器并滚动到底部
		pp.MustEval(`() => {
			// 找到Modal-content下的可滚动容器
			const modal = document.querySelector('.Modal-content, [class*="Modal-content"]');
			if (!modal) { window.scrollBy(0, 600); return; }

			// 递归查找可滚动的子元素
			function findScrollable(el, depth) {
				if (depth > 5) return null;
				if (el.scrollHeight > el.clientHeight + 10 && el.clientHeight > 100) {
					return el;
				}
				for (const child of el.children) {
					const found = findScrollable(child, depth + 1);
					if (found) return found;
				}
				return null;
			}

			const scrollable = findScrollable(modal, 0);
			if (scrollable) {
				scrollable.scrollTop = scrollable.scrollHeight;
			} else {
				modal.scrollTop = modal.scrollHeight;
			}
		}`)
		time.Sleep(1500 * time.Millisecond)
	}
}

// extractModalComments 从评论弹窗中提取所有评论，保留根评论和子回复的嵌套关系。
// 知乎评论弹窗DOM结构:
//
//	Modal-content → 列表容器 → 评论项(itemDiv: 头像区+正文区)
//	正文区(bodyDiv): 用户名区 + CommentContent + 操作栏(时间·IP·回复·赞)
//	根评论 depth=5, 子回复 depth=6 (离 Modal-content 的层级距离)
func (a *FetchCommentsAction) extractModalComments(pp *rod.Page) []ZhihuComment {
	result := pp.MustEval(`() => {
		function clean(s) {
			return (s || '').replace(/[\u200b\u200c\u200d\ufeff]/g, '').trim();
		}

		// 计算元素到 Modal-content 的层级深度
		function getDepth(el) {
			let depth = 0;
			while (el) {
				const cls = (typeof el.className === 'string') ? el.className : '';
				if (cls.includes('Modal-content')) break;
				depth++;
				el = el.parentElement;
			}
			return depth;
		}

		const rawList = [];
		const seen = new Set();
		const contentEls = document.querySelectorAll('.CommentContent');

		for (const contentEl of contentEls) {
			const content = clean(contentEl.textContent);
			if (!content || content.length < 1) continue;

			const bodyDiv = contentEl.parentElement;
			const itemDiv = bodyDiv ? bodyDiv.parentElement : null;
			if (!itemDiv) continue;

			// 用户名
			let author = '';
			const authorLinks = itemDiv.querySelectorAll('a[href*="/people/"]');
			for (const link of authorLinks) {
				const text = clean(link.textContent);
				if (text) { author = text; break; }
			}

			const key = author + '|' + content.substring(0, 30);
			if (seen.has(key)) continue;
			seen.add(key);

			// 点赞数
			let likes = 0;
			const btns = bodyDiv.querySelectorAll('button');
			for (const btn of btns) {
				const btnText = clean(btn.textContent);
				if (btnText === '回复' || btnText === '举报' || btnText === '收起') continue;
				if (/^\d+$/.test(btnText)) { likes = parseInt(btnText); break; }
				const kMatch = btnText.match(/^(\d+\.?\d*)\s*([Kk万wW])$/);
				if (kMatch) {
					const unit = (kMatch[2] === '万' || kMatch[2] === 'w' || kMatch[2] === 'W') ? 10000 : 1000;
					likes = Math.floor(parseFloat(kMatch[1]) * unit);
					break;
				}
			}

			// 时间和IP
			let timeStr = '';
			let ipLocation = '';
			const actionBar = bodyDiv.children[bodyDiv.children.length - 1];
			if (actionBar) {
				const barText = clean(actionBar.textContent);
				const timeMatch = barText.match(/(\d{2,4}-\d{2}(?:-\d{2})?)/);
				if (timeMatch) timeStr = timeMatch[1];
				const provinces = '北京|上海|广东|浙江|江苏|四川|湖北|湖南|山东|河南|河北|福建|安徽|重庆|天津|陕西|辽宁|吉林|黑龙江|广西|云南|贵州|山西|甘肃|海南|宁夏|青海|新疆|西藏|内蒙古|香港|澳门|台湾';
				const ipMatch = barText.match(new RegExp('(' + provinces + ')'));
				if (ipMatch) ipLocation = ipMatch[1];
			}

			// 层级深度: 根评论=5, 子回复=6
			const depth = getDepth(itemDiv);

			rawList.push({
				author: author,
				content: content,
				likes: likes,
				time: timeStr,
				ip: ipLocation,
				depth: depth,
			});
		}
		return rawList;
	}`)

	// 确定根评论的 depth（取最小值）
	minDepth := 999
	for _, item := range result.Arr() {
		d := item.Get("depth").Int()
		if d < minDepth {
			minDepth = d
		}
	}

	// 按 depth 分组：minDepth 为根评论，大于 minDepth 为子回复（挂到前一个根评论）
	var comments []ZhihuComment
	for _, item := range result.Arr() {
		c := ZhihuComment{
			Author:     item.Get("author").String(),
			Content:    item.Get("content").String(),
			Likes:      item.Get("likes").Int(),
			Time:       item.Get("time").String(),
			IPLocation: item.Get("ip").String(),
		}
		depth := item.Get("depth").Int()

		if depth <= minDepth {
			// 根评论
			comments = append(comments, c)
		} else if len(comments) > 0 {
			// 子回复，追加到最近的根评论
			comments[len(comments)-1].Replies = append(comments[len(comments)-1].Replies, c)
		}
	}

	return comments
}

// parseLikeCount 解析点赞文本为数字。
func parseLikeCount(text string) int {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "赞同 ")
	text = strings.TrimPrefix(text, "赞同")
	text = strings.TrimSpace(text)

	if text == "" || text == "赞" {
		return 0
	}

	text = strings.ToUpper(text)
	if strings.HasSuffix(text, "K") {
		numStr := strings.TrimSuffix(text, "K")
		if f, err := strconv.ParseFloat(numStr, 64); err == nil {
			return int(f * 1000)
		}
	}

	n, _ := strconv.Atoi(text)
	return n
}

// truncateStr 截取字符串前n个字符。
func truncateStr(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
