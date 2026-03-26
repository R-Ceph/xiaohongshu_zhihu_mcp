---
allowed-tools: Bash, Read, Write, Glob, Grep, Agent
description: 下载小红书博主主页所有笔记（图文+视频）。输入博主主页链接，自动下载全部笔记并按图文/视频分类保存。
---

# 小红书博主笔记批量下载

用户提供了一个小红书博主的主页链接: $ARGUMENTS

## 执行步骤

### 1. 确保 MCP 服务运行

```bash
lsof -i :18060 2>/dev/null | head -1
```

如果没有运行，启动服务：

```bash
cd /Users/devin/workspace/xiaohongshu_zhihu_mcp && ./xiaohongshu-mcp &
```

等待 2 秒后确认健康：

```bash
curl -s http://localhost:18060/health
```

### 2. 运行下载脚本

```bash
python3 /Users/devin/workspace/xiaohongshu_zhihu_mcp/.claude/skills/xhs-download/download_blogger.py \
  --url "$ARGUMENTS" \
  --output "/Users/devin/workspace/xiaohongshu_zhihu_mcp/download_files/xhs_note"
```

这个脚本会自动：
- 解析短链/完整链接获取 user_id
- 调用 MCP API 滚动加载博主全部笔记
- 视频笔记：保存封面图 + content.txt + 汇总 video_links.txt
- 图文笔记：逐条调详情 API 下载全部图片 + content.txt
- 支持断点续传（已完成的自动跳过）
- feeds/detail 失败时自动回退到 discovery/item 路径重试

### 3. 汇报结果

脚本完成后，汇报：
- 博主昵称
- 视频/图文笔记数量
- 成功/失败数
- 输出目录路径

如果有失败项，提示用户可以重新运行来重试失败的笔记。
