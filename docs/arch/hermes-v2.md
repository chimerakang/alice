# Hermes v2 設計藍圖（Phase 2）

延伸 [hermes-walking-agent.md](hermes-walking-agent.md)（Phase A / Phase 1）。本文涵蓋 issue #149 scope tree 中的 **B**：在 walking-agent 落地後，把 Hermes 整體運作再壓縮一輪。屬於藍圖性質——具體實作會依 Phase 1 的真實量測決定優先序。

不在本文範圍：scope tree 中的 **C**（sub-task 平行化、loop coordinator、跨 chat session pool）。那是更遠期的探索。

## 動機

Phase 1 預期讓 Executor 共用 session、cache_creation 降到 ~14% 的水準。但 Hermes 內部仍有兩個明顯的「N 倍呼叫 multiplier」沒處理：

1. **Planner 每次 task / re-plan 都重新 cold-start**：[hermes/state.go:42-45](../../internal/app/hermes/state.go#L42-L45) 註解明寫「Planner CallPlan path is intentionally session-less」。每次 `task_retry` 觸發 re-plan，整個 planner_rules + goal + 上次 review feedback 都從零跑一遍。
2. **每個 Reviewer 呼叫都是新 session**：[telegram.go:3782](../../internal/app/telegram.go#L3782) 設計上 reviewer 走相反 backend 拿獨立性，但即便維持「相反 backend」的設計，同 backend 內的多次 review（同 task 的 N 個 sub-task strict review）仍然各自開新 session。

對 N=5 strict-mode task：
- Planner: 1 cold + 0~1 re-plan = 1-2 cold start
- Executor (walking on)：1 cold start，N 個 turn 共用
- Reviewer: N cold start（每 sub-task 一次）

reviewer 的 N 倍乘數是 Hermes v2 真正能再省的地方。

## Phase 2 子項目

### 2.1 Planner session reuse（`--resume` across re-plans）✅ implemented

**原問題**：[hermes/planner.go](../../internal/app/hermes/planner.go) `PlannerSession.Plan` 在 `task_retry` 觸發時被重新呼叫；雖然 PlannerSession 內部把 `sessionID` 存了起來，但 Alice 的 CallPlan path **沒有傳 `--resume`**，所以每次都是 fresh `claude -p`。

**已落地**：`CallPlan` 介面現在接受 `sessionID`，`PlannerSession.Plan` / `Compress` 會把目前 Planner session 往下傳，Claude CLI path 會加 `--resume <id>`，Codex path 也會用 `codex exec resume <id>` 嘗試接續。`task_retry` 與 Planner JSON retry 會保留同一 Planner session。

**設計**：

a. ✅ CallPlan 的 args 改成支援 sessionID（接受 `--resume <id>`，跟 CallStream 對稱）
b. ✅ `hermes_plan_bridge.go` `makePlanFn` 傳遞 `state.PlannerSessionID`
c. ✅ PlannerSession.Plan 在 retry/JSON 修復迴圈中用同一 session
d. `task_retry` 跨 attempt 時是否仍 resume？兩種選擇：
   - **保留**：節省 planner_rules 的重複付費，model 看到「上次的 plan + reviewer feedback + 改進指示」自然延伸思考
   - **不保留**：每個 attempt 新 session 確保 model 不被前一次失敗的思考束縛
   建議走「**保留**」，因為 task_retry 罕見發生，且 strict-mode reviewer 已給明確 feedback；節省的 cache_creation 是真的，不確定的「思考路徑污染」是理論的。

**預期收益**：對 task_retry 場景省 ~80% Planner 重複付費（rare path）。對主流程影響小，因為 plan 通常一次成功。

**風險 / 觀察點**：
- Planner 的 `--max-turns 3` 政策在 resume 路徑仍由 CLI args 帶入；需要用 production log 觀察是否有 `error_max_turns` 回歸。
- Codex planner resume 若 session 不可用會回 `ErrSessionUnavailable`，目前未做 bridge fallback；需要觀察實際發生率。

**前置條件**：Phase 1 walking-agent session 邊界與 token accounting 已修正；2.1 可先行落地。

### 2.2 Reviewer 共用 session 可行性研究

**問題**：N 個 sub-task → N 個 reviewer cold start。Reviewer 的 prompt 結構（[engine/review.go BuildReviewPrompt](../../internal/app/engine/review.go) 附近）也是大量穩定 prefix（review rules + goal + plan + 已完成 sub-task 結果）+ 變動的 current sub-task 結果。Cache hit 友善，但仍付 cache_creation N 次。

**設計選項**：

**選項 A：同 task 內 reviewer session 共用**
- 第一個 sub-task review：cold prompt（rules + goal + plan + sub-task 1 結果）
- 第二個起：slim（"Now review sub-task k: <result>"）
- 與 walking-agent 同套機制；reviewer-side `WalkingToggleable`
- **優點**：同 session 自然累積前序 sub-task 結果，reviewer 可做更整體的 cross-task 審視
- **缺點**：reviewer 看到前序成功 sub-task 後可能對後續更寬鬆（confirmation bias）

**選項 B：保留 reviewer-per-subtask 獨立 session 設計**
- 不動現有結構
- 但確認 5m cache prefix-match 在不同 session 間生效（spike 已證實）
- **優點**：保留評審獨立性，無 bias 風險
- **缺點**：N 倍 cache_creation 沒省

**建議**：A 跑一個 spike，量化 review verdict 的差異（同 task A 跑一次 vs B 跑一次，比較 verdict + score）。如果 A 的 review 沒有顯著放鬆（score 偏差 < 5），採 A；否則 B。

**預期收益（如採 A）**：reviewer-side cache_creation 省 ~70%。對 N=5 strict task 約再省 0.5-1 倍 walking 已省的金額。

**風險**：審視品質下降。Phase 2.2 的核心問題不是技術可行性（容易做），是「review 獨立性 vs 成本」這個哲學選擇。

### 2.3 FSM 簡化（review）

**現況**：commit `548f6e1`（4/30）已經做過一波壓平：
- 移除 `TaskStatusValidating`
- 凍結 `interrupt_policy` 為 "inject"
- 移除 `progress_verbosity` 動態行為

**剩下可審視的**：

a. `TaskStatusInterrupted` 與 `TaskStatusFailed` 的差異是否仍有價值？目前 interrupted 主要在 Telegram bot 收到 `/abort` 時轉換；若幾乎不影響後續邏輯（重試、儲存、報告）可考慮合併。
b. `SubTaskStatus.SubTaskInProgress` 與 `SubTaskPending` 的轉換是否在 walking-agent 模式下還有意義？walking 下每個 sub-task 是 session 內的一個 turn，沒有「啟動但尚未完成」的中間態能被外部觀察。
c. `SubTaskSkipped` 與 `SubTaskFailed` 對外語義差異是否仍清楚？檢視 reporter / dashboard 是否真的區分。

**設計**：審視 + 文件化現有 FSM 的每個狀態的「**唯一決定的下游行為**」。如果某狀態沒有獨特決定下游行為（即 `pending` vs `in_progress` 都觸發同樣後續），合併。

**預期收益**：程式碼簡化，**不是** 成本降低。屬「技術債清理」性質。

**風險**：低（FSM 簡化是純重構），但需要 review 一遍既有的 status-aware 邏輯（例如 reporter、dashboard、retry path），否則可能漏砍。

### 2.4 Cache hit % 監控

**現況**：(b) 已加 `/usage` 指令的 cache breakdown 與 `/api/health` 的 walking-agent 狀態。但**沒有歷史趨勢**——沒辦法看「過去 24 小時 cache hit rate 是 90% 還是 60%」。

**設計**：

a. `performance.go RecordAPICall` 已收 cache 欄位（#148 1A），但 dashboard 顯示層只看 `tokensUsed`。
b. 新增 dashboard panel：cache hit rate 時序圖（取 `cache_read / (cache_read + uncached + cache_write)`）
c. 新增告警：cache hit rate < 60% 持續 1 小時 → 寫一行 log `[performance] cache hit rate degraded ...`，觸發 operator 注意
d. 跨日報表：每日 cache token 總量 / cost 估算 / walking-agent on/off 比率

**預期收益**：可觀測性。讓未來架構決策有量化依據（不像這次需要靠對話一次次猜）。

**風險**：低，純加值。

### 2.5 Partial re-plan retry（保留已完成 sub-task）

**現況**：[plan_execute.go:312-314](../../internal/app/engine/plan_execute.go#L312-L314) `task_retry` 觸發 re-plan 時，`UpdateAccumulated(taskID, "")` 直接清空 accumulated，整個 plan 從 sub-task 1 重做。即便前面 5 個 sub-task 都 review 滿分，re-plan 也會把它們全部重來。

**症狀**：用戶 `/retry` 或追接 Hermes 訊息時，看到「重審不通過 → 自動 re-plan」訊息後，所有已完成的 sub-task 重跑一遍，浪費 token + wallclock。

**已修但仍部分**：commit `6c2ad92` 修了「review 被略過時誤判 fail」這個入口（empty verdict 不再觸發 retry）。但即便 review 真的回 fail，re-plan 還是會砍掉重練。

**設計選項**：

**選項 A：僅 re-plan 失敗範圍**
- review 結果包含 per-sub-task score / verdict（已有 `SubTaskResults`）
- engine 標記低分 sub-task 為 SubTaskFailed，高分 sub-task 保留為 SubTaskDone
- re-plan 只針對「failing 範圍 + 從 review feedback 推斷的補充項」生成新 sub-task
- accumulated 保留高分 sub-task 的 result
- **優點**：保留有用工作、token 大幅省下
- **缺點**：需要區分「sub-task 本身 fail」vs「整個 task design 不對」。前者該保留前序、後者該重做。

**選項 B：操作者選擇**
- task_retry 觸發時 pause + 詢問操作者「保留前序 / 全部重做」
- 跟 OnSubTaskFailurePause 同模式
- **優點**：最安全，操作者掌握決策
- **缺點**：增加 UX 步驟，干擾自動化流程

**選項 C：Plan diff retry**
- re-plan 後，對比新舊 plan
- 共用 sub-task（description 相同或語意接近）保留 result
- 新 sub-task 從 SubTaskPending 開始
- **優點**：自動化、不需用戶介入
- **缺點**：判斷「sub-task 等價」很難（同 description 可能在不同 plan context 下要做不同事）

**建議**：選項 A，但需要設計 review feedback → 哪些 sub-task 算 fail 的對應規則。比如：
- review.IssueTags 提到的 sub-task ID（或編號）→ fail
- SubTaskResults[i].Score < threshold → fail
- 其他保留為 done

預期收益：對 task_retry 場景省 50-80%（看 fail 範圍），且**user 投訴最大的「白白浪費 token」直接消失**。

**前置條件**：Phase 2.1（Planner --resume）已落地。Phase 2.5（partial-retry）跟 walking-agent 完全正交。

### 2.6 跨 Run 狀態繼承（最高優先；最大紅利）

**症狀**：Issue #305（asgard 加 2 個欄位 + migration + 1 個 e2e 測試）跑了 3-4 次 Hermes，累積 ~6M tokens。每次跑的 Planner / Executor 都不知道上次的進度，整個 issue 從「讀檔案理解結構」開始重做：

| Run | 時長 | sub-task | 用量 | 實際做了什麼 |
|---|---|---|---|---|
| 1 | 29 min | 8/10 | (撞 Max 配額) | 加欄位 + proto + migration（核心工作）|
| 2 | 7 min | 4/4 | 3.4M | 補 e2e 測試（**要先讀 3 個檔案再寫**）|
| 3 | 5 min | 4/4 | 2.7M | 驗證 e2e 測試存在 + 跑測試（**已經測過了**）|
| 4 | 進行中 | 4 | ? | 又一次驗證 |

Run 2 / 3 / 4 的多數 sub-task 都是「Read X to understand Y」「Run go test to verify Z」——上次跑完的東西這次再讀一次、再跑一次。

**根因**：Hermes 啟動時 Planner 只看到 issue body + repo 路徑，沒有：
- 上次 Run 留下的工作記錄（Run 1 完成了什麼）
- 當前 git diff（要做的事是否已 commit）
- 當前 build / test 狀態（測試是否已綠）
- Issue body 勾選表狀態（哪幾項已勾）

於是每次都規劃完整 4-10 個 sub-task「從零驗證」。

**設計選項**：

**選項 A：Pre-flight check（Haiku 快查）**
- Hermes 啟動前先跑一個輕量 Haiku 呼叫
- 餵 issue body + git diff + git log -10 + build/test 狀態
- 讓 Haiku 評估「issue 完成度 0-100%」
- 完成度 > 80%：直接結束，回報「issue 似乎已完成，請人工確認」
- 完成度 < 80%：把 Haiku 的「已完成項 / 待做項」分析塞進 Planner 的 goal context，讓 Planner 規劃時跳過已完成部分

預期效果：你的案例裡 Run 2/3/4 都會被 Pre-flight 直接 skip，省 8M+ tokens。

**Phase 2.6.A — 並行 preflight guardrail (✅ implemented)**

Sequential preflight 會把 Haiku 的 10–30s 全部疊在 user-perceived 啟動延遲上。借用 OpenAI Agents SDK `run_in_parallel=True` 的 input-guardrail 模式：

- preflight goroutine 與 Planner 同時起跑（Planner 拿原始 goal）。
- 若 preflight 在 Planner 完成前 tripwire（`shouldSkip` 為真）→ 透過 `interruptibleCoordinator.InterruptWith(0)` 中斷正在跑的 task，把 Haiku 寫的「已完成項」訊息回送 chat。
- 若 Planner 先完成 → preflight 結果僅作 telemetry，goal augmentation 不再注入（這是相對 sequential 模式的 trade-off）。
- 若 preflight error → 紀錄 log，Planner 不受影響繼續跑。

開關：`hermes.preflight.parallel`（default `false`，保留舊有 sequential 行為）。Code path 在 [internal/app/hermes_preflight.go](../../internal/app/hermes_preflight.go) (`runHermesIssuePreflightAsync` + `handleParallelPreflightVerdict`) 與 [internal/app/telegram.go](../../internal/app/telegram.go) `startHermesFromIssueMode`。

**選項 B：Issue 勾選表自動回讀**
- Issue body 含 `- [x]` / `- [ ]` 列表時，Planner 規則明確指示「勾選的不重做」
- 規劃時排除已勾選項目
- 完成新項後**自動回 issue 勾選**（Github Integration 已有部分能力）

預期效果：對於有勾選表的 issue（asgard 大量採用此格式），規劃精度大幅提升。

**選項 C：Repo state snapshot**
- Hermes 啟動時自動跑：
  - `git status --porcelain`（未 commit 的改動）
  - `git diff --stat HEAD~5`（最近 5 個 commit 的變動範圍）
  - `git log --oneline -10`（recent commits — 看是否包含本 issue 修法）
  - `go build ./... 2>&1 | tail -5` / `make test 2>&1 | tail` 狀態（如有）
- 把這些塞進 Planner 的 goal pre-amble
- Planner 規劃時參考實際 repo 狀態，避免規劃「已經做過」的步驟

**建議**：A + B + C 同時做，協同。最便宜且立刻見效是 **B（issue 勾選表）**——只要動 Planner 的 prompt 加幾句話。然後是 **A（pre-flight Haiku）** 因為 Haiku 一次呼叫成本 < $0.01，能擋掉整個誤觸發 run。**C 最徹底但最大改動**。

**預期收益**：Issue #305 案例如果 Run 2/3/4 都被 Pre-flight 攔下，省 ~8M tokens（占當天 Claude 用量的相當比例）。對「user 重複 /retry / 追接 hermes」這個用戶常見模式，這條路徑收益最大。

**風險**：
- A 可能 false positive 把真正需要做的工作也 skip 掉 → 需要保留 `--force` 旁路
- B 依賴 issue body 格式工整 → 對沒勾選表的 issue 沒幫助
- C 要做 git/build 探測，本身有開銷（雖然遠小於整個 Hermes run）

### 2.7 Sub-task granularity threshold（高優先；簡單）

**症狀**：同 #305 案例，Planner 對「補一個 e2e 測試」這種人類眼中**單一動作**的工作，產出 4 個 sub-task：
1. Read 測試檔看 pattern
2. Read source 看 function signature
3. Write 測試
4. Run 測試

每個 sub-task 自己一個 Executor + Reviewer 全套（Reviewer 跨 backend），4 個 sub-task = 4× Reviewer cold start + 4× Executor cold/walking start。

**設計**：在 PlannerSession 加一個顯式規則：「**對於『新增 1 個檔案 / 補 1 個測試 / 改 1 個 function』這類單一動作，回 1 個 sub-task** with description 含『先讀必要 context、寫、跑驗證』」。Planner_rules.md 加一條：

```
SINGLE-ACTION RULE:
If the user goal is "add 1 test", "fix 1 function", "rename 1 symbol",
"add 1 field" — return ONE sub-task with description that bundles all
the natural steps (read → modify → verify). Do not split read/write/
verify into separate sub-tasks; the Executor's tool chain handles
that internally within one call.

Anti-pattern (over-decomposition):
  s1: Read foo.go to understand
  s2: Add field X to foo.go
  s3: Run go build to verify
This wastes Reviewer cold-start × 3.

Correct (bundled):
  s1: Add field X to foo.go (read context first, then edit, then
       run go build to verify; finalize when build green)
```

**預期效果**：Run 2 / Run 3 的 4 sub-task 應該被壓成 1 個。Reviewer 開銷從 4× 降到 1×。

**風險**：低，純 Planner prompt 調整。可以 A/B 測（先在 .md 加註，看實際 Plan 是否變少）。

### 2.8 預設啟用 Walking Agent（Hermes Phase 2 rollout）

按 [hermes-walking-agent.md](hermes-walking-agent.md) 的 rollout 章節：

1. Phase 1 在生產跑 1-2 週、無 regression
2. `WalkingAgentEnabled` 預設改 true（保留 `/no-walking` 旁路 flag 給 escape hatch）
3. 觀察 1 週
4. 移除 `WalkingAgentEnabled` config 欄位 + 移除 legacy ClearSessionForModel 路徑

時間點取決於 Phase 1 真實表現。沒有量到就不推進。

## 跨子項目交互

```
walking-agent (Phase 1)
    │
    ├─► 2.1 Planner --resume      ✅ implemented
    │
    ├─► 2.2 Reviewer in-session study
    │       ├── if score不退步 ─► implement (medium impact)
    │       └── if 退步 ─► keep current (no-op)
    │
    ├─► 2.3 FSM 簡化              (no cost impact, debt cleanup; do anytime)
    │
    ├─► 2.4 Cache hit monitoring  (foundational; should precede 2.8 default-on)
    │
    ├─► 2.5 Partial re-plan retry (#149 6c2ad92 partial done; full design pending)
    │
    ├─► 2.6 跨 Run 狀態繼承        (HIGHEST PRIORITY — biggest realised waste)
    │       ├─► 2.6.A Pre-flight Haiku check
    │       ├─► 2.6.B Issue 勾選表回讀
    │       └─► 2.6.C Repo state snapshot
    │
    ├─► 2.7 Sub-task granularity  (Planner prompt tweak; cheap)
    │
    └─► 2.8 Default-on rollout    (gated on 2.4 + 1-2 week soak)
```

依賴關係：
- **2.4 應該在 2.8 之前做**——不然 default-on 之後出問題沒儀表板看
- **2.6 / 2.7 互相獨立**，且都跟 walking-agent / 2.1 / 2.2 正交，可平行排程
- 2.6 / 2.7 的收益**比 walking-agent 本身可能還大**，因為它們處理的是「issue 重複跑」這個結構性浪費，而非單次 run 內部的優化

## 決策點（Phase 1 真實數據驅動）

下列決策**等到** Phase 1 在生產跑滿 1-2 週後再敲：

1. **Walking-agent 預期省 30% / 實際省 X%**：
   - X >= 30%：按計畫推 2.5 default-on
   - 15% <= X < 30%：保留 opt-in，不 default
   - X < 15%：考慮回滾，重新評估架構

2. **Sub-task平均長度（N）**：
   - 大部分 task N <= 3：reviewer per-subtask 開銷低，2.2 性價比差，不做
   - 大部分 task N >= 5：2.2 收益顯著，優先做

3. **Watermark 觸發頻率**：
   - 多數 task 不觸發：120K 設定合理
   - 經常觸發：要麼上調，要麼設計 fallback path（自動把長 task 拆成多個 walking session）

4. **Codex 路徑表現**：
   - Codex /ghermes 用戶比例高且也有相同收益：把 walking 擴展到 Codex（codex resume 在 #145 已修復，可用）
   - Codex 用戶少：先不做 Codex，集中資源在 Claude path 優化

## 範圍外（屬 C 的東西）

明確不在 Hermes v2 討論：

- Sub-task 平行化（受外部 24/7 多 agent 架構啟發；sub-task 之間若無依賴可並行）
- Loop coordinator pattern（cheap Claude session 跑 retry/觀察 loop）
- 跨 chat / 跨 task 的 session warm pool
- 跨 backend smart routing（依 cache 命中歷史動態選 Claude / Codex）
- Hermes pre-flight: 用 Haiku 快速估算 task 大小決定要不要走 Hermes

這些是更大的架構討論，留待 Hermes v2 真實落地後再評估。

## 相關 issue

- #145: codex exec resume `-C` 不相容（已修，2.1 / Codex 擴展前置條件）
- #146: direct_model_switch bridge 仍可能注入語意 memory（並行進行中）
- #147: review timeout 30s → 120s（已修）
- #148: token / cost 報表落差修正（Phase 1A/1B/1D/1E 已修；2.4 監控依賴）
- #149: walking-agent Phase 1 主 issue + B/C scope tree
- 後續若 Phase 2.1 / 2.2 / 2.3 / 2.4 各自開 issue，會在這裡補連結
