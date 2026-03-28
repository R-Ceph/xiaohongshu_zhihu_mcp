#!/usr/bin/env python3
"""小红书博主笔记批量下载工具

用法:
    python3 download_blogger.py --url <博主链接> [--output <输出目录>]

支持链接格式:
    - 短链: https://xhslink.com/m/xxxxx
    - 完整链接: https://www.xiaohongshu.com/user/profile/{userId}?xsec_token=xxx
"""

import argparse
import json
import os
import re
import subprocess
import sys
import time
import urllib.request
import urllib.error
import urllib.parse
from pathlib import Path

# ==================== 配置 ====================

API_BASE = "http://localhost:18060/api/v1"
API_TIMEOUT = 60
REQUEST_INTERVAL = 2
MAX_SCROLL_COUNT = 300
PROJECT_DIR = Path(__file__).resolve().parent.parent.parent.parent

# ==================== 工具函数 ====================


def sanitize_folder_name(title):
    """生成安全的文件夹名"""
    safe = re.sub(r'[\\/:*?"<>|\n\r\t]', '_', title)
    safe = safe.replace('｜', '_')
    safe = re.sub(r'_+', '_', safe).strip('_ .')
    return safe[:50] if safe else "untitled"


def api_post(path, payload, timeout=API_TIMEOUT):
    """调用 MCP API"""
    url = f"{API_BASE}{path}"
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})
    resp = urllib.request.urlopen(req, timeout=timeout)
    return json.loads(resp.read().decode("utf-8"))


# ==================== 链接解析 ====================


def resolve_short_link(url):
    """解析短链接，跟随重定向获取真实URL"""
    result = subprocess.run(
        ["curl", "-sL", "-o", "/dev/null", "-w", "%{url_effective}", url],
        capture_output=True, text=True, timeout=30
    )
    return result.stdout.strip()


def parse_profile_url(url):
    """从 URL 中提取 user_id 和 xsec_token"""
    # 如果是短链先解析
    if "xhslink.com" in url:
        url = resolve_short_link(url)
        print(f"  短链解析: {url[:80]}...")

    # 提取 user_id
    m = re.search(r'/user/profile/([a-f0-9]+)', url)
    if not m:
        raise ValueError(f"无法从 URL 提取 user_id: {url}")
    user_id = m.group(1)

    # 提取 xsec_token
    parsed = urllib.parse.urlparse(url)
    params = urllib.parse.parse_qs(parsed.query)
    xsec_token = params.get("xsec_token", [""])[0]

    return user_id, xsec_token


# ==================== MCP 服务管理 ====================


def check_mcp_service():
    """检查 MCP 服务是否运行"""
    try:
        req = urllib.request.Request(f"{API_BASE.replace('/api/v1', '')}/health")
        resp = urllib.request.urlopen(req, timeout=5)
        return resp.status == 200
    except Exception:
        return False


def ensure_mcp_service():
    """确保 MCP 服务运行"""
    if check_mcp_service():
        print("✓ MCP 服务已运行")
        return

    print("⏳ 启动 MCP 服务...")
    binary = PROJECT_DIR / "xiaohongshu-mcp"
    if not binary.exists():
        print(f"✗ 找不到 MCP 服务: {binary}")
        sys.exit(1)

    subprocess.Popen(
        [str(binary)],
        cwd=str(PROJECT_DIR),
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )

    for i in range(10):
        time.sleep(2)
        if check_mcp_service():
            print("✓ MCP 服务已启动")
            return

    print("✗ MCP 服务启动超时")
    sys.exit(1)


# ==================== 加载博主笔记 ====================


def load_blogger_notes(user_id, xsec_token):
    """加载博主全部笔记"""
    print(f"⏳ 加载博主笔记 (user_id={user_id[:12]}...)，请耐心等待...")

    data = api_post("/user/profile", {
        "user_id": user_id,
        "xsec_token": xsec_token,
        "max_scroll_count": MAX_SCROLL_COUNT,
    }, timeout=600)

    inner = data.get("data", {}).get("data", data.get("data", {}))
    basic_info = inner.get("userBasicInfo", {})
    feeds = inner.get("feeds", [])

    # 去重
    seen = set()
    unique = []
    for f in feeds:
        fid = f.get("id", "")
        if fid and fid not in seen:
            seen.add(fid)
            unique.append(f)

    nickname = basic_info.get("nickname", "未知博主")

    image_notes = [f for f in unique if f.get("noteCard", {}).get("type") == "normal"]
    video_notes = [f for f in unique if f.get("noteCard", {}).get("type") == "video"]

    print(f"✓ 博主: {nickname}")
    print(f"  总笔记: {len(unique)} | 图文: {len(image_notes)} | 视频: {len(video_notes)}")

    return nickname, unique, image_notes, video_notes


# ==================== 进度管理 ====================


def load_progress(progress_file):
    """加载进度"""
    if progress_file.exists():
        with open(progress_file) as f:
            return json.load(f)
    return {"completed": [], "failed": {}}


def save_progress(progress_file, progress):
    """保存进度"""
    with open(progress_file, "w") as f:
        json.dump(progress, f, ensure_ascii=False, indent=2)


# ==================== 笔记详情获取 ====================


def fetch_note_detail(feed_id, xsec_token):
    """获取笔记详情（含前20条高赞评论），失败时自动回退到 discovery/item 路径"""
    # 方式1: feeds/detail (explore 路径)
    try:
        data = api_post("/feeds/detail", {
            "feed_id": feed_id,
            "xsec_token": xsec_token,
            "load_all_comments": True,
        })
        inner = data.get("data", {}).get("data", {})
        note = inner.get("note", {})
        comments = inner.get("comments", {}).get("list", [])
        if note and note.get("title"):
            return note, comments
    except urllib.error.HTTPError:
        pass
    except Exception:
        pass

    # 方式2: fetch_note_by_url (discovery/item 路径)
    try:
        discovery_url = (
            f"https://www.xiaohongshu.com/discovery/item/{feed_id}"
            f"?xsec_token={urllib.parse.quote(xsec_token, safe='')}&xsec_source=pc_share"
        )
        data = api_post("/feeds/fetch_by_url", {
            "url": discovery_url,
            "load_all_comments": True,
            "max_comment_items": 20,
            "sort_by_likes": True,
        })
        inner = data.get("data", {}).get("data", {})
        note = inner.get("note", {})
        comments = inner.get("comments", {}).get("list", [])
        if note and note.get("title"):
            return note, comments
    except Exception:
        pass

    return None, []


# ==================== 下载函数 ====================


def download_file(url, path):
    """下载文件"""
    try:
        urllib.request.urlretrieve(url, str(path))
        return True
    except Exception:
        return False


def format_timestamp(ts):
    """将毫秒时间戳转为可读时间"""
    if not ts:
        return ""
    try:
        import datetime
        dt = datetime.datetime.fromtimestamp(ts / 1000)
        return dt.strftime("%Y-%m-%d %H:%M")
    except Exception:
        return str(ts)


def format_comments(comments, top_n=20):
    """格式化评论列表，取前 top_n 条高赞评论（含子评论）"""
    if not comments:
        return ""

    # 按点赞数排序
    def like_count(c):
        try:
            s = c.get("likeCount", "0")
            if "万" in s:
                return int(float(s.replace("万", "")) * 10000)
            return int(s)
        except (ValueError, TypeError):
            return 0

    sorted_comments = sorted(comments, key=like_count, reverse=True)[:top_n]

    lines = [f"\n{'=' * 40}", f"热门评论 (Top {min(top_n, len(comments))})", "=" * 40]

    for i, c in enumerate(sorted_comments, 1):
        user = c.get("userInfo", {})
        nickname = user.get("nickName", "") or user.get("nickname", "匿名")
        content = c.get("content", "")
        likes = c.get("likeCount", "0")
        ip = c.get("ipLocation", "")
        ct = format_timestamp(c.get("createTime", 0))

        lines.append(f"\n{i}. 【{nickname}】{f' ({ip})' if ip else ''} {ct}  👍{likes}")
        lines.append(f"   {content}")

        # 子评论
        sub_comments = c.get("subComments", [])
        if sub_comments:
            for sc in sub_comments:
                sc_user = sc.get("userInfo", {})
                sc_nick = sc_user.get("nickName", "") or sc_user.get("nickname", "匿名")
                sc_content = sc.get("content", "")
                sc_likes = sc.get("likeCount", "0")
                lines.append(f"   ↳ 【{sc_nick}】👍{sc_likes}: {sc_content}")

    return "\n".join(lines)


def process_image_note(feed, idx, output_dir, progress_file, progress):
    """处理一条图文笔记"""
    feed_id = feed["id"]
    xsec_token = feed.get("xsecToken", "")
    display_title = feed.get("noteCard", {}).get("displayTitle", "无标题")

    if feed_id in progress["completed"]:
        return "skip"

    note, comments = fetch_note_detail(feed_id, xsec_token)
    if not note:
        progress["failed"][feed_id] = f"API 失败 ({display_title})"
        save_progress(progress_file, progress)
        return "fail"

    title = note.get("title", display_title)
    desc = note.get("desc", "")
    interact = note.get("interactInfo", {})
    images = note.get("imageList", [])
    pub_time = format_timestamp(note.get("time", 0))

    # 创建文件夹
    folder_name = f"{idx:03d}_{sanitize_folder_name(title)}"
    folder = output_dir / folder_name
    folder.mkdir(parents=True, exist_ok=True)

    # 保存 content.txt
    comments_text = format_comments(comments)
    content = f"""标题: {title}
发布时间: {pub_time}
正文:
{desc}

---
feed_id: {feed_id}
点赞: {interact.get('likedCount', '')}
收藏: {interact.get('collectedCount', '')}
评论数: {interact.get('commentCount', '')}
图片数: {len(images)}
链接: https://www.xiaohongshu.com/explore/{feed_id}
{comments_text}
"""
    with open(folder / "content.txt", "w", encoding="utf-8") as f:
        f.write(content)

    # 下载图片
    img_count = 0
    for i, img in enumerate(images, 1):
        url = img.get("urlDefault", "")
        if url:
            ext = ".webp" if "webp" in url else ".jpg"
            if download_file(url, folder / f"image_{i:02d}{ext}"):
                img_count += 1

    progress["completed"].append(feed_id)
    save_progress(progress_file, progress)
    return f"ok:{img_count}"


def process_video_note(feed, idx, output_dir, progress_file, progress):
    """处理一条视频笔记（封面图 + 详情 + 评论）"""
    feed_id = feed["id"]
    xsec_token = feed.get("xsecToken", "")
    nc = feed.get("noteCard", {})
    display_title = nc.get("displayTitle", "无标题")
    cover_url = nc.get("cover", {}).get("urlDefault", "")

    if feed_id in progress["completed"]:
        return "skip"

    # 调详情 API 获取正文、发布时间、评论
    note, comments = fetch_note_detail(feed_id, xsec_token)

    folder_name = f"{idx:03d}_{sanitize_folder_name(display_title)}"
    folder = output_dir / folder_name
    folder.mkdir(parents=True, exist_ok=True)

    link = f"https://www.xiaohongshu.com/explore/{feed_id}"

    if note:
        title = note.get("title", display_title)
        desc = note.get("desc", "")
        interact = note.get("interactInfo", {})
        pub_time = format_timestamp(note.get("time", 0))
        comments_text = format_comments(comments)

        content = f"""标题: {title}
类型: 视频
发布时间: {pub_time}
正文:
{desc}

---
feed_id: {feed_id}
点赞: {interact.get('likedCount', '')}
收藏: {interact.get('collectedCount', '')}
评论数: {interact.get('commentCount', '')}
链接: {link}
{comments_text}
"""
    else:
        # API 失败时用列表数据兜底
        liked = nc.get("interactInfo", {}).get("likedCount", "0")
        content = f"""标题: {display_title}
类型: 视频

---
feed_id: {feed_id}
点赞: {liked}
链接: {link}
"""
        progress["failed"][feed_id] = f"详情获取失败，仅保存基础信息 ({display_title})"

    with open(folder / "content.txt", "w", encoding="utf-8") as f:
        f.write(content)

    # 封面图
    if cover_url:
        ext = ".webp" if "webp" in cover_url else ".jpg"
        download_file(cover_url, folder / f"cover{ext}")

    progress["completed"].append(feed_id)
    save_progress(progress_file, progress)
    return "ok" if note else "partial"


# ==================== 主流程 ====================


def main():
    parser = argparse.ArgumentParser(description="下载小红书博主全部笔记")
    parser.add_argument("--url", required=True, help="博主主页链接")
    parser.add_argument("--output", default=None, help="输出目录")
    args = parser.parse_args()

    # 默认输出目录
    if args.output:
        base_output = Path(args.output)
    else:
        base_output = PROJECT_DIR / "download_files" / "xhs_note"

    print("=" * 60)
    print("📥 小红书博主笔记批量下载")
    print("=" * 60)

    # 1. 确保 MCP 服务运行
    ensure_mcp_service()

    # 2. 解析链接
    print(f"\n📎 解析链接: {args.url[:60]}...")
    user_id, xsec_token = parse_profile_url(args.url)
    print(f"  user_id: {user_id}")

    # 3. 加载笔记
    print()
    nickname, all_feeds, image_notes, video_notes = load_blogger_notes(user_id, xsec_token)

    # 4. 创建输出目录
    blogger_dir = base_output / sanitize_folder_name(nickname)
    image_dir = blogger_dir / "图文"
    video_dir = blogger_dir / "视频"
    image_dir.mkdir(parents=True, exist_ok=True)
    video_dir.mkdir(parents=True, exist_ok=True)

    progress_file = blogger_dir / "progress.json"
    progress = load_progress(progress_file)

    # 5. 处理视频笔记（逐条调 API 获取正文+评论）
    print(f"\n🎬 处理视频笔记 ({len(video_notes)} 条)...")
    video_links = []
    vid_success = 0
    vid_skip = 0
    vid_fail = 0

    for idx, feed in enumerate(video_notes, 1):
        feed_id = feed["id"]
        display_title = feed.get("noteCard", {}).get("displayTitle", "")
        link = f"https://www.xiaohongshu.com/explore/{feed_id}"
        liked = feed.get("noteCard", {}).get("interactInfo", {}).get("likedCount", "0")
        video_links.append({"title": display_title, "link": link, "likedCount": liked, "id": feed_id})

        print(f"  [{idx}/{len(video_notes)}] {display_title[:40]}...", end=" ")

        result = process_video_note(feed, idx, video_dir, progress_file, progress)

        if result == "skip":
            vid_skip += 1
            print("跳过")
        elif result == "partial":
            vid_fail += 1
            print("⚠ 仅基础信息")
        else:
            vid_success += 1
            print("✓")

        time.sleep(REQUEST_INTERVAL)

    # 保存 video_links.txt
    with open(blogger_dir / "video_links.txt", "w", encoding="utf-8") as f:
        f.write(f"博主: {nickname} | 视频笔记: {len(video_links)} 条\n")
        f.write("=" * 60 + "\n\n")
        for i, vl in enumerate(video_links, 1):
            f.write(f'{i}. {vl["title"]}  (赞:{vl["likedCount"]})\n')
            f.write(f'   {vl["link"]}\n\n')

    save_progress(progress_file, progress)
    print(f"  视频: 成功 {vid_success} | 跳过 {vid_skip} | 部分 {vid_fail}")

    # 6. 处理图文笔记（慢，需要逐条调 API）
    print(f"\n🖼️  处理图文笔记 ({len(image_notes)} 条)...")
    img_success = 0
    img_skip = 0
    img_fail = 0
    total_images = 0

    for idx, feed in enumerate(image_notes, 1):
        display_title = feed.get("noteCard", {}).get("displayTitle", "无标题")
        print(f"  [{idx}/{len(image_notes)}] {display_title[:40]}...", end=" ")

        result = process_image_note(feed, idx, image_dir, progress_file, progress)

        if result == "skip":
            img_skip += 1
            print("跳过")
        elif result == "fail":
            img_fail += 1
            print("✗ 失败")
        else:
            count = int(result.split(":")[1])
            total_images += count
            img_success += 1
            print(f"✓ {count} 张图片")

        time.sleep(REQUEST_INTERVAL)

    # 7. 汇总
    print(f"\n{'=' * 60}")
    print(f"📊 下载完成!")
    print(f"   博主: {nickname}")
    print(f"   输出: {blogger_dir}")
    print(f"   视频: 成功 {vid_success} | 跳过 {vid_skip} | 部分 {vid_fail} (共 {len(video_links)} 条)")
    print(f"   图文: 成功 {img_success} | 跳过 {img_skip} | 失败 {img_fail}")
    print(f"   图片: 共 {total_images} 张")

    if progress.get("failed"):
        print(f"\n⚠ 失败列表 ({len(progress['failed'])} 条):")
        for fid, reason in progress["failed"].items():
            print(f"   {reason}")


if __name__ == "__main__":
    main()
