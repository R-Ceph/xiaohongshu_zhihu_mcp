(() => {
  const $ = (sel) => document.querySelector(sel);
  const resultEl = $("#result");
  const healthPill = $("#health-pill");

  const API_LIST = [
    ["GET", "/health"],
    ["GET", "/api/v1/login/status"],
    ["GET", "/api/v1/login/qrcode"],
    ["DELETE", "/api/v1/login/cookies"],
    ["GET", "/api/v1/feeds/list"],
    ["POST", "/api/v1/feeds/search"],
    ["POST", "/api/v1/feeds/detail"],
    ["POST", "/api/v1/feeds/fetch_by_url"],
    ["POST", "/api/v1/user/profile"],
    ["POST", "/api/v1/user/share_links"],
    ["GET", "/api/v1/user/me"],
    ["POST", "/api/v1/publish"],
    ["POST", "/api/v1/publish_video"],
    ["POST", "/api/v1/feeds/comment"],
    ["POST", "/api/v1/feeds/comment/reply"],
    ["GET", "/api/v1/zhihu/login/status"],
    ["GET", "/api/v1/zhihu/login/qrcode"],
    ["DELETE", "/api/v1/zhihu/login/cookies"],
    ["POST", "/api/v1/zhihu/page"],
    ["POST", "/api/v1/zhihu/user/answers"],
  ];

  function showResult(data) {
    resultEl.innerHTML = `<code>${escapeHtml(
      typeof data === "string" ? data : JSON.stringify(data, null, 2)
    )}</code>`;
  }

  function escapeHtml(str) {
    return str
      .replaceAll("&", "&amp;")
      .replaceAll("<", "&lt;")
      .replaceAll(">", "&gt;");
  }

  async function api(path, options = {}) {
    const res = await fetch(path, {
      headers: { "Content-Type": "application/json", ...(options.headers || {}) },
      ...options,
    });
    let body;
    try {
      body = await res.json();
    } catch {
      body = { error: "非 JSON 响应", status: res.status };
    }
    if (!res.ok) {
      throw Object.assign(new Error(body.error || res.statusText), { body, status: res.status });
    }
    return body;
  }

  function setBusy(btn, busy) {
    if (!btn) return;
    btn.disabled = busy;
  }

  function setLoginUI(prefix, loggedIn) {
    const dot = $(`#${prefix}-status-dot`);
    const text = $(`#${prefix}-status-text`);
    if (!dot || !text) return;
    dot.classList.toggle("ok", !!loggedIn);
    dot.classList.toggle("bad", loggedIn === false);
    text.textContent = loggedIn ? "已登录" : loggedIn === false ? "未登录" : "未知";
  }

  function renderQrcode(boxId, imgData) {
    const box = $(boxId);
    if (!box) return;
    if (!imgData) {
      box.classList.add("empty");
      box.textContent = "未返回二维码";
      return;
    }
    box.classList.remove("empty");
    const src = imgData.startsWith("data:") ? imgData : `data:image/png;base64,${imgData}`;
    box.innerHTML = `<img alt="登录二维码" src="${src}" />`;
  }

  async function withAction(btn, fn) {
    setBusy(btn, true);
    try {
      await fn();
    } catch (err) {
      showResult({
        error: err.message,
        status: err.status,
        details: err.body || String(err),
      });
    } finally {
      setBusy(btn, false);
    }
  }

  // Tabs
  document.querySelectorAll(".tab").forEach((tab) => {
    tab.addEventListener("click", () => {
      document.querySelectorAll(".tab").forEach((t) => t.classList.remove("is-active"));
      document.querySelectorAll(".panel").forEach((p) => {
        p.classList.remove("is-active");
        p.hidden = true;
      });
      tab.classList.add("is-active");
      const panel = $(`#panel-${tab.dataset.panel}`);
      if (panel) {
        panel.hidden = false;
        panel.classList.add("is-active");
      }
    });
  });

  // API list
  const apiList = $("#api-list");
  if (apiList) {
    apiList.innerHTML = API_LIST.map(
      ([method, path]) =>
        `<li><span class="method ${method.toLowerCase()}">${method}</span><span class="path">${path}</span></li>`
    ).join("");
  }

  $("#clear-result")?.addEventListener("click", () => {
    showResult("等待操作…");
  });

  document.body.addEventListener("click", (ev) => {
    const btn = ev.target.closest("[data-action]");
    if (!btn) return;
    const action = btn.dataset.action;

    const handlers = {
      "xhs-status": async () => {
        const data = await api("/api/v1/login/status");
        setLoginUI("xhs", data?.data?.is_logged_in);
        showResult(data);
      },
      "xhs-qrcode": async () => {
        const data = await api("/api/v1/login/qrcode");
        setLoginUI("xhs", data?.data?.is_logged_in);
        renderQrcode("#xhs-qrcode", data?.data?.img || data?.data?.image || data?.data?.qrcode);
        showResult(data);
      },
      "xhs-logout": async () => {
        if (!confirm("确认清除小红书 Cookie？")) return;
        const data = await api("/api/v1/login/cookies", { method: "DELETE" });
        setLoginUI("xhs", false);
        showResult(data);
      },
      "xhs-search": async () => {
        const keyword = $("#xhs-keyword")?.value?.trim();
        if (!keyword) {
          showResult({ error: "请输入关键词" });
          return;
        }
        const data = await api("/api/v1/feeds/search", {
          method: "POST",
          body: JSON.stringify({ keyword }),
        });
        showResult(data);
      },
      "xhs-fetch-url": async () => {
        const url = $("#xhs-note-url")?.value?.trim();
        if (!url) {
          showResult({ error: "请输入笔记 URL" });
          return;
        }
        const data = await api("/api/v1/feeds/fetch_by_url", {
          method: "POST",
          body: JSON.stringify({ url, load_all_comments: true, sort_by_likes: true }),
        });
        showResult(data);
      },
      "zhihu-status": async () => {
        const data = await api("/api/v1/zhihu/login/status");
        setLoginUI("zhihu", data?.data?.is_logged_in);
        showResult(data);
      },
      "zhihu-qrcode": async () => {
        const data = await api("/api/v1/zhihu/login/qrcode");
        setLoginUI("zhihu", data?.data?.is_logged_in);
        renderQrcode("#zhihu-qrcode", data?.data?.img || data?.data?.image || data?.data?.qrcode);
        showResult(data);
      },
      "zhihu-logout": async () => {
        if (!confirm("确认清除知乎 Cookie？")) return;
        const data = await api("/api/v1/zhihu/login/cookies", { method: "DELETE" });
        setLoginUI("zhihu", false);
        showResult(data);
      },
      "zhihu-page": async () => {
        const url = $("#zhihu-page-url")?.value?.trim();
        if (!url) {
          showResult({ error: "请输入知乎 URL" });
          return;
        }
        const data = await api("/api/v1/zhihu/page", {
          method: "POST",
          body: JSON.stringify({ url }),
        });
        showResult(data);
      },
    };

    const fn = handlers[action];
    if (fn) withAction(btn, fn);
  });

  async function boot() {
    try {
      const health = await api("/health");
      healthPill.className = "pill pill-ok";
      healthPill.textContent = "服务正常";
      showResult({ message: "控制台已连接", health });
    } catch (err) {
      healthPill.className = "pill pill-bad";
      healthPill.textContent = "服务异常";
      showResult({ error: "健康检查失败", details: err.body || err.message });
      return;
    }

    // 静默拉一次登录状态
    try {
      const xhs = await api("/api/v1/login/status");
      setLoginUI("xhs", xhs?.data?.is_logged_in);
    } catch {
      setLoginUI("xhs", false);
    }
    try {
      const zhihu = await api("/api/v1/zhihu/login/status");
      setLoginUI("zhihu", zhihu?.data?.is_logged_in);
    } catch {
      setLoginUI("zhihu", false);
    }
  }

  boot();
})();
