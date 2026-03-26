#!/usr/bin/env python3
"""批量下载小红书图文笔记并按考公分类体系归档"""

import json
import os
import re
import sys
import time
import urllib.request
from pathlib import Path

# ==================== 配置 ====================

# 数据源
RAW_DATA_PATH = "/tmp/xhs_profile_raw.json"

# 输出路径
BASE_DIR = Path("/Users/devin/workspace/xiaohongshu_zhihu_mcp/download_files/xhs_note")

# MCP API
API_URL = "http://localhost:18060/api/v1/feeds/detail"
API_TIMEOUT = 60
REQUEST_INTERVAL = 2  # 每次 API 请求间隔秒数

# 进度文件
PROGRESS_FILE = BASE_DIR / "progress.json"

# ==================== 分类规则 ====================
# 按优先级顺序匹配，首个命中即归类
CATEGORIES = [
    ("行测/言语理解", ["言语", "逻辑填空", "张弓", "中心理解"]),
    ("行测/判断推理", [
        "图推", "类比", "逻辑判断", "花生逻辑", "刘义恒", "龙飞",
        "定义判断", "截面", "花生老师逻辑", "花生600", "花生逻辑600",
        "花生老师判断", "逻辑推理"
    ]),
    ("行测/数量关系", ["数量", "数推", "几何图形", "十字交叉"]),
    ("行测/资料分析", ["资料分析", "高照", "百化分", "混合比例", "默写表"]),
    ("行测/政治理论", ["政治理论", "党史", "科技航天", "基层治理"]),
    ("行测/常识判断", ["常识", "陈怀安"]),
    ("申论", ["申论", "公文写作", "大作文", "袁东", "行政执法"]),
    ("面试", ["面试", "老夏", "结构化"]),
]


def classify(title, desc=""):
    """根据标题+正文关键词分类"""
    text = f"{title} {desc}"
    for category, keywords in CATEGORIES:
        for kw in keywords:
            if kw in text:
                return category
    return "其他"


def sanitize_folder_name(title):
    """生成安全的文件夹名，只替换文件系统不允许的字符"""
    safe = re.sub(r'[\\/:*?"<>|\n\r\t]', '_', title)
    safe = safe.replace('｜', '_')
    safe = re.sub(r'_+', '_', safe).strip('_ .')
    return safe[:50] if safe else "untitled"


# ==================== 进度管理 ====================

def load_progress():
    """加载进度文件"""
    if PROGRESS_FILE.exists():
        with open(PROGRESS_FILE, "r") as f:
            return json.load(f)
    return {"completed": [], "failed": {}, "last_index": 0}


def save_progress(progress):
    """保存进度文件"""
    PROGRESS_FILE.parent.mkdir(parents=True, exist_ok=True)
    with open(PROGRESS_FILE, "w") as f:
        json.dump(progress, f, ensure_ascii=False, indent=2)


# ==================== API 调用 ====================

def fetch_detail(feed_id, xsec_token, retries=2):
    """调用 MCP API 获取笔记详情"""
    payload = json.dumps({
        "feed_id": feed_id,
        "xsec_token": xsec_token,
        "load_all_comments": False,
    }).encode("utf-8")

    for attempt in range(retries + 1):
        try:
            req = urllib.request.Request(
                API_URL,
                data=payload,
                headers={"Content-Type": "application/json"},
            )
            with urllib.request.urlopen(req, timeout=API_TIMEOUT) as resp:
                data = json.loads(resp.read().decode("utf-8"))
                note = data.get("data", {}).get("data", {}).get("note", {})
                if note and note.get("title"):
                    return note
                return None
        except urllib.error.HTTPError as e:
            if e.code == 500:
                return None  # token 过期，不重试
            if attempt < retries:
                time.sleep(5)
            else:
                return None
        except Exception as e:
            if attempt < retries:
                print(f"    ⚠ 请求异常: {e}, 重试 ({attempt+1}/{retries})")
                time.sleep(10)
            else:
                return None
    return None


# ==================== 下载图片 ====================

def download_images(image_list, folder):
    """下载所有图片到指定文件夹"""
    count = 0
    for i, img in enumerate(image_list, 1):
        url = img.get("urlDefault", "")
        if not url:
            continue
        ext = ".webp" if "webp" in url else ".jpg"
        path = folder / f"image_{i:02d}{ext}"
        try:
            urllib.request.urlretrieve(url, str(path))
            count += 1
        except Exception as e:
            print(f"    ⚠ 图片 {i} 下载失败: {e}")
    return count


# ==================== 保存内容 ====================

def save_content(note, category, folder, feed_id):
    """保存 content.txt"""
    interact = note.get("interactInfo", {})
    image_count = len(note.get("imageList", []))
    content = f"""标题: {note.get('title', '')}
正文:
{note.get('desc', '')}

---
feed_id: {feed_id}
分类: {category}
点赞: {interact.get('likedCount', '')}
收藏: {interact.get('collectedCount', '')}
评论: {interact.get('commentCount', '')}
图片数: {image_count}
链接: https://www.xiaohongshu.com/explore/{feed_id}
"""
    with open(folder / "content.txt", "w", encoding="utf-8") as f:
        f.write(content)


# ==================== 主流程 ====================

def main():
    # 加载原始数据
    print("📂 加载原始数据...")
    with open(RAW_DATA_PATH, "r") as f:
        raw = json.load(f)

    feeds = raw.get("data", {}).get("data", {}).get("feeds", [])

    # 去重 + 过滤图文笔记
    seen = set()
    image_notes = []
    for f in feeds:
        fid = f.get("id", "")
        nc = f.get("noteCard", {})
        if fid and fid not in seen and nc.get("type") == "normal":
            seen.add(fid)
            image_notes.append(f)

    print(f"📝 共 {len(image_notes)} 条图文笔记\n")

    # 加载进度
    progress = load_progress()
    completed = set(progress["completed"])
    failed = progress["failed"]

    # 创建分类目录
    for cat, _ in CATEGORIES:
        (BASE_DIR / cat).mkdir(parents=True, exist_ok=True)
    (BASE_DIR / "其他").mkdir(parents=True, exist_ok=True)
    (BASE_DIR / "选岗").mkdir(parents=True, exist_ok=True)

    # 统计
    total = len(image_notes)
    success_count = 0
    skip_count = 0
    fail_count = 0
    total_images = 0

    for idx, feed in enumerate(image_notes, 1):
        feed_id = feed["id"]
        xsec_token = feed.get("xsecToken", "")
        display_title = feed.get("noteCard", {}).get("displayTitle", "无标题")

        # 跳过已完成
        if feed_id in completed:
            skip_count += 1
            continue

        print(f"[{idx}/{total}] {display_title[:40]}...")

        # 调 API 获取详情
        note = fetch_detail(feed_id, xsec_token)
        if not note:
            fail_count += 1
            failed[feed_id] = f"API 失败 (title: {display_title})"
            progress["failed"] = failed
            save_progress(progress)
            print(f"    ✗ 获取详情失败，跳过")
            time.sleep(REQUEST_INTERVAL)
            continue

        # 分类
        title = note.get("title", display_title)
        desc = note.get("desc", "")
        category = classify(title, desc)

        # 创建文件夹
        folder_name = f"{idx:03d}_{sanitize_folder_name(title)}"
        folder_path = BASE_DIR / category / folder_name
        folder_path.mkdir(parents=True, exist_ok=True)

        # 保存内容
        save_content(note, category, folder_path, feed_id)

        # 下载图片
        image_list = note.get("imageList", [])
        img_count = download_images(image_list, folder_path)
        total_images += img_count

        # 更新进度
        completed.add(feed_id)
        progress["completed"] = list(completed)
        progress["last_index"] = idx
        save_progress(progress)

        success_count += 1
        print(f"    ✓ [{category}] {img_count} 张图片")

        time.sleep(REQUEST_INTERVAL)

    # 汇总
    print(f"\n{'='*60}")
    print(f"📊 下载完成!")
    print(f"   成功: {success_count}")
    print(f"   跳过(已完成): {skip_count}")
    print(f"   失败: {fail_count}")
    print(f"   总图片: {total_images}")
    print(f"   输出目录: {BASE_DIR}")

    if failed:
        print(f"\n⚠ 失败列表 ({len(failed)} 条):")
        for fid, reason in failed.items():
            print(f"   {fid}: {reason}")


if __name__ == "__main__":
    main()
