# Alice Subtask 拆分框架

**版本**: 1.0 | **最後更新**: 2026-04-27 | **目的**: 系統化提升 Agent 任務品質

---

## 📋 目錄

1. [核心概念](#核心概念)
2. [Task 分類系統](#task-分類系統)
3. [拆分決策樹](#拆分決策樹)
4. [驗收標準模板](#驗收標準模板)
5. [自動化品質檢查](#自動化品質檢查)
6. [案例研究](#案例研究)

---

## 核心概念

### 什麼是好的 Subtask？

```
┌─────────────────────────────────────────────┐
│ 獨立完成  │ 明確驗收  │ ≤ 2-3 天工時  │ 無隱藏陷阱 │
└─────────────────────────────────────────────┘
```

**4 大特徵**：
1. **單一職責** — 一個 subtask = 一個功能單元或修復
2. **驗收清晰** — 定義「完成」的確切條件（code、test、doc）
3. **工時可控** — 1-3 天可完成，超過則需重新拆分
4. **依賴透明** — 明確標注 blocking issue，避免 agent 盲目等待

### 拆分的三個層次

| 層次 | 工時 | 驗收項目 | 依賴 | 範例 |
|------|------|---------|------|------|
| **原子 (Atom)** | 4-8h | code + 1 test file | 無 | 新增 1 個 message key + 3 個語言版本 |
| **功能 (Feature)** | 1-3d | code + unit/e2e + doc | ≤2 個原子 | i18n Image Analysis (18 個 keys) |
| **Epic** | 2-8w | 完整功能 + migration | 多個功能 | Hermes Architecture Unification (#120) |

---

## Task 分類系統

### 分類樹

```
Task
├─ 🔴 BUG (優先度最高)
│  ├─ Critical (用戶被完全阻擋)
│  ├─ High (用戶 workaround 困難)
│  └─ Normal (有 workaround)
│
├─ 🟢 FEATURE
│  ├─ New Module (新建整個模組)
│  ├─ Extend Existing (擴展已有功能)
│  └─ Config/Tuning (調參、微調)
│
├─ 🟠 REFACTOR
│  ├─ Restructure (改變架構/介面)
│  ├─ Consolidate (去重複)
│  └─ Optimize (性能改善)
│
├─ 🟡 HARDCODED TEXT (多國語系)
│  ├─ UI Message (用戶可見訊息)
│  ├─ Error Message (錯誤訊息)
│  └─ System Prompt (AI 提示詞)
│
└─ ⚪ DOCS / CHORES
   ├─ Documentation
   ├─ Test Coverage
   └─ Dependency Update
```

### 各分類的預設拆分粒度

#### 🔴 BUG — 按受影響範圍

| 類型 | 拆分方式 | 例子 |
|------|---------|------|
| **Logic Bug** | 單一修復 + 迴歸測試 | #116: schema migration 缺欄位 |
| **Multi-layer Bug** | 按層分 + 集成測試 | #108: context loss (Planner + Executor + DB) |
| **Data Corruption** | 修復 + 回補舊資料 + 驗證 | 需要 3 個獨立 subtask |

**例子**：Issue #108 (Hermes follow-up context loss)
```
拆分為：
  A. Planner 層：follow-up detection (1d)
  B. Executor 層：context injection (1d)
  C. Storage 層：session persistence (1d)
  D. Integration test (0.5d)
  總計：3.5d，分 4 個 subtask
```

#### 🟢 FEATURE — 按資料結構 + 流程

| 場景 | 拆分方式 | 工時 |
|------|---------|------|
| **全新模組** | schema → API → UI → 整合 | 3-5d |
| **新增欄位** | DB migration → query 更新 → API → 展示 → test | 2-4d |
| **新指令** | 命令邏輯 → 幫助文本 → 單元測試 → e2e | 1-2d |

#### 🟠 REFACTOR — 按架構層

| 類型 | 拆分方式 | 風險 |
|------|---------|------|
| **Interface Extraction** | 定義 interface → 改 A 實作 → 改 B 實作 → 整合測試 | 中 |
| **Consolidation** | 找重複 → 提取公共函數 → 更新所有呼叫端 → 測試 | 中 |
| **Migration** | dual-write 準備 → 舊邏輯遷 → 切流量 → 清舊 | 高 |

#### 🟡 HARDCODED TEXT (i18n) — 按功能模組

| 模組 | 文本數 | 拆分粒度 | 工時 |
|------|--------|---------|------|
| Image Analysis | 18 | 1 subtask | 1.5d |
| File Handling | 10 | 1 subtask | 1d |
| Multi-Agent | 9 | 1 subtask | 1d |
| Checkpoint | 11 | 2 subtask (4 keys + 7 keys) | 1d + 1d |
| Task Stats | 8 | 2 subtask (4 keys + 4 keys) | 1d + 1d |

---

## 拆分決策樹

```
Issue 進來
  │
  ├─ 是 Epic? (>5d 或多個功能區域)
  │  └─ YES → 拆成 Feature + Subtask
  │          標示 epic label，創建進度看板
  │
  ├─ 是 Bug?
  │  ├─ YES, Critical
  │  │  └─ 按受影響層級拆 (Logic / Multi-layer / Data)
  │  │     每層 0.5-2d，最多 4 subtask
  │  │
  │  └─ YES, Normal
  │     └─ 能一次修完? YES → 保持原 issue
  │                  NO  → 拆成 3 subtask
  │
  ├─ 是 Feature?
  │  ├─ 新模組? → 拆 5 層：DB → Logic → API → UI → Test
  │  ├─ 新欄位? → 拆 3 層：DB migration → 邏輯 → UI + Test
  │  └─ 新指令? → 保持原 issue (1-2d 內)
  │
  ├─ 是 Refactor?
  │  ├─ 改 Interface? → 拆 3 層：define → impl A → impl B → test
  │  ├─ 去重複? → 保持原 issue (1-2d 內)
  │  └─ Migration? → 拆 4 層：dual-write → migrate → switch → cleanup
  │
  ├─ 是 i18n Text?
  │  ├─ <10 keys? → 保持原 issue
  │  ├─ 10-20 keys? → 1 subtask
  │  └─ >20 keys? → 按模組拆 (Image/File/etc)
  │
  └─ 是 Docs/Chore?
     └─ 保持原 issue (<1d)
```

---

## 驗收標準模板

### 通用驗收標準

**所有 subtask 都必須有這些**：

```markdown
## Acceptance Criteria

### Code Changes
- [ ] 代碼邏輯正確無誤
- [ ] 無新增 linting 警告
- [ ] 遵守 project CLAUDE.md 安全規範
- [ ] 無硬編碼敏感資訊

### Tests
- [ ] ≥ 1 個新單元測試
- [ ] 所有現有測試仍通過
- [ ] 覆蓋率變化: `X% → Y%`

### Documentation
- [ ] 代碼內聲明清晰（必要時）
- [ ] 如有新 API / 命令，更新 CLAUDE.md
- [ ] 性能基準線記錄（若涉及效能）

### Deployment
- [ ] 無 DB migration 或已隔離測試
- [ ] 無破壞性改動
- [ ] 可獨立 revert（無其他 issue 依賴）
```

### 分類別驗收標準範本

#### 🔴 BUG 驗收標準

```markdown
## 修復驗收

### Root Cause Analysis
- [ ] 說明根本原因（不只是「修了個 typo」）
- [ ] 說明為何過去沒被發現（測試缺口 / 邊界情況）

### 修復驗收
- [ ] 舊的失敗測試現在通過
- [ ] 新增迴歸測試防止重發
- [ ] 所有相關層都檢查過 (數據層 / 邏輯層 / UI層)

### 實機驗證 (如適用)
- [ ] 手動複現舊 bug → 確認已修復
- [ ] 邊界情況測試 (空值 / 大值 / 特殊字符)
- [ ] 性能無降級

### 後續檢查
- [ ] 檢查類似 bug 是否在其他地方存在
- [ ] 如果涉及數據，檢查歷史數據是否需要修補
```

#### 🟢 FEATURE 驗收標準

```markdown
## 功能驗收

### 需求完整性
- [ ] 核心功能實現 (所有 requirement 列舉清楚)
- [ ] Edge case 考慮 (空值、大值、權限、國際化)

### 代碼品質
- [ ] 主要邏輯有單元測試
- [ ] 端對端測試 / 手動驗證可用
- [ ] error message 使用 i18n key (不硬編碼)

### 文檔完整
- [ ] 新 API / 命令已加入 CLAUDE.md
- [ ] 如有新概念，加進 docs/
- [ ] 困難決定有註釋說明為什麼

### 用戶體驗
- [ ] 幫助文本清楚（如有新命令）
- [ ] UI 反應合理 (loading state / error state)
- [ ] 國際化支援 (中文/英文)
```

#### 🟠 REFACTOR 驗收標準

```markdown
## 重構驗收

### 結構改進
- [ ] 新架構圖解清楚
- [ ] 接口契約明確 (函數簽名 / 結構定義)
- [ ] 無遺留的舊實現

### 破壞性評估
- [ ] 如有 public API 改動，檢查所有呼叫端
- [ ] 如有 DB schema 改動，migration 已準備
- [ ] 如無法完全相容，deprecation warning 已加入

### 測試覆蓋
- [ ] 舊測試仍通過 (行為相同)
- [ ] 新架構邊界測試 (interface 交界)
- [ ] 性能基準線 (vs before refactor)

### 可回退性
- [ ] 重構可獨立 revert
- [ ] 沒有其他 issue 強依賴此重構
```

#### 🟡 HARDCODED TEXT (i18n) 驗收標準

```markdown
## i18n 驗收

### 文本清單
- [ ] 列舉所有硬編碼文本 (N 個 keys)
- [ ] 確認無遺漏 (與需求對應)

### i18n 集成
- [ ] 所有 key 加入 `i18n/messages.json`
- [ ] 至少 2 種語言翻譯完成 (簡體中文 + 繁體中文 或 英文)
- [ ] 無遺留硬編碼字符串

### 代碼驗證
- [ ] 所有 `t.send()` / `fmt.Sprintf()` 改用 i18n key
- [ ] 使用 `i18n.GetMessage("key", ctx, params)` 模式
- [ ] 動態參數正確插入

### 測試
- [ ] 測試驗證訊息內容正確
- [ ] 切換語言後訊息改變
- [ ] 特殊字符 (emoji / 非 ASCII) 顯示正確
```

---

## 自動化品質檢查

### PR 合併前檢查清單

```bash
# 1. 驗收標準檢查
❌ PR 缺少 "Acceptance Criteria" 章節
❌ Acceptance 項目有未勾選 ([ ] 未改成 [x])

# 2. 代碼品質檢查
❌ git diff 有硬編碼的中文 (i18n 類 PR)
❌ 新增的 fmt.Sprintf 沒有對應測試
❌ linting warning 數量增加

# 3. 測試覆蓋檢查
❌ 測試覆蓋率下降 (需說明原因)
❌ 新增 .go 文件無對應 _test.go

# 4. 文檔檢查
❌ 新 API / 命令未加入 CLAUDE.md
❌ 重構無說明文檔

# 5. 提交檢查
❌ commit message 無說明為什麼改 (只有 what)
❌ 新增大文件 (> 1MB)
```

### 自動化 CI 規則建議

```yaml
# .github/workflows/subtask-quality.yml (未來)
on: [pull_request]

jobs:
  acceptance_check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - name: Check Acceptance Criteria
        run: |
          # 檢查 PR body 是否有 "## Acceptance Criteria"
          # 檢查所有 [ ] 是否都改成 [x]
          grep "## Acceptance Criteria" ${{ github.event.pull_request.body }}
          
  code_quality:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - name: Hardcoded Chinese Check (for i18n PR)
        if: contains(github.event.pull_request.labels, 'i18n')
        run: |
          # 檢查新增行 (git diff) 是否有中文字符
          git diff --unified=0 | grep "^+" | grep -E "[\u4E00-\u9FA5]"
          
      - name: Test Coverage
        run: go test -cover ./...
```

---

## 案例研究

### 案例 1: Bug Fix — #116 (schema 缺欄位)

**原始 Issue**:
```
Hermes: hermes_tasks schema 缺 budget 欄位 → ResetBudgetStartedAt 失敗
```

**拆分決定**: ✅ **不拆分，單一 subtask**

**理由**:
- 工時 < 1 天
- 單一修復點 (加欄位 + migration + 1 個查詢更新)
- 無隱藏複雜性

**驗收標準** (Bug 類):
```markdown
## Acceptance Criteria

### Root Cause
- [x] schema 缺欄位導致 NULL 參照
- [x] 為何未被測試 → old schema 未正確 mock

### 修復
- [x] 新增 budget 欄位到 hermes_tasks
- [x] Migration 版本號遞增
- [x] ResetBudgetStartedAt 查詢補上 budget
- [x] 新增 test: hermes_tasks_budget_test.go
  - 驗證欄位存在
  - 驗證 ResetBudgetStartedAt 通過

### 驗證
- [x] 手動測試: hermes task 完成時 budget reset 成功
- [x] 現有測試仍過 (test_coordinator.go)
```

---

### 案例 2: Feature — i18n Image Analysis (18 keys)

**原始 Issue**:
```
[HARDCODED TEXT] Image Analysis 功能未國際化（18 個文本）
```

**拆分決定**: ✅ **1 個 subtask**

**理由**:
- 單一功能模組 (Image Analysis)
- 18 個 key，預計 1.5 天
- 所有 key 在同一檔案 (telegram.go 行 1684-2147)

**驗收標準** (i18n 類):
```markdown
## Acceptance Criteria

### 文本清單
- [x] 枚舉所有 18 個硬編碼中文字符串
  1. "圖片分析功能目前未啟用"
  2. "正在分析 %d 張圖片..."
  ... (清單省略)

### i18n 集成
- [x] 新增 18 個 message keys 到 i18n/messages.json
  - "image_analysis_disabled"
  - "image_analysis_processing"
  - ... (清單)

- [x] 繁體中文翻譯完成 (native)
- [x] 英文翻譯完成 (assistant)
- [x] 簡體中文翻譯完成 (assistant)

### 代碼修改
- [x] 所有 t.send(..., "硬編碼") 改為 t.send(key, ...)
- [x] 所有 fmt.Sprintf 中的訊息改為 i18n.GetMessage()
- [x] 驗證無遺漏 (grep "圖片分析" 零結果)

### 測試
- [x] 新增 test_image_analysis_i18n_test.go
  - 驗證 18 個 key 存在於所有語言
  - 驗證正確 key 被調用
  - 驗證特殊字符 (emoji) 正確

- [x] 手動測試: 切換語言，圖片分析訊息改變
```

---

### 案例 3: Refactor — #110-111 (架構抽象)

**原始 Issue**:
```
Epic #120 - 階段①②: ChatContext 抽取 + ExecutionEngine 介面
```

**拆分決定**: ✅ **2 個依序 subtask**

```
Subtask 1: #110 (3-4 days)
  → 抽出 ChatContext struct
  → 修改 Agent.Run()、Coordinator 簽名
  → 現有測試仍過

Subtask 2: #111 (3-4 days，依賴 #110)
  → 定義 ExecutionEngine interface
  → DirectEngine 實現 (包住 Agent.Run)
  → PlanExecuteEngine stub
  → 集成測試
```

**驗收標準** (Refactor 類 - #110 範例):
```markdown
## Acceptance Criteria

### 結構改進
- [x] ChatContext 定義清楚
  ```go
  type ChatContext struct {
    RecentMessages []Message
    SessionID      string     // Claude Code session ID
    CodelID        string     // Hermes coordinator ID
    Metadata       map[string]interface{}
  }
  ```

- [x] Agent.Run() 簽名改為：
  `func (a *Agent) Run(ctx *ChatContext) (...)`

- [x] Coordinator.Execute() 簽名改為：
  `func (c *Coordinator) Execute(ctx *ChatContext) (...)`

### 破壞性評估
- [x] 檢查所有 Agent.Run() 呼叫端 (telegram.go 行 XXX)
  → 所有更新完成
- [x] 檢查所有 Coordinator.Execute() 呼叫端
  → 所有更新完成

### 測試覆蓋
- [x] 現有 test_agent_test.go 仍通過 (邏輯無改)
- [x] 新增 test_chat_context_test.go
  - 驗證 ChatContext 跨 engine 傳遞
  - 驗證 SessionID 保留
  - 驗證 metadata 不丟失

- [x] 性能基準線 (vs before)
  ```
  Agent.Run() latency: 450ms → 451ms (OK, +0.2%)
  ```

### 可回退性
- [x] 此 subtask 無其他 issue 依賴（除了 #111 是故意依賴）
- [x] 可獨立 revert
```

---

## 推薦工作流程

### 新 Issue 進來時

```
1. Issue Triage (Owner / Lead)
   ├─ 分類 (Bug / Feature / Refactor / i18n / Docs)
   ├─ 評估工時
   ├─ 決策拆分 (使用決策樹)
   └─ 建立子任務清單或 Epic

2. 建立 GitHub Issue / Subtask
   ├─ 標題清晰 (X: 做什麼，為什麼)
   ├─ 加入驗收標準 (複製對應模板)
   ├─ 標籤完整 (priority / category / complexity)
   └─ 標注依賴 (blocking / depends on)

3. Assign 給 Agent
   ├─ Agent 優先檢查驗收標準是否清晰
   ├─ 若不清晰，回報 (comment on issue)
   └─ 否則開始實現

4. Code Review
   ├─ 檢查驗收標準是否全部 ✅
   ├─ 檢查自動化品質檢查
   └─ 批准或請改

5. Merge & Close
   ├─ 驗收標準全勾 → 可 merge
   └─ 自動關閉相關 issue
```

---

## 常見反模式 ⚠️

| 反模式 | 症狀 | 修正 |
|--------|------|------|
| **驗收標準模糊** | Agent 多次問「現在可以 PR 嗎」 | 提供具體檢查清單 |
| **拆分太細** | 10 個 0.5 天的 subtask | 合併相關的 (≥ 1 天) |
| **拆分太粗** | 1 個 5 天的 subtask | 按層級或模組再拆 |
| **隱藏依賴** | Agent 做到一半發現被卡住 | 明確標註 blocking issue |
| **需求變更** | 實現到一半才改驗收標準 | 修改時 notify agent，必要時新開 issue |
| **測試遺漏** | 代碼通過但沒人驗 | 驗收標準明確要求 ≥1 test |

---

## 附錄：自動化檢查腳本 (未來)

```bash
#!/bin/bash
# scripts/check-subtask-quality.sh

# 1. 檢查 PR body 有驗收標準
if ! grep -q "## Acceptance Criteria" "$PR_BODY_FILE"; then
    echo "❌ 缺少驗收標準章節"
    exit 1
fi

# 2. 檢查 i18n PR 無硬編碼中文
if [[ "$PR_LABELS" == *"i18n"* ]]; then
    if git diff --unified=0 | grep "^+" | grep -E "[\u4E00-\u9FA5]" | grep -v test; then
        echo "⚠️ 警告：i18n PR 仍有硬編碼中文字符"
    fi
fi

# 3. 檢查測試覆蓋率未降低
COVERAGE_BEFORE=$(git show HEAD:coverage.txt 2>/dev/null || echo "0")
COVERAGE_AFTER=$(go test -cover ./... | tail -1)
echo "覆蓋率: $COVERAGE_BEFORE → $COVERAGE_AFTER"
```

---

**備註**: 本框架基於 Alice 項目的實際經驗。隨著團隊成長，根據反饋持續迭代。
