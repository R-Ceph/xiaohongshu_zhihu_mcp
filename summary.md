# xiaohongshu_zhihu_mcp 项目总结

## 1. 项目概述

**xiaohongshu_zhihu_mcp** 是一个基于 Go 语言开发的 MCP（Model Context Protocol）服务器，通过浏览器自动化技术（go-rod + Chrome），为 AI 客户端提供操作小红书（Xiaohongshu）和知乎（Zhihu）的能力。

- **服务端口**：`18060`
- **MCP 端点**：`http://localhost:18060/mcp`
- **健康检查**：`GET http://localhost:18060/health`
- **协议**：MCP Streamable HTTP + REST API 双协议支持
- **登录方式**：扫描二维码（Cookie 持久化，无需重复登录）

---

## 2. 项目目录结构

```
xiaohongshu_zhihu_mcp/
├── main.go                    # 程序入口，解析命令行参数，初始化并启动服务
├── app_server.go              # AppServer 结构体，管理 HTTP 服务器与 MCP 服务器
├── mcp_server.go              # MCP 服务器初始化，注册全部 19 个 MCP 工具
├── mcp_handlers.go            # MCP 工具处理逻辑实现
├── handlers_api.go            # REST HTTP API 处理函数
├── routes.go                  # HTTP 路由注册（Gin 框架）
├── types.go                   # 通用请求/响应数据结构
├── service.go                 # 小红书业务服务层（XiaohongshuService）
├── service_zhihu.go           # 知乎业务服务层（ZhihuService）
├── middleware.go              # HTTP 中间件（CORS、统一错误处理）
│
├── xiaohongshu/               # 小红书功能模块（核心实现）
│   ├── types.go               # 小红书专用数据结构（Feed、Comment、User 等）
│   ├── login.go               # 扫码登录实现
│   ├── publish.go             # 图文发布实现
│   ├── publish_video.go       # 视频发布实现
│   ├── search.go              # 关键词搜索实现
│   ├── feeds.go               # 首页推荐 Feed 列表获取
│   ├── feed_detail.go         # 笔记详情及评论加载（含 fetch_note_by_url 逻辑）
│   ├── user_profile.go        # 用户主页信息获取
│   ├── comment_feed.go        # 发表评论、回复评论
│   ├── like_favorite.go       # 点赞、收藏操作
│   ├── share_link.go          # 用户主页批量获取分享链接 + 视频真实 URL（新增）
│   └── navigate.go            # 页面导航辅助函数
│
├── zhihu/                     # 知乎功能模块
│   ├── login.go               # 知乎扫码登录
│   ├── page.go                # 页面内容抓取（含发布时间、赞同数提取）
│   └── user_answers.go        # 用户回答列表获取（含发布时间、赞同数）
│
├── browser/
│   └── browser.go             # 浏览器实例封装（go-rod），自动注入 Cookie
│
├── configs/
│   ├── browser.go             # 浏览器路径与无头模式配置
│   ├── image.go               # 图片下载目录配置
│   └── username.go            # 服务名称配置（xiaohongshu_zhihu_mcp）
│
├── cookies/
│   └── cookies.go             # Cookie 文件的读写、路径管理
│
├── errors/
│   └── errors.go              # 自定义错误类型定义
│
├── pkg/
│   ├── downloader/            # 图片下载器（支持 HTTP 链接转本地文件）
│   └── xhsutil/               # 小红书工具函数（文字处理等）
│
├── cmd/
│   └── login/main.go          # 独立登录工具（交互式扫码登录）
│
├── docker/                    # Docker 部署配置
├── deploy/                    # 部署相关脚本
├── examples/                  # 客户端集成示例（n8n、Cursor、VSCode 等）
└── docs/                      # 文档（Windows 安装指南等）
```

---

## 3. 主要代码文件说明

| 文件 | 作用 |
|------|------|
| `main.go` | 解析 `-headless`、`-bin`、`-port` 参数，设置配置，启动服务 |
| `app_server.go` | 聚合 XiaohongshuService、ZhihuService、MCP Server、HTTP Server |
| `mcp_server.go` | 注册 20 个 MCP 工具，定义参数结构体（Args），设置工具描述和 Schema |
| `mcp_handlers.go` | 每个 MCP 工具的具体处理函数，调用 service 层执行业务逻辑 |
| `handlers_api.go` | REST API 处理函数，供 HTTP 直接调用（非 MCP 场景） |
| `routes.go` | 注册 `/mcp`、`/health`、`/api/v1/...` 路由 |
| `service.go` | 小红书业务封装：创建浏览器实例、加载 Cookie、调用 xiaohongshu 子包 |
| `service_zhihu.go` | 知乎业务封装：创建浏览器实例、调用 zhihu 子包 |
| `xiaohongshu/feed_detail.go` | 核心：笔记详情抓取、评论滚动加载、URL 直取笔记（含 xsec_token 自动处理）、评论排序 |
| `xiaohongshu/share_link.go` | 新增：批量获取用户主页所有笔记的分享链接，视频笔记同时提取真实 mp4 URL |
| `xiaohongshu/publish.go` | 模拟浏览器操作发布图文，支持本地图片和 HTTP 图片、定时发布 |
| `zhihu/page.go` | 抓取知乎文章/回答/问题页，提取 Markdown 内容、发布时间、赞同数 |

---

## 4. MCP 工具列表（共 20 个）

### 4.1 小红书工具（14 个）

#### 登录管理（3 个）

| 工具名 | 描述 | 参数 |
|--------|------|------|
| `check_login_status` | 检查小红书登录状态，返回是否已登录 | 无 |
| `get_login_qrcode` | 获取登录二维码（Base64 图片），用于扫码登录 | 无 |
| `delete_cookies` | 删除 cookies 文件，重置登录状态 | 无 |

#### 内容发布（2 个）

| 工具名 | 描述 | 主要参数 |
|--------|------|----------|
| `publish_content` | 发布图文内容到小红书 | `title`（≤20字）、`content`（≤1000字）、`images`（本地路径或 HTTP 链接）、`tags`（可选）、`schedule_at`（可选，定时发布） |
| `publish_with_video` | 发布视频内容到小红书 | `title`、`content`、`video`（仅本地路径）、`tags`（可选）、`schedule_at`（可选） |

#### 内容浏览（4 个）

| 工具名 | 描述 | 主要参数 |
|--------|------|----------|
| `list_feeds` | 获取首页推荐 Feed 列表 | 无 |
| `search_feeds` | 按关键词搜索笔记 | `keyword`、`filters`（可选：排序/类型/时间/范围/位置） |
| `get_feed_detail` | 获取笔记详情（含评论） | `feed_id`、`xsec_token`、`load_all_comments`、`limit`、`click_more_replies`、`scroll_speed` |
| `fetch_note_by_url` | 直接通过链接获取笔记详情 | `url`（完整链接/短链接/分享链接）、`load_all_comments`、`max_comment_items`、`sort_by_likes` |

#### 社交互动（4 个）

| 工具名 | 描述 | 主要参数 |
|--------|------|----------|
| `post_comment_to_feed` | 发表评论到笔记 | `feed_id`、`xsec_token`、`content` |
| `reply_comment_in_feed` | 回复笔记下的指定评论 | `feed_id`、`xsec_token`、`comment_id` 或 `user_id`、`content` |
| `like_feed` | 点赞 / 取消点赞笔记 | `feed_id`、`xsec_token`、`unlike`（可选） |
| `favorite_feed` | 收藏 / 取消收藏笔记 | `feed_id`、`xsec_token`、`unfavorite`（可选） |

#### 用户信息（2 个）

| 工具名 | 描述 | 主要参数 |
|--------|------|----------|
| `user_profile` | 获取指定用户主页（基本信息、粉丝数、笔记列表） | `user_id`、`xsec_token` |
| `get_user_share_links` | 获取指定用户主页所有笔记的分享链接，视频笔记同时返回真实 mp4 URL | `user_id`、`xsec_token`、`max_scroll_count`（可选，默认5） |

---

### 4.2 知乎工具（6 个）

| 工具名 | 描述 | 主要参数 |
|--------|------|----------|
| `zhihu_check_login_status` | 检查知乎登录状态 | 无 |
| `zhihu_get_login_qrcode` | 获取知乎登录二维码 | 无 |
| `zhihu_delete_cookies` | 删除知乎 cookies | 无 |
| `zhihu_fetch_page` | 抓取知乎页面内容，转为 Markdown（含发布时间、赞同数） | `url`（文章/回答/问题页 URL） |
| `zhihu_user_answers` | 获取知乎用户回答列表（含赞同数、发布时间） | `url`（用户回答页 URL）、`limit`（可选，默认 100） |

> 注：工具列表中 `favorite_feed` 为第 14 个工具，完整列表与 `mcp_server.go` 注册顺序一致，共 19 个。

---

## 5. REST API 路由

### 小红书 API（`/api/v1/`）

```
# 登录管理
GET    /api/v1/login/status          检查登录状态
GET    /api/v1/login/qrcode          获取登录二维码
DELETE /api/v1/login/cookies         删除 cookies

# 内容发布
POST   /api/v1/publish               发布图文内容
POST   /api/v1/publish_video         发布视频内容

# 内容获取
GET    /api/v1/feeds/list            获取首页推荐列表
GET    /api/v1/feeds/search          搜索内容（Query 参数）
POST   /api/v1/feeds/search          搜索内容（JSON Body）
POST   /api/v1/feeds/detail          获取笔记详情
POST   /api/v1/feeds/fetch_by_url    通过 URL 获取笔记详情

# 社交互动
POST   /api/v1/feeds/comment         发表评论
POST   /api/v1/feeds/comment/reply   回复评论
POST   /api/v1/feeds/like            点赞笔记
POST   /api/v1/feeds/favorite        收藏笔记

# 用户信息
GET    /api/v1/user/me               当前登录用户信息
POST   /api/v1/user/profile          指定用户主页信息
```

### 知乎 API（`/api/v1/zhihu/`）

```
GET    /api/v1/zhihu/login/status    检查知乎登录状态
GET    /api/v1/zhihu/login/qrcode    获取知乎登录二维码
DELETE /api/v1/zhihu/login/cookies   删除知乎 cookies
POST   /api/v1/zhihu/page            抓取知乎页面内容
POST   /api/v1/zhihu/user/answers    获取用户回答列表
```

### 系统端点

```
GET    /health    健康检查
POST   /mcp       MCP Streamable HTTP 端点
```

---

## 6. 启动方式

### 6.1 前置准备

**第一步：登录小红书（一次性操作，Cookie 持久保存）**

```bash
# 使用源码
go run cmd/login/main.go

# 使用预编译二进制（macOS Apple Silicon）
./xiaohongshu-login-darwin-arm64
```

扫描弹出的二维码后，Cookie 自动保存到本地 `cookies.json`，后续无需重复登录。

**第二步：启动 MCP 服务**

```bash
# 方式一：源码运行（无头模式，推荐）
ROD_BROWSER_BIN="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" go run .

# 方式二：源码运行（有界面模式，便于调试）
go run . -headless=false

# 方式三：预编译二进制（macOS）
./xiaohongshu-mcp-darwin-arm64

# 方式四：Docker（最简单，无需安装 Go 环境）
cd docker && docker compose up -d
```

### 6.2 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-headless` | `true` | 是否无头模式（不显示浏览器界面） |
| `-bin` | 空（自动查找） | Chrome 浏览器二进制路径 |
| `-port` | `:18060` | HTTP 服务监听端口 |

**示例：**
```bash
./xiaohongshu-mcp -headless=false -port=:8080
```

### 6.3 浏览器路径配置

Chrome 路径优先级：`-bin` 参数 > `ROD_BROWSER_BIN` 环境变量 > go-rod 自动下载（~150MB）

```bash
# macOS
export ROD_BROWSER_BIN="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

# Linux / Docker 环境
export ROD_BROWSER_BIN=/usr/local/bin/google-chrome
```

### 6.4 验证服务

```bash
# 健康检查
curl http://localhost:18060/health

# MCP 协议验证
npx @modelcontextprotocol/inspector
# 浏览器打开后输入：http://localhost:18060/mcp，点击 Connect
```

---

## 7. MCP 客户端接入

### Claude Code CLI

```bash
claude mcp add --transport http xiaohongshu-mcp http://localhost:18060/mcp
claude mcp list
```

### Cursor

在项目根目录创建 `.cursor/mcp.json`：

```json
{
  "mcpServers": {
    "xiaohongshu-mcp": {
      "url": "http://localhost:18060/mcp"
    }
  }
}
```

### VSCode

在项目根目录创建 `.vscode/mcp.json`：

```json
{
  "servers": {
    "xiaohongshu-mcp": {
      "url": "http://localhost:18060/mcp",
      "type": "http"
    }
  }
}
```

### Cline

在 Cline MCP 设置中添加：

```json
{
  "xiaohongshu-mcp": {
    "url": "http://localhost:18060/mcp",
    "type": "streamableHttp"
  }
}
```

### Google Gemini CLI

在 `~/.gemini/settings.json` 中添加：

```json
{
  "mcpServers": {
    "xiaohongshu": {
      "httpUrl": "http://localhost:18060/mcp",
      "timeout": 30000
    }
  }
}
```

> **Docker 环境注意**：请将 `localhost` 替换为 `host.docker.internal`

---

## 8. 典型使用流程示例

### 搜索并获取笔记详情

```
1. 调用 search_feeds，keyword="美食"
   → 获得 feed 列表，每条包含 id 和 xsecToken

2. 调用 get_feed_detail，feed_id="xxx", xsec_token="xxx"
   → 获得笔记正文、图片、点赞/收藏/评论数、评论列表

3. 调用 post_comment_to_feed 发表评论
```

### 通过链接直接获取笔记

```
调用 fetch_note_by_url，url="https://www.xiaohongshu.com/explore/{noteID}?xsec_token=xxx"
→ 自动加载评论并按点赞数排序，返回完整笔记数据

注意：链接中必须包含 xsec_token（从 App 分享链接中获取）
```

### 发布图文内容

```
调用 publish_content：
- title: "春天来了"（不超过20字）
- content: "正文内容..."（不超过1000字）
- images: ["/Users/me/photo.jpg"]
- tags: ["春天", "生活"]
```

### 抓取知乎内容

```
调用 zhihu_fetch_page，url="https://zhuanlan.zhihu.com/p/xxxxxx"
→ 返回 Markdown 格式正文，包含发布时间和赞同数

调用 zhihu_user_answers，url="https://www.zhihu.com/people/xxx/answers"
→ 返回该用户所有回答列表，含赞同数和发布时间
```

### 获取用户所有笔记的分享链接与视频 URL

```
调用 get_user_share_links：
- user_id: "64bc0408000000001403f4b6"（从 user_profile 或主页 URL 获取）
- xsec_token: "ABoSOj..."（从 list_feeds / search_feeds 结果获取）
- max_scroll_count: 5（可选，控制滚动加载笔记数量）

→ 返回每条笔记的：
  - feed_id：笔记 ID
  - title：笔记标题
  - note_type：笔记类型（"normal" 图文 / "video" 视频）
  - share_url：完整分享链接（/discovery/item/...?xsec_token=...）
  - video_url：视频笔记的真实 mp4 CDN 链接（图文笔记无此字段）

注意：
1. 每条笔记约耗时 8-12 秒（需打开详情页、点击分享按钮）
2. 视频 CDN 链接（sns-video-bd.xhscdn.com / sns-bak-*.xhscdn.com）
   Cache-Control: max-age=2592000（30天缓存），URL 本身无时效参数，
   只要博主未删除笔记链接长期有效
3. 分享链接（share_url）的 xsec_token 绑定 discovery/item 路径，
   与 explore 路径 token 不通用
```

---

## 9. 主要依赖

| 依赖 | 版本 | 用途 |
|------|------|------|
| `github.com/go-rod/rod` | v0.116.2 | 浏览器自动化（Chrome DevTools Protocol） |
| `github.com/gin-gonic/gin` | v1.10.1 | HTTP Web 框架 |
| `github.com/modelcontextprotocol/go-sdk` | v0.7.0 | MCP 协议官方 Go SDK |
| `github.com/sirupsen/logrus` | - | 结构化日志 |
| `github.com/avast/retry-go/v4` | - | 重试逻辑封装 |
| `github.com/xpzouying/headless_browser` | v0.2.0 | 无头浏览器封装层 |

---

## 10. 注意事项

1. **同一账号不能多端网页登录**：登录 MCP 后，不要再在其他网页端登录同一小红书账号，否则 Cookie 会失效。
2. **xsec_token 是访问笔记的必要参数**：`get_feed_detail`、`user_profile` 等工具需要从搜索/列表结果中获取 `xsecToken`。`fetch_note_by_url` 所用的 URL 也需要包含 `xsec_token`（从 App 分享链接中提取）。
3. **发布限制**：标题 ≤ 20 字，正文 ≤ 1000 字，每日发帖建议 ≤ 50 篇。
4. **图片推荐使用本地路径**，稳定性优于 HTTP 链接。
5. **视频仅支持本地文件路径**，不支持 HTTP 链接。
6. **`get_user_share_links` 耗时较长**：每条笔记约 8-12 秒，100 条笔记约需 15-20 分钟，建议先用小 `max_scroll_count` 测试。
7. **开发调试**命令：
   ```bash
   go test ./pkg/... && go test -run TestFilterValidation ./xiaohongshu/
   go vet ./...
   go build -o xiaohongshu-mcp .
   ```
