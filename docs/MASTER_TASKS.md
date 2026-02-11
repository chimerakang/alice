# Alice AI Agent - Master Tasks

> AI 開發審計系統 — 追蹤 AI 的推理、操作、檔案變更
> Last updated: 2026-02-12 05:00 AM

## Status Legend

| Status | Label |
|--------|-------|
| 📋 | 規劃中 |
| 🔄 | 開發中 |
| 🧪 | 測試中 |
| ✅ | 已完成 |
| ⏸️ | 暫停 |

---

## Phase Overview

| Phase | Description | Progress | Status |
|-------|-------------|----------|--------|
| P1 - Core Backend | Telegram Bot + Claude CLI 整合 | 100% | ✅ |
| P2 - Monitoring | Web Dashboard + API + 監控系統 | 100% | ✅ |
| P3 - Data Layer | 持久化 + Git 整合 + Checkpoint | 100% | ✅ |
| P4 - Proto-First | Protocol Buffers 架構遷移 | 100% | ✅ |
| P5 - Frontend Foundation | React + Vite 框架 + 基礎元件 | 90% | 🔄 |
| P6 - AI Audit System | AI 開發追蹤核心功能 | 100% | ✅ |
| P7 - Dashboard & Analytics | 儀表板強化 + 分析圖表 | 75% | 🔄 |
| P8 - Control API | 遠端控制 + 中斷/回溯 | 60% | 🔄 |

---

## P1 - Core Backend (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| 1.1 | Telegram Bot + Claude CLI 整合 | — | ✅ |
| 1.2 | Per-chat agent 隔離 | — | ✅ |
| 1.3 | Streaming tool feedback | — | ✅ |
| 1.4 | Forum Topics 支援 | — | ✅ |
| 1.5 | Multi-project 支援 | — | ✅ |

## P2 - Monitoring & Dashboard (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| 2.1 | Web Dashboard HTTP Server | [#1](https://github.com/chimerakang/alice/issues/1) | ✅ |
| 2.2 | AI Transparency & Decision Logging | [#2](https://github.com/chimerakang/alice/issues/2) | ✅ |
| 2.3 | Multi-Agent Coordination System | [#3](https://github.com/chimerakang/alice/issues/3) | ✅ |
| 2.4 | Performance Monitoring & Analytics | [#4](https://github.com/chimerakang/alice/issues/4) | ✅ |
| 2.5 | Security & Privacy Enhancements | [#5](https://github.com/chimerakang/alice/issues/5) | ✅ |
| 2.6 | Deployment & DevOps | [#6](https://github.com/chimerakang/alice/issues/6) | ✅ |

## P3 - Data Layer (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| 3.1 | SQLite Persistence Layer | [#7](https://github.com/chimerakang/alice/issues/7) | ✅ |
| 3.2 | WebSocket Real-time Dashboard | [#8](https://github.com/chimerakang/alice/issues/8) | ✅ |
| 3.3 | Git Integration & Commit Correlation | [#9](https://github.com/chimerakang/alice/issues/9) | ✅ |
| 3.4 | Checkpoint & State Snapshot System | [#10](https://github.com/chimerakang/alice/issues/10) | ✅ |
| 3.5 | Dashboard Timeline & Terminal | [#12](https://github.com/chimerakang/alice/issues/12) | ✅ |

## P4 - Proto-First Architecture (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| 4.1 | Proto 定義 + buf 工具鏈 | [#13](https://github.com/chimerakang/alice/issues/13) | ✅ |
| 4.2 | Go 代碼生成 (18 .pb.go files) | [#13](https://github.com/chimerakang/alice/issues/13) | ✅ |
| 4.3 | TypeScript 類型生成 | [#13](https://github.com/chimerakang/alice/issues/13) | ✅ |
| 4.4 | API 端點遷移至 proto 型別 | [#13](https://github.com/chimerakang/alice/issues/13) | ✅ |

## P5 - Frontend Foundation (✅ 90%)

| # | Task | Issue | Status | Priority |
|---|------|-------|--------|----------|
| 5.1 | React + Vite + TypeScript 專案初始化 | [#15](https://github.com/chimerakang/alice/issues/15) | ✅ | P0 |
| 5.2 | Bot/Dashboard 架構分離 (nginx + host) | [#15](https://github.com/chimerakang/alice/issues/15) | ✅ | P0 |
| 5.3 | 共用 UI 元件庫 | [#16](https://github.com/chimerakang/alice/issues/16) | ✅ | P0 |
| | — CollapsiblePanel, MarkdownRenderer, StatusBadge | | ✅ | |
| | — DiffViewer, ToolCallGantt, SearchFilter | | ✅ | |
| | — TimelineEntry | | ✅ | |
| 5.4 | WebSocket Hook + Zustand 狀態管理 | [#17](https://github.com/chimerakang/alice/issues/17) | 🔄 | P0 |
| | — WebSocket hook ✅ | | | |
| | — Zustand store ✅ | | | |
| | — 剩餘: notifications, event recovery | | | |

## P6 - AI Audit System: 核心功能 (✅ 100%)

| # | Task | Issue | Status | Priority |
|---|------|-------|--------|----------|
| 6.1 | **AI Decision Timeline** | [#21](https://github.com/chimerakang/alice/issues/21) | ✅ | **P0** |
| | — 時間軸列表 (prompt → tools → outcome) | | ✅ | |
| | — 可展開的 tool call details | | ✅ | |
| | — 篩選: 專案/狀態/全文搜尋 | | ✅ | |
| | — WebSocket 即時新增 entries | | ✅ | |
| 6.2 | **Decision Detail + Diff Viewer** | [#22](https://github.com/chimerakang/alice/issues/22) | ✅ | **P0** |
| | — 完整 Decision 詳情頁 + prev/next 導航 | | ✅ | |
| | — AI Response markdown 渲染 | | ✅ | |
| | — Tool call Gantt timeline | | ✅ | |
| | — Git diff viewer (inline red/green) | | ✅ | |
| | — Token/cost breakdown | | ✅ | |
| | — Backend: GET /api/git/diff endpoint | | ✅ | |
| 6.3 | **Checkpoint Management UI** | [#23](https://github.com/chimerakang/alice/issues/23) | ✅ | P1 |
| | — Checkpoint 列表 + 操作 (restore/create) | | ✅ | |
| | — Checkpoint ↔ Decision 關聯 (via session_id) | | ✅ | |
| | — ConfirmDialog 確認元件 | | ✅ | |
| 6.4 | **擴充 CallStream 擷取完整 AI 思考內容** | [#27](https://github.com/chimerakang/alice/issues/27) | ✅ | **P1** |
| | — 解析 `thinking` / `text` / `tool_result` content blocks | | ✅ | |
| | — DecisionLog 新增 ThinkingContent 欄位 + SQLite migration | | ✅ | |
| | — Frontend Timeline/Checkpoint 顯示 AI Thinking 面板 | | ✅ | |
| | — **🎉 Issue #27 已關閉** (2026-02-12) | | ✅ | |

## P7 - Dashboard & Analytics (🔄 50%)

| # | Task | Issue | Status | Priority |
|---|------|-------|--------|----------|
| 7.1 | Dashboard Enhancement (AI Activity Overview) | [#24](https://github.com/chimerakang/alice/issues/24) | ✅ | P1 |
| | — Git 狀態卡片 | | ✅ | |
| | — 最近 AI 決策摘要 | | ✅ | |
| | — Recharts 趨勢圖表 (Activity/Token/Tool Success) | | ✅ | |
| | — 系統狀態面板 | | ✅ | |
| 7.2 | Checkpoint ↔ DecisionLog 直接關聯 | [#26](https://github.com/chimerakang/alice/issues/26) | ✅ | P1 |
| | — 🔧 Fixed: Checkpoint description 換行顯示問題 | | ✅ | |
| | — 🔧 Fixed: Checkpoint cards 新增 dangerous_op 風險提示 | | ✅ | |
| | — Backend: Checkpoint struct + SQLite 新增 decision_log_id | | ✅ | |
| | — Backend: CreateCheckpoint() 傳入 decision context | | ✅ | |
| | — Backend: Web API 支援 decision_log_id 參數 | | ✅ | |
| | — Frontend: 優先用 decision_log_id 直接查詢 | | ✅ | |
| | — Frontend: 保留時間戳配對為 fallback | | ✅ | |
| | — **🎉 Issue #26 已關閉** (2026-02-12) | | ✅ | |
| 7.3 | Performance & Security Analysis | [#25](https://github.com/chimerakang/alice/issues/25) | 📋 | P2 |
| | — Performance: 響應時間/Token趨勢圖表 | | | |
| | — Security: 事件列表/嚴重度分佈 | | | |

## P8 - Remote Control API (🔄 60%)

| # | Task | Issue | Status | Priority |
|---|------|-------|--------|----------|
| 8.1 | Checkpoint restore endpoint | [#11](https://github.com/chimerakang/alice/issues/11) | ✅ | — |
| 8.2 | Agent interrupt endpoint (Process.Kill) | [#11](https://github.com/chimerakang/alice/issues/11) | 📋 | P1 |
| 8.3 | Agent reset endpoint (Web API) | [#11](https://github.com/chimerakang/alice/issues/11) | 📋 | P1 |
| 8.4 | 控制端點認證 (Bearer token) | [#11](https://github.com/chimerakang/alice/issues/11) | 📋 | P2 |

---

## 建議開發順序

```
Phase 1 (基礎) ─── #16 UI Components ──→ #17 完善 WebSocket/Zustand
                         │
Phase 2 (核心) ─── #21 AI Timeline ─────→ #22 Detail + Diff Viewer
                         │
Phase 3 (強化) ─── #23 Checkpoint UI ──→ #24 Dashboard Enhancement
                         │
Phase 4 (分析) ─── #25 Performance/Security ──→ #11 Remote Control
```

### P0 (必須先做)
1. **#16** 共用 UI 元件庫 — 被所有後續 issue 依賴
2. **#21** AI Decision Timeline — **核心功能**
3. **#22** Decision Detail + Diff Viewer

### P1 (核心完成後)
4. **#23** Checkpoint Management UI
5. **#24** Dashboard Enhancement

### P2 (最後)
6. **#25** Performance & Security
7. **#11** Remote Control API 完善

---

## Issue Tracker Summary

| Issue | Title | Phase | Status |
|-------|-------|-------|--------|
| [#1-#10, #12-#13](https://github.com/chimerakang/alice/issues) | P1-P4 Backend | P1-P4 | ✅ All Closed |
| [#15](https://github.com/chimerakang/alice/issues/15) | React + Vite 專案初始化 | P5 | ✅ Closed |
| [#16](https://github.com/chimerakang/alice/issues/16) | 共用 UI 元件庫 | P5 | ✅ Closed |
| [#17](https://github.com/chimerakang/alice/issues/17) | WebSocket + Zustand | P5 | 🔄 Open (80%) |
| [#18](https://github.com/chimerakang/alice/issues/18) | ~~Dashboard 主頁面~~ | — | ❌ Replaced by #24 |
| [#19](https://github.com/chimerakang/alice/issues/19) | ~~Timeline + Terminal~~ | — | ❌ Replaced by #21 |
| [#20](https://github.com/chimerakang/alice/issues/20) | ~~Git/Checkpoint/Perf/Security~~ | — | ❌ Split into #22-#25 |
| [#21](https://github.com/chimerakang/alice/issues/21) | **AI Decision Timeline** | P6 | ✅ Closed |
| [#22](https://github.com/chimerakang/alice/issues/22) | Decision Detail + Diff Viewer | P6 | ✅ Closed |
| [#23](https://github.com/chimerakang/alice/issues/23) | Checkpoint Management UI | P6 | ✅ Closed |
| [#24](https://github.com/chimerakang/alice/issues/24) | Dashboard Enhancement | P7 | ✅ Closed |
| [#25](https://github.com/chimerakang/alice/issues/25) | Performance & Security | P7 | 📋 Open |
| [#26](https://github.com/chimerakang/alice/issues/26) | Checkpoint ↔ DecisionLog 直接關聯 | P7 | ✅ Closed |
| [#27](https://github.com/chimerakang/alice/issues/27) | 擴充 CallStream 擷取完整 AI 思考內容 | P6 | ✅ Closed |
| [#11](https://github.com/chimerakang/alice/issues/11) | Remote Control API | P8 | 🔄 Open (60%) |

---

## Architecture

```
=== Runtime Architecture ===

Host (native):
  Telegram ←→ TelegramBot ←→ Agent ←→ CLIClient (subprocess)
                                          ↕
                                     Claude Code CLI

  Agent ──→ ToolLogger ──→ [SQLite]
       ──→ DecisionLogger ─↗
       ──→ PerformanceMonitor ─↗
       ──→ SecurityManager ─↗
       ──→ CheckpointManager ─↗

  Go HTTP Server (:8082) ──→ REST API + WebSocket

Docker (nginx):
  React SPA (:3939) ──→ proxy /api/* ──→ Host :8082
                    ──→ proxy /ws    ──→ Host :8082

=== Frontend Stack ===
  React 18 + Vite + TypeScript
  Tailwind CSS v4 (OLED dark theme)
  Zustand (state management)
  Recharts (data visualization)
  React Router (client-side routing)
  Proto-generated TypeScript types
```
