package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mohammad-safakhou/stalker/internal/store"
)

type Handler struct {
	Store *store.Store
}

func New(s *store.Store) *Handler {
	return &Handler{Store: s}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/ui" || r.URL.Path == "/ui/":
		h.index(w, r)
	case r.URL.Path == "/api/exchanges":
		h.list(w, r)
	case r.URL.Path == "/api/tokens/summary":
		h.tokenSummary(w, r)
	case r.URL.Path == "/api/tokens/stream":
		h.tokenStream(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/exchanges/"):
		h.exchange(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	items, err := h.Store.List(r.Context(), store.ListFilter{
		Limit:  limit,
		Offset: offset,
		Query:  r.URL.Query().Get("q"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, items)
}

func (h *Handler) tokenSummary(w http.ResponseWriter, r *http.Request) {
	totals, err := h.Store.TokenTotals(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, totals)
}

func (h *Handler) tokenStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming is not supported"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	send := func() bool {
		totals, err := h.Store.TokenTotals(r.Context())
		if err != nil {
			_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", jsonString(map[string]string{"error": err.Error()}))
			flusher.Flush()
			return false
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", jsonString(totals))
		flusher.Flush()
		return true
	}

	if !send() {
		return
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}

func (h *Handler) exchange(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/exchanges/")
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && parts[1] == "tokens" {
		h.tokens(w, r, parts[0])
		return
	}
	if len(parts) == 3 && parts[1] == "body" {
		h.body(w, r, parts[0], parts[2])
		return
	}
	if len(parts) != 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}

	ex, err := h.Store.Get(r.Context(), parts[0])
	if err != nil {
		if store.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, ex)
}

func (h *Handler) tokens(w http.ResponseWriter, r *http.Request, id string) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	report, err := h.Store.TokenReport(r.Context(), id, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, report)
}

func (h *Handler) body(w http.ResponseWriter, r *http.Request, id string, side string) {
	info, err := h.Store.Body(r.Context(), id, side)
	if err != nil {
		if store.IsNotFound(err) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Stalker-Body-Bytes", strconv.FormatInt(info.Bytes, 10))
	if info.Encoding != "" {
		w.Header().Set("X-Stalker-Decoded-From", info.Encoding)
	}
	body, err := h.Store.OpenBody(info)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer body.Close()
	_, _ = io.Copy(w, body)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func jsonString(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Stalker</title>
  <style>
    :root { color-scheme: light dark; --bg: #f7f7f4; --fg: #171717; --muted: #6b6b63; --line: #d8d6cf; --panel: #ffffff; --accent: #0f766e; --accent2: #7c3aed; --bad: #b42318; }
    @media (prefers-color-scheme: dark) { :root { --bg: #141412; --fg: #eeeeea; --muted: #a7a59d; --line: #34342f; --panel: #1d1d1a; --accent: #2dd4bf; --accent2: #a78bfa; --bad: #ff8a80; } }
    * { box-sizing: border-box; }
    body { margin: 0; font: 14px/1.45 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: var(--bg); color: var(--fg); }
    header { height: 52px; display: flex; align-items: center; gap: 12px; padding: 0 16px; border-bottom: 1px solid var(--line); background: var(--panel); }
    header strong { font-size: 16px; }
    input, button { font: inherit; }
    input { height: 32px; min-width: 280px; border: 1px solid var(--line); background: transparent; color: var(--fg); padding: 0 10px; border-radius: 6px; }
    button { height: 32px; border: 1px solid var(--line); background: var(--panel); color: var(--fg); border-radius: 6px; padding: 0 10px; cursor: pointer; }
    .spacer { flex: 1; }
    .hidden { display: none !important; }
    #dashboard { min-height: calc(100vh - 52px); padding: 18px; }
    .token-hero { height: calc(100vh - 88px); display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
    .token-column { min-width: 0; border: 1px solid var(--line); border-radius: 8px; background: var(--panel); display: grid; grid-template-rows: minmax(190px, 34%) 1fr; overflow: hidden; }
    .token-total { display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 18px; border-bottom: 1px solid var(--line); }
    .token-total span { color: var(--muted); font-size: 13px; text-transform: uppercase; letter-spacing: 0; }
    .token-total b { display: block; margin-top: 10px; font-size: 72px; line-height: 1; letter-spacing: 0; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; text-align: center; }
    .input .token-total b { color: var(--accent); }
    .output .token-total b { color: var(--accent2); }
    .burn-list { overflow: auto; padding: 12px; }
    .burn-list h2 { margin: 0 0 10px; font-size: 13px; color: var(--muted); text-transform: uppercase; letter-spacing: 0; }
    .burn-row { display: grid; grid-template-columns: minmax(0, 1fr) max-content; gap: 12px; align-items: center; padding: 9px 0; border-bottom: 1px solid var(--line); }
    .burn-row code { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 13px/1.3 ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; color: var(--fg); }
    .burn-row b { font-variant-numeric: tabular-nums; font-size: 13px; }
    main { display: grid; grid-template-columns: minmax(360px, 42%) 1fr; height: calc(100vh - 52px); }
    #list { border-right: 1px solid var(--line); overflow: auto; background: var(--panel); }
    .row { display: grid; grid-template-columns: 70px 1fr 64px; gap: 10px; padding: 10px 12px; border-bottom: 1px solid var(--line); cursor: pointer; }
    .row:hover, .row.active { background: color-mix(in srgb, var(--accent) 10%, transparent); }
    .method { font-weight: 700; }
    .path { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .meta { color: var(--muted); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .status { text-align: right; font-variant-numeric: tabular-nums; }
    .status.bad { color: var(--bad); }
    #detail { overflow: auto; padding: 16px; }
    .empty { color: var(--muted); padding: 24px; }
    .grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px; margin-bottom: 14px; }
    .stat { border: 1px solid var(--line); border-radius: 6px; padding: 8px; background: var(--panel); min-width: 0; }
    .stat span { display: block; color: var(--muted); font-size: 12px; }
    .stat b { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    h2 { font-size: 14px; margin: 18px 0 8px; }
    pre { margin: 0; border: 1px solid var(--line); border-radius: 6px; padding: 12px; background: var(--panel); white-space: pre-wrap; word-break: break-word; max-height: 42vh; overflow: auto; }
    .messages { display: grid; gap: 8px; margin-bottom: 6px; }
    .msg { border: 1px solid var(--line); border-radius: 6px; padding: 8px 10px; background: var(--panel); }
    .msg b { display: block; margin-bottom: 4px; color: var(--accent); }
    .msg div { white-space: pre-wrap; word-break: break-word; }
    a { color: var(--accent); text-decoration: none; }
    @media (max-width: 800px) { #dashboard { padding: 12px; } .token-hero { height: auto; grid-template-columns: 1fr; } .token-column { min-height: 460px; } .token-total b { font-size: 48px; } main { grid-template-columns: 1fr; } #list { height: 42vh; border-right: 0; border-bottom: 1px solid var(--line); } input { min-width: 0; flex: 1; } .grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
  </style>
</head>
<body>
  <header>
    <strong>Stalker</strong>
    <input id="q" class="hidden" placeholder="Search path, route, body preview">
    <span class="spacer"></span>
    <button id="refresh">Refresh</button>
    <button id="viewToggle">Messages</button>
    <span id="count" class="meta"></span>
  </header>
  <section id="dashboard">
    <div class="token-hero">
      <section class="token-column input">
        <div class="token-total"><span>Input tokens</span><b id="inputTokens">0</b></div>
        <div class="burn-list"><h2>Top input tokens burned</h2><div id="inputTop"></div></div>
      </section>
      <section class="token-column output">
        <div class="token-total"><span>Output tokens</span><b id="outputTokens">0</b></div>
        <div class="burn-list"><h2>Top output tokens burned</h2><div id="outputTop"></div></div>
      </section>
    </div>
  </section>
  <main id="explorer" class="hidden">
    <section id="list"><div class="empty">Loading...</div></section>
    <section id="detail"><div class="empty">Select a request.</div></section>
  </main>
  <script>
    const dashboard = document.querySelector("#dashboard");
    const explorer = document.querySelector("#explorer");
    const list = document.querySelector("#list");
    const detail = document.querySelector("#detail");
    const q = document.querySelector("#q");
    const count = document.querySelector("#count");
    const viewToggle = document.querySelector("#viewToggle");
    const inputTokens = document.querySelector("#inputTokens");
    const outputTokens = document.querySelector("#outputTokens");
    const inputTop = document.querySelector("#inputTop");
    const outputTop = document.querySelector("#outputTop");
    let selected = "";
    let view = "dashboard";
    let tokenStream = null;

    function applyTotals(totals) {
      inputTokens.textContent = fmt(totals.input_tokens || 0);
      outputTokens.textContent = fmt(totals.output_tokens || 0);
      renderTop(inputTop, totals.top && totals.top.input || []);
      renderTop(outputTop, totals.top && totals.top.output || []);
      count.textContent = "";
    }

    async function loadDashboard() {
      const res = await fetch("/api/tokens/summary");
      const totals = await res.json();
      applyTotals(totals);
    }

    function startTokenStream() {
      stopTokenStream();
      if (!window.EventSource) {
        loadDashboard();
        return;
      }
      tokenStream = new EventSource("/api/tokens/stream");
      tokenStream.onmessage = event => {
        try { applyTotals(JSON.parse(event.data)); } catch {}
      };
      tokenStream.onerror = () => {
        stopTokenStream();
        loadDashboard();
      };
    }

    function stopTokenStream() {
      if (tokenStream) {
        tokenStream.close();
        tokenStream = null;
      }
    }

    async function load() {
      const url = new URL("/api/exchanges", location.origin);
      url.searchParams.set("limit", "100");
      if (q.value.trim()) url.searchParams.set("q", q.value.trim());
      const res = await fetch(url);
      const rows = await res.json();
      count.textContent = rows.length + " shown";
      list.innerHTML = "";
      if (!rows.length) {
        list.innerHTML = '<div class="empty">No captured traffic yet.</div>';
        return;
      }
      for (const row of rows) {
        const el = document.createElement("div");
        el.className = "row" + (row.id === selected ? " active" : "");
        el.innerHTML = '<div class="method"></div><div><div class="path"></div><div class="meta"></div></div><div class="status"></div>';
        el.children[0].textContent = row.method;
        el.children[1].children[0].textContent = row.path + (row.query ? "?" + row.query : "");
        el.children[1].children[1].textContent = row.route + " -> " + row.target_url;
        el.children[2].textContent = row.status_code || "ERR";
        if (row.status_code >= 400 || row.error) el.children[2].classList.add("bad");
        el.onclick = () => select(row.id);
        list.appendChild(el);
      }
    }

    async function refresh() {
      if (view === "dashboard") await loadDashboard();
      else await load();
    }

    function showDashboard() {
      view = "dashboard";
      dashboard.classList.remove("hidden");
      explorer.classList.add("hidden");
      q.classList.add("hidden");
      viewToggle.textContent = "Messages";
      startTokenStream();
    }

    function showExplorer() {
      view = "explorer";
      stopTokenStream();
      dashboard.classList.add("hidden");
      explorer.classList.remove("hidden");
      q.classList.remove("hidden");
      viewToggle.textContent = "Dashboard";
      load();
    }

    async function select(id) {
      selected = id;
      await load();
      const res = await fetch("/api/exchanges/" + encodeURIComponent(id));
      const row = await res.json();
      detail.innerHTML = "";
      const title = document.createElement("h1");
      title.style.fontSize = "18px";
      title.textContent = row.method + " " + row.path;
      detail.appendChild(title);

      const stats = document.createElement("div");
      stats.className = "grid";
      stats.innerHTML = stat("Status", row.status_code || "error") + stat("Duration", row.duration_ms + " ms") + stat("Route", row.route) + stat("Bytes", row.request_bytes + " / " + row.response_bytes);
      detail.appendChild(stats);

      addMessages(row.request_preview, row.response_preview);
      addBlock("Request", row.request_preview, row.request_body_path ? "/api/exchanges/" + encodeURIComponent(id) + "/body/request" : null);
      addBlock("Response", row.response_preview, row.response_body_path ? "/api/exchanges/" + encodeURIComponent(id) + "/body/response" : null);
      addBlock("Request headers", pretty(row.request_headers), null);
      addBlock("Response headers", pretty(row.response_headers), null);
      if (row.error) addBlock("Error", row.error, null);
    }

    function stat(label, value) {
      return '<div class="stat"><span>' + esc(label) + '</span><b>' + esc(String(value)) + '</b></div>';
    }

    function addBlock(label, text, href) {
      const h = document.createElement("h2");
      h.textContent = label + " ";
      if (href) {
        const a = document.createElement("a");
        a.href = href;
        a.target = "_blank";
        a.textContent = "open full";
        h.appendChild(a);
      }
      const pre = document.createElement("pre");
      pre.textContent = formatBody(text || "");
      detail.appendChild(h);
      detail.appendChild(pre);
    }

    function addMessages(reqRaw, respRaw) {
      const messages = [...extractMessages(reqRaw, "request"), ...extractMessages(respRaw, "response")];
      if (!messages.length) return;
      const h = document.createElement("h2");
      h.textContent = "Messages";
      const wrap = document.createElement("div");
      wrap.className = "messages";
      for (const msg of messages.slice(0, 30)) {
        const el = document.createElement("div");
        el.className = "msg";
        const role = document.createElement("b");
        role.textContent = msg.side + " / " + msg.role;
        const body = document.createElement("div");
        body.textContent = msg.text;
        el.appendChild(role);
        el.appendChild(body);
        wrap.appendChild(el);
      }
      detail.appendChild(h);
      detail.appendChild(wrap);
    }

    function extractMessages(raw, side) {
      if (!raw) return [];
      const out = [];
      const push = (role, text) => {
        text = textValue(text);
        if (text) out.push({ side, role: role || "unknown", text });
      };
      const consume = data => {
        const before = out.length;
        if (Array.isArray(data.messages)) {
          for (const m of data.messages) push(m.role, m.content);
        }
        if (typeof data.input === "string") push("input", data.input);
        if (Array.isArray(data.input)) {
          for (const item of data.input) push(item.role || item.type || "input", item.content || item.output || item.text);
        }
        if (Array.isArray(data.output)) {
          for (const item of data.output) push(item.role || item.type || "output", item.content || item.output || item.text);
        }
        if (Array.isArray(data.choices)) {
          for (const choice of data.choices) {
            if (choice.message) push(choice.message.role || "assistant", choice.message.content);
            if (choice.delta) push("delta", choice.delta.content || choice.delta.text);
          }
        }
        if (data.type && data.text) push(data.type, data.text);
        if (data.event && data.data) push(data.event, data.data);
        return out.length > before;
      };
      try {
        consume(JSON.parse(raw));
        return out;
      } catch {}
      for (const line of raw.split("\n")) {
        if (!line.trim()) continue;
        let frame;
        try { frame = JSON.parse(line); } catch { continue; }
        if (typeof frame.text === "string") {
          let used = false;
          try { used = consume(JSON.parse(frame.text)); } catch {}
          if (!used) push(frame.opcode || "websocket", frame.text);
        } else if (frame.base64) {
          push(frame.opcode || "binary", "[base64] " + frame.base64);
        }
      }
      return out;
    }

    function textValue(value) {
      if (value == null) return "";
      if (typeof value === "string") return value;
      if (Array.isArray(value)) return value.map(textValue).filter(Boolean).join("\n");
      if (typeof value === "object") return value.text || value.input_text || value.output_text || JSON.stringify(value, null, 2);
      return String(value);
    }

    function pretty(raw) {
      try { return JSON.stringify(JSON.parse(raw || "{}"), null, 2); } catch { return raw || ""; }
    }

    function formatBody(raw) {
      try { return JSON.stringify(JSON.parse(raw), null, 2); } catch { return raw; }
    }

    function esc(value) {
      return value.replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
    }

    function fmt(value) {
      return new Intl.NumberFormat().format(value);
    }

    function renderTop(target, rows) {
      target.innerHTML = "";
      if (!rows.length) {
        target.innerHTML = '<div class="empty">No token data yet.</div>';
        return;
      }
      for (const row of rows) {
        const el = document.createElement("div");
        el.className = "burn-row";
        const token = document.createElement("code");
        token.textContent = tokenLabel(row.token);
        token.title = "sha256: " + row.token_hash;
        const count = document.createElement("b");
        count.textContent = fmt(row.occurrences || 0);
        el.appendChild(token);
        el.appendChild(count);
        target.appendChild(el);
      }
    }

    function tokenLabel(token) {
      if (token == null || token === "") return "(empty)";
      return JSON.stringify(token);
    }

    document.querySelector("#refresh").onclick = refresh;
    viewToggle.onclick = () => view === "dashboard" ? showExplorer() : showDashboard();
    q.addEventListener("keydown", e => { if (e.key === "Enter") load(); });
    showDashboard();
    setInterval(() => { if (view !== "dashboard") refresh(); }, 5000);
  </script>
</body>
</html>`
