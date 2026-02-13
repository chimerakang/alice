# Alice AI Agent - Master Tasks

> AI 開發審計系統 — 追蹤 AI 的推理、操作、檔案變更
> Last updated: 2026-02-13 11:25 AM

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
| P5 - Frontend Foundation | React + Vite 框架 + 基礎元件 | 100% | ✅ |
| P6 - AI Audit System | AI 開發追蹤核心功能 | 100% | ✅ |
| P7 - Dashboard & Analytics | 儀表板強化 + 分析圖表 + 歷史資料查詢 | 100% | ✅ |
| P8 - Control API | 遠端控制 + 中斷/回溯 | 100% | ✅ |
| P8.5 - TG 指令增強 | /tasks 待辦清單 + Topic 設定持久化 | 100% | ✅ |
| P9 - Multimedia Input | 圖片分析 + 語音轉文字 | 100% | ✅ |
| P9.5 - Multimedia Enhancement | 多張圖片批次處理 + 媒體群組支援 | 100% | ✅ |
| P10 - Claude Code Hooks | 攔截所有 Claude Code 互動（Terminal/VSCode/TG） | 100% | ✅ |
| P11 - User Experience | 指令健全性和用戶體驗改善 | 100% | ✅ |
| P12 - Dashboard Analytics | Claude Code Hooks UI 增強：統計圖表 + 用戶指南 | 100% | ✅ |

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

## P5 - Frontend Foundation (✅ 100%)

| # | Task | Issue | Status | Priority |
|---|------|-------|--------|----------|
| 5.1 | React + Vite + TypeScript 專案初始化 | [#15](https://github.com/chimerakang/alice/issues/15) | ✅ | P0 |
| 5.2 | Bot/Dashboard 架構分離 (nginx + host) | [#15](https://github.com/chimerakang/alice/issues/15) | ✅ | P0 |
| 5.3 | 共用 UI 元件庫 | [#16](https://github.com/chimerakang/alice/issues/16) | ✅ | P0 |
| | — CollapsiblePanel, MarkdownRenderer, StatusBadge | | ✅ | |
| | — DiffViewer, ToolCallGantt, SearchFilter | | ✅ | |
| | — TimelineEntry | | ✅ | |
| 5.4 | WebSocket Hook + Zustand 狀態管理 | [#17](https://github.com/chimerakang/alice/issues/17) | ✅ | P0 |
| | — WebSocket hook (exponential backoff 重連) ✅ | | ✅ | |
| | — Zustand store (6種事件類型 + devtools) ✅ | | ✅ | |
| | — API client + 初始數據載入 ✅ | | ✅ | |

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

## P7 - Dashboard & Analytics (🔄 90%)

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
| 7.3 | Performance & Security Analysis | [#25](https://github.com/chimerakang/alice/issues/25) | ✅ | P2 |
| | — Performance: 響應時間/Token/成本/記憶體趨勢圖表 ✅ | | ✅ | |
| | — Security: 事件列表/嚴重度分佈/PII記錄 ✅ | | ✅ | |
| | — **🎉 Issue #25 已關閉** (2026-02-12) | | ✅ | |
| 7.4 | Dashboard 歷史資料整合 & API 增強 | Custom Enhancement | ✅ | P1 |
| | — 修復 Dashboard 僅顯示 WebSocket 緩存問題 ✅ | | ✅ | |
| | — 新增時間範圍查詢 API 端點 (decisions/tool-executions/security/performance) ✅ | | ✅ | |
| | — 實作 DateRangeFilter 元件支援歷史資料瀏覽 ✅ | | ✅ | |
| | — 統一所有頁面採用混合資料載入模式 (API + WebSocket) ✅ | | ✅ | |
| | — 🔧 Fixed: WebSocket BroadcastDecisionEvent 缺少 user_prompt/tool_calls 等欄位 ✅ | | ✅ | |
| 7.5 | Checkpoints 頁面重新定位：AI 決策歷程 + 安全快照 | [#30](https://github.com/chimerakang/alice/issues/30) | ✅ | P1 |
| | — 重構頁面佈局：DecisionLog 為主體，Checkpoint 為附註標記 ✅ | | ✅ | |
| | — 加入 DateRangeFilter + server-side 分頁（復用 Timeline 模式） ✅ | | ✅ | |
| | — 重新設計卡片：user prompt → tool chain → outcome 為主，git/snapshot 為 collapsible 次要區塊 ✅ | | ✅ | |
| | — 加入 slide-over Detail Panel（完整 thinking/response/ToolCallGantt/GitDiff + checkpoint restore） ✅ | | ✅ | |
| | — 搜尋與篩選（搜尋 prompts/tools、filter by trigger type/project） ✅ | | ✅ | |
| | — 建置驗證 + Docker dashboard 重建 ✅ | | ✅ | |
| | — **🎉 Issue #30 已完成** (2026-02-13) | | ✅ | |

## P8 - Remote Control API (✅ 100%)

| # | Task | Issue | Status | Priority |
|---|------|-------|--------|----------|
| 8.1 | Checkpoint restore endpoint | [#11](https://github.com/chimerakang/alice/issues/11) | ✅ | — |
| 8.2 | Agent interrupt endpoint (Process.Kill) | [#11](https://github.com/chimerakang/alice/issues/11) | ✅ | P1 |
| 8.3 | Agent reset endpoint (Web API) | [#11](https://github.com/chimerakang/alice/issues/11) | ✅ | P1 |
| 8.4 | Agent project switch endpoint | [#11](https://github.com/chimerakang/alice/issues/11) | ✅ | P1 |
| 8.5 | 控制端點認證 (Bearer token) | [#11](https://github.com/chimerakang/alice/issues/11) | ✅ | P2 |
| | — **🎉 Issue #11 已關閉** (2026-02-12) | | ✅ | |

## P8.5 - Telegram 指令增強 (✅ 100%)

| # | Task | Issue | Status | Priority |
|---|------|-------|--------|----------|
| 8.6 | **/tasks 指令 — 查看待辦工作清單** | [#31](https://github.com/chimerakang/alice/issues/31) | ✅ | P1 |
| | — 解析 MASTER_TASKS.md 提取未完成項目（📋/🔄/🧪） ✅ | | ✅ | |
| | — handleCommand 新增 /tasks case ✅ | | ✅ | |
| | — 格式化輸出 Phase + 任務清單到 Telegram ✅ | | ✅ | |
| | — 更新 /help 指令說明 ✅ | | ✅ | |
| | — 更新指令註冊清單 ✅ | | ✅ | |
| | — **🎉 Issue #31 已完成** (2026-02-13) | | ✅ | |
| 8.7 | **Topic-Project 對應持久化 — 重啟後保留設定** | [#33](https://github.com/chimerakang/alice/issues/33) | ✅ | P1 |
| | — 新增 SQLite `topic_settings` 表 (chat_id, thread_id, project_dir) ✅ | | ✅ | |
| | — `/project` 切換時同步寫入資料庫 ✅ | | ✅ | |
| | — `getAgent()` 建立新 Agent 時先查資料庫還原設定 ✅ | | ✅ | |
| | — **🎉 Issue #33 已完成** (2026-02-13) | | ✅ | |

## P9 - Multimedia Input (✅ 100%)

| # | Task | Issue | Status | Priority |
|---|------|-------|--------|----------|
| 9.1 | **Telegram 圖片訊息支援** | [#28](https://github.com/chimerakang/alice/issues/28) | ✅ | **P0** |
| | — 擴展 TG update 解析結構（Photo + Caption 欄位） ✅ | | ✅ | |
| | — 實作 Telegram getFile + downloadFile 圖片下載 ✅ | | ✅ | |
| | — 組合圖片路徑 prompt，引導 Claude Read tool 讀取 ✅ | | ✅ | |
| | — 臨時檔案管理（下載目錄 + 使用後清理） ✅ | | ✅ | |
| | — **🎉 Issue #28 已完成** (2026-02-13) | | ✅ | |
| 9.2 | **Telegram 語音訊息轉文字** | [#29](https://github.com/chimerakang/alice/issues/29) | ✅ | P1 |
| | — 擴展 TG update 解析結構（Voice 欄位） ✅ | | ✅ | |
| | — 實作 STT 客戶端（OpenAI Whisper API） ✅ | | ✅ | |
| | — 語音下載 → 轉錄 → 文字傳給 Claude CLI ✅ | | ✅ | |
| | — 回覆中顯示轉錄文字供用戶確認 ✅ | | ✅ | |
| | — **🎉 Issue #29 已完成** (2026-02-13) | | ✅ | |
| 9.3 | 共用媒體基礎設施 | — | ✅ | P0 |
| | — Telegram 檔案下載共用函數 ✅ | | ✅ | |
| | — 臨時目錄管理 + 定期清理機制 ✅ | | ✅ | |
| | — MultimediaConfig 接線到實際邏輯 ✅ | | ✅ | |
| | — config.example.json 更新多媒體設定範例 ✅ | | ✅ | |

## P9.5 - Multimedia Enhancement: 多張圖片批次處理 (✅ 100%)

| # | Task | Issue | Status | Priority |
|---|------|-------|--------|----------|
| 9.4 | **多張圖片批次處理支援** | [#34](https://github.com/chimerakang/alice/issues/34) | ✅ | **P0** |
| | — 時間窗口批次機制（3-5秒內圖片歸為同批） ✅ | | ✅ | |
| | — 暫存多張圖片等待組合分析 ✅ | | ✅ | |
| | — 支援 Telegram media_group_id 偵測 ✅ | | ✅ | |
| | — 組合 prompt 將多張圖片一起傳給 Claude ✅ | | ✅ | |
| 9.5 | **媒體群組處理最佳化** | TBD | ✅ | P1 |
| | — 避免單張圖片觸發多次回應 ✅ | | ✅ | |
| | — 優雅處理混合媒體（圖片+文字+語音） ✅ | | ✅ | |
| | — 大量圖片記憶體管理與清理 ✅ | | ✅ | |
| | — 用戶反饋：顯示「正在處理第 X/Y 張圖片...」 ✅ | | ✅ | |
| 9.6 | **🔧 修復跨專案圖片存取問題** | [#35](https://github.com/chimerakang/alice/issues/35) | ✅ | **P0** |
| | — 圖片複製到目標專案臨時目錄解決路徑問題 ✅ | | ✅ | |
| | — 確保 Claude CLI 能正確存取圖片檔案 ✅ | | ✅ | |
| | — 統一臨時檔案清理機制（專案級別） ✅ | | ✅ | |
| | — **🚨 修復圖片+文字組合處理問題** ✅ | | ✅ | |
| | — 問題：圖片+文字說明時使用 EnhancedCLIClient 啟動新子進程 ✅ | | ✅ | |
| | — 錯誤：需要 CLAUDE_CODE_SESSION_ACCESS_TOKEN 認證 ✅ | | ✅ | |
| | — 解決：改用 agent.Run() 通過現有會話處理，如語音轉文字邏輯 ✅ | | ✅ | |

## P10 - Claude Code Hooks 整合 (✅ 100%)

| # | Task | Issue | Status | Priority |
|---|------|-------|--------|----------|
| 10.1 | **Claude Code Hooks 攔截全部互動** | [#32](https://github.com/chimerakang/alice/issues/32) | ✅ | **P0** |
| | — Hook script：攔截 SessionStart/Stop + UserPromptSubmit 事件 ✅ | | ✅ | |
| | — 解析 transcript_path JSONL 檔案擷取完整對話 ✅ | | ✅ | |
| | — HTTP POST 推送到 Alice API（/api/hooks/claude-code） ✅ | | ✅ | |
| | — 去重邏輯：避免 Telegram 來源重複記錄 ✅ | | ✅ | |
| 10.2 | **Alice 接收端 API + 儲存** | [#32](https://github.com/chimerakang/alice/issues/32) | ✅ | **P0** |
| | — 新增 /api/hooks/claude-code 端點接收 hook 資料 ✅ | | ✅ | |
| | — 解析 JSONL transcript 轉換為 DecisionLog 格式 ✅ | | ✅ | |
| | — 儲存至 SQLite + WebSocket 即時推播 ✅ | | ✅ | |
| | — 來源標記：terminal / vscode / telegram 區分 ✅ | | ✅ | |
| 10.3 | **Dashboard 多來源整合顯示** | [#32](https://github.com/chimerakang/alice/issues/32) | ✅ | P1 |
| | — Timeline 顯示來源標籤（Terminal/VSCode/Telegram） ✅ | | ✅ | |
| | — 篩選器支援按來源過濾 ✅ | | ✅ | |
| | — Dashboard 統計圖表（來源分布/效能對比） ✅ | [#36](https://github.com/chimerakang/alice/issues/36) | ✅ | |
| | — 安裝指南：.claude/settings.json hook 配置說明 ✅ | [#36](https://github.com/chimerakang/alice/issues/36) | ✅ | |

## P11 - User Experience 改善 (✅ 100%)

| # | Task | Issue | Status | Priority |
|---|------|-------|--------|----------|
| 11.1 | **Telegram 指令健全性增強** | [#37](https://github.com/chimerakang/alice/issues/37) | ✅ | **P0** |
| | — /project 指令路徑存在性驗證 ✅ | | ✅ | |
| | — 智慧路徑建議（模糊搜尋相似目錄名稱） ✅ | | ✅ | |
| | — 友善的錯誤訊息和設定確認 ✅ | | ✅ | |
| | — 專案有效性檢查（偵測專案類型） ✅ | | ✅ | |

## P12 - Dashboard Analytics: Claude Code Hooks UI 增強 (✅ 100%)

| # | Task | Issue | Status | Priority |
|---|------|-------|--------|----------|
| 12.1 | **Dashboard 來源統計圖表** | [#36](https://github.com/chimerakang/alice/issues/36) | ✅ | **P0** |
| | — SourceDistributionChart: 來源分布餅狀圖 (Terminal/VSCode/Telegram) ✅ | | ✅ | |
| | — SourcePerformanceChart: 效能對比柱狀圖 (執行時間/成功率) ✅ | | ✅ | |
| | — API 端點：/api/decisions/sources/stats + performance ✅ | | ✅ | |
| | — 完整整合至 Dashboard 頁面 ✅ | | ✅ | |
| 12.2 | **Claude Code Hooks 用戶指南** | [#36](https://github.com/chimerakang/alice/issues/36) | ✅ | P1 |
| | — CLAUDE_CODE_HOOKS.md 完整文檔 ✅ | | ✅ | |
| | — claude-hook.sh 腳本實作 ✅ | | ✅ | |
| | — install-hooks.sh 自動安裝工具 ✅ | | ✅ | |
| | — 故障排除和最佳實踐指南 ✅ | | ✅ | |

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
| [#17](https://github.com/chimerakang/alice/issues/17) | WebSocket + Zustand | P5 | ✅ Closed |
| [#18](https://github.com/chimerakang/alice/issues/18) | ~~Dashboard 主頁面~~ | — | ❌ Replaced by #24 |
| [#19](https://github.com/chimerakang/alice/issues/19) | ~~Timeline + Terminal~~ | — | ❌ Replaced by #21 |
| [#20](https://github.com/chimerakang/alice/issues/20) | ~~Git/Checkpoint/Perf/Security~~ | — | ❌ Split into #22-#25 |
| [#21](https://github.com/chimerakang/alice/issues/21) | **AI Decision Timeline** | P6 | ✅ Closed |
| [#22](https://github.com/chimerakang/alice/issues/22) | Decision Detail + Diff Viewer | P6 | ✅ Closed |
| [#23](https://github.com/chimerakang/alice/issues/23) | Checkpoint Management UI | P6 | ✅ Closed |
| [#24](https://github.com/chimerakang/alice/issues/24) | Dashboard Enhancement | P7 | ✅ Closed |
| [#25](https://github.com/chimerakang/alice/issues/25) | Performance & Security | P7 | ✅ Closed |
| [#26](https://github.com/chimerakang/alice/issues/26) | Checkpoint ↔ DecisionLog 直接關聯 | P7 | ✅ Closed |
| [#27](https://github.com/chimerakang/alice/issues/27) | 擴充 CallStream 擷取完整 AI 思考內容 | P6 | ✅ Closed |
| [#11](https://github.com/chimerakang/alice/issues/11) | Remote Control API | P8 | ✅ Closed |
| [#31](https://github.com/chimerakang/alice/issues/31) | **/tasks 指令 — 查看待辦工作清單** | P8.5 | ✅ Closed |
| [#28](https://github.com/chimerakang/alice/issues/28) | **Telegram 圖片訊息支援** | P9 | ✅ Closed |
| [#29](https://github.com/chimerakang/alice/issues/29) | **Telegram 語音訊息轉文字** | P9 | ✅ Closed |
| [#30](https://github.com/chimerakang/alice/issues/30) | **Checkpoints 頁面重新定位：AI 決策歷程 + 安全快照** | P7 | ✅ Closed |
| [#33](https://github.com/chimerakang/alice/issues/33) | **Topic-Project 對應持久化 — 重啟後保留設定** | P8.5 | ✅ Closed |
| [#34](https://github.com/chimerakang/alice/issues/34) | **多張圖片批次處理支援** | P9.5 | ✅ Completed |
| [#35](https://github.com/chimerakang/alice/issues/35) | **🔧 修復跨專案圖片存取問題** | P9.5 | ✅ Completed |
| [#32](https://github.com/chimerakang/alice/issues/32) | **Claude Code Hooks 整合 — 攔截所有互動** | P10 | ✅ Completed |
| [#36](https://github.com/chimerakang/alice/issues/36) | **Claude Code Hooks UI 增強：Dashboard 統計圖表 + 用戶指南** | P10+ | ✅ Completed |
| [#37](https://github.com/chimerakang/alice/issues/37) | **🔍 /project 指令路徑驗證：防止設定不存在的專案目錄** | P11 | ✅ Completed |

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
