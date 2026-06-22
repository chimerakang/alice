# Alice Subtask 拆分決策卡

**快速參考** | 用於新 Issue Triage 時判斷是否拆分

---

## 🔍 第一步：識別 Issue 類型

```
Issue 進來

看標題和描述，判斷：

  □ 有用戶被完全阻擋的 bug？        → 🔴 BUG (Critical)
  □ 有邏輯 bug 但有 workaround？    → 🔴 BUG (Normal)
  □ 要新增功能 / API / 命令？        → 🟢 FEATURE
  □ 要改架構 / 介面？               → 🟠 REFACTOR
  □ 有硬編碼中文訊息？              → 🟡 i18n
  □ 純文檔 / 測試 / 依賴？          → ⚪ DOCS/CHORE

  ↓ 然後根據下方決策樹判斷拆分
```

---

## 🔴 BUG (Critical / Normal)

### 決策樹

```
Bug
  │
  ├─ 是否可一次修完？(≤ 1 天工時)
  │  └─ YES → 保持單一 issue
  │  └─ NO  → 按層級拆分
  │
  ├─ 涉及幾層？
  │  ├─ 1 層 (邏輯 or DB or UI)
  │  │  └─ 保持單一 issue
  │  │
  │  ├─ 2 層 (邏輯 + UI / DB + 邏輯)
  │  │  └─ 按層拆成 2 subtask
  │  │
  │  └─ 3+ 層 (DB + 邏輯 + UI + API)
  │     └─ 按層拆成 3-4 subtask（並序）
  │
  └─ 需要 migration 或回補數據？
     └─ YES → 加 +1 subtask
```

### 例子對照表

| Bug 描述 | 涉及層 | 工時 | 拆分 | 判斷 |
|---------|--------|------|------|------|
| 修正計算公式錯誤 | 邏輯層 | 0.5d | ✅ 單一 | 直接改 |
| 訊息顯示位置錯誤 | UI 層 | 0.3d | ✅ 單一 | 直接改 |
| DB 缺欄位 + Query 改 | DB + 邏輯 | 1d | ✅ 單一 | 可一次完成 |
| follow-up context 丟失 | 邏輯 + 存儲 + UI | 3d | ❌ 拆 3 個 | #A: Planner → #B: Storage → #C: UI |
| 數據損壞 + 回補 | DB + 驗證 + 修復 | 2d | ❌ 拆 2 個 | #A: 修復 + migration → #B: 數據驗證 + 回補 |

---

## 🟢 FEATURE

### 決策樹

```
Feature
  │
  ├─ 是新模組還是擴展現有功能？
  │  ├─ 新模組 (e.g. Multimedia)
  │  │  └─ 拆 4-5 層：DB → Logic → API → UI → Test
  │  │
  │  └─ 擴展現有 (e.g. 新欄位 / 新選項)
  │     └─ 拆 2-3 層：DB → Logic → UI
  │
  ├─ 工時評估？
  │  ├─ < 2 天 → 保持單一 issue (新指令)
  │  ├─ 2-5 天 → 拆成 2-3 subtask
  │  └─ > 5 天 → 拆成 4+ subtask，標註 Epic
  │
  └─ 涉及國際化嗎？
     └─ YES → +1 subtask for i18n
```

### 例子對照表

| 功能 | 層級 | 工時 | 拆分 | 例子 |
|------|------|------|------|------|
| 新指令 (e.g. /help) | 邏輯 + I18n | 1.5d | ✅ 單一 | 定義命令 + 幫助文本 + test |
| 新欄位 (e.g. budget) | DB + Query + UI | 2d | ✅ 單一 | migration + 更新查詢 + 展示 |
| 新模組 (Multimedia) | DB + Logic + API + UI | 5d | ❌ 拆 4 | #A: schema → #B: parser logic → #C: API → #D: UI |
| API 新端點 + Dashboard | API + UI + test | 3d | ❌ 拆 2 | #A: API 端點 → #B: Dashboard 面板 |

---

## 🟠 REFACTOR

### 決策樹

```
Refactor
  │
  ├─ 改動類型？
  │  ├─ Interface 抽取 / 改簽名
  │  │  └─ 拆成 N 個實作 + 1 個測試 subtask
  │  │     (define → impl A → impl B → ... → test)
  │  │
  │  ├─ 去重複 (consolidate)
  │  │  └─ 有無隱藏破壞性改動？
  │  │     ├─ YES → 拆成 2 subtask (提取 → 更新呼叫端)
  │  │     └─ NO  → 保持單一
  │  │
  │  └─ Migration (舊 → 新)
  │     └─ 拆 3-4 層：dual-write 準備 → migrate → switch → cleanup
  │
  ├─ 可否完全相容？
  │  ├─ YES → 可在現有測試基礎上改
  │  └─ NO  → 需要 deprecation warning + 迴歸測試
  │
  └─ 其他 issue 依賴此改動？
     └─ YES → 優先完成此重構，標註 blocker
```

### 例子對照表

| 重構 | 改動 | 工時 | 拆分 | 例子 |
|------|------|------|------|------|
| 變數改名 (cached → legacy) | 邏輯 | 0.5d | ✅ 單一 | 全檔替換 + test |
| 提取 interface (ExecutionEngine) | 架構 | 3d | ❌ 拆 3 | #A: define interface → #B: DirectEngine impl → #C: test |
| 合併 2 個函數 | 去重複 | 1d | ✅ 單一 | 提取 + 更新呼叫 + test |
| 舊 schema → 新 schema | Migration | 2d | ❌ 拆 2 | #A: migration + dual-write → #B: 數據遷移 + switch + cleanup |

---

## 🟡 i18n (硬編碼文本)

### 決策樹

```
i18n
  │
  ├─ 文本數量？
  │  ├─ 1-5 個   → 合併到相關功能 issue
  │  ├─ 5-15 個  → 1 subtask (同一模組)
  │  ├─ 15-30 個 → 2 subtask (拆成 2 模組)
  │  └─ > 30 個  → 3+ subtask
  │
  ├─ 來自同一模組？
  │  ├─ YES → 1 subtask (Image Analysis / File Handling / etc)
  │  └─ NO  → 按模組分 subtask
  │
  └─ 需要翻譯幾種語言？
     └─ 都翻 (繁體/簡體/英文) → 1 個 subtask 完成
```

### 例子對照表

| 模組 | 文本數 | 工時 | 拆分 | 例子 |
|------|--------|------|------|------|
| Image Analysis | 18 | 1.5d | ✅ 1 subtask | 18 個 keys + 3 語言翻譯 + test |
| File Handling | 10 | 1d | ✅ 1 subtask | 10 個 keys + 3 語言翻譯 + test |
| Multi-Agent Status | 9 | 1d | ✅ 1 subtask | 9 個 keys + 3 語言翻譯 + test |
| Checkpoint System | 11 | 1.5d | ❌ 拆 2 | #A: 核心信息 (6 keys) → #B: 狀態提示 (5 keys) |

---

## 🎯 實用檢查清單

### 決定拆分時問自己

```
□ 工時估計 > 2-3 天？
□ 涉及多個文件 / 層級 / 模組？
□ 其他 issue 依賴中間成果嗎？
□ 是否存在「無隱藏陷阱的切割點」？
□ 拆分後每個 subtask 都能獨立驗收嗎？

全勾 → 應該拆分
0-1 個勾 → 保持單一 issue
```

### Issue Triage 流程 (5 分鐘)

```
1. 讀 issue 標題 + 描述 (1 min)
   ↓
2. 對照決策樹判斷 (2 min)
   ├─ Bug? Feature? Refactor? i18n?
   ├─ 工時估計
   └─ 拆分？
   ↓
3. 建立 Epic (如需) 或填驗收標準 (2 min)
   ├─ 複製對應模板
   ├─ 標註 blocking / depends-on
   └─ Ready for agent
```

---

## 常見決策表

### 「應該拆分嗎？」決策表

| 問題 | 答案 | 拆分 |
|------|------|------|
| 工時 < 1 天？ | YES | 保持單一 |
| 涉及 ≥ 3 層？ | YES | 按層拆分 |
| 其他 issue 依賴中間成果？ | YES | 拆分優先順序，標註 blocking |
| 文本 < 10 個？ | YES | 合併到相關功能 issue |
| 可獨立驗收嗎？ | NO | 合併到上一層級 |
| 需要 migration？ | YES | +1 subtask for migration |

### 工時快速估算

| 任務類型 | 典型工時 | 是否應拆 |
|---------|---------|----------|
| 修正 typo / 小 bug | 0.3d | 直接做 |
| 新增單一欄位 + migration | 1d | 直接做 |
| 新增命令 + help text | 1.5d | 直接做 |
| 新增 API 端點 | 1-2d | 單一或拆 2 (API + test) |
| 新增完整功能 (包 UI) | 3-5d | 拆 3-4 subtask |
| 大型重構 | 5+ d | 拆 4+ subtask，標 Epic |

---

## 模板快速查詢

### 根據 Issue 類型選擇模板

```
🔴 Bug Fix Subtask
   → .github/ISSUE_TEMPLATE/subtask-bug.yml

🟢 Feature Subtask
   → .github/ISSUE_TEMPLATE/subtask-feature.yml

🟡 i18n Hardcoded Text
   → .github/ISSUE_TEMPLATE/subtask-i18n.yml

🟠 Refactor / Architecture
   → 複製 Feature 模板，修改「功能需求」為「結構改進」

⚪ Docs / Chore
   → 複製 Feature 模板，簡化驗收標準
```

---

## 例外情況

### 何時不拆分（即使 > 2 天）

```
✅ 代碼高度耦合，無法獨立驗收
✅ 拆分反而增加複雜性（例如重複測試）
✅ 中間成果無意義（必須完整才能驗收）
✅ 團隊只有 1 人，拆分無意義
```

### 何時強制拆分（即使 < 2 天）

```
🚩 涉及 critical bug，需要快速 review + merge
🚩 多人並行開發，需要切割點
🚩 存在隱藏陷阱，需要 review 指引
🚩 是 Epic 的必要基礎，需要優先完成
```

---

**備註**: 本決策卡基於過去 20+ 個 issue 的經驗。根據實際情況靈活應用，而非機械遵循。
