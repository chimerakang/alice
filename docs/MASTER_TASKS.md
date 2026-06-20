# Master Tasks

> Auto-generated from GitHub Issues.
> Last updated: 2026-05-29
> Run `/task-sync` to regenerate.

## Status Legend

| Status | Label |
|--------|-------|
| 📋 | 規劃中 |
| 🔄 | 開發中 |
| 🧪 | 測試中 |
| ✅ | 已完成 |
| ⏸️ | 暫停 |
| ❌ | 已取消 |

---

## Phase Overview

| Phase | Description | Progress | Status |
|-------|-------------|----------|--------|
| P1 - Core Backend | Telegram Bot + Claude CLI 整合 | 0% (0/0) | 📋 |
| P10 - Claude Code Hooks | 攔截所有 Claude Code 互動（Terminal/VSCode/TG） | 100% (1/1) | ✅ |
| P11 - User Experience | 指令健全性和用戶體驗改善 | 100% (1/1) | ✅ |
| P12 - Dashboard Analytics | Claude Code Hooks UI 增強：統計圖表 + 用戶指南 | 100% (1/1) | ✅ |
| P13 - Future Enhancements | 未來功能增強與優化 | 76% (29/38) | 🔄 |
| P14 - Commercialization Strategy | Alice AI Agent 商業化發展策略與產品定位 | 16% (1/6) | 🔄 |
| P15 - Hermes Stabilization & Cleanup | Hermes v2 stabilization, memory isolation, token observability, routing cleanup, and execution-engine consolidation. | 83% (15/18) | 🔄 |
| P15 - Parallel Subagents & Orchestration | Implementation of parallel subagent execution with isolated contexts, tool-level parallelism, and orchestration | 91% (11/12) | 🔄 |
| P16 - Multi-Backend Execution | Support for multiple execution backends: Local, Docker, SSH | 100% (1/1) | ✅ |
| P2 - Monitoring | Web Dashboard + API + 監控系統 | 100% (7/7) | ✅ |
| P3 - Data Layer | 持久化 + Git 整合 + Checkpoint | 100% (5/5) | ✅ |
| P4 - Proto-First | Protocol Buffers 架構遷移 | 100% (1/1) | ✅ |
| P5 - Frontend Foundation | React + Vite 框架 + 基礎元件 | 100% (6/6) | ✅ |
| P6 - AI Audit System | AI 開發追蹤核心功能 | 100% (4/4) | ✅ |
| P7 - Dashboard & Analytics | 儀表板強化 + 分析圖表 + 歷史資料查詢 | 100% (4/4) | ✅ |
| P8 - Control API | 遠端控制 + 中斷/回溯 | 100% (1/1) | ✅ |
| P8.5 - TG 指令增強 | /tasks 待辦清單 + Topic 設定持久化 | 100% (2/2) | ✅ |
| P9 - Multimedia Input | 圖片分析 + 語音轉文字 | 100% (2/2) | ✅ |
| P9.5 - Multimedia Enhancement | 多張圖片批次處理 + 媒體群組支援 | 100% (2/2) | ✅ |

---

## P1 - Core Backend (📋 0%)

| # | Task | Issue | Status |
|---|------|-------|--------|

## P10 - Claude Code Hooks (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P10.1 | **Claude Code Hooks 整合：攔截所有 AI Agent 互動** | [#32](https://github.com/chimerakang/alice/issues/32) | ✅ |
| | — 建立 `alice-hook` shell script（讀 stdin JSON → POST 到 Alice API） | | ✅ |
| | — 後端新增 `POST /api/hooks/claude-code` endpoint | | ✅ |
| | — 解析 hook event data（session_id、hook_event_name、transcript_path 等） | | ✅ |
| | — 實作 transcript JSONL 解析器（提取 prompts、responses、tool calls、token usage） | | ✅ |
| | — 將 hook 資料轉換為 DecisionLog 格式存入 SQLite | | ✅ |
| | — 區分資料來源（hook vs telegram）以避免重複 | | ✅ |
| | — WebSocket 即時推送 hook 事件到 Dashboard | | ✅ |
| | — 處理 transcript flush race condition（Stop hook 觸發時 transcript 可能未完全寫入） | | ✅ |
| | — 實作 `alice hooks install` CLI 指令（寫入 .claude/settings.json） | | ✅ |
| | — 支援 global 安裝（~/.claude/settings.json）和 per-project 安裝 | | ✅ |
| | — 安裝時保留現有 settings 不覆蓋 | | ✅ |
| | — 實作 `alice hooks uninstall` 反安裝 | | ✅ |
| | — Dashboard 顯示 hook 來源的互動（標記來源：Terminal / VSCode / Telegram） | | ✅ |
| | — Session 檢視功能（同一 session 的所有互動聚合顯示） | | ✅ |
| | — Token 用量統計包含所有來源 | | ✅ |

## P11 - User Experience (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P11.1 | **🔍 /project 指令需要路徑驗證：防止設定不存在的專案目錄** | [#37](https://github.com/chimerakang/alice/issues/37) | ✅ |

## P12 - Dashboard Analytics (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P12.1 | **📊 Claude Code Hooks UI 增強：Dashboard 統計圖表 + 用戶指南** | [#36](https://github.com/chimerakang/alice/issues/36) | ✅ |
| | — **來源分布餅圖**：顯示 Terminal/VSCode/Telegram 的使用占比 | | ✅ |
| | — **來源效能對比**：各來源的平均執行時間、成功率對比圖表 | | ✅ |
| | — **來源使用趨勢**：過去 7 天各來源的使用變化線圖 | | ✅ |
| | — 建立 `docs/CLAUDE_CODE_HOOKS.md` 設置指南 | | ✅ |
| | — 提供 `.claude/settings.json` 設定範例 | | ✅ |
| | — 包含完整的 Hook Script 範例 | | ✅ |
| | — `GET /api/decisions/sources/stats` 來源統計端點 | | ✅ |
| | — `GET /api/decisions/sources/performance` 效能對比端點 | | ✅ |
| | — Dashboard 可視覺化顯示各來源的分布和效能 | | ✅ |
| | — 用戶可依照文件快速設置 Claude Code Hooks | | ✅ |
| | — 設置指南包含完整的故障排除說明 | | ✅ |

## P13 - Future Enhancements (🔄 76%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P13.1 | **🐛 Telegram 429 Rate Limiting: 多 Agent 同時發送導致訊息遺失** | [#38](https://github.com/chimerakang/alice/issues/38) | ✅ |
| P13.2 | **🐛 Telegram 訊息 UTF-8 編碼錯誤導致發送失敗** | [#39](https://github.com/chimerakang/alice/issues/39) | ✅ |
| P13.3 | **Dashboard Bug: Storage 顯示 — + 端口衝突導致 nginx 代理失效** | [#44](https://github.com/chimerakang/alice/issues/44) | ✅ |
| P13.4 | **【MVP】AI 開發審計系統 - 企業安全合規功能** | [#48](https://github.com/chimerakang/alice/issues/48) | 🔄 |
| | — **完整決策記錄追蹤** | | ☐ |
| | — 記錄每個 AI 決策的完整上下文 | | ☐ |
| | — 儲存使用者請求、AI 回應、工具執行結果 | | ☐ |
| | — 建立決策樹狀結構，支援溯源查詢 | | ☐ |
| | — **程式碼來源標註系統** | | ☐ |
| | — 自動標記 AI 生成的程式碼片段 | | ☐ |
| | — 記錄程式碼生成的資料來源與參考 | | ☐ |
| | — 支援版本控制整合，Git commit 包含來源資訊 | | ☐ |
| | — **智慧風險評估** | | ☐ |
| | — 即時檢測潛在的智慧財產權風險 | | ☐ |
| | — 安全漏洞自動掃描與警告 | | ☐ |
| | — 敏感資訊洩漏檢測（API keys, 密碼等） | | ☐ |
| | — **合規報告生成器** | | ☐ |
| | — 一鍵匯出法務/稽核專用報告 | | ☐ |
| | — 支援多種合規標準（GDPR, SOX, HIPAA） | | ☐ |
| | — 可自訂報告範本與格式 | | ☐ |
| | — **團隊生產力分析** | | ☐ |
| | — ROI 計算與視覺化儀表板 | | ☐ |
| | — 開發時間節省統計 | | ☐ |
| | — 程式碼品質改善指標 | | ☐ |
| | — 團隊使用分析與建議 | | ☐ |
| | — 完成功能 MVP 開發 | | ☐ |
| | — 獲得至少 3 家企業的興趣表達 | | ☐ |
| | — 驗證技術架構可擴展性 | | ☐ |
| | — 確認商業模式可行性 | | ☐ |
| P13.5 | **【商業功能】單機版主管報告系統 - AI 使用效益監控** | [#50](https://github.com/chimerakang/alice/issues/50) | 🔄 |
| | — **團隊 AI 使用健康度指標** | | ☐ |
| | — 個人效益排行榜（誰用AI效果好/差） | | ☐ |
| | — Bug率變化趨勢（AI輔助前後對比） | | ☐ |
| | — 開發效率提升統計 | | ☐ |
| | — 異常使用模式檢測（重複修改同一功能） | | ☐ |
| | — **成本效益分析** | | ☐ |
| | — 每週/每月AI總花費統計 | | ☐ |
| | — 節省工時計算（基於完成任務時間） | | ☐ |
| | — ROI計算和視覺化 | | ☐ |
| | — 成本趨勢預警（使用量異常增長） | | ☐ |
| | — **風險預警系統** | | ☐ |
| | — 檢測「玩AI」vs「工作」的使用模式 | | ☐ |
| | — Token消耗異常警告 | | ☐ |
| | — 連續失敗操作檢測 | | ☐ |
| | — 技術方向偏離提醒 | | ☐ |
| | — **週報自動生成** | | ☐ |
| | — 團隊AI使用摘要 | | ☐ |
| | — 成本效益報告 | | ☐ |
| | — 個人表現排行 | | ☐ |
| | — 異常事件提醒 | | ☐ |
| | — **郵件/通知系統** | | ☐ |
| | — 週報自動發送給主管 | | ☐ |
| | — 異常情況即時通知 | | ☐ |
| | — 可自定義報告頻率和內容 | | ☐ |
| | — **使用行為分析** | | ☐ |
| | — 工具使用模式分析 | | ☐ |
| | — 任務完成時間統計 | | ☐ |
| | — 成功率vs重試率分析 | | ☐ |
| | — 專案上下文關聯分析 | | ☐ |
| | — **效益計算算法** | | ☐ |
| | — 開發時間節省估算 | | ☐ |
| | — 程式碼品質改善度量 | | ☐ |
| | — 學習曲線加速計算 | | ☐ |
| | — 投資回報率計算模型 | | ☐ |
| | — Manager View 頁面載入時間 < 2秒 | | ☐ |
| | — 報告生成時間 < 5秒 | | ☐ |
| | — 異常檢測準確率 > 85% | | ☐ |
| | — 至少3個小團隊願意試用 | | ☐ |
| | — 週報開信率 > 60% | | ☐ |
| | — 功能使用率 > 70% | | ☐ |
| P13.6 | **🔧 擴展 PerformanceMetrics - 增加管理層洞察數據收集** | [#52](https://github.com/chimerakang/alice/issues/52) | 🔄 |
| | — 在 `internal/app/performance.go` 中新增 `ManagerInsights` 結構 | | ☐ |
| | — 擴展 `PerformanceMetrics` 包含用戶識別和任務上下文 | | ☐ |
| | — 添加任務成功率和重試次數追蹤 | | ☐ |
| | — 新增 `manager_insights` 資料表 | | ☐ |
| | — 新增 `task_sessions` 資料表（追蹤任務從開始到完成） | | ☐ |
| | — 更新 SQLite storage 介面支援新數據類型 | | ☐ |
| | — 在 Agent 執行工具時自動記錄任務上下文 | | ☐ |
| | — 檢測任務開始/結束時間點 | | ☐ |
| | — 實現任務成功/失敗的智能判斷邏輯 | | ☐ |
| | — 統計工具使用序列和重試模式 | | ☐ |
| | — 新增 `/api/insights/usage-patterns` 端點 | | ☐ |
| | — 新增 `/api/insights/task-efficiency` 端點 | | ☐ |
| | — 擴展現有效能 API 包含管理層數據 | | ☐ |
| | — 新數據結構能正確記錄任務會話 | | ☐ |
| | — 數據庫遷移正常執行，不影響現有功能 | | ☐ |
| | — API 能返回團隊使用模式分析數據 | | ☐ |
| | — 任務成功率判斷準確率 > 80% | | ☐ |
| P13.7 | **🎨 Manager Dashboard 前端介面 - 主管視角的 AI 使用分析頁面** | [#55](https://github.com/chimerakang/alice/issues/55) | 🔄 |
| | — 建立 `src/pages/ManagerDashboard.tsx` | | ☐ |
| | — 在路由中添加 `/manager` 路由 | | ☐ |
| | — 在導航選單中新增 Manager 入口 | | ☐ |
| | — `src/components/manager/TeamEfficiencyChart.tsx` - 團隊效率趨勢圖 | | ☐ |
| | — `src/components/manager/CostAnalysisCard.tsx` - 成本分析卡片 | | ☐ |
| | — `src/components/manager/UserRankingTable.tsx` - 個人效益排行榜 | | ☐ |
| | — `src/components/manager/AlertsPanel.tsx` - 異常警告面板 | | ☐ |
| | — ROI 趨勢線圖 (Chart.js 或 Recharts) | | ☐ |
| | — 成本 vs 節省工時對比圓餅圖 | | ☐ |
| | — 個人效率雷達圖 | | ☐ |
| | — 工具使用分布橫條圖 | | ☐ |
| | — 整合 WebSocket 接收即時數據更新 | | ☐ |
| | — 實現數據自動刷新機制 | | ☐ |
| | — 添加手動重新整理按鈕 | | ☐ |
| | — 支援桌面和平板顯示 | | ☐ |
| | — 確保圖表在不同螢幕尺寸下正常顯示 | | ☐ |
| | — 優化載入狀態和錯誤處理 | | ☐ |
| | — 點擊個人排行可查看詳細分析 | | ☐ |
| | — hover 顯示具體數值和說明 | | ☐ |
| | — 支援時間範圍選擇 (7天/30天/90天) | | ☐ |
| | — 匯出報告功能 (PDF/CSV) | | ☐ |
| | — 頁面能正確載入並顯示數據 | | ☐ |
| | — 圖表互動功能正常 | | ☐ |
| | — 實時數據更新正常 | | ☐ |
| | — 響應式佈局在不同裝置上正常 | | ☐ |
| | — 首屏載入時間 < 2秒 | | ☐ |
| | — 圖表渲染流暢，無卡頓 | | ☐ |
| | — WebSocket 連線穩定 | | ☐ |
| | — 主管能在 5秒內找到關鍵資訊 | | ☐ |
| | — 數據呈現清晰易懂 | | ☐ |
| | — 警告和異常能快速識別 | | ☐ |
| P13.8 | **📊 自動週報生成系統 - 團隊 AI 使用效益報告** | [#57](https://github.com/chimerakang/alice/issues/57) | 🔄 |
| | — 建立 `internal/app/reports.go` 報告生成模組 | | ☐ |
| | — 實現週報數據收集和分析算法 | | ☐ |
| | — 設計報告模板和格式化邏輯 | | ☐ |
| | — 添加報告歷史存儲功能 | | ☐ |
| | — **執行摘要** | | ☐ |
| | — 團隊總體 AI 使用概況 | | ☐ |
| | — 成本效益快速摘要 | | ☐ |
| | — 重要警告和建議 | | ☐ |
| | — **詳細分析** | | ☐ |
| | — 個人效益排行榜 | | ☐ |
| | — 工具使用統計分析 | | ☐ |
| | — 任務完成效率趨勢 | | ☐ |
| | — 異常使用模式檢測 | | ☐ |
| | — **行動建議** | | ☐ |
| | — 識別需要 AI 使用培訓的成員 | | ☐ |
| | — 成本優化建議 | | ☐ |
| | — 效率提升建議 | | ☐ |
| | — **HTML 格式** (網頁檢視) | | ☐ |
| | — 響應式設計，支援手機閱讀 | | ☐ |
| | — 包含互動式圖表 | | ☐ |
| | — 支援列印友好格式 | | ☐ |
| | — **PDF 格式** (正式報告) | | ☐ |
| | — 專業排版和格式 | | ☐ |
| | — 包含圖表和統計數據 | | ☐ |
| | — 適合存檔和分享 | | ☐ |
| | — **JSON/CSV 格式** (數據匯出) | | ☐ |
| | — 原始數據匯出 | | ☐ |
| | — 便於進一步分析 | | ☐ |
| | — 實現週報自動生成排程 | | ☐ |
| | — 支援自訂報告頻率 (週報/雙週/月報) | | ☐ |
| | — 添加手動觸發報告生成功能 | | ☐ |
| | — 實現報告生成失敗重試機制 | | ☐ |
| | — **郵件發送功能** | | ☐ |
| | — SMTP 設定和郵件範本 | | ☐ |
| | — 支援多個收件人 | | ☐ |
| | — HTML 和純文字雙格式 | | ☐ |
| | — **Telegram 通知** | | ☐ |
| | — 報告生成完成通知 | | ☐ |
| | — 重要異常即時通知 | | ☐ |
| | — 簡化版報告摘要 | | ☐ |
| | — **Web Dashboard 整合** | | ☐ |
| | — 報告歷史瀏覽頁面 | | ☐ |
| | — 線上報告檢視器 | | ☐ |
| | — 能自動生成包含所有必要資訊的週報 | | ☐ |
| | — 排程系統準時觸發報告生成 | | ☐ |
| | — 多格式輸出正常 (HTML/PDF/JSON) | | ☐ |
| | — 郵件發送成功率 > 95% | | ☐ |
| | — 報告數據準確性 > 95% | | ☐ |
| | — PDF 格式專業美觀 | | ☐ |
| | — HTML 版本響應式正常 | | ☐ |
| | — 報告生成時間 < 10秒 | | ☐ |
| | — 管理者能快速 (< 2分鐘) 掌握團隊狀況 | | ☐ |
| | — 警告和建議具體可行 | | ☐ |
| | — 報告易於理解和分享 | | ☐ |
| P13.9 | **🚨 智能異常檢測系統 - AI 使用模式風險預警** | [#59](https://github.com/chimerakang/alice/issues/59) | 🔄 |
| | — **成本異常** | | ☐ |
| | — Token 消耗量突然激增 (> 平常 200%) | | ☐ |
| | — 單次對話成本過高 (> $5) | | ☐ |
| | — 連續高成本操作檢測 | | ☐ |
| | — **效率異常** | | ☐ |
| | — 同一任務重複執行 > 5 次 | | ☐ |
| | — 任務完成時間異常延長 | | ☐ |
| | — 工具使用序列不合理 (如反覆 read/write 同一檔案) | | ☐ |
| | — **行為異常** | | ☐ |
| | — 非工作時間大量 AI 使用 | | ☐ |
| | — 與專案無關的操作過多 | | ☐ |
| | — 長時間無進展的工作會話 | | ☐ |
| | — 建立 `internal/app/anomaly_detector.go` | | ☐ |
| | — 實現基線建立算法 (正常使用模式學習) | | ☐ |
| | — 開發即時異常評分系統 | | ☐ |
| | — 實現異常嚴重性分級 (低/中/高/緊急) | | ☐ |
| | — **即時監控** | | ☐ |
| | — 每次 AI 操作後進行異常評估 | | ☐ |
| | — 累積異常評分超過閾值時觸發警告 | | ☐ |
| | — 支援不同類型異常的獨立閾值設定 | | ☐ |
| | — **批次分析** | | ☐ |
| | — 每小時進行一次深度分析 | | ☐ |
| | — 檢測較長週期的異常模式 | | ☐ |
| | — 生成趨勢異常報告 | | ☐ |
| | — **多通道通知** | | ☐ |
| | — Telegram 即時通知 | | ☐ |
| | — 郵件警告 (嚴重異常) | | ☐ |
| | — Dashboard 警告面板 | | ☐ |
| | — WebSocket 推送到前端 | | ☐ |
| | — **分級通知策略** | | ☐ |
| | — 警告確認和關閉機制 | | ☐ |
| | — 誤報標記和學習功能 | | ☐ |
| | — 警告歷史查詢和統計 | | ☐ |
| | — 自動警告降級 (問題解決後) | | ☐ |
| | — 使用 Isolation Forest 檢測異常點 | | ☐ |
| | — K-means 聚類分析用戶行為模式 | | ☐ |
| | — 自動調整異常檢測閾值 | | ☐ |
| | — 季節性趨勢分析 | | ☐ |
| | — 異常點檢測和預測 | | ☐ |
| | — 自適應基線調整 | | ☐ |
| | — 真正異常檢測率 > 80% | | ☐ |
| | — 誤報率 < 15% | | ☐ |
| | — 檢測延遲 < 5 分鐘 | | ☐ |
| | — 即時檢測不影響正常功能效能 | | ☐ |
| | — 批次分析在 1 分鐘內完成 | | ☐ |
| | — 警告通知 99% 成功送達 | | ☐ |
| | — 警告描述清晰易懂 | | ☐ |
| | — 提供具體的處理建議 | | ☐ |
| | — 支援快速確認和關閉 | | ☐ |
| P13.10 | **💰【商業功能】Alice SecureGuard - AI 開發防洩密系統** | [#60](https://github.com/chimerakang/alice/issues/60) | 🔄 |
| | — **Token 模式識別引擎** | | ☐ |
| | — AWS/Azure/GCP access keys | | ☐ |
| | — Database connection strings   | | ☐ |
| | — API keys (Stripe, Slack, etc.) | | ☐ |
| | — SSH private keys | | ☐ |
| | — JWT secrets 和加密金鑰 | | ☐ |
| | — **自動檢查點 + 修復** | | ☐ |
| | — 檢測到敏感資訊立即建立 checkpoint | | ☐ |
| | — 自動替換為環境變數引用 | | ☐ |
| | — 更新 .env.example 範本 | | ☐ |
| | — 確保 .env 在 .gitignore 中 | | ☐ |
| | — **提交前安全掃描** | | ☐ |
| | — 全檔案樹敏感資訊掃描 | | ☐ |
| | — .env 檔案洩漏檢測 | | ☐ |
| | — 硬編碼密碼檢測 | | ☐ |
| | — 第三方套件安全漏洞檢查 | | ☐ |
| | — **智能修復建議** | | ☐ |
| | — 一鍵修復所有安全問題 | | ☐ |
| | — 生成安全修復報告 | | ☐ |
| | — 可選擇個別修復或全部修復 | | ☐ |
| | — **可配置安全政策** | | ☐ |
| | — 自定義敏感資訊規則 | | ☐ |
| | — 部門別安全等級設定 | | ☐ |
| | — 專案別風險閾值 | | ☐ |
| | — 合規標準模板 (GDPR/SOX/ISO27001) | | ☐ |
| | — **中央化監控** | | ☐ |
| | — 全公司安全事件儀表板 | | ☐ |
| | — 風險熱點分析 | | ☐ |
| | — 開發者安全分數排行 | | ☐ |
| | — 自動生成合規報告 | | ☐ |
| | — **政策管理** | | ☐ |
| | — 安全規則編輯器 | | ☐ |
| | — 例外情況審批流程 | | ☐ |
| | — 緊急情況處理程序 | | ☐ |
| | — 安全培訓追蹤 | | ☐ |
| | — **監控儀表板** | | ☐ |
| | — 即時安全事件流 | | ☐ |
| | — 風險趨勢分析 | | ☐ |
| | — 團隊安全評分 | | ☐ |
| | — 合規狀態總覽 | | ☐ |
| | — **完整審計追蹤** | | ☐ |
| | — 每個 AI 操作的完整記錄 | | ☐ |
| | — 敏感資訊處理日誌 | | ☐ |
| | — 安全事件時間軸 | | ☐ |
| | — 用戶行為分析 | | ☐ |
| | — **自動報告生成** | | ☐ |
| | — 週/月安全報告 | | ☐ |
| | — 合規證明文件 | | ☐ |
| | — 風險評估報告 | | ☐ |
| | — 董事會級別摘要 | | ☐ |
| | — **背景運作** | | ☐ |
| | — 不干擾正常開發流程 | | ☐ |
| | — 智能學習常用模式 | | ☐ |
| | — 減少誤報率 | | ☐ |
| | — 快速操作響應 (<0.5秒) | | ☐ |
| | — **智能建議** | | ☐ |
| | — 安全最佳實踐提示 | | ☐ |
| | — 個人化安全培訓 | | ☐ |
| | — 程式碼安全評分 | | ☐ |
| | — 同儕比較和激勵 | | ☐ |
| | — 檢測準確率 > 95% | | ☐ |
| | — 誤報率 < 5% | | ☐ |
| | — 響應時間 < 0.5 秒 | | ☐ |
| | — 系統可用性 > 99.9% | | ☐ |
| | — 第一年獲得 100 個付費企業客戶 | | ☐ |
| | — 客戶留存率 > 90% | | ☐ |
| | — 年度經常性收入 > $500 萬 | | ☐ |
| | — 淨推薦分數 (NPS) > 50 | | ☐ |
| | — 在 AI 開發安全領域建立領導地位 | | ☐ |
| | — 獲得主要安全認證 (SOC2, ISO27001) | | ☐ |
| | — 成為行業標準解決方案 | | ☐ |
| | — 被主要分析師機構 (Gartner) 認可 | | ☐ |
| P13.11 | **🔍 智能 Token 檢測引擎 - SecureGuard 核心功能** | [#61](https://github.com/chimerakang/alice/issues/61) | 🔄 |
| | — **常見 Token 模式庫** | | ☐ |
| | — AWS Access/Secret Keys (AKIA*, ASIA*) | | ☐ |
| | — Google API Keys (AIza*) | | ☐ |
| | — Azure Keys 和 Connection Strings   | | ☐ |
| | — GitHub Personal Access Tokens (ghp_*, gho_*) | | ☐ |
| | — Stripe API Keys (sk_live_*, pk_live_*) | | ☐ |
| | — Database URLs (postgres://, mysql://, mongodb://) | | ☐ |
| | — JWT Tokens 和 Secrets | | ☐ |
| | — SSH Private Keys (-----BEGIN RSA PRIVATE KEY-----) | | ☐ |
| | — API Endpoints 含認證參數 | | ☐ |
| | — **動態模式學習** | | ☐ |
| | — 企業自定義 Token 格式學習 | | ☐ |
| | — 上下文相關的敏感度評估 | | ☐ |
| | — 誤報模式識別和排除 | | ☐ |
| | — 持續學習用戶標記結果 | | ☐ |
| | — **多層檢測策略** | | ☐ |
| | — **實時檢測引擎** | | ☐ |
| | — 檔案變更監聽 | | ☐ |
| | — 增量檢測 (只檢查變更部分) | | ☐ |
| | — 異步處理避免阻塞 | | ☐ |
| | — 檢測結果快取優化 | | ☐ |
| | — **智能替換策略** | | ☐ |
| | — **環境變數管理** | | ☐ |
| | — 自動生成 .env.example | | ☐ |
| | — 確保 .env 在 .gitignore | | ☐ |
| | — 生成安全的隨機 secrets | | ☐ |
| | — Docker/K8s secrets 整合 | | ☐ |
| | — **觸發機制增強** | | ☐ |
| | — **安全檢查點類型** | | ☐ |
| | — `TriggerSecurityBreach` - 檢測到高風險 token | | ☐ |
| | — `TriggerPreCommit` - Git 提交前安全檢查 | | ☐ |
| | — `TriggerPolicyViolation` - 違反企業安全政策 | | ☐ |
| | — `TriggerAutofix` - 自動修復前的備份 | | ☐ |
| | — **可配置規則系統** | | ☐ |
| | — **部門層級政策** | | ☐ |
| | — 不同團隊不同安全等級 | | ☐ |
| | — 專案別風險閾值設定 | | ☐ |
| | — 角色別權限控制 | | ☐ |
| | — 例外申請審批流程 | | ☐ |
| | — 單檔案檢測 < 100ms | | ☐ |
| | — 全專案掃描 < 5s (1000 檔案) | | ☐ |
| | — 即時檢測延遲 < 50ms | | ☐ |
| | — Checkpoint 建立 < 2s | | ☐ |
| | — Token 檢測準確率 > 95% | | ☐ |
| | — 誤報率 < 5% | | ☐ |
| | — 漏報率 < 1% (高風險 token) | | ☐ |
| | — 自動修復成功率 > 90% | | ☐ |
| | — 檢測過程中不記錄實際 token 內容 | | ☐ |
| | — 僅記錄 token 類型和位置資訊 | | ☐ |
| | — 本地處理，不上傳到雲端 | | ☐ |
| | — 檢測結果加密存儲 | | ☐ |
| | — 遵守企業資料保護政策 | | ☐ |
| | — 支援資料本地化要求 | | ☐ |
| | — 提供資料清除功能 | | ☐ |
| | — 審計日誌訪問控制 | | ☐ |
| | — 各種 token 模式檢測測試 | | ☐ |
| | — 誤報和漏報測試用例 | | ☐ |
| | — 效能壓力測試 | | ☐ |
| | — 邊界條件測試 | | ☐ |
| | — 與 Checkpoint 系統整合測試 | | ☐ |
| | — Git hook 整合測試 | | ☐ |
| | — 企業政策引擎測試 | | ☐ |
| | — 多用戶權限測試 | | ☐ |
| | — 繞過檢測嘗試測試 | | ☐ |
| | — 資料洩漏風險評估 | | ☐ |
| | — 權限提升漏洞測試 | | ☐ |
| | — 拒絕服務攻擊測試 | | ☐ |
| | — 支援 Docker 容器化部署 | | ☐ |
| | — 企業防火牆內網部署 | | ☐ |
| | — 多實例負載均衡 | | ☐ |
| | — 零停機更新機制 | | ☐ |
| | — 檢測引擎效能監控 | | ☐ |
| | — 安全事件統計 | | ☐ |
| | — 用戶行為分析 | | ☐ |
| | — 系統健康度監控 | | ☐ |
| P13.12 | **Performance Bug: 時間範圍篩選器對 Tool Distribution 無效 — API 未支援時間參數** | [#62](https://github.com/chimerakang/alice/issues/62) | ✅ |
| P13.13 | **🐛 Hook 腳本未提取 session duration 和 token 數據** | [#68](https://github.com/chimerakang/alice/issues/68) | ✅ |
| P13.14 | **Security 頁面：PII Detection Records 缺乏上下文資訊，無法判斷問題內容** | [#69](https://github.com/chimerakang/alice/issues/69) | ✅ |
| | — Project Context：Modal 中清楚顯示 project_path | | ☐ |
| | — Conversation Context：Modal 中顯示 message ID 和 message type | | ☐ |
| | — User Tracking：Modal 中獨立顯示 User ID | | ☐ |
| | — 所有信息都正確填充，無空值或 N/A | | ☐ |
| | — 編譯成功，Dashboard 可訪問，PII 記錄表格正常運作 | | ☐ |
| P13.15 | **Security 頁面：Events Trend 圖表未跟隨時間篩選器 + 標題硬編碼** | [#70](https://github.com/chimerakang/alice/issues/70) | ✅ |
| P13.16 | **🐛 Telegram /tasks 無法顯示 GitHub Issues（private repo 認證失敗）** | [#71](https://github.com/chimerakang/alice/issues/71) | ✅ |
| P13.17 | **P13: Dynamic Model Routing - 智慧模型路由降低 Token 成本** | [#72](https://github.com/chimerakang/alice/issues/72) | ✅ |
| | — 1.1: CLIClient model override 參數 | | ✅ |
| | — 1.2: Agent.selectModel() 方法 | | ✅ |
| | — 1.3: 路由規則引擎 + 預設規則 | | ✅ |
| | — 1.4: Session 隔離策略（Haiku one-shot） | | ✅ |
| | — 1.5: Config model_routing 設定區塊 | | ✅ |
| | — 1.6: Decision Log 記錄 routing 資訊 | | ✅ |
| | — 1.7: `/fast` `/deep` Telegram 指令 | | ✅ |
| | — 2.1: classifyTaskComplexity() 函數（複用 OpenAI API Key） | | ✅ |
| | — 2.2: 三層 Fallback 策略整合 | | ✅ |
| | — 2.3: 路由延遲與成本監控 | | ✅ |
| | — 3.1: 模型使用比例圓餅圖 | | ✅ |
| | — 3.2: 節省金額計算與顯示 | | ✅ |
| | — 3.3: 路由決策歷史頁面 | | ✅ |
| | — 4.1: /api/prompt endpoint 支援路由 | | ✅ |
| | — 4.2: Extension 透過 API 提交帶路由的請求 | | ✅ |
| P13.18 | **P13: Per-Model Cost Tracking - 按模型記錄 Token 成本與用量** | [#73](https://github.com/chimerakang/alice/issues/73) | ✅ |
| | — Database migration（decision_logs + performance_metrics 加 model 欄位） | | ✅ |
| | — TokenStats struct 加 Model 欄位 | | ✅ |
| | — DecisionLog struct 加 Model / RoutingReason / RoutingLatency | | ✅ |
| | — RecordAPICall() 加 model 參數 | | ✅ |
| | — logDecision() 使用獨立 model 欄位 | | ✅ |
| | — SaveDecisionLog() INSERT 新欄位 | | ✅ |
| | — GetDecisionLogs() / query 函數 SELECT 新欄位 | | ✅ |
| | — 模型費率表 ModelPricing | | ✅ |
| | — GET /api/costs/by-model endpoint | | ✅ |
| | — GET /api/costs/summary endpoint | | ✅ |
| | — 向後相容：舊紀錄 model 預設為 config 中的模型 | | ✅ |
| P13.19 | **P13: Savings Calculator - 智慧路由省錢可視化（商業賣點）** | [#74](https://github.com/chimerakang/alice/issues/74) | ✅ |
| | — `GetCostSavings()` SQL 查詢（計算實際 vs 假設成本） | | ☐ |
| | — `GET /api/costs/savings` endpoint | | ☐ |
| | — 模型費率表 `ModelPricing`（用於假設成本計算） | | ☐ |
| | — `/savings` Telegram 指令 | | ☐ |
| | — SavingsBanner 元件（節省金額 + 百分比 + 進度條） | | ☐ |
| | — ModelDistributionChart 圓餅圖（Haiku/Sonnet/Opus 比例） | | ☐ |
| | — CostTrendChart 雙線圖（實際 vs 假設，shaded area = savings） | | ☐ |
| | — Decision Timeline 增強（每個 decision 標記模型 + 節省金額） | | ☐ |
| | — Dashboard 頁面整合 | | ☐ |
| | — Performance 頁面整合 | | ☐ |
| P13.20 | **回填歷史資料缺失的 model 與費用欄位** | [#75](https://github.com/chimerakang/alice/issues/75) | ✅ |
| | — 為 `decision_logs` 表中 model 為空的紀錄回填 model 為 `claude-sonnet-4-5-20250929` | | ✅ |
| | — 根據 tokens_input/tokens_output 計算並回填 `cost_usd`（使用 Sonnet 定價） | | ✅ |
| | — 為 `performance_metrics` 表中 model 為空的紀錄回填 model 為 `claude-sonnet-4-5-20250929` | | ✅ |
| | — 根據 tokens_used 計算並回填 `estimated_cost` | | ✅ |
| | — 確保修復腳本具有冪等性（重複執行不會造成問題） | | ✅ |
| | — 修復後驗證 /api/costs/savings 端點數據正確性 | | ✅ |
| P13.21 | **Bot 多國語系支援 — 可切換顯示語言** | [#76](https://github.com/chimerakang/alice/issues/76) | ✅ |
| | — 設計 i18n 架構（語系檔格式、載入機制） | | ✅ |
| | — 建立語系檔目錄結構（如 `locales/zh-TW.json`, `locales/en.json`） | | ✅ |
| | — 抽取現有硬編碼文字到語系檔 (69 hardcoded messages + 134 message keys) | | ✅ |
| | — 實作 Telegram Bot `/lang` 指令切換語言 | | ✅ |
| | — 每個 chat 獨立儲存語系偏好（持久化到 SQLite） | | ✅ |
| | — 前端 Dashboard 語系切換功能 (P3 - 待實作) | | ☐ |
| | — 初始支援語系：繁體中文（zh-TW）、英文（en） | | ✅ |
| | — 撰寫新增語系的開發指南文件 | | ✅ |
| P13.22 | **/usage 指令增加按模型分類的 token 用量與費用顯示** | [#77](https://github.com/chimerakang/alice/issues/77) | ✅ |
| | — 修改 `/usage` handler，增加從 storage 查詢 per-model token 用量 | | ✅ |
| | — 顯示每個模型的呼叫次數、input/output tokens、費用 | | ✅ |
| | — 無 storage 或無數據時 graceful fallback（僅顯示現有整體統計） | | ✅ |
| | — 在 /Volumes/eclipse/projects/gameA 讀取 tools/ai_tests/scenarios/town_npc_trading.json 與 tools/ai_tests/scenarios/town_npc_interaction_order.json，整理 scenario 內覆蓋的 NPC、購買/收購步驟、金幣與背包斷言重點行號作為留言證據 | | ✅ |
| | — 在 /Volumes/eclipse/projects/gameA 執行 ./tools/godot_validate.sh text、godot --path . --headless --script tools/validate_town_maps.gd 與 python3 -m tools.ai_tests.run --list | rg 'town_npc_trading|town_npc_interaction_order'，記錄最新輸出片段 | | ✅ |
| | — 在 /Volumes/eclipse/projects/gameA 嘗試 python3 -m tools.ai_tests.run town_npc_trading 與 python3 -m tools.ai_tests.run town_npc_interaction_order，若 AI test server 未連線則記錄錯誤訊息作為剩餘風險證據，不修改任何檔案 | | ✅ |
| | — 在 /Volumes/eclipse/projects/gameA 使用 gh issue comment 77 -F 發表結構化中文留言，內容包含三項驗收對應證據、s2/s3 命令輸出摘要、scenario 路徑與行號、剩餘風險（互動實測需遊戲調試模式），最後 gh issue view 77 --comments | tail -n 40 確認張貼成功 | | ✅ |
| P13.23 | **Add project_path field to performance_metrics table for per-project token cost tracking** | [#78](https://github.com/chimerakang/alice/issues/78) | ✅ |
| | — Add `project_path` VARCHAR column to `performance_metrics` table migration | | ☐ |
| | — Update `PerformanceMetrics` struct in performance.go to include ProjectPath field | | ☐ |
| | — Modify `InsertPerformanceMetric` function to persist project_path | | ☐ |
| | — Add optional `projectPath` parameter to `GetCostSavings()" for project filtering | | ☐ |
| | — Update Telegram `/usage" and `/savings" commands to support per-project queries | | ☐ |
| | — Update Web API endpoints to accept optional project_dir parameter | | ☐ |
| | — Test per-project cost tracking end-to-end | | ☐ |
| P13.24 | **🐛 Smart Routing 導致對話上下文丟失：Model 切換時強制清空 Session** | [#79](https://github.com/chimerakang/alice/issues/79) | ✅ |
| | — 移除強制清空 sessionID 邏輯 | | ✅ |
| | — 改進決策日誌以記錄同一 session 內的 model 切換 | | ✅ |
| | — 改進成本計算，分別追蹤每個 model 的 token 使用 | | ✅ |
| | — 測試跨 model session 的對話連貫性 | | ✅ |
| | — 驗證成本計算的準確性 | | ✅ |
| P13.25 | **🎨 Cost Trend 頁面 UI 修正：標籤更新 + 卡片橫向排列** | [#80](https://github.com/chimerakang/alice/issues/80) | ✅ |
| P13.26 | **🔀 Smart Routing 上下文橋接：continuation 偵測 + model 切換時保留對話記憶** | [#81](https://github.com/chimerakang/alice/issues/81) | ✅ |
| | — 新增 `isContinuationMessage()` 函數（偵測短繼續語） | | ☐ |
| | — 繼續語跳過 triage，保持當前 model + session | | ☐ |
| | — 在 `projectState` 加入 `recentMessages []contextMessage`（最近 5 輪） | | ☐ |
| | — 每輪結束後更新 `recentMessages` | | ☐ |
| | — Model 切換時呼叫 `buildContextBridge()` 產生摘要 | | ☐ |
| | — 將摘要 prepend 到新 session 的訊息 | | ☐ |
| | — Phase 1 hybrid triage：本地算法高信心 → 跳過 Haiku，模糊才呼叫 Haiku | | ☐ |
| P13.27 | **Agent 媒體發送功能 - 圖片/影片/文件回傳到 Telegram chat** | [#82](https://github.com/chimerakang/alice/issues/82) | ✅ |
| | — 文件類型檢測與驗證邏輯 | | ✅ |
| | — 文件大小檢查（對比 Telegram 限制） | | ✅ |
| | — 自動識別模式 - Agent 掃描和發送邏輯 | | ✅ |
| | — 手動指定模式 - 新增用戶命令（如 `/send-file`） | | ✅ |
| | — Telegram API 整合（SendPhoto/SendVideo/SendDocument） | | ✅ |
| | — 錯誤處理和用戶提示 | | ✅ |
| P13.28 | **實現 Bot 網頁截圖預覽功能 - 使用 skill 的 playwright-cli 支援任意 URL 截圖** | [#83](https://github.com/chimerakang/alice/issues/83) | ✅ |
| | — 整合 skill 的 playwright-cli 截圖流程 | | ✅ |
| | — 實現 Bot 後端截圖邏輯 | | ✅ |
| | — 新增 /preview <URL> 指令 | | ✅ |
| | — 測試各種 URL（外部、內部、無效） | | ✅ |
| | — 性能優化和超時處理 | | ✅ |
| | — 文檔更新 | | ✅ |
| P13.29 | **🧠 OpusPlan 兩階段模型調用 - 計劃用 Opus、執行用 Sonnet** | [#85](https://github.com/chimerakang/alice/issues/85) | ✅ |
| | — `api.go`: `CallStream()` 新增 `maxTurns` 參數支援 | | ☐ |
| | — `agent.go`: 實作兩階段調用邏輯（plan + execute） | | ☐ |
| | — `agent.go`: 計劃階段的 system prompt 設計 | | ☐ |
| | — `telegram.go`: 新增 `/plan` 命令處理 | | ☐ |
| | — `telegram.go`: auto mode 整合（複雜度 ≥ 6 自動啟用） | | ☐ |
| | — `main.go` / `config`: 新增 `plan_model` 設定項 | | ☐ |
| | — i18n: 新增 zh-TW / en 訊息 keys | | ☐ |
| | — 成本追蹤：兩階段的 token/cost 合併計算 | | ☐ |
| | — 測試：驗證計劃品質與執行正確性 | | ☐ |
| P13.30 | **feat: Implement auto-skill generation system** | [#86](https://github.com/chimerakang/alice/issues/86) | ✅ |
| | — 在 storage.go 中新增 `auto_skills` 表的 schema 和遷移 | | ☐ |
| | — 實現 AutoSkill 結構體和數據模型（`internal/app/skills.go`） | | ☐ |
| | — 在 agent.go 中實現 Skill 識別邏輯（工具序列監測） | | ☐ |
| | — 實現基於關鍵字的 Skill 檢索 | | ☐ |
| | — 修改 system prompt 生成邏輯，整合 Skill 注入 | | ☐ |
| | — 實現 Skill 匹配算法（關鍵字 + 工具類型比對） | | ☐ |
| | — 添加注入長度限制（1000 tokens cap） | | ☐ |
| | — 實現時間過期機制（30 天 inactive） | | ☐ |
| | — 實現 git diff 變更偵測失效 | | ☐ |
| | — 實現成功率追蹤和自動停用 | | ☐ |
| | — 添加 Telegram 命令：`/skills`（列出）、`/skill delete`（刪除） | | ☐ |
| | — Dashboard 顯示 Skill 列表和統計 | | ☐ |
| | — 記錄 Skill 命中率和節省的 token 數 | | ☐ |
| P13.31 | **feat: Implement cron scheduler for automated tasks** | [#89](https://github.com/chimerakang/alice/issues/89) | ✅ |
| | — 新增 `internal/app/cron.go` | | ☐ |
| | — 實現 CronScheduler 結構體 | | ☐ |
| | — 在 storage.go 中新增 `scheduled_tasks` 表和 CRUD | | ☐ |
| | — 在 Main() 啟動時載入並啟動排程器 | | ☐ |
| | — 優雅關閉（context cancellation） | | ☐ |
| | — 實現 `/cron add` 命令（含自然語言解析） | | ☐ |
| | — 實現 `/cron list`、`/cron delete`、`/cron pause`、`/cron resume` | | ☐ |
| | — 實現 `/cron run`（手動觸發） | | ☐ |
| | — 任務執行結果格式化為 Telegram 訊息 | | ☐ |
| | — 所有命令的 i18n 支援（zh-TW、en） | | ☐ |
| | — 支援 "command" 類型（直接執行 shell 命令） | | ☐ |
| | — 支援 "prompt" 類型（發送 prompt 給 Agent 並回傳結果） | | ☐ |
| | — 執行超時控制（預設 5 分鐘） | | ☐ |
| | — 失敗重試機制（最多 1 次重試） | | ☐ |
| | — API 端點：`GET /api/cron/tasks`、`POST /api/cron/tasks` | | ☐ |
| | — Dashboard 排程任務管理頁面 | | ☐ |
| | — 執行歷史和成功率統計 | | ☐ |
| | — WebSocket 推送任務執行狀態 | | ☐ |
| | — 排程任務只能由建立者管理（chatID 綁定） | | ☐ |
| | — 每個用戶的任務數量上限（預設 20） | | ☐ |
| | — 最小排程間隔限制（≥ 5 分鐘，防止濫用） | | ☐ |
| | — "command" 類型受限於 ToolExecutor 允許的命令 | | ☐ |
| | — "prompt" 類型受限於用戶的 allowed_user_ids 權限 | | ☐ |
| P13.32 | **bug: CallStream 丟棄 CLI exit error 時的 streaming 結果導致「執行錯誤」** | [#90](https://github.com/chimerakang/alice/issues/90) | ✅ |
| | — `api.go` `CallStream()`: CLI exit error 時若 `finalResp` 存在，返回 `finalResp` 而非 `nil` | | ☐ |
| | — `api.go` `Call()` (非 streaming): 同樣的修復 | | ☐ |
| | — `enhanced_cli.go`: 檢查並修復相同模式 | | ☐ |
| | — 將 `--max-turns` 改為可設定（config.json） | | ☐ |
| | — 預設值從 25 提高到合理值（如 50） | | ☐ |
| | — 當達到 max-turns 時，回覆中包含提示訊息告知用戶可以繼續 | | ☐ |
| | — 追蹤每個 session 的累積 turns 數 | | ☐ |
| | — 當 session 過長時自動開新 session | | ☐ |
| | — 帶入最近對話的 context 摘要到新 session | | ☐ |
| | — Haiku triage timeout (`signal: killed`): 考慮增加 timeout 或改用更輕量的方式 | | ☐ |
| | — Voice analysis 空 stderr: 檢查 stdout JSON 中的 error 訊息 | | ☐ |
| P13.33 | **feat: Claude Design 整合 - UI 原型與設計生成 [等待 API 開放]** | [#91](https://github.com/chimerakang/alice/issues/91) | ✅ |
| | — 調查 Claude Design API 文檔和認證方式 | | ☐ |
| | — 實作 `DesignGenerator` 核心類別 | | ☐ |
| | — 新增 Telegram `/design` 命令 | | ☐ |
| | — 實作設計系統自動掃描 | | ☐ |
| | — 新增資料庫遷移和存儲層 | | ☐ |
| | — 建立 Web 儀表板設計頁面 | | ☐ |
| | — 整合程式碼移交流程 | | ☐ |
| | — 撰寫整合測試 | | ☐ |
| | — 更新文檔 (CLAUDE.md) | | ☐ |
| | — 更新 i18n 訊息（zh-TW.json, en.json） | | ☐ |
| P13.34 | **feat: Telegram 對話式 UI 原型生成 (/prototype 命令)** | [#92](https://github.com/chimerakang/alice/issues/92) | ✅ |
| | — 建立 `PrototypeManager` 核心類別 (`internal/app/prototype.go`) | | ☐ |
| | — 實作 Claude Code CLI prompt 策略（生成 + 修改） | | ☐ |
| | — 整合 chromedp 伺服器端截圖 | | ☐ |
| | — 新增 Telegram `/prototype` 命令處理 | | ☐ |
| | — 實作 Inline Keyboard 主選單 + 子選單 | | ☐ |
| | — 實作 Callback Query 處理（按鈕點擊） | | ☐ |
| | — 實作回覆訊息偵測（自然語言修改） | | ☐ |
| | — 新增 `/prototype-edit`、`/prototype-list`、`/prototype-export` | | ☐ |
| | — 新增資料庫 prototypes 表和 CRUD 操作 | | ☐ |
| | — 新增 i18n 訊息（zh-TW.json, en.json） | | ☐ |
| | — 撰寫測試 | | ☐ |
| | — 更新 CLAUDE.md 文檔 | | ✅ |
| | — Issue #92 全 12 項 checklist 實作完成並驗證綠燈，無剩餘工作；輸出一句話狀態報告表示 #92 實作完成、issue 仍為 OPEN，等待使用者明訓授權才進行 commit / push / `gh issue close 92`，不進行任何 git 或 GitHub 寫入操作 | | ✅ |
| P13.35 | **Model routing 造成 session context 丟失：改採 Sticky + Follow-up detection** | [#93](https://github.com/chimerakang/alice/issues/93) | ✅ |
| | — [agent.go](../blob/main/internal/app/agent.go) — 重構 model routing 邏輯，加入 sticky 判斷 | | ☐ |
| | — [agent.go](../blob/main/internal/app/agent.go) — 重寫 \`isContinuationMessage()\` | | ☐ |
| | — [security.go](../blob/main/internal/app/security.go) — \`ModelRoutingConfig\` 加入 \`SessionIdleTimeout\`、\`StickyMode\` 設定 | | ☐ |
| | — [telegram.go](../blob/main/internal/app/telegram.go) — \`/fast\` \`/smart\` \`/deep\` 切換時提示上下文將重置 | | ☐ |
| | — [telegram.go](../blob/main/internal/app/telegram.go) — 加入 \`/clear\` 指令主動重置 session | | ☐ |
| | — 新增 i18n 訊息 key 到 \`zh-TW.json\` 與 \`en.json\` | | ☐ |
| | — 更新 \`docs/CLAUDE.md\` 描述新的 routing 行為 | | ☐ |
| | — 連續對話中，auto-triage 不會在中途切換模型 | | ☐ |
| | — 手動切換模型時，使用者收到「上下文將重置」提示 | | ☐ |
| | — Session 閒置 > 閒置超時後，下一則訊息重新進入 triage | | ☐ |
| | — Follow-up 測試案例（上述事故流程）可重現修復結果 | | ☐ |
| | — Inspect the current implementation and tests around GitHub #93 in internal/app/agent.go, internal/app/security.go, internal/app/telegram.go, i18n JSON files, docs/CLAUDE.md, and existing git diff to determine what is already implemented and what remains. | | ✅ |
| | — Modify internal/app/agent.go so model routing uses sticky session behavior by default, only re-triages after configured idle timeout or explicit reset/model switch, and prevents auto-triage from switching models during continuous conversations. | | ✅ |
| | — Rewrite isContinuationMessage() in internal/app/agent.go and add or update focused Go tests covering explicit continuation phrases, pronoun references, short why/how follow-ups, and short-message follow-up behavior from the issue scenario. | | ✅ |
| | — Update internal/app/security.go to add ModelRoutingConfig fields SessionIdleTimeout and StickyMode with conservative defaults and ensure existing config loading/validation continues to work. | | ✅ |
| | — Update internal/app/telegram.go so /fast, /smart, and /deep manual model switches warn that context will reset, and add /clear and /reset handling to explicitly reset the current session. | | ✅ |
| | — Add the required i18n message keys for model-switch reset warnings and clear/reset responses in zh-TW.json and en.json, preserving valid JSON formatting. | | ✅ |
| | — Update docs/CLAUDE.md to document the new sticky routing behavior, follow-up detection rules, manual model switch reset warning, /clear or /reset behavior, and idle-timeout re-triage behavior. | | ✅ |
| | — Run focused verification for #93, including go test ./internal/app/... and any targeted tests for routing, telegram commands, i18n loading, idle timeout, and follow-up detection; fix failures caused by the implementation. | | ✅ |
| P13.36 | **整合 OpenAI Image Generation (gpt-image-2 / DALL-E) 為遊戲開發鋪路** | [#129](https://github.com/chimerakang/alice/issues/129) | 🔄 |
| | — 決定方案 A vs B（或混合） | | ☐ |
| | — 新增 config schema：`multimedia.image_generation_provider`、`multimedia.image_model`（預設 `gpt-image-2`，fallback `dall-e-3`）、預設尺寸、單次/每日配額 | | ☐ |
| | — 實作 `generateImage(ctx, prompt, opts) → filepath`，存入 `TempDownloadDir` | | ☐ |
| | — 實作 image edit/variation（image-to-image）走 `/v1/images/edits` | | ☐ |
| | — 與 `sendMediaFile()` 整合送回 Telegram | | ☐ |
| | — 加 i18n keys（觸發/失敗/配額耗盡訊息，zh-TW + en）— 遵循 skill `alice-i18n` | | ☐ |
| | — 安全限制：prompt 長度、頻率限制（per chat / per user）、**費用告警**（output $30/1M tokens） | | ☐ |
| | — 加入 quality stats / cost tracking（沿用 [`internal/app/quality_stats.go`](internal/app/quality_stats.go) 模式） | | ☐ |
| | — 文件：`docs/DEPLOYMENT.md` 增加 image gen 開關說明 | | ☐ |
| P13.37 | **research: VS Code 上的 Codex CLI 訊息攔截方案** | [#130](https://github.com/chimerakang/alice/issues/130) | ✅ |
| | — 是否有等價於 Claude Code `hooks` 的官方機制？（UserPromptSubmit / Stop / SessionEnd） | | ✅ |
| | — 是否支援 MCP server？若支援，能否藉由 MCP 觀察到 prompt？（MCP 一般是 tool 層，不一定看得到 user prompt） | | ✅ |
| | — 設定檔位置與格式（類似 `~/.claude/settings.json` 的東西） | | ✅ |
| | — 是否會輸出 transcript / session log 到固定路徑（讓我們可以 tail / watch） | | ✅ |
| | — CLI 是否有 \`--pre-prompt\` / \`--on-event\` 之類的 flag | | ✅ |
| | — 推薦方案（含理由與 trade-off） | | ✅ |
| | — 是否需要新建 endpoint（例：`/api/hooks/codex-prompt-submit`），還是共用既有 `/api/hooks/user-prompt-submit` 並用 `source` 欄位區分 (`source: \"codex-vscode\"`) | | ✅ |
| | — Phase 1 (observe) / Phase 2 (block) 在 Codex 上的可行性（block 是否能像 Claude Code 那樣靠 hook 回 stdout 中斷？） | | ✅ |
| | — [internal/app/hooks.go](internal/app/hooks.go) 的 `UserPromptSubmitPayload` schema 是否需要擴充以容納 Codex 的欄位差異 | | ✅ |
| | — 對 [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) Hermes GPT tier 段落的更新 | | ✅ |
| P13.38 | **Implement Codex CLI VS Code interception (Phase 1: JSONL watcher)** | [#150](https://github.com/chimerakang/alice/issues/150) | ✅ |

## P14 - Commercialization Strategy (🔄 16%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P14.1 | **Alice 商業化：單機版隱私優先定位策略** | [#49](https://github.com/chimerakang/alice/issues/49) | 🔄 |
| | — 制定 "完全本地化 AI 助手" 的品牌定位 | | ☐ |
| | — 對比分析：Alice vs 雲端競品的隱私優勢 | | ☐ |
| | — 準備隱私合規認證材料（GDPR、SOC2、ISO 27001） | | ☐ |
| | — 金融機構：資料不出境要求 | | ☐ |
| | — 政府單位：機密專案開發 | | ☐ |
| | — 醫療軟體：HIPAA 合規要求   | | ☐ |
| | — 國防承包商：高安全等級要求 | | ☐ |
| | — 企業授權模式：單機 $2,999/年 | | ☐ |
| | — 部門授權：$9,999/年（10台機器） | | ☐ |
| | — 個人開發者：$199/年 | | ☐ |
| | — 分析 Entire Checkpoints 的雲端 SaaS 模式 | | ☐ |
| | — 識別單機版無法被雲端服務取代的場景 | | ☐ |
| | — 制定反雲端化的行銷策略 | | ☐ |
| P14.2 | **多人版架構設計：從單機到團隊協作** | [#51](https://github.com/chimerakang/alice/issues/51) | 🔄 |
| | — 分析當前架構：單一 Bot 實例 + 本地 SQLite | | ☐ |
| | — 識別多人協作的技術瓶頸 | | ☐ |
| | — 評估現有程式碼的重用性 | | ☐ |
| | — 研究 Entire Checkpoints 的多人協作功能 | | ☐ |
| | — 分析 GitHub Copilot Business 的團隊管理 | | ☐ |
| | — 比較其他 AI 編程工具的協作模式 | | ☐ |
| | — SQLite → PostgreSQL 資料庫遷移 | | ☐ |
| | — 用戶認證與權限系統設計 | | ☐ |
| | — 多專案隔離機制 | | ☐ |
| | — 基於角色的存取控制（RBAC） | | ☐ |
| | — 設計可選雲端同步機制 | | ☐ |
| | — 團隊設定中心化管理 | | ☐ |
| | — 跨實例數據聚合分析 | | ☐ |
| | — 企業級 SSO 整合規劃 | | ☐ |
| | — 多租戶架構設計 | | ☐ |
| | — 微服務拆分策略 | | ☐ |
| | — 水平擴展能力規劃 | | ☐ |
| | — API 開放與生態建設 | | ☐ |
| | — 多用戶認證系統 | | ☐ |
| | — 資料庫遷移方案 | | ☐ |
| | — 基礎權限管理 | | ☐ |
| | — 團隊協作功能 | | ☐ |
| | — 跨專案管理 | | ☐ |
| | — 企業整合介面 | | ☐ |
| | — 完整 SaaS 平台 | | ☐ |
| | — 高級分析功能 | | ☐ |
| | — 第三方生態 | | ☐ |
| | — 架構重構的工程量估算 | | ☐ |
| | — 向下兼容性考量 | | ☐ |
| | — 資料遷移風險控制 | | ☐ |
| | — 效能影響評估 | | ☐ |
| P14.3 | **競品分析深化：Entire Checkpoints vs Alice 差異化策略** | [#53](https://github.com/chimerakang/alice/issues/53) | ✅ |
| | — 下載並試用 Entire Checkpoints 開源工具 | | ☐ |
| | — 分析其 Git 整合和透明化追蹤功能 | | ☐ |
| | — 研究支援的 AI 工具（Claude Code、Gemini CLI） | | ☐ |
| | — 評估其開發者體驗和易用性 | | ☐ |
| | — 分析 Entire 的三層平台架構 | | ☐ |
| | — 研究其企業級功能規劃 | | ☐ |
| | — 評估其定價策略（如果公開） | | ☐ |
| | — 分析其目標市場定位 | | ☐ |
| | — Alice vs Entire 功能對比矩陣 | | ☐ |
| | — 技術實現方式差異分析 | | ☐ |
| | — 各自的技術優劣勢評估 | | ☐ |
| | — Telegram 整合優勢分析 | | ☐ |
| | — 多模態支援（語音、圖像）競爭力 | | ☐ |
| | — 完全本地化部署的市場需求 | | ☐ |
| | — 企業 Dashboard 的可視化價值 | | ☐ |
| | — 制定 "Mobile-First AI Assistant" 定位 | | ☐ |
| | — 遠程工作場景的應用分析 | | ☐ |
| | — 亞洲市場 Telegram 普及度研究 | | ☐ |
| | — 跨時區團隊協作價值主張 | | ☐ |
| | — 對比雲端 vs 本地化的隱私優勢 | | ☐ |
| | — 合規市場（金融、醫療、政府）需求分析 | | ☐ |
| | — "Never sends code to cloud" 行銷策略 | | ☐ |
| | — 資料主權完全掌控的企業價值 | | ☐ |
| | — 識別 Entire 不會涉足的市場區隔 | | ☐ |
| | — 中小企業市場策略 | | ☐ |
| | — 特定垂直行業策略 | | ☐ |
| | — 地理市場差異化（亞洲優先） | | ☐ |
| | — 評估成為 Entire 生態夥伴的可能性 | | ☐ |
| | — Alice 作為 Entire 的 Telegram 介面 | | ☐ |
| | — 技術互補合作模式研究 | | ☐ |
| | — 建立 Entire 產品更新追蹤機制 | | ☐ |
| | — 競爭對手動態監控流程 | | ☐ |
| | — 市場反應收集與分析 | | ☐ |
| P14.4 | **產品授權與定價模式設計** | [#54](https://github.com/chimerakang/alice/issues/54) | 🔄 |
| | — 單機授權：$2,999/年 | | ☐ |
| | — 部門授權：$9,999/年（最多10台機器） | | ☐ |
| | — 企業授權：$29,999/年（無限機器） | | ☐ |
| | — 免費版：基礎功能 | | ☐ |
| | — Pro 版：$199/年 | | ☐ |
| | — Studio 版：$499/年 | | ☐ |
| | — 設計離線授權驗證機制 | | ☐ |
| | — 實現授權密鑰管理系統 | | ☐ |
| | — 建立授權有效期檢查 | | ☐ |
| | — 設計功能開關控制 | | ☐ |
| | — AI 使用量統計機制 | | ☐ |
| | — 成本追蹤與報告系統 | | ☐ |
| | — 授權續約提醒功能 | | ☐ |
| | — 使用情況分析報告 | | ☐ |
| | — 客戶資料庫設計 | | ☐ |
| | — 授權分發管理介面 | | ☐ |
| | — 技術支援工單系統 | | ☐ |
| | — 客戶使用情況監控 | | ☐ |
| | — GitHub Copilot: $10/月個人，$39/月企業 | | ☐ |
| | — Cursor: $20/月 Pro | | ☐ |
| | — Tabnine Enterprise: $234K+/年（500人） | | ☐ |
| | — Telegram Bot 平台：$19/月起 | | ☐ |
| | — 客戶 ROI 計算模型 | | ☐ |
| | — 價格敏感度測試 | | ☐ |
| | — 不同市場區隔接受度調研 | | ☐ |
| | — 定價彈性分析 | | ☐ |
| | — 企業客戶開發策略 | | ☐ |
| | — POC（概念驗證）流程設計 | | ☐ |
| | — 合約談判標準化 | | ☐ |
| | — 客戶成功管理流程 | | ☐ |
| | — 線上購買流程設計 | | ☐ |
| | — 免費試用體驗優化 | | ☐ |
| | — 付費轉換漏斗分析 | | ☐ |
| | — 客戶自助服務平台 | | ☐ |
| | — EULA（終端使用者授權協議）制定 | | ☐ |
| | — 資料處理與隱私條款 | | ☐ |
| | — 技術支援條款與 SLA | | ☐ |
| | — 智慧財產權保護 | | ☐ |
| | — 不同國家的授權法規研究 | | ☐ |
| | — 跨國企業授權模式設計 | | ☐ |
| | — 稅務與會計處理方案 | | ☐ |
| P14.5 | **品牌定位與行銷策略規劃** | [#56](https://github.com/chimerakang/alice/issues/56) | 🔄 |
| | — 制定品牌標語："The AI coding assistant that stays home" | | ☐ |
| | — 對比競品雲端模式的差異化訊息 | | ☐ |
| | — 隱私優先的品牌形象建立 | | ☐ |
| | — 企業資料主權的價值主張 | | ☐ |
| | — 強調 Telegram 整合的便利性 | | ☐ |
| | — 遠程工作時代的移動協作價值 | | ☐ |
| | — 跨時區團隊的即時 AI 支援 | | ☐ |
| | — 多模態互動的創新體驗 | | ☐ |
| | — 金融科技公司：合規要求嚴格 | | ☐ |
| | — 政府承包商：資安等級要求高 | | ☐ |
| | — 醫療軟體公司：HIPAA 合規需求 | | ☐ |
| | — 中型軟體公司：成本敏感但注重隱私 | | ☐ |
| | — 個人開發者：重視隱私的技術專家 | | ☐ |
| | — 新創公司：資源有限但需要 AI 助手 | | ☐ |
| | — 教育機構：學生實驗環境需求 | | ☐ |
| | — 諮詢公司：需要向客戶展示 AI 過程 | | ☐ |
| | — "為什麼企業需要本地化 AI 助手" 白皮書 | | ☐ |
| | — "AI 編程工具的隱私風險分析" 研究報告 | | ☐ |
| | — "計算雲端 AI 的真實成本" ROI 分析 | | ☐ |
| | — "遠程團隊的 AI 協作最佳實踐" 指南 | | ☐ |
| | — "一鍵回滾 AI 錯誤" 功能演示影片 | | ☐ |
| | — "Telegram + Dashboard 雙重體驗" 使用案例 | | ☐ |
| | — "隱私保護 vs 雲端風險" 對比演示 | | ☐ |
| | — "企業級監控與透明化" 功能展示 | | ☐ |
| | — "AI 編程工具使用情況" 年度報告 | | ☐ |
| | — "企業 AI 採用障礙" 調查分析 | | ☐ |
| | — "開發者隱私關注" 趨勢研究 | | ☐ |
| | — Alice 使用者成功案例集 | | ☐ |
| | — 產品官網設計與開發 | | ☐ |
| | — SEO 關鍵字策略規劃 | | ☐ |
| | — 著陸頁面 A/B 測試 | | ☐ |
| | — 轉換率優化（CRO） | | ☐ |
| | — CTO/技術主管目標廣告 | | ☐ |
| | — 企業決策者內容行銷 | | ☐ |
| | — 行業專家意見領袖合作 | | ☐ |
| | — 企業客戶成功故事分享 | | ☐ |
| | — 開發者社群參與策略 | | ☐ |
| | — 技術內容分享計畫 | | ☐ |
| | — KOL 與技術專家合作 | | ☐ |
| | — 開源社群關係建立 | | ☐ |
| | — 產品教學影片系列 | | ☐ |
| | — 技術深度分析內容 | | ☐ |
| | — 客戶訪談與案例分享 | | ☐ |
| | — 競品對比分析影片 | | ☐ |
| | — 技術會議演講申請 | | ☐ |
| | — 工程師大會展位規劃 | | ☐ |
| | — 隱私與資安會議參與 | | ☐ |
| | — 企業 IT 高峰會議行銷 | | ☐ |
| | — 科技媒體關係建立 | | ☐ |
| | — 產品發布新聞稿撰寫 | | ☐ |
| | — 記者專訪安排 | | ☐ |
| | — 行業報告引用推廣 | | ☐ |
| | — AI 顧問公司合作計畫 | | ☐ |
| | — 系統整合商夥伴方案 | | ☐ |
| | — 企業軟體廠商聯盟 | | ☐ |
| | — 教育機構合作專案 | | ☐ |
| | — 軟體分銷商合作 | | ☐ |
| | — 企業採購平台上架 | | ☐ |
| | — 雲端市場（AWS、Azure）評估 | | ☐ |
| | — 第三方軟體商店策略 | | ☐ |
| | — 品牌知名度調查 | | ☐ |
| | — 客戶滿意度監控 | | ☐ |
| | — 競品品牌對比分析 | | ☐ |
| | — 行銷 ROI 測量機制 | | ☐ |
| | — 商標註冊申請 | | ☐ |
| | — 網域名稱保護 | | ☐ |
| | — 負面評論管理機制 | | ☐ |
| | — 品牌形象危機處理預案 | | ☐ |
| P14.6 | **Alice 商業化執行藍圖：6個月行動計畫** | [#58](https://github.com/chimerakang/alice/issues/58) | 🔄 |
| | — 實現基礎授權驗證系統 | | ☐ |
| | — 添加使用量追蹤與計費功能 | | ☐ |
| | — 優化 Dashboard 企業級展示 | | ☐ |
| | — 改善產品穩定性與效能 | | ☐ |
| | — 制定 EULA 與服務條款 | | ☐ |
| | — 準備隱私政策與 GDPR 合規文件 | | ☐ |
| | — 申請必要的軟體授權與商標 | | ☐ |
| | — 建立客戶支援與 SLA 框架 | | ☐ |
| | — 完成 Entire Checkpoints 深度競品分析 | | ☐ |
| | — 進行目標客戶訪談 (5-10 家企業) | | ☐ |
| | — 驗證定價策略與市場接受度 | | ☐ |
| | — 完善客戶人群分析與需求調研 | | ☐ |
| | — 建立產品官網與註冊系統 | | ☐ |
| | — 開放 Beta 測試申請 | | ☐ |
| | — 招募 10-15 個企業 Beta 客戶 | | ☐ |
| | — 建立客戶反饋收集機制 | | ☐ |
| | — 執行客戶開發訪談 | | ☐ |
| | — 收集產品使用情況數據 | | ☐ |
| | — 分析客戶痛點與需求變化 | | ☐ |
| | — 調整產品功能優先級 | | ☐ |
| | — 建立產品使用情況分析 | | ☐ |
| | — 追蹤客戶滿意度與 NPS | | ☐ |
| | — 分析定價策略有效性 | | ☐ |
| | — 收集客戶成功案例 | | ☐ |
| | — 完成品牌視覺識別設計 | | ☐ |
| | — 制作產品演示影片與簡報 | | ☐ |
| | — 準備客戶案例研究報告 | | ☐ |
| | — 建立完整的行銷素材庫 | | ☐ |
| | — 官網 SEO 優化與內容行銷 | | ☐ |
| | — LinkedIn B2B 廣告投放 | | ☐ |
| | — Twitter 技術社群參與 | | ☐ |
| | — YouTube 產品教學頻道建立 | | ☐ |
| | — 建立 AI 顧問公司合作關係 | | ☐ |
| | — 接觸系統整合商夥伴 | | ☐ |
| | — 參加相關技術會議與展覽 | | ☐ |
| | — 建立 KOL 與意見領袖網絡 | | ☐ |
| | — Beta 客戶數量：15+ 家企業 | | ☐ |
| | — 產品穩定性：99%+ uptime | | ☐ |
| | — 客戶滿意度：NPS > 50 | | ☐ |
| | — 功能完成度：核心功能 100% | | ☐ |
| | — 付費客戶轉換率：20%+ | | ☐ |
| | — 月度經常性收入 (MRR)：K+ | | ☐ |
| | — 客戶獲取成本 (CAC)：< ,000 | | ☐ |
| | — 客戶生命週期價值 (LTV)：> ,000 | | ☐ |
| | — 官網月訪問量：1,000+ UV | | ☐ |
| | — 行銷合格潛在客戶 (MQL)：50+/月 | | ☐ |
| | — 銷售合格潛在客戶 (SQL)：10+/月 | | ☐ |
| | — 內容行銷覆蓋率：10K+ 閱讀 | | ☐ |
| | — 月 2 結束：是否繼續單機版策略 vs 開發多人版 | | ☐ |
| | — 月 4 結束：是否調整定價策略與目標市場 | | ☐ |
| | — 月 6 結束：是否進入規模化成長階段 | | ☐ |
| | — 技術延遲：建立開發緩衝時間 | | ☐ |
| | — 競品壓力：準備差異化應對策略 | | ☐ |
| | — 客戶需求變化：保持產品靈活性 | | ☐ |
| | — 市場接受度低：準備產品定位調整 | | ☐ |
| | — 定價策略失效：建立動態定價機制 | | ☐ |
| | — 競爭加劇：強化獨特價值主張 | | ☐ |
| | — 現金流壓力：控制燒錢速度 | | ☐ |
| | — 客戶付款延遲：建立付款保障機制 | | ☐ |
| | — 投資回報不佳：設定止損線 | | ☐ |

## P15 - Hermes Stabilization & Cleanup (🔄 83%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P15.1 | **Post-stabilization cleanup: retire legacy coordinator / DecisionLog bridge** | [#115](https://github.com/chimerakang/alice/issues/115) | 🔄 |
| P15.2 | **[Closed/Superseded Epic] Alice architecture unification: ExecutionEngine + Review feedback** | [#120](https://github.com/chimerakang/alice/issues/120) | ✅ |
| | — #116 hermes_tasks schema 缺 budget 欄位 | | ✅ |
| | — #117 Pre-Planner skip 多動詞訊息應走 Planner | | ✅ |
| | — #118 Hermes 啟用時 #N 引用應 auto-fetch issue body | | ✅ |
| | — #109 Hermes native session resume（commit \`c18709c\` + \`726bf9d\` token attribution 已實作 — **待 audit & close**） | | ☐ |
| | — #108 Hermes/Codex follow-up 失憶（buildHermesGoalWithContext 已實作 — **待 audit & close**） | | ☐ |
| | — #110 階段① ChatContext 抽取（commit \`d027da7\` — **待 audit & close**） | | ☐ |
| | — #111 階段② ExecutionEngine + DirectEngine（commit \`d027da7\` — **待 audit & close**） | | ☐ |
| | — #112 階段③ PlanExecuteEngine（commit \`77430ff\` — **待 audit & close**） | | ☐ |
| | — #113 階段④ UnifiedTaskStore（合併入 #114） | | ✅ |
| | — #114 階段④ UnifiedTaskStore + Dashboard 全可視化（含 review_results） | | ✅ |
| | — #119 ReviewPhase Epic（reviewer + Dashboard panel + PlanRulesTuner 週報） | | ✅ |
| | — #122 Dashboard/Timeline 顯示 sub-task 各自的 review 分數 | | ✅ |
| | — #123 Dashboard 加入專屬的 Review History 頁 | | ✅ |
| | — **#125** 新增 \`/retry\` 指令（StrictReviewMode 前菜，**P1，1 天**） | | ☐ |
| | — **#128** Quality Analytics 頁（拆分效果 + 評分達標 + 自動洞察，**P1，4.5 天**） | | ☐ |
| | — **#124** ReviewPhase Phase 2: StrictReviewMode（硬 BLOCK + 對手 backend 互審 + auto-trigger，**P2，8 天**） | | ☐ |
| | — #113 階段⑤ ChatRouter | | ☐ |
| | — #115 階段⑥ 拆舊收尾（依賴 #110-#112 全部 close） | | ☐ |
| P15.3 | **建立 Alice Unified Memory Architecture** | [#143](https://github.com/chimerakang/alice/issues/143) | ✅ |
| | — 有 `docs/arch/memory.md` 描述現有與目標 memory 架構。 | | ☐ |
| | — 定義 `MemoryResolver` 或等價統一入口，至少能組合 recent messages + Hermes TaskState + issue-scoped memory。 | | ☐ |
| | — Hermes issue follow-up、一般 direct follow-up、文件分析 follow-up 都能透過同一套 memory policy 取得上下文。 | | ☐ |
| | — 非 Hermes 任務能留下可續跑的 persisted summary 或 memory card。 | | ☐ |
| | — Memory 注入結果有 log 或 API 可觀測。 | | ☐ |
| | — 測試覆蓋：同 issue 優先、同 project recent task、model switch/resume fallback、無關 active task 不污染指定 issue context。 | | ☐ |
| | — 文件能回答「目前 Alice 哪些地方有 memory」。 | | ☐ |
| | — 文件能回答「一則訊息進來時，應該查哪些 memory source」。 | | ☐ |
| | — 文件明確列出哪些 runner 目前已接 memory、哪些尚未接。 | | ☐ |
| | — 有 `MemoryResolver` 或等價統一入口。 | | ☐ |
| | — 能依 issue number 優先載入同 issue memory。 | | ☐ |
| | — 能依 chat/thread/project 載入最近任務與 recent messages。 | | ☐ |
| | — 能輸出可直接注入 prompt 的 compact memory bundle。 | | ☐ |
| | — 有測試覆蓋 prompt budget/clamping 與 source ordering。 | | ☐ |
| | — `/hermes #N`、`/ghermes #N`、`請繼續處理 #N` 都使用同一套 issue memory policy。 | | ☐ |
| | — 同聊天室有其他 active task 時，明確指定 issue 不會被其他 task 污染。 | | ☐ |
| | — 測試覆蓋 same issue、different issue、active task、recent completed task。 | | ☐ |
| | — `/gdeep` 後的文件分析 follow-up 不會因新 backend session 失去前文。 | | ☐ |
| | — `runAgentForStopButton` / multimedia path 能套用 explicit model preference 與 memory bundle。 | | ☐ |
| | — 一般 direct follow-up 在 session unavailable fallback 時，不只注入 recent messages，也能注入相關 task/issue memory。 | | ☐ |
| | — 非 Hermes 任務完成後會留下可查詢 summary。 | | ☐ |
| | — MemoryResolver 可讀取 general task memory。 | | ☐ |
| | — Dashboard 或 API 可檢視 memory source、scope、updated_at、preview。 | | ☐ |
| | — Debug log 能看到本次注入了哪些 memory sections 與裁切後大小。 | | ☐ |
| P15.4 | **Hermes mode 架構精簡：路由規則、狀態機、訊息流** | [#144](https://github.com/chimerakang/alice/issues/144) | ✅ |
| | — classifier.go 規則 < 30 行，刪除 status query 反向判斷 | | ☐ |
| | — 新增 `/hermes` 指令明確 opt-in | | ☐ |
| | — `TaskStatusValidating` 移除，state 縮為 `planning/executing/done/failed/interrupted` | | ☐ |
| | — `InterruptPolicy` 移除，固定 inject 行為 | | ☐ |
| | — store.go SubTask 變更入口收斂為 `UpdateSubTask` | | ☐ |
| | — 既有 hermes 測試通過 (`go test ./internal/app/hermes/... ./internal/app/...`) | | ☐ |
| P15.5 | **SessionPolicy: direct bridge / model switch memory source policy** | [#146](https://github.com/chimerakang/alice/issues/146) | ✅ |
| P15.6 | **Runtime trace + token/cache observability** | [#148](https://github.com/chimerakang/alice/issues/148) | ✅ |
| | — **1A**: \`Usage\` struct 加 \`CacheReadInputTokens\` / \`CacheCreationInputTokens\` 欄位，[api.go](internal/app/api.go) 與 [enhanced_cli.go](internal/app/enhanced_cli.go) 三處 JSON 解析補齊 | | ☐ |
| | — **1B**: [agent.go:843-867](internal/app/agent.go#L843-L867) cost delta 修正：session ID 變動時重置 \`lastTotalCostUSD\`；或乾脆改用每 call 非累計值（從 CLI 拿 per-call cost 而不是 session cumulative） | | ☐ |
| | — **1C**: dashboard schema 加上 cache 欄位顯示，總 token 用 \`InputTokens + CacheReadInputTokens + CacheCreationInputTokens\` 計 | | ☐ |
| | — **1D**: Max 訂閱回 \`total_cost_usd=0\` 時，用 token×rate fallback 估算 cost 寫入 dashboard，並在 UI 標 \"訂閱估價\" | | ☐ |
| | — **1E**: Hermes summary 改用 \`resp.TotalCostUSD\` 真實值（搭配 1B）；無真實值時 fallback 到 token×rate 並標明 | | ☐ |
| | — **2A**: Planner CallPlan 改用 session resume（\`--resume\`）讓 planner_rules 命中 cache。需要決定 session 生命週期（per-task / per-chat）。 | | ☐ |
| | — **2B**: 調查 Executor sub-task 之間是否真的 session resume，補 log 量化 cache 命中率 | | ☐ |
| | — **2C**: 評估 strict mode review 的成本：是否所有 sub-task 都需要 review？low-risk sub-task 可不送 review 即省 5×N 中的 N 次 | | ☐ |
| | — **3A**: dashboard 加 \"cache hit %\" 指標，實時看 caching 是否有效 | | ☐ |
| | — **3B**: 每個 Hermes task 結束時輸出 token breakdown（uncached / cache_read / cache_creation / output） | | ☐ |
| P15.7 | **Spike: Single-session walking agent via Python Claude Agent SDK (#148 Phase 2)** | [#149](https://github.com/chimerakang/alice/issues/149) | ✅ |
| | — 獨立 Python script (\`scripts/spike_walking_agent.py\` 或類似位置) | | ☐ |
| | — 用 \`ClaudeSDKClient\` 跑 N 個假 sub-task（用真實 Hermes Executor rules，但 sub-task description 用測試固定組） | | ☐ |
| | — 同樣 N 個 sub-task 用 Alice 現有 \`claude -p\` 模式跑一次（複製 buildSubTaskGoal 邏輯） | | ☐ |
| | — 兩邊輸出 cost / token / latency 比較表 | | ☐ |
| | — 跑 N=5 / N=10 / N=20 三個規模看是否有 prompt-length issue | | ☐ |
| P15.8 | **Optimize Hermes token efficiency with outlier reporting and compact continuation context** | [#158](https://github.com/chimerakang/alice/issues/158) | 🔄 |
| | — 在 runtime / dashboard 增加 Hermes token efficiency 區塊 | | ☐ |
| | — 顯示每個 Hermes task 的 nominal tokens、cache-adjusted tokens、saved%、cache_read% | | ☐ |
| | — 顯示 planner / executor / reviewer phase breakdown | | ☐ |
| | — 標記 outlier：例如 `>10M tokens`、`cache_read <30%`、`fresh session too often` | | ☐ |
| | — 提供最近 24h / 7d 聚合：平均、中位數、P95、top outliers | | ☐ |
| | — 補 backend API 或現有 API 擴充，讓 frontend 不需要自己解析 raw JSON | | ☐ |
| | — 任務開始前估算 context snapshot 大小 | | ☐ |
| | — 超過門檻時自動 compact，只保留 issue、未完成 checklist、最近 task 摘要、必要檔案線索 | | ☐ |
| | — walking session watermark 觸發前先嘗試 compact continuation context | | ☐ |
| | — 在 runtime event 中記錄 compact 前後 token / char 大小 | | ☐ |
| | — 對超大 fresh task 加 warning 或降載策略，避免直接膨脹到 10M+ / 40M+ | | ☐ |
| | — all-failed retry 只注入該批低分 subtask 的 reviewer feedback | | ☐ |
| | — index retry 只注入指定 subtask 的結果、feedback、必要上下文 | | ☐ |
| | — continuation 依 GitHub issue unchecked checklist 建立最小上下文 | | ☐ |
| | — 避免把完整前次 Hermes transcript 或 accumulated 整包帶入 | | ☐ |
| | — 確認 #331 類型任務 retry 後 token 明顯下降 | | ☐ |
| | — Dashboard / API 可以列出 top token outliers 與 cache efficiency | | ☐ |
| | — 至少能比較 nominal tokens vs cache-adjusted tokens | | ☐ |
| | — 最近任務可看出 phase breakdown，包含 planner / executor / reviewer | | ☐ |
| | — 針對一個真實 issue retry / continuation，token 使用量比原始 fresh task 明顯下降 | | ☐ |
| | — 補測試：token aggregation、cache-adjusted 計算、outlier 標記、compact context 選取規則 | | ☐ |
| P15.9 | **Prevent Hermes no-op continuation loops when GitHub checklist is unsynced** | [#159](https://github.com/chimerakang/alice/issues/159) | ✅ |
| | — 在 issue reconciliation 中偵測「Hermes/review 已通過但 GitHub checklist 未勾」的狀態，輸出明確原因：`checklist_unsynced`。 | | ✅ |
| | — Telegram 完成訊息增加選項： | | ✅ |
| | — `同步 checklist`：根據 Hermes 執行計劃或已完成 subtask 嘗試勾選 issue body checklist。 | | ✅ |
| | — `重新驗證`：只跑 final validation，不重新實作。 | | ✅ |
| | — `重新規劃剩餘項`：只針對仍未被證據覆蓋的 checklist。 | | ✅ |
| | — 避免 planner 對已驗證完成但 checklist 未勾的 issue 產生 no-op executor task；應改成 checklist sync / validation action。 | | ✅ |
| | — 在 GitHub issue comment 中明確列出：哪些項目已由證據覆蓋、哪些只是 checklist 未勾、哪些仍缺實作。 | | ✅ |
| | — 若無法安全自動勾選 checklist，至少提示使用者「目前阻塞點是 GitHub checklist 未同步」。 | | ✅ |
| | — 補 runtime event，例如 `IssueChecklistUnsynced` 或在既有 `IssueQualityGate` payload 加 `reason=checklist_unsynced`。 | | ✅ |
| | — 用 #157 類型案例重現時，不會連續產生只回報「已完成」的 no-op Hermes continuation。 | | ✅ |
| | — Telegram 能清楚顯示：卡住原因是 checklist 未同步，而不是 executor 失敗。 | | ✅ |
| | — 使用者可透過按鈕或指令選擇同步 checklist、重新驗證或只重規劃真正剩餘工作。 | | ✅ |
| | — GitHub issue comment / body 更新結果可追蹤，避免誤勾沒有證據的 checklist。 | | ✅ |
| | — 補測試涵蓋：review passed + unchecked checklist、no-op continuation prevention、checklist sync action rendering。 | | ✅ |
| P15.10 | **Add IssueOps Agent FSM for GitHub issue lifecycle, checklist sync, and close readiness** | [#160](https://github.com/chimerakang/alice/issues/160) | ✅ |
| | — 新增 IssueState / IssueEvent / IssueTransition 型別，沿用或對齊現有 Task FSM pattern。 | | ✅ |
| | — 實作 `ValidIssueTransition(from, event, to)` 或等價 guard-driven transition helper。 | | ✅ |
| | — 補 `IsTerminal()` / `NeedsHumanDecision()` 等共通介面。 | | ✅ |
| | — 加入 transition table 測試，覆蓋 happy path、checklist unsynced、blocked、closed。 | | ✅ |
| | — 建立 IssueOps service / agent，不直接混在 Executor 裡。 | | ✅ |
| | — 負責讀取 GitHub issue body、解析 checklist、建立 checklist mapping。 | | ✅ |
| | — 負責收集 Hermes subtask result、review result、validation command 作為 evidence。 | | ✅ |
| | — 負責產生 issue comment / body patch，但實際寫入前需通過 guard。 | | ✅ |
| | — 對外提供高階操作：`PlanIssue`、`RecordEvidence`、`SyncChecklist`、`AssessCloseReadiness`、`CloseIssue`。 | | ✅ |
| | — 將目前 post-run issue reconciliation 改為呼叫 IssueOps FSM，而不是只讀 checklist 判斷。 | | ✅ |
| | — #159 的 `checklist_unsynced` 狀態應由 IssueOps Agent 發出。 | | ✅ |
| | — Telegram completion message 根據 Issue FSM state 顯示不同 action：同步 checklist、重新驗證、重新規劃剩餘項、關閉 issue。 | | ✅ |
| | — 避免 planner 對 `checklist_unsynced` 產生 no-op continuation。 | | ✅ |
| | — Runtime event 增加 Issue FSM state transition 記錄。 | | ✅ |
| | — 自動勾 checklist 前列出 evidence mapping。 | | ✅ |
| | — 對低信心 mapping 要求人工確認。 | | ✅ |
| | — 支援 dry-run：只顯示將更新哪些 checklist / comment，不真正寫入。 | | ✅ |
| | — GitHub API / gh CLI 失敗時進入 `blocked`，並提供 retry action。 | | ✅ |
| | — 防止 issue body 被覆蓋掉使用者手動新增內容。 | | ✅ |
| | — #157 類型案例不再出現 executor/review 已完成但 checklist 未同步後反覆 no-op continuation。 | | ✅ |
| | — IssueOps FSM 能明確輸出目前 state，例如 `checklist_unsynced`、`ready_to_close`、`blocked`。 | | ✅ |
| | — Telegram 可以依 Issue FSM state 顯示正確操作，而不是單純「繼續 / 重新規劃 / 停止」。 | | ✅ |
| | — 每個自動勾選 checklist item 都能追溯到 Hermes subtask result、review result 或 validation command。 | | ✅ |
| | — Issue close 前必須通過 `CanAutoClose` guard。 | | ✅ |
| | — 補單元測試：transition table、guard、checklist mapping、sync dry-run、blocked recovery。 | | ✅ |
| | — Add final IssueOps acceptance regression coverage for GitHub #160 by inspecting `internal/app/issueops`, `internal/app/hermes`, `internal/app/engine`, and `internal/app/telegram`, then modifying the relevant Go test files to assert that `checklist_unsynced`, `ready_to_close`, and `blocked` states drive non-no-op continuation actions, evidence-backed checklist sync, blocked retry recovery, runtime transition recording, and `CanAutoClose` guarding; finish by running the focused package tests and `go test ./...` to report concrete file paths, line numbers, and command output. | | ✅ |
| P15.11 | **Hermes: durable execution runtime with reducer-based snapshots** | [#161](https://github.com/chimerakang/alice/issues/161) | ✅ |
| | — A Hermes task can be resumed from the latest snapshot after process restart. | | ☐ |
| | — Step completion writes one durable snapshot with `state`, `next`, and metadata. | | ✅ |
| | — Existing plan-execute-review behavior still works. | | ✅ |
| | — Failed or panicked tasks do not remain indefinitely stuck in `executing`. | | ☐ |
| | — `/retry sub-task N` is documented either as deferred to a later phase (time-travel / fork from snapshot — out of scope here) or expressed as forward-only resume from the latest snapshot. | | ☐ |
| | — Approval / interruption state can be represented durably, even if Telegram callback wiring remains unchanged initially. | | ☐ |
| | — #162 — schema + types | | ✅ |
| | — #163 — reducer apply layer (batch semantics) | | ✅ |
| | — #164 — step-boundary checkpoint writes (atomic) | | ✅ |
| | — #165 — resume / recover (idempotent) | | ☐ |
| | — #166 — durable interrupt (with expiry) | | ☐ |
| | — Phase 1: snapshot schema and runtime types (#162) | | ✅ |
| | — Phase 2: reducer apply layer (#163) | | ✅ |
| | — Phase 3: step-boundary checkpointing (#164) | | ✅ |
| | — Phase 4: resume/recover (#165) | | ☐ |
| | — Phase 5: durable interrupt (#166) | | ☐ |
| P15.12 | **Hermes runtime Phase 1: add snapshot schema and runtime types** | [#162](https://github.com/chimerakang/alice/issues/162) | ✅ |
| | — Migration adds a snapshot table suitable for Hermes task checkpoints, including the required indexes above. | | ✅ |
| | — `channel_versions_json` column is present and serialized as JSON, even if always empty in this phase. | | ✅ |
| | — Go runtime types compile and are covered by basic serialization tests. | | ✅ |
| | — Store API can create a snapshot, fetch latest snapshot by task/thread, and list snapshot history. | | ✅ |
| | — Existing Hermes behavior is unchanged. | | ✅ |
| | — Related package tests pass. | | ✅ |
| P15.13 | **Hermes runtime Phase 2: centralize state updates behind reducers** | [#163](https://github.com/chimerakang/alice/issues/163) | ✅ |
| | — `ApplyStateUpdates` accepts `[]StateUpdate`; single-update is just a slice of length 1. | | ✅ |
| | — Reducer behavior is covered by table tests including the multi-update batch cases listed above for `accumulated` and `subtask_results`. | | ✅ |
| | — Concurrent-style or multi-update inputs for append/merge fields behave deterministically (same input batch → same output state, regardless of platform map iteration order). | | ✅ |
| | — Existing calls such as subtask completion, accumulated context update, and task advancement can be expressed as `StateUpdate` values. | | ✅ |
| | — Existing plan-execute-review behavior remains unchanged. | | ✅ |
| | — Related package tests pass. | | ✅ |
| P15.14 | **Hermes runtime Phase 3: write snapshots at execution step boundaries** | [#164](https://github.com/chimerakang/alice/issues/164) | ✅ |
| | — Each supported boundary writes one snapshot with `state`, `next`, and metadata. | | ✅ |
| | — Snapshot write and legacy table updates are atomic per the contract above. | | ✅ |
| | — Snapshot history can be inspected for a task. | | ✅ |
| | — Existing task/subtask status remains compatible with current UI and Telegram flows. | | ✅ |
| | — Failed snapshot writes fail safely rather than silently advancing in memory only. | | ✅ |
| | — A test exercises the failure mode "snapshot insert errors mid-step" and confirms in-memory state is rolled back / not advanced. | | ✅ |
| | — Related package tests pass. | | ✅ |
| P15.15 | **Hermes runtime Phase 4: resume and recover from latest snapshot** | [#165](https://github.com/chimerakang/alice/issues/165) | ✅ |
| | — A Hermes task can be resumed from the latest snapshot after process restart. | | ☐ |
| | — Tasks with a valid `next_step` do not remain indefinitely stuck in `executing`. | | ☐ |
| | — Existing defer/recover sweep logic is either reduced, delegated to snapshot recovery, or documented as a fallback. | | ☐ |
| | — Resume behavior is covered by tests using persisted snapshots. | | ☐ |
| | — Concurrency safety: a test exercises the double-resume case (lease contention or idempotent replay) and shows no double execution. | | ☐ |
| | — Related package tests pass. | | ☐ |
| P15.16 | **Hermes runtime Phase 5: persist interrupts and approval waits** | [#166](https://github.com/chimerakang/alice/issues/166) | ✅ |
| | — A task waiting for approval has durable interrupt state in the latest snapshot. | | ☐ |
| | — Process restart does not lose the fact that the task is waiting for a human decision. | | ☐ |
| | — Resume can validate that the callback decision matches the pending interrupt (id-based idempotency). | | ☐ |
| | — Expiry behavior is implemented per the chosen policy and covered by tests (resume after expiry, sweep after expiry). | | ☐ |
| | — Existing Telegram callback flow still works or has a documented migration path. | | ☐ |
| | — Related package tests pass. | | ☐ |
| P15.17 | **Align Telegram Markdown render with leaf approach** | [#167](https://github.com/chimerakang/alice/issues/167) | ✅ |
| | — 先整理 `leaf` 目前 markdown/render 的處理方式，列出其支援範圍、轉換策略與重要邊界條件。 | | ☐ |
| | — 盤點目前 `internal/app/telegram.go` 的 Telegram render pipeline，明確列出與 `leaf` 的能力落差。 | | ☐ |
| | — 依分析結果，提出並實作一版 Telegram render 修正，優先補齊最常用且高價值的 Markdown 結構。 | | ☐ |
| | — 對修正內容補上測試，覆蓋至少一組複合 markdown 輸入與預期 Telegram HTML 輸出。 | | ☐ |
| | — 確保長訊息分段後仍維持可接受的 render 行為，不因補強 parser 造成明顯退化。 | | ☐ |
| | — 保留 Telegram 相容性，避免輸出 Telegram 不接受的 HTML 標籤或危險巢狀。 | | ☐ |
| | — 分析 leaf 專案的 markdown/render 實作：用 Glob/Grep 在 /Volumes/eclipse/projects 下定位 leaf 專案 markdown render 相關原始碼，Read 關鍵檔案，整理 leaf 支援的 Markdown 結構（headings、lists、blockquote、code fence、inline code、bold/italic、links、tables、strikethrough）、轉換策略（AST vs 規則式）、Telegram HTML 安全邊界，作為後續比對基礎。 | | ✅ |
| | — 盤點 alice 現有 Telegram render pipeline：Read /Volumes/eclipse/projects/alice/internal/app/telegram.go 的 sendLongMarkdown、markdownToTelegramHTML 與相關 helper，Read /Volumes/eclipse/projects/alice/internal/app/telegram_test.go 對應測試，列出目前支援結構與邊界條件，對照 s1 的 leaf 能力清單產出明確 gap list（未支援結構、不穩巢狀、有風險的 HTML tag）。 | | ✅ |
| | — 依 s2 的 gap list 在 /Volumes/eclipse/projects/alice/internal/app/telegram.go 實作 markdown→Telegram HTML render 修正，以 leaf 策略為基準補齊 lists、blockquote、inline 巢狀、code fence/inline code、escape 與 Telegram 接受的 HTML 子集（b/i/u/s/code/pre/a/blockquote），同步調整 sendLongMarkdown 長訊息分段邏輯避免切斷標籤；完成後執行 `cd /Volumes/eclipse/projects/alice && go build ./...` 確認可編譯。 | | ✅ |
| | — 在 /Volumes/eclipse/projects/alice/internal/app/telegram_test.go 補 markdown render 測試：至少一組複合 markdown 輸入（含 heading/list/blockquote/code fence/inline 巢狀/link）對應預期 Telegram HTML 輸出，並補一個長訊息分段案例驗證跨段不留下未閉合標籤，涵蓋 Telegram 不接受的 HTML tag 不會被輸出；執行 `cd /Volumes/eclipse/projects/alice && go test ./internal/app/...` 並確認綠燈。 | | ✅ |
| | — 以 Telegram sendMessage HTML parse_mode 規格做 smoke check：檢視 s3/s4 最終輸出，列出所有產生的 HTML tag/屬性，比對 Telegram Bot API 允許清單（b/strong、i/em、u、s/strike/del、code、pre、a href、blockquote、tg-spoiler），確認無 unsupported tag、無危險巢狀、href 已 escape；若偏離立即回到 telegram.go 修正並重跑 `go test ./internal/app/...`，最後整理中文總結回報落實的 checklist 項目與驗證結果。 | | ✅ |
| P15.18 | **Hermes milestone review command: /mr GitHub-sourced closeout review** | [#177](https://github.com/chimerakang/alice/issues/177) | 🔄 |
| | — ... | | ☐ |
| | — `/mr #<issue>` resolves the issue's GitHub milestone and reviews it. | | ☐ |
| | — `/mr <query>` resolves against GitHub milestone titles only. | | ☐ |
| | — Ambiguous milestone queries return a candidate list instead of guessing. | | ☐ |
| | — Missing milestone or issue-without-milestone cases return actionable errors. | | ☐ |
| | — The report includes verdict, score, blockers, gaps, inconsistencies, closeout checklist, and recommended next actions. | | ☐ |
| | — First version is read-only and does not mutate GitHub state. | | ☐ |
| | — Unit tests cover selector parsing and milestone matching edge cases. | | ☐ |
| | — Integration-style tests stub `gh` output for issue-based and title-based resolution. | | ☐ |
| | — Existing Hermes issue execution commands continue to work. | | ☐ |

## P15 - Parallel Subagents & Orchestration (🔄 91%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P15.1 | **feat: Implement parallel subagent execution with isolated contexts** | [#87](https://github.com/chimerakang/alice/issues/87) | ✅ |
| | — 實現 SubAgentOrchestrator 結構體和並行邏輯 | | ☐ |
| | — 修改 `ExecuteCoordinatedTask()` 從序列改為並行 | | ☐ |
| | — 實現 semaphore 控制最大並發數 | | ☐ |
| | — 實現任務依賴分析（DAG 拓撲排序） | | ☐ |
| | — 實現結果聚合邏輯 | | ☐ |
| | — 整體 timeout 控制（預設 10 分鐘） | | ☐ |
| | — 進度回報（每個子任務完成時通知） | | ☐ |
| | — 支援 `/parallel` 命令觸發並行任務 | | ☐ |
| | — 進度回報：每個子任務完成時推送更新 | | ☐ |
| | — `/parallel status` 查看執行狀態 | | ☐ |
| | — i18n 支援（zh-TW、en） | | ☐ |
| | — SQLite `sub_agent_executions` 表記錄並行執行歷史 | | ☐ |
| | — Dashboard API 端點 | | ☐ |
| | — WebSocket 推送子任務狀態更新 | | ☐ |
| | — 測試 Claude CLI 同機器多 session 的並發行為 | | ☐ |
| | — 測量多 session 的 token 成本倍數 | | ☐ |
| | — 確認帳號級別的 rate limit | | ☐ |
| | — 基於驗證結果調整 maxConcurrent 預設值 | | ☐ |
| | — 最大並發數限制（防止資源耗盡） | | ☐ |
| | — 每個子任務獨立的 timeout | | ☐ |
| | — 並行寫入衝突偵測（同一檔案被多個子任務修改） | | ☐ |
| | — 並行執行的 token 成本預警 | | ☐ |
| P15.2 | **[Epic] Alice Hermes 化路徑圖：Brain-Executor 架構遷移** | [#95](https://github.com/chimerakang/alice/issues/95) | ✅ |
| | — #96 — Tool Execution Hooks（Post-validator + Path guard）✅ commit 9b3867a | | ✅ |
| | — #97 — Task State 持久化層（支援 Planner/Executor 雙 session 協作）✅ commit 9b3867a | | ✅ |
| | — #98 — Planner-Executor 架構（雙 CLI session 實作）✅ commit 9b3867a | | ✅ |
| | — #99 — Prompt 核心規則 prepend（沿用 skills，不自製 profile）✅ commit 9b3867a | | ✅ |
| | — #101 — GitHub Issue 整合層（/hermes #N、checklist 同步、comment）✅ commit 2ffef3e | | ✅ |
| | — #102 — Task Summary Report（token 用量分拆、可切換開關）✅ commit 095233a | | ✅ |
| | — #103 — 過度拆解防護（Complexity Gate + 粒度規則 + 數量上限）🔄 進行中 | | ☐ |
| | — #100 — SDK 混用方案（Deferred — 未來備選） | | ☐ |
| | — 可選擇性啟用 Hermes 模式（`/hermes` 切換，與現有 routing 並存） | | ☐ |
| | — 端到端示範：`/hermes #<n>` 啟動 → 自動執行 → Issue checklist 同步 → 觸發 `/task-sync` | | ☐ |
| | — Token 成本量測：對比純 Opus、純 routing、Hermes 三種模式的**訂閱額度消耗** | | ☐ |
| | — Planner JSON 輸出穩定率量測（< 90% 即觸發 #100 再評估） | | ☐ |
| | — Inspect GitHub #95 epic state and local Hermes implementation in /Volumes/eclipse/projects/alice, focusing on routes/commands for /hermes, issue checklist sync, task-sync trigger paths, token usage reporting, and Planner JSON retry/parse metrics; do not use docs/MASTER_TASKS.md as issue-state authority. | | ☐ |
| | — Implement or finish the optional Hermes mode switch so /hermes can coexist with existing dynamic model routing, including any necessary command parsing, routing branch, and user-facing response behavior in the relevant internal/app files. | | ✅ |
| | — Implement or finish the end-to-end /hermes #<n> GitHub workflow so it can fetch an issue, execute subtasks, update issue checklist/comment state, and trigger or document the existing /task-sync integration path without modifying forbidden files. | | ✅ |
| | — Implement or finish token cost measurement/reporting for pure Opus, pure routing, and Hermes modes, using existing token accounting structures where available and adding focused tests for the comparison output. | | ✅ |
| | — Implement or finish Planner JSON output stability measurement, including parse success/failure counters or logs, retry-rate reporting, and a clear threshold path for re-evaluating GitHub #100 when stability drops below 90%. | | ✅ |
| | — Run the relevant Go test suite and build commands for /Volumes/eclipse/projects/alice, then verify the GitHub #95 acceptance items with concrete command output and file references. | | ☐ |
| | — If all verification passes, update GitHub #95 checklist/comment state via gh CLI to reflect completed acceptance items and summarize any remaining deferred items such as #100. | | ☐ |
| P15.3 | **Hermes: Tool Execution Hooks — Post-validator + Path guard** | [#96](https://github.com/chimerakang/alice/issues/96) | ✅ |
| | — `PreHook` / `PostHook` interface 與註冊機制 | | ☐ |
| | — `PathGuard` 實作 + 單元測試（denylist 邊界 case） | | ☐ |
| | — 至少一個 post validator（`GoBuild`）實作 + 測試 | | ☐ |
| | — 錯誤訊息格式為「純事實」：無 \"you made a mistake\" 類語言 | | ☐ |
| | — 可透過 config 個別啟停 | | ☐ |
| | — 文件：`docs/hermes/hooks.md` | | ☐ |
| P15.4 | **Hermes: Task State 持久化層 — state.json for cold-start executors** | [#97](https://github.com/chimerakang/alice/issues/97) | ✅ |
| | — `TaskState` / `SubTask` / `Artifact` / `TokenBudget` struct + SQLite migration | | ☐ |
| | — `TaskStateStore` interface + 實作 + 單元測試 | | ☐ |
| | — CRUD：Create / GetCurrent / UpdateSubTask / AppendArtifact / UpdateAccumulated / MarkInterrupted | | ☐ |
| | — 三種中斷策略至少實作 `queue` 與 `interrupt` | | ☐ |
| | — Accumulated cheap path + expensive path 切換邏輯 | | ☐ |
| | — TokenBudget 超限檢查 hook（在 Coordinator 每步開始時呼叫） | | ☐ |
| | — 文件：`docs/hermes/state-management.md` | | ☐ |
| P15.5 | **Hermes: Planner-Executor 架構拆分 — Brain-Executor 協作核心** | [#98](https://github.com/chimerakang/alice/issues/98) | ✅ |
| | — `internal/app/hermes/` package 完整生命週期（plan → execute → validate → summarize → done） | | ☐ |
| | — 雙 `CLIClient` 實例正確分離：Planner session 持久、Executor session 冷啟動 | | ☐ |
| | — `/hermes` 指令切換模式；`/auto` 切回 | | ☐ |
| | — Planner JSON schema 驗證 + retry；失敗降級到 routing 模式 | | ☐ |
| | — Retry 機制：SubTask 失敗最多重試 N 次，超過交 Planner 重新規劃 | | ☐ |
| | — Progress reporting 至少 `normal` verbosity 實作 | | ☐ |
| | — Budget 超限 abort 機制 + Telegram 通知 | | ☐ |
| | — 端到端測試：「在 telegram.go 加一個新指令」任務能自動完成 | | ☐ |
| | — Metrics：tokens、latency、retry count（接入 performance.go） | | ☐ |
| | — Planner JSON 成功率量測儀表板 | | ☐ |
| | — 文件：`docs/hermes/architecture.md` | | ☐ |
| P15.6 | **Hermes: Core Rules Prepender — 沿用 skills 不自製 profile 系統** | [#99](https://github.com/chimerakang/alice/issues/99) | ✅ |
| | — `planner_rules.md` / `executor_rules.md` 撰寫完成（各 ~30 行） | | ☐ |
| | — `PromptBuilder` 實作 + 單元測試（填值、角色切換、空值處理） | | ☐ |
| | — `CLIClient` 整合：啟動時接收組好的 prompt | | ☐ |
| | — 端到端測試：Executor 處理 i18n 任務時，`alice-i18n` skill 正確自動載入 | | ☐ |
| | — 文件：`docs/hermes/prompts.md`（含規則撰寫指南） | | ☐ |
| P15.7 | **Hermes (Future): SDK 混用方案探索 — 結構化輸出與並行優化** | [#100](https://github.com/chimerakang/alice/issues/100) | ✅ |
| | — 純 CLI 的 Planner JSON 輸出穩定率 < 90%（retry 成本 > 結構化輸出收益） | | ☐ |
| | — 單一 chat 需要真正並行執行多個 SubTask（CLI subprocess 並行成本過高） | | ☐ |
| | — Alice 走商業化路徑（#P14），多租戶情境下訂閱模型不再適用 | | ☐ |
| | — Anthropic 推出針對 agent 場景的訂閱式 API（抹平成本差異） | | ☐ |
| | — 在 /Volumes/eclipse/projects/alice 檢查 GitHub #100 相關背景與現有 Hermes/Planner 架構檔案，確認此 Deferred/Future issue 是否只需要記錄或標記觸發條件而非實作；不得修改 config.json、.git/、.env 或 *.pem。 | | ✅ |
| | — 搜尋 repository 中與 #100、#95、Hermes Planner、SDK 混用、JSON 穩定率、SubTask 並行、商業化 #P14、Anthropic agent API 觀察相關的文件或任務追蹤位置，判斷四個 unchecked checklist 是否已有對應狀態或證據可更新。 | | ✅ |
| | — 若專案有任務同步或 issue 狀態回寫流程，依現有格式更新 #100 對應的本地追蹤文件或產出回報內容，保留其 Deferred/Future 性質並逐項處理四個觸發條件；若沒有本地可改檔案，僅產出可回覆 GitHub issue 的狀態摘要。 | | ✅ |
| | — 驗證本次未引入不必要程式碼變更：執行 git status --short 與必要的 diff 檢查，若有文件變更則確認只限 #100 相關內容且未碰禁止修改路徑。 | | ✅ |
| P15.8 | **Hermes: GitHub Issue 整合層 — /hermes #N、checklist 同步、自動 comment** | [#101](https://github.com/chimerakang/alice/issues/101) | ✅ |
| | — `/hermes #N` 指令可正確 fetch Issue 並啟動 Hermes | | ☐ |
| | — Issue body checklist 存在時正確作為 SubTask 雛形 | | ☐ |
| | — SubTask 完成時 Issue checklist 自動勾選 | | ☐ |
| | — 生命週期事件正確 comment 到 Issue | | ☐ |
| | — `hermes-auto-close` label 在所有 SubTask 通過時觸發 close | | ☐ |
| | — Hermes 失敗時自動加 `hermes-failed` label | | ☐ |
| | — `complexity:*` label 正確映射到 TokenBudget | | ☐ |
| | — 完成後自動觸發 `/task-sync` | | ☐ |
| | — 端到端測試：`/hermes #<test-issue>` → Hermes 執行 → Issue 更新 → MASTER_TASKS.md 同步 | | ☐ |
| | — 文件：`docs/hermes/github-integration.md` | | ☐ |
| | — Read internal/app/hermes/coordinator.go and task_state.go to understand existing lifecycle events and TaskState.GithubIssueNumber field | | ✅ |
| | — Read docs/MASTER_TASKS.md and .claude/skills/task-sync files to understand existing GitHub Issue workflow integration | | ✅ |
| | — Create internal/app/hermes/github.go with IssueContext struct and FetchIssue(ctx, number) function that shells out to 'gh issue view N --json title,body,labels' | | ✅ |
| | — Implement ParseChecklist helper in internal/app/hermes/github.go that extracts '- [ ]' and '- [x]' items from Issue body with line numbers for sync anchoring | | ✅ |
| | — Implement SyncChecklist(ctx, number, subtasks) in github.go that updates Issue body checking off completed SubTasks via 'gh issue edit N --body' | | ✅ |
| | — Implement PostComment(ctx, number, event, payload) in github.go supporting start/complete/fail/budget_exceeded event templates with artifacts, tokens, wallclock | | ✅ |
| | — Implement ApplyLabel(ctx, number, label) and CloseIssue(ctx, number) in github.go via 'gh issue edit --add-label' and 'gh issue close' | | ✅ |
| | — Extend config struct in internal/app/ to load hermes.github_integration section including comment_on_events, sync_checklist, auto_close_on_label, complexity_budget_map | | ✅ |
| | — Wire Coordinator lifecycle hooks (start/subtask-complete/complete/fail/budget_exceeded) to call github.go functions when TaskState.GithubIssueNumber is set | | ✅ |
| | — Implement complexity label → TokenBudget mapping: when fetching issue, read complexity:* label and apply budget from config.complexity_budget_map to TaskState | | ✅ |
| | — Add /hermes #N command handler in internal/app/telegram.go that parses the issue number, calls FetchIssue, seeds Planner Goal, and starts Hermes run | | ☐ |
| | — Update Planner system prompt (in internal/app/hermes/planner.go or equivalent) to prefer existing Issue checklist items as SubTasks when provided | | ☐ |
| | — Add post-completion hook in Coordinator that runs '/task-sync' skill (or equivalent bash) when trigger_task_sync_on_complete is true | | ☐ |
| | — Write table-driven unit tests in internal/app/hermes/github_test.go for ParseChecklist, SyncChecklist body rewriting, and comment template rendering using mocked gh CLI | | ☐ |
| | — Create docs/hermes/github-integration.md documenting /hermes #N usage, checklist sync rules, label semantics, config options, and end-to-end flow | | ☐ |
| P15.9 | **Hermes: Task Summary Report — token 用量分拆與可切換開關** | [#102](https://github.com/chimerakang/alice/issues/102) | ✅ |
| | — `ModelUsage` struct + `TaskState.AddUsage()` 實作 | | ☐ |
| | — `TaskState.ModelUsages` 在 Coordinator 每次 CLI 呼叫後正確累加 | | ☐ |
| | — Minimal 與 Detailed 兩種格式實作 + 單元測試 | | ☐ |
| | — `EventTaskSummary` 事件在 task 完成時觸發，依 config 路由到對應 target | | ☐ |
| | — `/hermes-stats` 指令 (最近一次 / week / chat 三種) 實作 | | ☐ |
| | — 聚合查詢 SQL 與 `AggregatedStats` struct 實作 + 測試 | | ☐ |
| | — Cost estimate 可選，無對應 rate 時不計算不報錯 | | ☐ |
| | — 開關 `enabled: false` 時完全不推送（但 `/hermes-stats` 仍可手動查詢） | | ☐ |
| | — 所有輸出文字走 `alice-i18n`（zh-TW + en） | | ☐ |
| | — 文件：`docs/hermes/summary-report.md` | | ☐ |
| P15.10 | **Hermes: 過度拆解防護 — Complexity Gate + 粒度規則 + 數量上限** | [#103](https://github.com/chimerakang/alice/issues/103) | ✅ |
| | — `classifyComplexity()` heuristic 實作 + 單元測試（含邊界 case） | | ☐ |
| | — 可選升級 Haiku 分類的開關與實作 | | ☐ |
| | — Complexity gate 在 Hermes 入口 bypass trivial/moderate 到現有 routing | | ☐ |
| | — Planner system prompt 更新含粒度規則 + 範例（更新 #99 `planner_rules.md`） | | ☐ |
| | — Executor system prompt 加自主工具鏈授權（更新 #99 `executor_rules.md`） | | ☐ |
| | — SubTask 數量上限檢查 + 超限 reject + 回注 Planner 重試 | | ☐ |
| | — Retry 上限後降級到 routing 模式 + Telegram 告知 | | ☐ |
| | — 回歸測試：用實戰案例「commit & push to github 另外幫忙打上一個版本號」驗證 bypass 觸發 | | ☐ |
| | — 回歸測試：複雜任務「重構 i18n 系統」確認仍走 Hermes | | ☐ |
| | — 量測：Hermes 啟動率（bypass vs engage 比例）加入 #102 summary report | | ☐ |
| | — 文件：`docs/hermes/overdecomposition-prevention.md` | | ☐ |
| P15.11 | **[Experiment] Hermes: VS Code UserPromptSubmit hook 攔截實驗** | [#104](https://github.com/chimerakang/alice/issues/104) | ✅ |
| | — 新增 endpoint `POST /api/hooks/user-prompt-submit` | | ☐ |
| | — Payload：`session_id`, `cwd`, `prompt`, `source`（從 env / parent process 推測） | | ☐ |
| | — 執行 #103 的 `classifyComplexity()` heuristic（純本地，零成本） | | ☐ |
| | — 寫入新表 `prompt_classifications`：session_id, timestamp, prompt_snippet, classification, source | | ☐ |
| | — **Hook 回傳空 response**（不 block、不注入 context）— 使用者完全無感 | | ☐ |
| | — Dashboard 新增觀測頁：最近 N 次分類、trivial/moderate/complex 比例、可疑樣本列表 | | ☐ |
| | — Hook 對 complex 判定回傳 `{\"decision\": \"block\", \"reason\": \"redirected to Alice Hermes\"}` | | ☐ |
| | — 同時透過 Alice TG bot / Dashboard 通知使用者：「此任務已轉交 Hermes 處理，見 #<task_id>」 | | ☐ |
| | — Hermes task 完成時 push 結果（artifacts、diff summary）回使用者可見介面 | | ☐ |
| | — 為了不讓 VS Code 使用者困惑，提供 `~/.claude/settings.json` 層級的**個別專案啟用開關**（只對 Alice 專案啟用，避免影響其他專案） | | ☐ |
| | — Hook 導致 VS Code 明顯延遲（> 500ms） | | ☐ |
| | — 分類 heuristic 準確度 < 60% | | ☐ |
| | — 使用者回報有干擾到正常工作流 | | ☐ |
| | — 技術上 block 模式無法優雅地把結果帶回 VS Code 使用者 | | ☐ |
| | — Endpoint 上線，VS Code session 成功記錄到 `prompt_classifications` 表 | | ☐ |
| | — Dashboard 觀測頁可視化分類分佈 | | ☐ |
| | — 量測 1 週累積至少 50 個樣本 | | ☐ |
| | — 手動標註 20 個樣本的實際複雜度，計算 heuristic 準確率 | | ☐ |
| | — 決策會議：是否進入 Phase 2 | | ☐ |
| P15.12 | **Hermes: FetchIssue/gh 呼叫沒傳 project_dir，跨 repo 情境 exit status 1** | [#105](https://github.com/chimerakang/alice/issues/105) | ✅ |
| | — 所有 \`gh\` 呼叫都在正確 project_dir 下執行 | | ☐ |
| | — 跨 repo 測試：Alice chat 用 \`/hermes #1\`，其他 repo chat 用 \`/hermes #239\` 都能成功 | | ☐ |
| | — Coordinator 取得 project_dir 的路徑有單元測試 | | ☐ |
| | — 相容性：若某些內部流程呼叫時沒有 project_dir（backward compat），fallback 到 cwd 不崩潰 | | ☐ |

## P16 - Multi-Backend Execution (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P16.1 | **feat: Implement multi-backend execution environment support** | [#88](https://github.com/chimerakang/alice/issues/88) | ✅ |
| | — 設計 ExecutionBackend 介面（`internal/app/backend.go`） | | ☐ |
| | — 實現 BackendManager | | ☐ |
| | — 實現 LocalBackend（封裝現有 os/exec 行為） | | ☐ |
| | — 修改 ToolExecutor 使用 BackendManager 路由 | | ☐ |
| | — 確保向後相容（預設行為完全不變） | | ☐ |
| | — 單元測試 | | ☐ |
| | — 實現 DockerBackend（使用 `github.com/docker/docker/client`） | | ☐ |
| | — 支援容器生命週期管理（create/start/exec/stop/remove） | | ☐ |
| | — 支援 volume mount（項目目錄 → 容器） | | ☐ |
| | — 支援資源限制（memory、cpu） | | ☐ |
| | — 支援網絡隔離（預設 `--network none`） | | ☐ |
| | — 自動清理過期容器 | | ☐ |
| | — 整合測試 | | ☐ |
| | — 實現 SSHBackend（使用 `golang.org/x/crypto/ssh`） | | ☐ |
| | — 支援密鑰認證 | | ☐ |
| | — 檔案操作透過 SFTP（`github.com/pkg/sftp`） | | ☐ |
| | — 連接池和重連機制 | | ☐ |
| | — 整合測試 | | ☐ |
| | — Telegram 命令：`/backend`（查看）、`/backend switch <name>`（切換） | | ☐ |
| | — API 端點：`GET /api/backends`、`POST /api/backends/{name}/switch` | | ☐ |
| | — Dashboard 後端狀態面板 | | ☐ |
| | — 執行日誌記錄（哪個後端、耗時、成功/失敗） | | ☐ |
| | — Docker 容器預設以非 root 用戶運行 | | ☐ |
| | — Docker 預設無網絡存取（`--network none`） | | ☐ |
| | — SSH 密鑰路徑不寫入 config.json（使用環境變數） | | ☐ |
| | — 資源限制防止容器耗盡主機資源 | | ☐ |
| | — 執行命令審計日誌 | | ☐ |

## P2 - Monitoring (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P2.1 | **🎛️ Web Dashboard Integration** | [#1](https://github.com/chimerakang/alice/issues/1) | ✅ |
| | — HTTP server for serving dashboard at `/dashboard` | | ✅ |
| | — WebSocket support for real-time tool execution updates | | ✅ |
| | — REST API endpoints for statistics and monitoring data | | ✅ |
| | — Tool execution logging system with structured data | | ✅ |
| | — Agent status tracking across multiple projects | | ✅ |
| | — Tool execution metrics collection (name, duration, success/failure) | | ✅ |
| | — Conversation history persistence (local JSON storage) | | ✅ |
| | — Project insights generation (most used tools, performance stats) | | ✅ |
| | — Real-time performance monitoring | | ✅ |
| | — \`GET /api/stats\` - Current statistics (active sessions, tools executed, success rate) | | ✅ |
| | — \`GET /api/agents\` - Active agent list with project information | | ✅ |
| | — \`GET /api/tools/recent\` - Recent tool executions with status | | ✅ |
| | — \`GET /api/conversations\` - Conversation history by project | | ✅ |
| | — \`WebSocket /ws\` - Real-time tool execution and status updates | | ✅ |
| | — Add \`--dashboard-port\` CLI flag (default: 8080) | | ✅ |
| | — Optional dashboard enable/disable setting | | ✅ |
| | — Dashboard access control (optional authentication) | | ✅ |
| P2.2 | **🔍 AI Agent Transparency & Decision Logging** | [#2](https://github.com/chimerakang/alice/issues/2) | ✅ |
| | — Record AI reasoning process for each tool execution | | ✅ |
| | — Capture user prompts and agent responses with full context | | ✅ |
| | — Log tool selection rationale and decision-making process | | ✅ |
| | — Store conversation flow and agent state transitions | | ✅ |
| | — Complete tool execution history with inputs/outputs | | ✅ |
| | — Error tracking and debugging information | | ✅ |
| | — Performance metrics (execution time, token usage) | | ✅ |
| | — Project-specific decision patterns and learning | | ✅ |
| | — Export decision logs to JSON/CSV for analysis | | ✅ |
| | — Integration with external analysis tools | | ✅ |
| | — Pattern recognition in agent behavior | | ✅ |
| | — Performance trend analysis | | ✅ |
| | — Configurable logging levels (off/basic/detailed) | | ✅ |
| | — Sensitive data filtering (passwords, API keys) | | ✅ |
| | — Local-only storage with optional cloud sync | | ✅ |
| | — Data retention policies | | ✅ |
| | — Add logging hooks to \`Agent.Run()\` method | | ✅ |
| | — Capture tool execution context in \`ToolExecutor\` | | ✅ |
| | — Enhance conversation history with decision context | | ✅ |
| | — Add reasoning extraction from API responses | | ✅ |
| | — JSON file-based persistence for decision logs | | ✅ |
| | — Efficient indexing for large log volumes | | ✅ |
| | — Compressed storage for long-term retention | | ✅ |
| | — Backup and restore capabilities | | ✅ |
| | — Dashboard endpoints for decision log visualization | | ✅ |
| | — Real-time decision streaming via WebSocket | | ✅ |
| | — Search and filter capabilities | | ✅ |
| | — Export functionality | | ✅ |
| | — \`ALICE_LOG_LEVEL\` environment variable | | ✅ |
| | — \`--enable-transparency\` CLI flag | | ✅ |
| | — \`transparency_config.json\` for detailed settings | | ✅ |
| | — Dashboard toggle for real-time decision viewing | | ✅ |
| P2.3 | **🤖 Multi-Agent Coordination System** | [#3](https://github.com/chimerakang/alice/issues/3) | ✅ |
| | — **CodeReview Agent**: Focuses on code quality, security, best practices | | ✅ |
| | — **Testing Agent**: Specializes in writing and running tests | | ✅ |
| | — **Documentation Agent**: Creates and maintains project documentation | | ✅ |
| | — **Deployment Agent**: Handles CI/CD, Docker, deployment tasks | | ✅ |
| | — **Debug Agent**: Specialized in troubleshooting and error resolution | | ✅ |
| | — Intelligent task analysis and routing | | ✅ |
| | — Agent capability matching | | ✅ |
| | — Workload balancing across agents | | ✅ |
| | — Inter-agent communication protocols | | ✅ |
| | — Shared context and knowledge base | | ✅ |
| | — Dynamic agent instantiation based on task requirements | | ✅ |
| | — Agent lifecycle management (create, pause, terminate) | | ✅ |
| | — Resource allocation and limits per agent | | ✅ |
| | — Agent performance monitoring and optimization | | ✅ |
| | — Shared memory/context between agents | | ✅ |
| | — Message passing for agent coordination | | ✅ |
| | — Event-driven agent notifications | | ✅ |
| | — Conflict resolution for concurrent operations | | ✅ |
| | — Automatic task breakdown and assignment | | ✅ |
| | — Priority-based task scheduling | | ✅ |
| | — Dependency management between sub-tasks | | ✅ |
| | — Progress aggregation and reporting | | ✅ |
| | — Multi-agent status in chat interface | | ✅ |
| | — Agent-specific command routing | | ✅ |
| | — Group chat collaboration with multiple agents | | ✅ |
| | — Agent handoff notifications | | ✅ |
| | — Agent enable/disable toggles per project | | ✅ |
| | — Resource limits (memory, API calls, execution time) | | ✅ |
| | — Specialization level settings (basic/advanced) | | ✅ |
| | — Coordination strategy selection (parallel/sequential/hybrid) | | ✅ |
| | — Extend existing \`Agent\` struct for specialization | | ✅ |
| | — Add coordinator logic to \`agent.go\` | | ✅ |
| | — Implement agent registry and discovery | | ✅ |
| | — Create specialized prompting for each agent type | | ✅ |
| | — Add multi-agent dashboard views | | ✅ |
| P2.4 | **📊 Performance Monitoring & Analytics** | [#4](https://github.com/chimerakang/alice/issues/4) | ✅ |
| | — API call latency and success rates | | ✅ |
| | — Tool execution performance | | ✅ |
| | — Memory and CPU usage | | ✅ |
| | — Token consumption and cost tracking | | ✅ |
| | — Error rates and failure patterns | | ✅ |
| | — Usage pattern analysis | | ✅ |
| | — Performance trend visualization | | ✅ |
| | — Cost optimization recommendations | | ✅ |
| | — Bottleneck identification | | ✅ |
| | — Success/failure correlation analysis | | ✅ |
| | — Metrics collection in core components | | ✅ |
| | — Time-series data storage | | ✅ |
| | — Dashboard integration for visualization | | ✅ |
| | — Alert system for performance issues | | ✅ |
| | — Export capabilities for external analysis | | ✅ |
| P2.5 | **🔐 Security & Privacy Enhancements** | [#5](https://github.com/chimerakang/alice/issues/5) | ✅ |
| | — API key rotation and management | | ✅ |
| | — Secure storage of sensitive configuration | | ✅ |
| | — Rate limiting and abuse prevention | | ✅ |
| | — Input validation and sanitization | | ✅ |
| | — Audit logging for security events | | ✅ |
| | — Data retention policies | | ✅ |
| | — PII detection and filtering | | ✅ |
| | — Conversation history encryption | | ✅ |
| | — Configurable data sharing settings | | ✅ |
| | — GDPR compliance features | | ✅ |
| | — Security middleware for HTTP endpoints | | ✅ |
| | — Encryption for sensitive data storage | | ✅ |
| | — Access control for dashboard | | ✅ |
| | — Security scanning integration | | ✅ |
| | — Privacy policy and consent management | | ✅ |
| P2.6 | **🖥️ Dashboard Enhancement: Timeline & Terminal** | [#12](https://github.com/chimerakang/alice/issues/12) | ✅ |
| | — 時間軸正確顯示所有 AI 操作步驟 | | ✅ |
| | — 可展開查看每步的詳細 Input/Output | | ✅ |
| | — 終端機顯示 CLI 即時輸出 | | ✅ |
| | — 支援多 agent 輸出切換 | | ✅ |
| | — 保持 OLED 黑化風格一致性 | | ✅ |
| P2.7 | **🚀 Feature: Complete Dashboard, Monitoring & Checkpoint System** | [#14](https://github.com/chimerakang/alice/pull/14) | ✅ |

## P3 - Data Layer (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P3.1 | **🚀 Deployment & DevOps Improvements** | [#6](https://github.com/chimerakang/alice/issues/6) | ✅ |
| | — Multi-stage Docker builds for smaller images | | ✅ |
| | — Docker Compose templates for common setups | | ✅ |
| | — Health checks and graceful shutdowns | | ✅ |
| | — Volume management for persistent data | | ✅ |
| | — Environment-specific configurations | | ✅ |
| | — GitHub Actions for automated testing | | ✅ |
| | — Automated Docker image builds | | ✅ |
| | — Release automation and changelog generation | | ✅ |
| | — Security scanning in pipeline | | ✅ |
| | — Performance regression testing | | ✅ |
| | — Kubernetes manifests and Helm charts | | ✅ |
| | — Cloud provider deployment guides | | ✅ |
| | — Systemd service files for Linux | | ✅ |
| | — Windows service wrapper | | ✅ |
| | — Configuration management templates | | ✅ |
| | — Prometheus metrics export | | ✅ |
| | — Grafana dashboard templates | | ✅ |
| | — Log aggregation setup guides | | ✅ |
| | — Health check endpoints | | ✅ |
| | — Application monitoring best practices | | ✅ |
| P3.2 | **💾 Data Persistence Layer (SQLite)** | [#7](https://github.com/chimerakang/alice/issues/7) | ✅ |
| | — 程式重啟後，歷史資料仍可查詢 | | ✅ |
| | — Web API 支援時間範圍查詢 | | ✅ |
| | — 資料保留政策自動執行 | | ✅ |
| | — 不影響現有記憶體 buffer 的即時查詢效能 | | ✅ |
| P3.3 | **🔌 WebSocket Real-time Dashboard Connection** | [#8](https://github.com/chimerakang/alice/issues/8) | ✅ |
| | — Dashboard 顯示真實的即時 tool 執行資料 | | ✅ |
| | — Agent 狀態即時更新 | | ✅ |
| | — 效能指標即時刷新 | | ✅ |
| | — 斷線自動重連 | | ✅ |
| | — 移除所有 mock/simulated data | | ✅ |
| P3.4 | **🔗 Git Integration & Commit Correlation** | [#9](https://github.com/chimerakang/alice/issues/9) | ✅ |
| | — 每筆 tool execution 記錄包含 git commit hash | | ✅ |
| | — 每筆 decision log 包含 git branch 和 commit | | ✅ |
| | — Web API 支援按 git 資訊篩選 | | ✅ |
| | — 當專案非 git repo 時不出錯（graceful fallback） | | ✅ |
| P3.5 | **📸 Checkpoint & State Snapshot System** | [#10](https://github.com/chimerakang/alice/issues/10) | ✅ |
| | — 危險操作前自動建立 checkpoint | | ✅ |
| | — 支援手動建立 checkpoint | | ✅ |
| | — 支援回溯到指定 checkpoint | | ✅ |
| | — Checkpoint 列表可在儀表板查看 | | ✅ |
| | — 非 git 專案也有基本備份機制 | | ✅ |

## P4 - Proto-First (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P4.1 | **🏗️ Architecture: Proto-First API with Protocol Buffers** | [#13](https://github.com/chimerakang/alice/issues/13) | ✅ |
| | — 建立 `proto/` 目錄結構 | | ✅ |
| | — 定義核心訊息類型的 .proto 檔案 | | ✅ |
| | — buf 工具鏈配置完成 | | ✅ |
| | — Go 代碼成功生成且可編譯 | | ✅ |
| | — 至少一個 API 端點改用 proto 生成的型別 | | ✅ |
| | — TypeScript 類型成功生成 | | ✅ |

## P5 - Frontend Foundation (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P5.1 | **⚛️ React + Vite + TypeScript 專案初始化** | [#15](https://github.com/chimerakang/alice/issues/15) | ✅ |
| | — `npm run dev` 可啟動開發伺服器，proxy 到 Go backend | | ✅ |
| | — `npm run build` 輸出到 `web/` 目錄 | | ✅ |
| | — Tailwind CSS build-time 編譯，非 CDN | | ✅ |
| | — Proto TypeScript 類型可正確 import | | ✅ |
| | — React Router 路由正常運作 | | ✅ |
| | — OLED 黑化主題色彩保留 | | ✅ |
| | — Docker build 可正常包含前端產出物 | | ✅ |
| P5.2 | **🧱 共用 UI 元件庫 (Layout, StatusBadge, MetricCard)** | [#16](https://github.com/chimerakang/alice/issues/16) | ✅ |
| | — AppLayout 包含 Sidebar 導航 + Header + Content area | | ✅ |
| | — MetricCard 支援動畫計數器 (數字從 0 漸增) | | ✅ |
| | — StatusBadge 支援 5 種狀態 + 對應顏色 | | ✅ |
| | — LoadingSkeleton 適用於卡片和列表 | | ✅ |
| | — Toast 支援 auto-dismiss + 堆疊 | | ✅ |
| | — 所有元件遵循 OLED 黑化主題 | | ✅ |
| | — Storybook 或 demo page 可預覽所有元件 | | ✅ |
| P5.3 | **🔌 WebSocket Hook + Zustand 狀態管理** | [#17](https://github.com/chimerakang/alice/issues/17) | ✅ |
| | — WebSocket 自動連線 + 斷線重連 | | ✅ |
| | — 所有 6 種事件正確分發到對應 store | | ✅ |
| | — API client hook 整合 proto TypeScript 類型 | | ✅ |
| | — 初始頁面載入時 fetch API 填充 stores | | ✅ |
| | — WebSocket 狀態在 Header 顯示 (綠燈/紅燈) | | ✅ |
| | — DevTools 可檢視 store 狀態 | | ✅ |
| P5.4 | **📊 Dashboard 主頁面 + 圖表元件** | [#18](https://github.com/chimerakang/alice/issues/18) | ✅ |
| | — 4 個 MetricCard 顯示正確數值 + 動畫計數器 | | ✅ |
| | — Git 狀態面板顯示 branch/commit/modified files | | ✅ |
| | — Activity Chart 顯示 24h 趨勢 | | ✅ |
| | — Tool Usage Chart 顯示 tool 分佈 | | ✅ |
| | — Live feed 即時更新 (WebSocket) | | ✅ |
| | — 30 秒自動刷新 fallback | | ✅ |
| | — 響應式佈局 (desktop/tablet) | | ✅ |
| P5.5 | **⏳ Timeline + Terminal 頁面** | [#19](https://github.com/chimerakang/alice/issues/19) | ✅ |
| | — 垂直時間軸正確顯示事件 | | ✅ |
| | — 事件可展開/收合查看詳情 | | ✅ |
| | — Filter 正常篩選事件 | | ✅ |
| | — Terminal 即時顯示 CLI 風格輸出 | | ✅ |
| | — Auto-scroll 正常運作 | | ✅ |
| | — WebSocket 即時推送新事件 | | ✅ |
| | — OLED 黑化主題一致 | | ✅ |
| P5.6 | **📋 Git/Checkpoint/Performance/Security 子頁面** | [#20](https://github.com/chimerakang/alice/issues/20) | ✅ |
| | — Performance 頁面顯示分析指標 + 趨勢圖 | | ✅ |
| | — Security 頁面顯示事件表格 + 統計 | | ✅ |
| | — Checkpoint 頁面可建立/回溯/列出 checkpoints | | ✅ |
| | — 所有 API 端點正確整合 | | ✅ |
| | — 錯誤處理 + loading states | | ✅ |

## P6 - AI Audit System (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P6.1 | **🔍 AI Decision Timeline — 核心審計頁面** | [#21](https://github.com/chimerakang/alice/issues/21) | ✅ |
| | — 日期範圍篩選 | | ✅ |
| | — 專案目錄篩選 | | ✅ |
| | — 狀態篩選 (success/error/all) | | ✅ |
| | — 全文搜尋 (prompt + response) | | ✅ |
| | — WebSocket 接收新 decision events | | ✅ |
| | — 新 entry 自動插入頂部 (with animation) | | ✅ |
| | — "Live" 指示燈 | | ✅ |
| P6.2 | **📋 Decision Detail View + Git Diff Viewer** | [#22](https://github.com/chimerakang/alice/issues/22) | ✅ |
| | — 完整 User Prompt 顯示 | | ✅ |
| | — AI Response — Markdown 渲染 (支援 code blocks, tables, lists) | | ✅ |
| | — 執行摘要 (duration, tokens in/out, cost, tool count) | | ✅ |
| | — 每個 tool call 的時間軸 (Gantt-like bar) | | ✅ |
| | — 展開每個 tool 查看 input 和 output | | ✅ |
| | — 顏色標記: ✅ success / ❌ error / ⏳ running | | ✅ |
| | — Error stack trace 顯示 | | ✅ |
| | — 該次互動期間變更的檔案列表 | | ✅ |
| | — Inline diff view (紅/綠色 diff) | | ✅ |
| | — 檔案新增/刪除/修改 標記 | | ✅ |
| | — 可折疊每個檔案的 diff | | ✅ |
| | — Input/Output tokens 分拆 | | ✅ |
| | — 估算成本顯示 | | ✅ |
| | — 與歷史平均比較 | | ✅ |
| | — ← → 上一筆/下一筆 Decision | | ✅ |
| | — 回到 Timeline | | ✅ |
| | — 連結到相關 Checkpoint | | ✅ |
| P6.3 | **📸 Checkpoint Management UI** | [#23](https://github.com/chimerakang/alice/issues/23) | ✅ |
| | — 以卡片或表格顯示所有 checkpoints | | ✅ |
| | — 顯示: timestamp, description, trigger type, git branch, commit hash, size | | ✅ |
| | — 標記 auto vs manual checkpoints | | ✅ |
| | — 標記「危險操作」觸發的 checkpoints | | ✅ |
| | — 🔄 Restore — 回滾到該 checkpoint (需確認 dialog) | | ✅ |
| | — 📝 Create — 手動建立新 checkpoint | | ✅ |
| | — 🗑️ Delete — 刪除不需要的 checkpoint | | ✅ |
| | — 📊 Compare — 比較兩個 checkpoint 的差異 | | ✅ |
| | — 顯示觸發此 checkpoint 的 decision | | ✅ |
| | — 從 Decision Detail 跳轉到相關 checkpoint | | ✅ |
| | — 時間軸上標記 checkpoint 位置 | | ✅ |
| | — 當前 active checkpoint 高亮 | | ✅ |
| | — Checkpoint 大小 (storage usage) | | ✅ |
| | — Auto-checkpoint 設定狀態 | | ✅ |
| P6.4 | **擴充 CallStream 擷取完整 AI 思考與文字內容** | [#27](https://github.com/chimerakang/alice/issues/27) | ✅ |
| | — 解析 `thinking` content blocks（AI 推理過程） | | ✅ |
| | — 解析 `text` content blocks（AI 中間文字回應） | | ✅ |
| | — 解析 `tool_result` 事件（工具執行結果） | | ✅ |
| | — 擴充 `CLIResponse` struct 新增 `ThinkingBlocks []string` 和 `TextBlocks []string` | | ✅ |
| | — 擴充 onToolUse callback 或新增 onContent callback | | ✅ |
| | — `DecisionLog` struct 新增 `ThinkingContent` 欄位 | | ✅ |
| | — `ToolExecution` struct 新增 `Output` 欄位（工具執行結果） | | ✅ |
| | — `logDecision()` 儲存 thinking 和 text 內容 | | ✅ |
| | — SQLite schema migration: decision_logs 加 `thinking_content` 欄位 | | ✅ |
| | — 更新 INSERT/SELECT 查詢 | | ✅ |
| | — TypeScript DecisionLog type 新增 `thinking_content` 欄位 | | ✅ |
| | — Timeline 頁面新增可展開的 "AI Thinking" 面板 | | ✅ |
| | — Checkpoint AIContextPanel 顯示 thinking 內容 | | ✅ |

## P7 - Dashboard & Analytics (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P7.1 | **📊 Dashboard Enhancement — AI Activity Overview** | [#24](https://github.com/chimerakang/alice/issues/24) | ✅ |
| | — 當前 branch + commit hash | | ✅ |
| | — Dirty/clean 狀態 | | ✅ |
| | — Modified files 數量 | | ✅ |
| | — Remote URL | | ✅ |
| | — 最近 5 筆 decisions 的精簡卡片 (prompt 摘要 + 結果) | | ✅ |
| | — 點擊可跳轉到 Timeline detail | | ✅ |
| | — 「查看全部」連結到 Timeline 頁面 | | ✅ |
| | — Tool execution 成功/失敗率趨勢 (折線圖) | | ✅ |
| | — Token 使用量趨勢 (柱狀圖) | | ✅ |
| | — 每日活動量 (area chart) | | ✅ |
| | — 時間範圍選擇器 (24h / 7d / 30d) | | ✅ |
| | — WebSocket 連線狀態 | | ✅ |
| | — Bot 運行時間 (uptime) | | ✅ |
| | — Storage 使用量 | | ✅ |
| | — 最近的 error/warning 提醒 | | ✅ |
| P7.2 | **📈 Performance & Security Analysis Pages** | [#25](https://github.com/chimerakang/alice/issues/25) | ✅ |
| | — API 響應時間趨勢 (line chart) | | ✅ |
| | — Tool 執行時間分佈 (histogram) | | ✅ |
| | — Token 使用量 by model (stacked bar) | | ✅ |
| | — 成本趨勢 (area chart) | | ✅ |
| | — Memory 使用量 | | ✅ |
| | — 效能優化建議列表 (`/api/performance/recommendations`) | | ✅ |
| | — Security events 列表 (sortable table) | | ✅ |
| | — Severity 分佈 (pie/donut chart) | | ✅ |
| | — 事件趨勢 (stacked area by severity) | | ✅ |
| | — Blocked attempts 統計 | | ✅ |
| | — PII detection 記錄 | | ✅ |
| | — Audit log export | | ✅ |
| P7.3 | **Checkpoint ↔ DecisionLog 直接關聯** | [#26](https://github.com/chimerakang/alice/issues/26) | ✅ |
| | — `Checkpoint` struct 新增 `DecisionLogID` 欄位 | | ✅ |
| | — SQLite schema 新增 `decision_log_id` 欄位（ALTER TABLE + migration） | | ✅ |
| | — `CreateCheckpoint()` 接受 `decisionLogID` 參數 | | ✅ |
| | — `checkAndCreateCheckpoint()` 傳入當前 decision context | | ✅ |
| | — API response 包含 `decision_log_id` | | ✅ |
| | — TypeScript `Checkpoint` interface 新增 `decision_log_id` 欄位 | | ✅ |
| | — `findLinkedDecision()` 優先使用 `decision_log_id` 直接查詢 | | ✅ |
| | — 移除純時間戳配對的 fallback（或保留為 backward compatibility） | | ✅ |
| | — AIContextPanel 可直接透過 ID 載入 decision 資料 | | ✅ |
| P7.4 | **Checkpoints 頁面重新定位：AI 決策歷程 + 安全快照** | [#30](https://github.com/chimerakang/alice/issues/30) | ✅ |
| | — 重構頁面佈局：DecisionLog 為主體，Checkpoint 為附註標記 | | ✅ |
| | — 加入 DateRangeFilter + server-side 分頁（復用 Timeline 模式） | | ✅ |
| | — 重新設計卡片：user prompt → tool chain → outcome 為主，git/snapshot 為 collapsible 次要區塊 | | ✅ |
| | — 加入 slide-over Detail Panel（完整 thinking/response/ToolCallGantt/GitDiff + checkpoint restore） | | ✅ |
| | — 搜尋與篩選（搜尋 prompts/tools、filter by trigger type/project） | | ✅ |
| | — 建置驗證 + Docker dashboard 重建 | | ✅ |

## P8 - Control API (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P8.1 | **🎮 Remote Control API (Interrupt & Rollback)** | [#11](https://github.com/chimerakang/alice/issues/11) | ✅ |
| | — 可從儀表板中斷正在執行的 agent | | ✅ |
| | — 可從儀表板重置 agent 對話 | | ✅ |
| | — 控制端點有認證保護 | | ✅ |
| | — 操作結果透過 WebSocket 即時反饋 | | ✅ |

## P8.5 - TG 指令增強 (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P8.5.1 | **📋 /tasks 指令 — Telegram 查看待辦工作清單** | [#31](https://github.com/chimerakang/alice/issues/31) | ✅ |
| | — `/tasks` 指令能正確列出所有未完成的工作項目 | | ✅ |
| | — 顯示 Phase 名稱、進度百分比、狀態 emoji | | ✅ |
| | — 顯示任務編號、名稱、Issue 編號 | | ✅ |
| | — 無待辦項目時顯示全部完成訊息 | | ✅ |
| | — `/help` 指令包含 `/tasks` 說明 | | ✅ |
| | — MASTER_TASKS.md 不存在時顯示友善錯誤訊息 | | ✅ |
| P8.5.2 | **🔄 Topic-Project 對應持久化 — 重啟後保留設定** | [#33](https://github.com/chimerakang/alice/issues/33) | ✅ |
| | — `/project` 設定後重啟 bot，topic 仍記得之前的 project 路徑 | | ✅ |
| | — 新 topic（從未設定過）仍使用預設路徑 | | ✅ |
| | — 多個 topic 各自獨立持久化 | | ✅ |

## P9 - Multimedia Input (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P9.1 | **📷 Telegram 圖片訊息支援 — 傳送圖片給 Claude 分析** | [#28](https://github.com/chimerakang/alice/issues/28) | ✅ |
| | — 可在 TG 傳送圖片，Claude 能描述圖片內容 | | ✅ |
| | — 圖片 + caption 文字一起傳送給 Claude | | ✅ |
| | — 臨時檔案使用後清理 | | ✅ |
| | — `enable_photo_support: false` 時回覆提示訊息 | | ✅ |
| | — 超過大小限制時回覆錯誤訊息 | | ✅ |
| P9.2 | **🎙️ Telegram 語音訊息轉文字 — 語音輸入支援** | [#29](https://github.com/chimerakang/alice/issues/29) | ✅ |
| | — 可在 TG 傳送語音訊息，Claude 能回覆 | | ✅ |
| | — 轉錄文字在回覆中顯示（讓用戶確認辨識結果） | | ✅ |
| | — 支援中文和英文語音 | | ✅ |
| | — 臨時檔案使用後清理 | | ✅ |
| | — `enable_voice_support: false` 時回覆提示訊息 | | ✅ |
| | — 無 OpenAI API Key 時顯示設定提示 | | ✅ |
| | — 轉錄失敗時回覆錯誤訊息 | | ✅ |

## P9.5 - Multimedia Enhancement (✅ 100%)

| # | Task | Issue | Status |
|---|------|-------|--------|
| P9.5.1 | **🖼️ 多張圖片批次處理支援 — Telegram 媒體群組分析** | [#34](https://github.com/chimerakang/alice/issues/34) | ✅ |
| | — **時間窗口批次機制** - 3-5秒內收到的圖片自動歸為同一批次 | | ✅ |
| | — **暫存多張圖片** - 等待組合後一起分析 | | ✅ |
| | — **Telegram media_group_id 支援** - 利用官方媒體群組機制 | | ✅ |
| | — **組合 Claude 分析** - 將多張圖片一起傳給 Claude 做綜合比較 | | ✅ |
| | — **避免重複回應** - 單張圖片不觸發多次分析 | | ✅ |
| | — **混合媒體處理** - 優雅處理圖片+文字+語音混合訊息 | | ✅ |
| | — **記憶體管理** - 大量圖片的儲存與清理機制 | | ✅ |
| | — **用戶反饋** - 顯示處理進度「正在分析第 X/Y 張圖片...」 | | ✅ |
| | — 用戶一次傳送多張圖片，Alice 等待完整批次後進行綜合分析 | | ✅ |
| | — 回應中包含多張圖片的比較和關聯分析 | | ✅ |
| | — 單張圖片仍正常處理，不受影響 | | ✅ |
| | — 大量圖片時記憶體使用合理 | | ✅ |
| | — 用戶能看到處理進度提示 | | ✅ |
| P9.5.2 | **🔧 修復跨專案圖片存取問題 — 圖片路徑權限修復** | [#35](https://github.com/chimerakang/alice/issues/35) | ✅ |
| | — **圖片複製機制** - 將圖片從 Alice 臨時目錄複製到目標專案 | | ✅ |
| | — **相對路徑使用** - 使用 `temp/photo.jpg` 而非絕對路徑 | | ✅ |
| | — **專案級清理** - Agent 結束時清理專案臨時檔案 | | ✅ |
| | — **目錄確保存在** - 自動建立 `{projectDir}/temp/` 目錄 | | ✅ |
| | — **錯誤處理** - 複製失敗時的適當錯誤訊息 | | ✅ |

---

## Issue Tracker

| Issue | Title | Phase | Status |
|-------|-------|-------|--------|
| [#32](https://github.com/chimerakang/alice/issues/32) | Claude Code Hooks 整合：攔截所有 AI Agent 互動 | P10 - Claude Code Hooks | ✅ |
| [#37](https://github.com/chimerakang/alice/issues/37) | 🔍 /project 指令需要路徑驗證：防止設定不存在的專案目錄 | P11 - User Experience | ✅ |
| [#36](https://github.com/chimerakang/alice/issues/36) | 📊 Claude Code Hooks UI 增強：Dashboard 統計圖表 + 用戶指南 | P12 - Dashboard Analytics | ✅ |
| [#38](https://github.com/chimerakang/alice/issues/38) | 🐛 Telegram 429 Rate Limiting: 多 Agent 同時發送導致訊息遺失 | P13 - Future Enhancements | ✅ |
| [#39](https://github.com/chimerakang/alice/issues/39) | 🐛 Telegram 訊息 UTF-8 編碼錯誤導致發送失敗 | P13 - Future Enhancements | ✅ |
| [#44](https://github.com/chimerakang/alice/issues/44) | Dashboard Bug: Storage 顯示 — + 端口衝突導致 nginx 代理失效 | P13 - Future Enhancements | ✅ |
| [#48](https://github.com/chimerakang/alice/issues/48) | 【MVP】AI 開發審計系統 - 企業安全合規功能 | P13 - Future Enhancements | 🔄 |
| [#50](https://github.com/chimerakang/alice/issues/50) | 【商業功能】單機版主管報告系統 - AI 使用效益監控 | P13 - Future Enhancements | 🔄 |
| [#52](https://github.com/chimerakang/alice/issues/52) | 🔧 擴展 PerformanceMetrics - 增加管理層洞察數據收集 | P13 - Future Enhancements | 🔄 |
| [#55](https://github.com/chimerakang/alice/issues/55) | 🎨 Manager Dashboard 前端介面 - 主管視角的 AI 使用分析頁面 | P13 - Future Enhancements | 🔄 |
| [#57](https://github.com/chimerakang/alice/issues/57) | 📊 自動週報生成系統 - 團隊 AI 使用效益報告 | P13 - Future Enhancements | 🔄 |
| [#59](https://github.com/chimerakang/alice/issues/59) | 🚨 智能異常檢測系統 - AI 使用模式風險預警 | P13 - Future Enhancements | 🔄 |
| [#60](https://github.com/chimerakang/alice/issues/60) | 💰【商業功能】Alice SecureGuard - AI 開發防洩密系統 | P13 - Future Enhancements | 🔄 |
| [#61](https://github.com/chimerakang/alice/issues/61) | 🔍 智能 Token 檢測引擎 - SecureGuard 核心功能 | P13 - Future Enhancements | 🔄 |
| [#62](https://github.com/chimerakang/alice/issues/62) | Performance Bug: 時間範圍篩選器對 Tool Distribution 無效 — API 未支援時間參數 | P13 - Future Enhancements | ✅ |
| [#68](https://github.com/chimerakang/alice/issues/68) | 🐛 Hook 腳本未提取 session duration 和 token 數據 | P13 - Future Enhancements | ✅ |
| [#69](https://github.com/chimerakang/alice/issues/69) | Security 頁面：PII Detection Records 缺乏上下文資訊，無法判斷問題內容 | P13 - Future Enhancements | ✅ |
| [#70](https://github.com/chimerakang/alice/issues/70) | Security 頁面：Events Trend 圖表未跟隨時間篩選器 + 標題硬編碼 | P13 - Future Enhancements | ✅ |
| [#71](https://github.com/chimerakang/alice/issues/71) | 🐛 Telegram /tasks 無法顯示 GitHub Issues（private repo 認證失敗） | P13 - Future Enhancements | ✅ |
| [#72](https://github.com/chimerakang/alice/issues/72) | P13: Dynamic Model Routing - 智慧模型路由降低 Token 成本 | P13 - Future Enhancements | ✅ |
| [#73](https://github.com/chimerakang/alice/issues/73) | P13: Per-Model Cost Tracking - 按模型記錄 Token 成本與用量 | P13 - Future Enhancements | ✅ |
| [#74](https://github.com/chimerakang/alice/issues/74) | P13: Savings Calculator - 智慧路由省錢可視化（商業賣點） | P13 - Future Enhancements | ✅ |
| [#75](https://github.com/chimerakang/alice/issues/75) | 回填歷史資料缺失的 model 與費用欄位 | P13 - Future Enhancements | ✅ |
| [#76](https://github.com/chimerakang/alice/issues/76) | Bot 多國語系支援 — 可切換顯示語言 | P13 - Future Enhancements | ✅ |
| [#77](https://github.com/chimerakang/alice/issues/77) | /usage 指令增加按模型分類的 token 用量與費用顯示 | P13 - Future Enhancements | ✅ |
| [#78](https://github.com/chimerakang/alice/issues/78) | Add project_path field to performance_metrics table for per-project token cost tracking | P13 - Future Enhancements | ✅ |
| [#79](https://github.com/chimerakang/alice/issues/79) | 🐛 Smart Routing 導致對話上下文丟失：Model 切換時強制清空 Session | P13 - Future Enhancements | ✅ |
| [#80](https://github.com/chimerakang/alice/issues/80) | 🎨 Cost Trend 頁面 UI 修正：標籤更新 + 卡片橫向排列 | P13 - Future Enhancements | ✅ |
| [#81](https://github.com/chimerakang/alice/issues/81) | 🔀 Smart Routing 上下文橋接：continuation 偵測 + model 切換時保留對話記憶 | P13 - Future Enhancements | ✅ |
| [#82](https://github.com/chimerakang/alice/issues/82) | Agent 媒體發送功能 - 圖片/影片/文件回傳到 Telegram chat | P13 - Future Enhancements | ✅ |
| [#83](https://github.com/chimerakang/alice/issues/83) | 實現 Bot 網頁截圖預覽功能 - 使用 skill 的 playwright-cli 支援任意 URL 截圖 | P13 - Future Enhancements | ✅ |
| [#85](https://github.com/chimerakang/alice/issues/85) | 🧠 OpusPlan 兩階段模型調用 - 計劃用 Opus、執行用 Sonnet | P13 - Future Enhancements | ✅ |
| [#86](https://github.com/chimerakang/alice/issues/86) | feat: Implement auto-skill generation system | P13 - Future Enhancements | ✅ |
| [#89](https://github.com/chimerakang/alice/issues/89) | feat: Implement cron scheduler for automated tasks | P13 - Future Enhancements | ✅ |
| [#90](https://github.com/chimerakang/alice/issues/90) | bug: CallStream 丟棄 CLI exit error 時的 streaming 結果導致「執行錯誤」 | P13 - Future Enhancements | ✅ |
| [#91](https://github.com/chimerakang/alice/issues/91) | feat: Claude Design 整合 - UI 原型與設計生成 [等待 API 開放] | P13 - Future Enhancements | ✅ |
| [#92](https://github.com/chimerakang/alice/issues/92) | feat: Telegram 對話式 UI 原型生成 (/prototype 命令) | P13 - Future Enhancements | ✅ |
| [#93](https://github.com/chimerakang/alice/issues/93) | Model routing 造成 session context 丟失：改採 Sticky + Follow-up detection | P13 - Future Enhancements | ✅ |
| [#129](https://github.com/chimerakang/alice/issues/129) | 整合 OpenAI Image Generation (gpt-image-2 / DALL-E) 為遊戲開發鋪路 | P13 - Future Enhancements | 🔄 |
| [#130](https://github.com/chimerakang/alice/issues/130) | research: VS Code 上的 Codex CLI 訊息攔截方案 | P13 - Future Enhancements | ✅ |
| [#150](https://github.com/chimerakang/alice/issues/150) | Implement Codex CLI VS Code interception (Phase 1: JSONL watcher) | P13 - Future Enhancements | ✅ |
| [#49](https://github.com/chimerakang/alice/issues/49) | Alice 商業化：單機版隱私優先定位策略 | P14 - Commercialization Strategy | 🔄 |
| [#51](https://github.com/chimerakang/alice/issues/51) | 多人版架構設計：從單機到團隊協作 | P14 - Commercialization Strategy | 🔄 |
| [#53](https://github.com/chimerakang/alice/issues/53) | 競品分析深化：Entire Checkpoints vs Alice 差異化策略 | P14 - Commercialization Strategy | ✅ |
| [#54](https://github.com/chimerakang/alice/issues/54) | 產品授權與定價模式設計 | P14 - Commercialization Strategy | 🔄 |
| [#56](https://github.com/chimerakang/alice/issues/56) | 品牌定位與行銷策略規劃 | P14 - Commercialization Strategy | 🔄 |
| [#58](https://github.com/chimerakang/alice/issues/58) | Alice 商業化執行藍圖：6個月行動計畫 | P14 - Commercialization Strategy | 🔄 |
| [#115](https://github.com/chimerakang/alice/issues/115) | Post-stabilization cleanup: retire legacy coordinator / DecisionLog bridge | P15 - Hermes Stabilization & Cleanup | 🔄 |
| [#120](https://github.com/chimerakang/alice/issues/120) | [Closed/Superseded Epic] Alice architecture unification: ExecutionEngine + Review feedback | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#143](https://github.com/chimerakang/alice/issues/143) | 建立 Alice Unified Memory Architecture | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#144](https://github.com/chimerakang/alice/issues/144) | Hermes mode 架構精簡：路由規則、狀態機、訊息流 | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#146](https://github.com/chimerakang/alice/issues/146) | SessionPolicy: direct bridge / model switch memory source policy | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#148](https://github.com/chimerakang/alice/issues/148) | Runtime trace + token/cache observability | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#149](https://github.com/chimerakang/alice/issues/149) | Spike: Single-session walking agent via Python Claude Agent SDK (#148 Phase 2) | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#158](https://github.com/chimerakang/alice/issues/158) | Optimize Hermes token efficiency with outlier reporting and compact continuation context | P15 - Hermes Stabilization & Cleanup | 🔄 |
| [#159](https://github.com/chimerakang/alice/issues/159) | Prevent Hermes no-op continuation loops when GitHub checklist is unsynced | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#160](https://github.com/chimerakang/alice/issues/160) | Add IssueOps Agent FSM for GitHub issue lifecycle, checklist sync, and close readiness | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#161](https://github.com/chimerakang/alice/issues/161) | Hermes: durable execution runtime with reducer-based snapshots | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#162](https://github.com/chimerakang/alice/issues/162) | Hermes runtime Phase 1: add snapshot schema and runtime types | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#163](https://github.com/chimerakang/alice/issues/163) | Hermes runtime Phase 2: centralize state updates behind reducers | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#164](https://github.com/chimerakang/alice/issues/164) | Hermes runtime Phase 3: write snapshots at execution step boundaries | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#165](https://github.com/chimerakang/alice/issues/165) | Hermes runtime Phase 4: resume and recover from latest snapshot | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#166](https://github.com/chimerakang/alice/issues/166) | Hermes runtime Phase 5: persist interrupts and approval waits | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#167](https://github.com/chimerakang/alice/issues/167) | Align Telegram Markdown render with leaf approach | P15 - Hermes Stabilization & Cleanup | ✅ |
| [#177](https://github.com/chimerakang/alice/issues/177) | Hermes milestone review command: /mr GitHub-sourced closeout review | P15 - Hermes Stabilization & Cleanup | 🔄 |
| [#87](https://github.com/chimerakang/alice/issues/87) | feat: Implement parallel subagent execution with isolated contexts | P15 - Parallel Subagents & Orchestration | ✅ |
| [#95](https://github.com/chimerakang/alice/issues/95) | [Epic] Alice Hermes 化路徑圖：Brain-Executor 架構遷移 | P15 - Parallel Subagents & Orchestration | ✅ |
| [#96](https://github.com/chimerakang/alice/issues/96) | Hermes: Tool Execution Hooks — Post-validator + Path guard | P15 - Parallel Subagents & Orchestration | ✅ |
| [#97](https://github.com/chimerakang/alice/issues/97) | Hermes: Task State 持久化層 — state.json for cold-start executors | P15 - Parallel Subagents & Orchestration | ✅ |
| [#98](https://github.com/chimerakang/alice/issues/98) | Hermes: Planner-Executor 架構拆分 — Brain-Executor 協作核心 | P15 - Parallel Subagents & Orchestration | ✅ |
| [#99](https://github.com/chimerakang/alice/issues/99) | Hermes: Core Rules Prepender — 沿用 skills 不自製 profile 系統 | P15 - Parallel Subagents & Orchestration | ✅ |
| [#100](https://github.com/chimerakang/alice/issues/100) | Hermes (Future): SDK 混用方案探索 — 結構化輸出與並行優化 | P15 - Parallel Subagents & Orchestration | ✅ |
| [#101](https://github.com/chimerakang/alice/issues/101) | Hermes: GitHub Issue 整合層 — /hermes #N、checklist 同步、自動 comment | P15 - Parallel Subagents & Orchestration | ✅ |
| [#102](https://github.com/chimerakang/alice/issues/102) | Hermes: Task Summary Report — token 用量分拆與可切換開關 | P15 - Parallel Subagents & Orchestration | ✅ |
| [#103](https://github.com/chimerakang/alice/issues/103) | Hermes: 過度拆解防護 — Complexity Gate + 粒度規則 + 數量上限 | P15 - Parallel Subagents & Orchestration | ✅ |
| [#104](https://github.com/chimerakang/alice/issues/104) | [Experiment] Hermes: VS Code UserPromptSubmit hook 攔截實驗 | P15 - Parallel Subagents & Orchestration | ✅ |
| [#105](https://github.com/chimerakang/alice/issues/105) | Hermes: FetchIssue/gh 呼叫沒傳 project_dir，跨 repo 情境 exit status 1 | P15 - Parallel Subagents & Orchestration | ✅ |
| [#88](https://github.com/chimerakang/alice/issues/88) | feat: Implement multi-backend execution environment support | P16 - Multi-Backend Execution | ✅ |
| [#1](https://github.com/chimerakang/alice/issues/1) | 🎛️ Web Dashboard Integration | P2 - Monitoring | ✅ |
| [#2](https://github.com/chimerakang/alice/issues/2) | 🔍 AI Agent Transparency & Decision Logging | P2 - Monitoring | ✅ |
| [#3](https://github.com/chimerakang/alice/issues/3) | 🤖 Multi-Agent Coordination System | P2 - Monitoring | ✅ |
| [#4](https://github.com/chimerakang/alice/issues/4) | 📊 Performance Monitoring & Analytics | P2 - Monitoring | ✅ |
| [#5](https://github.com/chimerakang/alice/issues/5) | 🔐 Security & Privacy Enhancements | P2 - Monitoring | ✅ |
| [#12](https://github.com/chimerakang/alice/issues/12) | 🖥️ Dashboard Enhancement: Timeline & Terminal | P2 - Monitoring | ✅ |
| [#14](https://github.com/chimerakang/alice/pull/14) | 🚀 Feature: Complete Dashboard, Monitoring & Checkpoint System | P2 - Monitoring | ✅ |
| [#6](https://github.com/chimerakang/alice/issues/6) | 🚀 Deployment & DevOps Improvements | P3 - Data Layer | ✅ |
| [#7](https://github.com/chimerakang/alice/issues/7) | 💾 Data Persistence Layer (SQLite) | P3 - Data Layer | ✅ |
| [#8](https://github.com/chimerakang/alice/issues/8) | 🔌 WebSocket Real-time Dashboard Connection | P3 - Data Layer | ✅ |
| [#9](https://github.com/chimerakang/alice/issues/9) | 🔗 Git Integration & Commit Correlation | P3 - Data Layer | ✅ |
| [#10](https://github.com/chimerakang/alice/issues/10) | 📸 Checkpoint & State Snapshot System | P3 - Data Layer | ✅ |
| [#13](https://github.com/chimerakang/alice/issues/13) | 🏗️ Architecture: Proto-First API with Protocol Buffers | P4 - Proto-First | ✅ |
| [#15](https://github.com/chimerakang/alice/issues/15) | ⚛️ React + Vite + TypeScript 專案初始化 | P5 - Frontend Foundation | ✅ |
| [#16](https://github.com/chimerakang/alice/issues/16) | 🧱 共用 UI 元件庫 (Layout, StatusBadge, MetricCard) | P5 - Frontend Foundation | ✅ |
| [#17](https://github.com/chimerakang/alice/issues/17) | 🔌 WebSocket Hook + Zustand 狀態管理 | P5 - Frontend Foundation | ✅ |
| [#18](https://github.com/chimerakang/alice/issues/18) | 📊 Dashboard 主頁面 + 圖表元件 | P5 - Frontend Foundation | ✅ |
| [#19](https://github.com/chimerakang/alice/issues/19) | ⏳ Timeline + Terminal 頁面 | P5 - Frontend Foundation | ✅ |
| [#20](https://github.com/chimerakang/alice/issues/20) | 📋 Git/Checkpoint/Performance/Security 子頁面 | P5 - Frontend Foundation | ✅ |
| [#21](https://github.com/chimerakang/alice/issues/21) | 🔍 AI Decision Timeline — 核心審計頁面 | P6 - AI Audit System | ✅ |
| [#22](https://github.com/chimerakang/alice/issues/22) | 📋 Decision Detail View + Git Diff Viewer | P6 - AI Audit System | ✅ |
| [#23](https://github.com/chimerakang/alice/issues/23) | 📸 Checkpoint Management UI | P6 - AI Audit System | ✅ |
| [#27](https://github.com/chimerakang/alice/issues/27) | 擴充 CallStream 擷取完整 AI 思考與文字內容 | P6 - AI Audit System | ✅ |
| [#24](https://github.com/chimerakang/alice/issues/24) | 📊 Dashboard Enhancement — AI Activity Overview | P7 - Dashboard & Analytics | ✅ |
| [#25](https://github.com/chimerakang/alice/issues/25) | 📈 Performance & Security Analysis Pages | P7 - Dashboard & Analytics | ✅ |
| [#26](https://github.com/chimerakang/alice/issues/26) | Checkpoint ↔ DecisionLog 直接關聯 | P7 - Dashboard & Analytics | ✅ |
| [#30](https://github.com/chimerakang/alice/issues/30) | Checkpoints 頁面重新定位：AI 決策歷程 + 安全快照 | P7 - Dashboard & Analytics | ✅ |
| [#11](https://github.com/chimerakang/alice/issues/11) | 🎮 Remote Control API (Interrupt & Rollback) | P8 - Control API | ✅ |
| [#31](https://github.com/chimerakang/alice/issues/31) | 📋 /tasks 指令 — Telegram 查看待辦工作清單 | P8.5 - TG 指令增強 | ✅ |
| [#33](https://github.com/chimerakang/alice/issues/33) | 🔄 Topic-Project 對應持久化 — 重啟後保留設定 | P8.5 - TG 指令增強 | ✅ |
| [#28](https://github.com/chimerakang/alice/issues/28) | 📷 Telegram 圖片訊息支援 — 傳送圖片給 Claude 分析 | P9 - Multimedia Input | ✅ |
| [#29](https://github.com/chimerakang/alice/issues/29) | 🎙️ Telegram 語音訊息轉文字 — 語音輸入支援 | P9 - Multimedia Input | ✅ |
| [#34](https://github.com/chimerakang/alice/issues/34) | 🖼️ 多張圖片批次處理支援 — Telegram 媒體群組分析 | P9.5 - Multimedia Enhancement | ✅ |
| [#35](https://github.com/chimerakang/alice/issues/35) | 🔧 修復跨專案圖片存取問題 — 圖片路徑權限修復 | P9.5 - Multimedia Enhancement | ✅ |

---

## Summary

**Total Issues:** 112
**Completed:** 94 ✅
**In Progress:** 18 🔄

**Last sync:** 2026-05-29 15:49 UTC

