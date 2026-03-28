---
name: xhs-download
description: 下载小红书博主主页所有笔记（图文+视频）。当用户给出小红书博主主页链接（xhslink.com 短链或 xiaohongshu.com 完整链接）并要求下载、保存、抓取该博主的笔记内容时触发此 skill。
allowed-tools: Bash(python3:*), Bash(curl:*), Bash(lsof:*), Bash(ls:*), Bash(cat:*), Bash(kill:*)
---

# 小红书博主笔记批量下载

下载指定小红书博主主页的所有笔记，自动分为图文笔记和视频笔记分别保存。

## 前置条件

1. MCP 服务（xiaohongshu-mcp）需要在 `:18060` 端口运行
2. 需要已登录小红书（有有效的 cookies）

检查并启动服务：

```bash
# 检查服务是否运行
lsof -i :18060

# 如果没运行，在项目目录启动
cd /Users/devin/workspace/xiaohongshu_zhihu_mcp && ./xiaohongshu-mcp &
```

## 核心流程

### Step 1: 解析博主链接

支持的链接格式：
- 短链：`https://xhslink.com/m/xxxxx`
- 完整链接：`https://www.xiaohongshu.com/user/profile/{userId}?xsec_token=xxx`

短链需要 curl 跟随重定向获取真实 URL：

```bash
curl -sL -o /dev/null -w '%{url_effective}' 'https://xhslink.com/m/xxxxx'
```

从重定向后的 URL 中提取 `user_id` 和 `xsec_token`。

### Step 2: 加载博主全部笔记

调用 MCP API 获取用户主页，设置足够大的 `max_scroll_count`：

```bash
curl -s -X POST http://localhost:18060/api/v1/user/profile \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"xxx","xsec_token":"xxx","max_scroll_count":300}'
```

服务会自动滚动加载，连续 3 次无新内容时判定到底。

### Step 3: 运行下载脚本

```bash
python3 /Users/devin/workspace/xiaohongshu_zhihu_mcp/.claude/skills/xhs-download/download_blogger.py \
  --url "博主链接" \
  --output "/Users/devin/workspace/xiaohongshu_zhihu_mcp/download_files/xhs_note"
```

### Step 4: 检查结果

脚本会打印进度和最终统计。如有失败项，可重新运行脚本（支持断点续传，已完成的会自动跳过）。

## 输出目录结构

```
download_files/xhs_note/{博主昵称}/
├── 图文/
│   ├── 001_笔记标题/
│   │   ├── content.txt          # 标题 + 正文 + 元数据
│   │   ├── image_01.webp        # 图片
│   │   └── image_02.webp
│   └── ...
├── 视频/
│   ├── 001_笔记标题/
│   │   ├── content.txt          # 标题 + 正文 + 元数据 + 播放链接
│   │   └── cover.webp           # 封面图
│   └── ...
├── video_links.txt              # 所有视频链接汇总
└── progress.json                # 断点续传进度文件
```

## xsec_token 失败回退策略

小红书的 xsec_token 与来源路径绑定。当 `feeds/detail`（explore 路径）失败时，自动构造 `discovery/item` 路径 URL 调用 `fetch_note_by_url` 重试。两种路径的 token 校验逻辑不同，回退可以提高成功率。

## 断点续传

- `progress.json` 记录已完成和失败的笔记 ID
- 重新运行脚本时自动跳过已完成项
- 清除 `progress.json` 中的 `failed` 字段后重跑可重试失败项
