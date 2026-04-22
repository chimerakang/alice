# Hermes Prompt System

本文件說明 Hermes 模式的 prompt 組裝架構與規則檔案維護指南。

## Prompt 組裝順序

```
[Claude Code 自動注入]  CLAUDE.md → 自動匹配的 skills（如 alice-i18n）
                        ↓
[Hermes 核心規則]       planner_rules.md 或 executor_rules.md（按角色選擇）
                        ↓
[內建格式規範]          plannerSystemPrompt（僅 Planner）
                        ↓
[動態任務上下文]         Goal / Accumulated / Current SubTask（僅 Executor）
                        ↓
[User input]            使用者目標（Planner）或 執行指令（Executor）
```

## 規則檔案位置

```
internal/app/hermes/prompts/
  planner_rules.md   — Planner 角色的 Hermes 核心規則
  executor_rules.md  — Executor 角色的 Hermes 核心規則
```

檔案缺失時，`PromptBuilder` 自動降級為嵌入式預設規則（定義於 `prompt_builder.go`）。

## 設定 PromptsDir

`config.json` 可指定規則目錄路徑（相對於執行目錄）：

```json
{
  "hermes": {
    "enabled": true,
    "prompts_dir": "internal/app/hermes/prompts"
  }
}
```

預設值：`"internal/app/hermes/prompts"`

## PromptBuilder API

```go
// 使用嵌入式預設規則
pb := hermes.DefaultPromptBuilder()

// 從目錄載入規則（缺失的檔案降級為預設）
pb := hermes.LoadPromptBuilder("/path/to/prompts")

// 取得特定角色的規則文字
plannerRules := pb.ForRole(hermes.RolePlanner)
executorRules := pb.ForRole(hermes.RoleExecutor)
```

## 規則撰寫指南

### Planner 規則（planner_rules.md）

Planner 規則的核心關注點：

1. **輸出格式**：強調 JSON 陣列格式（```json 區塊）
2. **子任務品質**：描述具體、工具 hints 精確
3. **禁止事項**：保護敏感檔案（`config.json`、`.env` 等）
4. **失敗處理**：JSON 格式失敗時的修正方式

Planner 規則會與內建的 `plannerSystemPrompt`（含完整 JSON schema 範例）**合併**，
前者在前、後者在後，兩者互補。

### Executor 規則（executor_rules.md）

Executor 規則的核心關注點：

1. **錯誤處理**：工具錯誤是事實，直接修正
2. **唯一匹配**：`file_patch` 的 `old_text` 必須唯一
3. **範圍限制**：只執行當前子任務
4. **結果摘要**：≤ 2 行的完成摘要
5. **Skills 自動載入**：確保子任務描述包含足夠的關鍵字

### 不應放入規則的內容

- **動態資訊**（Goal、Accumulated、SubTask）→ 由 `BuildExecutorPrompt` 注入
- **工具知識**（如 i18n 語法）→ 寫成 Claude Code skill
- **專案架構**（如檔案路徑）→ 放在 CLAUDE.md 分層

## 新增任務型知識的決策樹

```
需要 Hermes 知道某類任務的處理方式？
├── 「每次遇到這類子任務都需要」→ 寫成 skill（description 精準匹配）
├── 「全局安全/格式規範」→ 加入 executor_rules.md 的硬規則
└── 「Planner 拆解策略」→ 加入 planner_rules.md
```

## 未來擴充

若 skill 自動匹配準確度不足（量測到漏匹配率高），可升級為強制 profile 系統：
在 `PromptBuilder` 加入 `ForTask(taskKeywords []string) string`，
根據子任務關鍵字強制附加特定規則段落。

目前以量測到準確率足夠為前提，不預先實作。
