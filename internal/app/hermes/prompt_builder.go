package hermes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Role identifies which Hermes participant a prompt is built for.
type Role int

const (
	RolePlanner  Role = iota
	RoleExecutor Role = iota
)

func (r Role) String() string {
	switch r {
	case RolePlanner:
		return "planner"
	case RoleExecutor:
		return "executor"
	default:
		return "unknown"
	}
}

// defaultPlannerRules is the embedded fallback when no rules file is found.
const defaultPlannerRules = `# Hermes Planner Rules

你正在以 Hermes 模式執行任務，角色為 **Planner**。

你的職責：將使用者的目標拆解為原子化子任務清單，並以**結構化輸出（structured output）**回傳。
硬規則：
1. 把子任務陣列放進 sub_tasks 欄位作為結構化輸出；不要寫「Plan emitted」之類的散文總結，那不是計畫。
2. 每個子任務必須有 id、description、tool_hints 三個欄位。
3. 最多 15 個子任務；每個子任務能獨立執行。
4. 不要輸出 fenced JSON 區塊或前言。
5. **SINGLE-ACTION RULE**：補 1 個測試、修 1 個 function、新增 1 個欄位、驗證 1 個已完成改動，都必須是 1 個 bundled sub-task，把讀 context、修改 / 驗證、跑測試包進同一個 description。
6. 禁止把單一自然動作拆成 Read / Edit / Bash / Run 三個子任務；那只會讓 Executor / Reviewer 重複冷啟動。
7. 禁止修改 config.json、.git/、.env、*.pem。`

const defaultCodexPlannerRules = `# Hermes Planner Rules (Codex)

你正在以 Hermes 模式執行任務，角色為 **Planner**，執行環境為 **Codex tier**。

你的職責：將使用者的目標拆解為原子化子任務清單，供 Executor 逐一執行。
硬規則：
1. 輸出 ONLY 一個 json fenced code block，內容必須是單一 JSON array。
2. 每個子任務必須有 id、description、tool_hints 三個欄位。
3. tool_hints 必須使用 Codex executor 可用能力，優先使用 command_execution；不要使用 Read、Edit、Glob、Grep、file_patch。
4. **SINGLE-ACTION RULE**：補 1 個測試、修 1 個 function、新增 1 個欄位、驗證 1 個已完成改動，都必須是 1 個 bundled sub-task，把讀 context、修改 / 驗證、跑測試包進同一個 description。
5. 禁止把單一自然動作拆成 Read / Write / Run 三個子任務；那只會讓 Executor / Reviewer 重複冷啟動。
6. 禁止修改 config.json、.git/、.env、*.pem。`

// defaultExecutorRules is the embedded fallback when no rules file is found.
const defaultExecutorRules = `# Hermes Executor Rules

你正在以 Hermes 模式執行任務，角色為 **Executor**。

## 職責
執行當前子任務，完成後以 ≤ 2 行摘要回報結果。

## 硬規則

1. **工具決策自主性** — tool_hints 是建議，不是強制。
   - 如果 tool_hints 中的工具不適合當前狀態，自由選擇其他工具。
   - 例：Sub-task 要求編輯檔案但指定 tool_hints: ["Read", "Bash"]，改用 Edit。

2. **工具錯誤處理** — 工具錯誤是事實，直接根據錯誤修正，不爭論。

3. **file_patch 規則** — old_text 必須在檔案中唯一存在（若不唯一則縮小或擴大比對範圍）。

4. **禁止修改路徑** — config.json、.git/、.env、*.pem（PathGuard 攔截）。

5. **完成摘要** — 輸出 ≤ 2 行結果摘要（包含：做了什麼、影響的檔案）。

6. **工作邊界** — 不執行當前子任務以外的工作。`

// PromptBuilder loads and returns role-specific Hermes operating rules.
// Rules are loaded from .md files when available; embedded defaults are used as fallback.
type PromptBuilder struct {
	plannerRules  string
	executorRules string
}

// DefaultPromptBuilder returns a PromptBuilder using the embedded default rules.
func DefaultPromptBuilder() *PromptBuilder {
	return &PromptBuilder{
		plannerRules:  defaultPlannerRules,
		executorRules: defaultExecutorRules,
	}
}

// LoadPromptBuilder loads rules from <dir>/planner_rules.md and <dir>/executor_rules.md.
// If a file is missing or unreadable, the embedded default for that role is used instead.
func LoadPromptBuilder(dir string) *PromptBuilder {
	return LoadPromptBuilderForTier(dir, "")
}

// LoadPromptBuilderForTier loads role rules from the prompts directory, optionally
// preferring tier-specific variants before falling back to the default filenames.
// For tier == "codex", it tries *_rules_codex.md first, then the default files.
func LoadPromptBuilderForTier(dir string, tier string) *PromptBuilder {
	pb := DefaultPromptBuilder()
	isCodexTier := strings.EqualFold(strings.TrimSpace(tier), "codex")
	if isCodexTier {
		pb.plannerRules = defaultCodexPlannerRules
	}

	if rules, err := loadRulesForRole(dir, "planner", tier); err == nil {
		pb.plannerRules = rules
	}
	if rules, err := loadRulesForRole(dir, "executor", tier); err == nil {
		pb.executorRules = rules
	}
	return pb
}

func loadRulesForRole(dir string, role string, tier string) (string, error) {
	for _, name := range promptRuleCandidates(role, tier) {
		if rules, err := readMD(filepath.Join(dir, name)); err == nil {
			return rules, nil
		}
	}
	return "", fmt.Errorf("no prompt rules found for role %q in %s", role, dir)
}

// promptRuleCandidates resolves the prompt-rule files for a given role and
// tier. The two tiers describe different runtime tool surfaces and are not
// interchangeable — Codex assumes shell-only command_execution, Claude
// assumes file_patch / Read / Edit / Glob / etc. The rules contradict each
// other on tool_hints, so we dispatch directly by tier with no fallback
// chain (a stale fallback could load Claude rules into a Codex executor and
// produce JSON tool_hints the Codex runner cannot honour).
func promptRuleCandidates(role string, tier string) []string {
	if strings.EqualFold(strings.TrimSpace(tier), "codex") {
		return []string{role + "_rules_codex.md"}
	}
	return []string{role + "_rules.md"}
}

func readMD(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// ForRole returns the operating rules text for the given role.
func (b *PromptBuilder) ForRole(role Role) string {
	switch role {
	case RolePlanner:
		return b.plannerRules
	case RoleExecutor:
		return b.executorRules
	default:
		return ""
	}
}
