# Subtask 拆分框架 — 應用指南

**目的**: 幫助團隊實踐系統化的 Subtask 拆分，提升 Agent 任務品質

---

## 📍 快速開始 (5 分鐘)

### 你是... Issue Triage 負責人？

1. 新 issue 進來 → 打開 [SUBTASK_DECISION_CARD.md](SUBTASK_DECISION_CARD.md)
2. 按決策樹判斷 (2 min)
3. 決定拆分策略
4. 建立 issue 或 Epic
5. 複製對應的 GitHub issue 模板，填驗收標準

### 你是... Agent？

1. 收到新 issue → 檢查「Acceptance Criteria」章節是否完整
2. 不完整？comment on issue，要求澄清
3. 完整？開始實現
4. 完成時，逐項勾選驗收標準 ✅
5. 自動生成 PR，包含驗收標準清單

### 你是... Code Reviewer？

1. 收到 PR → 檢查 PR body 有「Acceptance Criteria」章節
2. 逐項驗收
3. 無遺漏？approve & merge
4. 自動關閉相關 issue

---

## 🔧 完整工作流程

### Phase 1: Issue Triage (Owner / Lead, 5-10 min)

```
新 Issue 進來
  ↓
Step 1: 快速閱讀 (2 min)
  • 看標題和描述，判斷類型 (Bug / Feature / Refactor / i18n)
  • 估算工時 (1 line: ~2d, 多個檔案: ~5d+)
  ↓
Step 2: 使用決策卡判斷拆分 (2 min)
  • 打開 SUBTASK_DECISION_CARD.md
  • 根據「Issue 類型 → 決策樹」判斷
  • 如果 > 2d 或多層，標記為「拆分」
  ↓
Step 3: 建立 Issue/Epic
  • 拆分？→ 建立 Epic，創建子任務
  • 不拆？→ 直接建立單一 Issue
  • 使用 GitHub issue 模板自動填驗收標準
  ↓
Step 4: 標記優先度和依賴
  • 添加 label (priority / type)
  • 標註 blocking issue / depends on
  ↓
Step 5: 分派給 Agent
  • Assign
  • 注意：驗收標準必須完整！
```

**檢查清單 (Triage 完成時)**:
```
□ Issue 分類正確 (Bug/Feature/Refactor/i18n)
□ 估算工時在評論中
□ 拆分決策已說明 (為什麼拆/不拆)
□ 如有 Epic，子任務清單完整
□ 驗收標準已填入 (✅ 必須)
□ blocking issue 標註清楚
□ 優先度標籤已加
```

---

### Phase 2: Implementation (Agent, Variable)

```
收到 Issue
  ↓
Step 1: 檢查驗收標準 (1 min)
  □ 是否有「Acceptance Criteria」章節？
  □ 所有項目都勾了嗎？
  
  不完整？ → comment on issue
  "需要澄清：[具體問題]"
  
  完整？ → 繼續
  ↓
Step 2: 設置本地環境
  • git checkout -b feature/xxx
  • 準備測試環境
  ↓
Step 3: 實現功能
  • 邊做邊檢查驗收標準
  • 困難的地方 → 補充註釋說明為什麼
  ↓
Step 4: 自測與驗收檢查 (30 min - 1 hour)
  • 逐項驗收標準檢查
  □ 代碼品質 (no linting warning, no hardcoded secrets)
  □ 測試覆蓋 (新 test + 舊測試仍過)
  □ 文檔更新 (CLAUDE.md / docs/)
  □ 邊界情況測試 (空值、大值、權限)
  □ 國際化 (無硬編碼中文)
  ↓
Step 5: 建立 PR
  • 標題: type: 簡短描述 (e.g., "fix: hermes context loss in follow-ups")
  • Body: 複製 issue 的完整驗收標準
  • 標籤: 同 issue 標籤
  ↓
Step 6: 監控 Review
  • Reviewer 批評 → 改代碼 re-commit
  • 無遺漏？ → 自動 merge
```

**檢查清單 (PR 推送時)**:
```
□ 代碼無新 linting warning
□ 無硬編碼中文 (i18n type)
□ 所有新增文件有對應測試
□ PR body 有驗收標準清單
□ commit message 說明「為什麼改」
□ 性能無降級 (或有合理說明)
```

---

### Phase 3: Code Review (Reviewer, 5-20 min)

```
收到 PR
  ↓
Step 1: 快速檢查 (1 min)
  □ PR body 有驗收標準？
  □ 標籤和優先度合理？
  ↓
Step 2: 逐項驗收 (5-15 min)
  • 打開 PR body 的驗收清單
  • 跟著項目逐一檢查代碼
  
  遺漏？ → request changes
  "缺少 [項目]，例如 Y"
  
  完整？ → approve
  ↓
Step 3: 審查深度 (根據 Issue 類型)
  🔴 Bug       → 檢查是否真的修復 + 迴歸測試
  🟢 Feature   → 檢查功能完整性 + 邊界情況
  🟠 Refactor  → 檢查兼容性 + 無遺留舊實現
  🟡 i18n      → 檢查無遺漏文本 + 翻譯正確
  ↓
Step 4: Merge & Auto-Close
  • 批准後，PR 自動 merge（CI 通過時）
  • 自動關閉相關 issue
  • 如有 Epic，更新進度
```

**檢查清單 (Review 時)**:
```
□ PR body 有驗收標準清單
□ 代碼邏輯正確
□ 測試充分 (≥ 1 新 test + 舊測試仍過)
□ 文檔更新 (CLAUDE.md / 新概念的 docs/)
□ 無性能降級
□ i18n 正確 (無硬編碼、翻譯完整)
□ 可獨立 revert (無隱藏依賴)
```

---

## 📊 預期成效

### 短期 (1-2 週)

| 指標 | 改善前 | 改善後 | 備註 |
|------|--------|--------|------|
| 驗收標準完整性 | 60% | 95%+ | 減少 agent 問「現在可以 PR 嗎」 |
| PR review 往返 | 平均 2-3 輪 | 1-2 輪 | 清晰的驗收標準 → 更快的 review |
| 平均完成工時 | +20% (失誤重做) | baseline | 減少返工 |

### 中期 (1 個月)

| 指標 | 改善前 | 改善後 | 備註 |
|------|--------|--------|------|
| Subtask 完成率 | 70% | 95%+ | 明確的驗收 → 更可靠的完成 |
| Code review 時間 | 20-30 min | 10-15 min | 驗收項目減少審查工作 |
| Bug reopen 率 | 15% | <5% | 驗收標準更嚴格 |
| 團隊文檔化程度 | 低 | 高 | 知識轉移更快 |

### 長期 (3+ 月)

| 指標 | 改善前 | 改善後 | 備註 |
|------|--------|--------|------|
| 新 agent onboard 時間 | 1+ 週 | 2-3 天 | 框架明確化 |
| 重大 bug 發生率 | 目前數據不詳 | -30% | 系統化拆分 → 更少遺漏 |
| 跨 project 協作效率 | 低 | 高 | 拆分框架可複用 |

---

## 🚀 推行計劃

### Week 1: 立法 ✅

- [x] 完成 SUBTASK_FRAMEWORK.md
- [x] 完成 SUBTASK_DECISION_CARD.md
- [x] 建立 GitHub issue 模板
- [ ] Team 同步（30 min meeting）

### Week 2: 試用

- [ ] 所有新 issue 使用決策卡判斷拆分
- [ ] 所有 subtask 使用 GitHub 模板
- [ ] 收集反饋

### Week 3: 迭代

- [ ] 根據反饋調整框架
- [ ] 更新決策卡
- [ ] 建立自動化檢查 (可選)

### Week 4+: 標準化

- [ ] 框架成為預設流程
- [ ] 所有 issue 都遵循此流程
- [ ] 定期檢視成效，持續改進

---

## ❓ 常見問題

### Q1: 「決策樹太複雜了」

**A**: 實際上只需要問 3 個問題：
1. 工時 > 2-3 天？
2. 涉及多個層級 / 模組？
3. 其他人依賴中間成果？

如果都是 NO，保持單一。如果有一個 YES，考慮拆分。

### Q2: 「某些 issue 無法按決策樹判斷」

**A**: 那就在 issue 評論中說明理由。決策樹是指南，不是鐵律。

### Q3: 「如何驗證驗收標準是否足夠清晰？」

**A**: 簡單的方法：
- 給沒看過此 issue 的人讀驗收標準
- 他們能理解「完成」的確切條件嗎？
- 能 → 清晰；不能 → 需要改進

### Q4: 「驗收標準中的項目太多了，怎麼辦？」

**A**: 說明你的 subtask 拆分得太粗。分成更小的 subtask。

驗收標準通常：
- 🔴 Bug: 5-10 項
- 🟢 Feature: 8-15 項
- 🟠 Refactor: 8-12 項
- 🟡 i18n: 5-8 項

超過 20 項 → 需要重新拆分

### Q5: 「某個驗收項目無法在 PR review 時檢查（例如『性能基準線』）」

**A**: 那不應該在 PR 層面驗收，應該：
1. 在 issue 層面標註（issue 評論中記錄）
2. 或改為「可驗證的條件」（例如 "性能無降級 ≥5%"）

---

## 🔍 品質檢查清單

### Issue Triage 完成度檢查

```bash
# 執行此檢查，確保 triage 完整
for issue in $(gh issue list --state open --limit 10); do
  echo "Issue #$issue:"
  gh issue view $issue --json body | grep -q "## Acceptance Criteria" \
    && echo "  ✅ 驗收標準完整" \
    || echo "  ❌ 缺少驗收標準"
done
```

### PR Review 前檢查

```bash
# 檢查 PR 是否符合標準
gh pr view --json body | grep -q "## Acceptance Criteria" \
  && echo "✅ 驗收標準已複製" \
  || echo "❌ PR body 缺少驗收標準"
```

---

## 📚 相關文檔

| 文件 | 用途 |
|------|------|
| [SUBTASK_FRAMEWORK.md](SUBTASK_FRAMEWORK.md) | 詳細的分類系統、拆分規則、驗收標準模板 |
| [SUBTASK_DECISION_CARD.md](SUBTASK_DECISION_CARD.md) | 快速決策參考（Triage 時使用） |
| `.github/ISSUE_TEMPLATE/*.yml` | GitHub issue 自動模板 |
| `CLAUDE.md` | 主專案指南（連結到 Subtask Framework） |

---

## 📧 反饋渠道

- 框架有不清楚的地方？ → comment on GitHub issue
- 遇到反模式？ → 記錄並提出改進建議
- 發現決策卡漏掉的情況？ → 更新決策卡

---

**最後更新**: 2026-04-27 | **版本**: 1.0

**下一步**: Team sync → Week 2 試用 → Week 3 迭代
