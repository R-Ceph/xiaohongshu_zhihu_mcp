package xiaohongshu

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/go-rod/rod"
	"github.com/sirupsen/logrus"
)

type UserProfileAction struct {
	page *rod.Page
}

func NewUserProfileAction(page *rod.Page) *UserProfileAction {
	pp := page.Timeout(30 * time.Minute)
	return &UserProfileAction{page: pp}
}

// UserProfile 获取用户基本信息及帖子
func (u *UserProfileAction) UserProfile(ctx context.Context, userID, xsecToken string, maxScrollCount int) (*UserProfileResponse, error) {
	page := u.page.Context(ctx)

	searchURL := makeUserProfileURL(userID, xsecToken)
	page.MustNavigate(searchURL)
	page.MustWaitStable()

	// 范围校验
	if maxScrollCount < 1 {
		maxScrollCount = 1
	}
	if maxScrollCount > 500 {
		maxScrollCount = 500
	}

	// 执行滚动加载
	if maxScrollCount > 1 {
		u.scrollAndLoadNotes(page, maxScrollCount)
	}

	return u.extractUserProfileData(page)
}

// scrollAndLoadNotes 通过模拟滚动触发懒加载，获取更多笔记
// maxScrollCount 为总滚动次数（含初始加载，实际滚动 maxScrollCount-1 次）
func (u *UserProfileAction) scrollAndLoadNotes(page *rod.Page, maxScrollCount int) {
	const jsCountNotes = `() => {
		if (window.__INITIAL_STATE__ && window.__INITIAL_STATE__.user && window.__INITIAL_STATE__.user.notes) {
			const notes = window.__INITIAL_STATE__.user.notes;
			const data = notes.value !== undefined ? notes.value : notes._value;
			if (data && Array.isArray(data)) {
				let total = 0;
				for (let arr of data) {
					if (Array.isArray(arr)) total += arr.length;
				}
				return total.toString();
			}
		}
		return "0";
	}`

	// 连续无新内容计数器，连续3次无新内容才判定到底
	noNewContentCount := 0
	const maxNoNewContent = 3

	for i := 0; i < maxScrollCount-1; i++ {
		// 滚动前的笔记数量
		beforeStr := page.MustEval(jsCountNotes).String()
		beforeCount, _ := strconv.Atoi(beforeStr)

		// 模拟滚动
		page.MustEval(`() => { window.scrollBy(0, window.innerHeight * 2); }`)

		// 随机等待，给懒加载时间响应
		time.Sleep(time.Duration(800+rand.Intn(700)) * time.Millisecond)

		// 滚动后的笔记数量
		afterStr := page.MustEval(jsCountNotes).String()
		afterCount, _ := strconv.Atoi(afterStr)

		logrus.Infof("user_profile scroll [%d/%d]: %d -> %d notes", i+1, maxScrollCount-1, beforeCount, afterCount)

		// 检测是否有新内容
		if afterCount <= beforeCount {
			noNewContentCount++
			logrus.Infof("user_profile scroll: no new notes (%d/%d consecutive)", noNewContentCount, maxNoNewContent)
			if noNewContentCount >= maxNoNewContent {
				logrus.Infof("user_profile scroll: 连续%d次无新内容，判定已到底，停止滚动 (共%d条)", maxNoNewContent, afterCount)
				break
			}
			// 多等一会儿再重试
			time.Sleep(time.Duration(1000+rand.Intn(500)) * time.Millisecond)
		} else {
			noNewContentCount = 0
			// 正常等待
			time.Sleep(time.Duration(200+rand.Intn(300)) * time.Millisecond)
		}

		// 每50次滚动多休息一下，防止反爬
		if (i+1)%50 == 0 {
			logrus.Infof("user_profile scroll: 已滚动%d次，短暂休息...", i+1)
			time.Sleep(time.Duration(2000+rand.Intn(1000)) * time.Millisecond)
		}
	}
}

// extractUserProfileData 从页面中提取用户资料数据的通用方法
func (u *UserProfileAction) extractUserProfileData(page *rod.Page) (*UserProfileResponse, error) {
	page.MustWait(`() => window.__INITIAL_STATE__ !== undefined`)

	userDataResult := page.MustEval(`() => {
		if (window.__INITIAL_STATE__ &&
		    window.__INITIAL_STATE__.user &&
		    window.__INITIAL_STATE__.user.userPageData) {
			const userPageData = window.__INITIAL_STATE__.user.userPageData;
			const data = userPageData.value !== undefined ? userPageData.value : userPageData._value;
			if (data) {
				return JSON.stringify(data);
			}
		}
		return "";
	}`).String()

	if userDataResult == "" {
		return nil, fmt.Errorf("user.userPageData.value not found in __INITIAL_STATE__")
	}

	// 2. 获取用户帖子：window.__INITIAL_STATE__.user.notes.value
	notesResult := page.MustEval(`() => {
		if (window.__INITIAL_STATE__ &&
		    window.__INITIAL_STATE__.user &&
		    window.__INITIAL_STATE__.user.notes) {
			const notes = window.__INITIAL_STATE__.user.notes;
			// 优先使用 value（getter），如果不存在则使用 _value（内部字段）
			const data = notes.value !== undefined ? notes.value : notes._value;
			if (data) {
				return JSON.stringify(data);
			}
		}
		return "";
	}`).String()

	if notesResult == "" {
		return nil, fmt.Errorf("user.notes.value not found in __INITIAL_STATE__")
	}

	// 解析用户信息
	var userPageData struct {
		Interactions []UserInteractions `json:"interactions"`
		BasicInfo    UserBasicInfo      `json:"basicInfo"`
	}
	if err := json.Unmarshal([]byte(userDataResult), &userPageData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal userPageData: %w", err)
	}

	// 解析帖子数据（帖子为双重数组）
	var notesFeeds [][]Feed
	if err := json.Unmarshal([]byte(notesResult), &notesFeeds); err != nil {
		return nil, fmt.Errorf("failed to unmarshal notes: %w", err)
	}

	// 组装响应
	response := &UserProfileResponse{
		UserBasicInfo: userPageData.BasicInfo,
		Interactions:  userPageData.Interactions,
	}

	// 添加用户帖子（展平双重数组）
	for _, feeds := range notesFeeds {
		if len(feeds) != 0 {
			response.Feeds = append(response.Feeds, feeds...)
		}
	}

	return response, nil
}

func makeUserProfileURL(userID, xsecToken string) string {
	return fmt.Sprintf("https://www.xiaohongshu.com/user/profile/%s?xsec_token=%s&xsec_source=pc_note", userID, xsecToken)
}

func (u *UserProfileAction) GetMyProfileViaSidebar(ctx context.Context) (*UserProfileResponse, error) {
	page := u.page.Context(ctx)

	// 创建导航动作
	navigate := NewNavigate(page)

	// 通过侧边栏导航到个人主页
	if err := navigate.ToProfilePage(ctx); err != nil {
		return nil, fmt.Errorf("failed to navigate to profile page via sidebar: %w", err)
	}

	// 等待页面加载完成并获取 __INITIAL_STATE__
	page.MustWaitStable()

	return u.extractUserProfileData(page)
}
