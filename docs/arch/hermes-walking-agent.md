# Hermes Walking-Agent Architecture

Hermes Executor 改造設計：將「每個 sub-task 開新 CLI session」轉為「同角色內共用單一 Claude session」，省下 cold-start 與 cache_creation 重複付費。

本文件僅涵蓋這一個改造（issue #149 Phase A）。Hermes v2 整體設計與更大範圍架構（平行化、loop coordinator）屬範圍外，見文末。

## 動機

### 量測到的問題（issue #148）

過去 2 小時生產 Hermes 流量（commit `0b8b948` 之後）：

| Backend | Calls | Sessions | Cache hit | Cost |
|---|---|---|---|---|
| Claude | 57 | 51 | 89.9% | $17.76 |
| Codex | 16 | 16 | 91.4% | $8.59 |

關鍵觀察：**51 個 session / 57 個 call** — 幾乎每個 sub-task 都新 session。Anthropic 自動 prefix-cache 仍給 90% hit rate，但每個新 session 都要重付 cache_creation（1.25× 全價），且每次都付 CLI cold-start（process 啟動 + model warmup ~3-7s）。

### Spike 驗證（issue #149）

Standalone Python script `scripts/spike_walking_agent/spike.py` 跑 5 個 read-only sub-task：

| 模式 | Cost | Wallclock | Cache hit |
|---|---|---|---|
| Baseline (5 × `claude -p`) | $0.3956 | 106.1s | 72.3% |
| Walking (single SDK session, 5m cache) | **$0.2060** | **79.7s** | 93.3% |

**Walking 省 47.9% cost、24.9% wallclock**，cache_write 從 74,781 → 19,534（省 73.9%）。對重 sub-task（包含 Edit/Write）效益預期更大。

### 為何現有 session 清除是 load-bearing

[hermes_executor_runner.go:66-74](../../internal/app/hermes_executor_runner.go#L66-L74) 故意每個 sub-task 清 session。commit `0da6c05`（2026-04-30）為解 gladsheim #108 sub-task 9 的 "Prompt is too long" 而引入：codex 的 transcript 替每個 sub-task 重播完整歷史，加上 prompt body 重灌 accumulated → O(N²) 爆炸。

直接打開 session reuse 而不改 prompt template 會 regress 此 bug。本設計同時處理兩件事。

## 設計

### 4-session 邊界

```
┌─────────────────────────────────────────────────────────────────┐
│  Hermes Task (one user request)                                 │
│                                                                 │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐         │
│  │ Planner      │   │ Executor     │   │ HeavyExec    │         │
│  │ Session A    │   │ Session B    │   │ Session C    │         │
│  │ model=opus   │   │ model=sonnet │   │ model=opus   │         │
│  └──────────────┘   └──────────────┘   └──────────────┘         │
│      │                  │                  │                    │
│      │                  │ (N light tasks)  │ (M heavy tasks)    │
│      │                  ▼                  ▼                    │
│      │           ┌──────────────────────────────┐               │
│      │           │ Reviewer Session D           │               │
│      │           │ model=opposite backend       │               │
│      │           │ (Claude→Codex or Codex→Claude)│              │
│      │           └──────────────────────────────┘               │
│      │                                                          │
│      └─ replan: re-uses Session A across attempts               │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

每個 task 啟動最多 4 個 session（不是 N+2）。

### 為何不能合併到單一 session

- **Cache key 含 model fingerprint** — 不同 model 不能 resume 同一 session
- **Reviewer 跨 backend 設計** — 故意要獨立性（Claude executor → Codex reviewer 互查），不應共享 transcript
- **HeavyExecutor 切換** — 同 backend 但不同 model（sonnet→opus），仍切 session

### Session 生命週期

| Session | 建立時機 | 重用時機 | 清除時機 |
|---|---|---|---|
| Planner (A) | Task 啟動 | replan attempt 2/3 | Task 結束、超過 5m idle |
| Executor (B) | 第一個 light sub-task | 後續 light sub-task | Task 結束、context window 接近上限、轉 HeavyExec |
| HeavyExecutor (C) | 第一個 heavy sub-task | 後續 heavy sub-task | Task 結束、轉回 light |
| Reviewer (D) | 第一個 review | 後續 review（同 task 內）| Task 結束 |

### Prompt template 改造

避免 gladsheim 回歸的關鍵：**第一輪後不再重灌 accumulated**。Session transcript 自然帶歷史。

**Round 1 (cold)** — 發完整 context：
```
<executor_rules>            ← cached
Original goal:
<goal>                      ← cached
Current sub-task (1/N):
<subtask 1 description>
```

**Round 2..N (warm)** — 只發 delta：
```
Now do sub-task (k/N):
<subtask k description>
```

**Round k retry** — 同 round k 但加 reviewer feedback：
```
Reviewer feedback to address before retrying:
<feedback>

Re-attempt sub-task (k/N).
```

注意：`Original goal` 不重發。`accumulated` 完全不重發。Session transcript 會帶之前 N-1 輪的完整 user + assistant 訊息。

### Context window 防爆機制

Session transcript 會線性增長（每個 assistant 輸出 ~1-5K tokens × N rounds + 工具呼叫結果）。Sonnet 4.5 context window 200K，opus 200K。

**保護機制**：
1. 每個 sub-task 開始前計算當前 session 的累計 input tokens（從 last `ResultMessage.usage.cache_read_input_tokens` + `cache_write_input_tokens` 估算）
2. 當累計超過 **120K tokens**（60% threshold）時，**force-clear session**，下一個 sub-task 走 cold path
3. 同步寫一行 log: `[hermes] session approaching context limit, forcing fresh session at sub-task k/N`

這個機制確保 walking agent 在長 task 上不會比 baseline 差（最壞情況退化為現狀）。

### Cache TTL 策略

ClaudeSDKClient 預設 1h cache（2× rate）；CLI 預設 5m（1.25× rate）。Spike 顯示 5m 對短期 task 更划算。

**強制策略**：
- **若走 Python SDK 路線**：在 spawn ClaudeSDKClient subprocess 時設 `FORCE_PROMPT_CACHING_5M=1` 環境變數（Claude Code v2.1.108+ 支援，未公開文件，[claude-code#48082](https://github.com/anthropics/claude-code/issues/48082)）
- **若走 anthropic-sdk-go 直接打 API**：自管 `cache_control: {"type":"ephemeral", "ttl":"5m"}` block，並透過 `extended-cache-ttl-2025-04-11` beta header 啟用

兩種路線都應**只用 5m cache**。1h cache 適合 staying time > 5min 的場景，Hermes sub-task 多在幾秒內完成，不適用。

## Go 端整合

Alice 是 Go bot，Anthropic 沒釋出 Go 版的 Claude Agent SDK。整合 walking agent 有兩條路：

### Path 1: Python helper subprocess

Alice 起一個長壽 Python 服務（`scripts/walking_agent_helper.py`），透過 stdin/stdout JSON 收 prompt 與 dispatch session。Python 端用 `ClaudeSDKClient` 抓 session 與 cache 行為。

**優點**：
- 沿用 Anthropic 官方 SDK 的 session 管理、tool dispatch、retry
- Python 端可同時用其他 SDK 工具（hooks、subagents）
- 邏輯改動侷限在 Python 端，Go 端只需協定設計

**缺點**：
- 多一個服務（process 管理、健康檢查、重啟邏輯）
- Go ↔ Python protocol 需自行設計與維護
- 部署複雜度上升（需 Python venv 或 container）

### Path 2: 直接 Go 打 Anthropic Messages API

用 `github.com/anthropics/anthropic-sdk-go` 從 Go 端直接打 Messages API，自管 conversation history、`cache_control` block、tool use loop。

**優點**：
- 純 Go，無外部 process
- 完整控制 cache TTL（`cache_control: {"ttl": "5m"}` 顯式指定）
- 與 Alice 既有 Go 程式整合自然

**缺點**：
- Tool use loop 要自己寫（read tool result → next turn → check stop_reason）
- Session 管理（conversation 歷史儲存、reload）要自己寫
- Hooks / subagents / file checkpointing 等 SDK 加值功能拿不到

### 決策表

| 維度 | Path 1 (Python) | Path 2 (Go) |
|---|---|---|
| 部署複雜度 | 中 | **低** |
| Cache TTL 控制 | env var (隱式) | **明確 cache_control** |
| Tool use 邏輯 | **SDK 處理** | 自己寫 |
| 與 Alice Go 程式整合 | 需 IPC | **原生** |
| 維護成本 | 中 | 中 |
| 失敗模式調試 | 中（跨 process）| **低（單 process）** |
| Anthropic SDK 加值 | **可用** | 不可用 |

### 建議

**Path 2 (Go 直打 API)** 對 Alice 的場景更合適：
- Alice 已是 Go 程式，新增 Python 服務增加運維負擔
- Cache TTL 是這個改造的核心，明確控制比依賴 env var 風險低
- Tool use loop 不複雜（Bash / Read / Edit / Write，現有 Go 邏輯可移植）
- 失敗模式單 process 較易追

但這是**重大決策**，建議在開始實作前另開一個小 spike：用 `anthropics/anthropic-sdk-go` 寫個簡化版 walking agent（不接 Hermes），跑同一組 5 sub-task 對比 Python spike 的數字。若 Go 版能複現 47.9% cost + 24.9% latency 的結果，正式採用 Path 2；若有意外（例如 Go SDK 不支援某 tool 類型），fall back Path 1。

## 風險與緩解

| 風險 | 緩解 |
|---|---|
| **Context window 爆炸**（gladsheim #108 回歸）| 累計 input tokens > 120K 強制重啟 session |
| **品質下降**（沒有 accumulated 顯式提示，模型可能漏掉前序 sub-task 結論）| Round 2+ prompt 結尾加「Refer to your work in previous sub-tasks if relevant」一行；長 task 強制 force-clear 時順便重灌 accumulated |
| **Cost 反而上升**（cache TTL 配置錯誤、或某些 task 走 1h cache）| 上線後監控 `cache_write_1h` 欄位（#148 1A 已捕獲），有非 0 即告警 |
| **Operator UX 退步**（單一 session 內 turn 之間沒有「sub-task 邊界」明顯訊號）| Reporter 仍按 sub-task 發 OnSubTaskStart / Done 事件；Telegram 訊息結構不變 |
| **Reviewer 看不到完整 transcript**（reviewer 用 opposite backend 獨立 session）| 維持現狀：reviewer prompt 帶 sub-task description + executor 結果文字，跟現在一樣 |
| **Codex 行為差異**（Codex resume 在 #145 才剛修好；對 5m cache 的支援可能不同）| Phase 1 只啟用 Claude path；Codex path 等 Phase 2 |

## 分階段 rollout

### Phase 1: Instrumentation + opt-in flag（建議優先）

不改現有行為，只加：
- `HermesConfig.WalkingAgentEnabled bool` config 欄位（預設 false）
- 設為 true 時：
  - 同 task 的同 model sub-task 共用 Claude session（透過 Path 2 Go SDK 直打 API）
  - Prompt template 改用簡化 round 2+ 格式
  - 累計 input tokens > 120K 時 force-clear
- Log 加 `[hermes.walking] reused_session model=X tokens_so_far=Y` 用於監控
- 既有 [hermes_executor_runner.go ClearSessionForModel](../../internal/app/hermes_executor_runner.go#L71) 在 walking_enabled=true 時跳過

風險：低（opt-in，可隨時關掉）。
產出：可在生產跑 A/B 對比，驗證 spike 結論是否在真實 workload 重現。

### Phase 2: 預設啟用 Claude /hermes

待 Phase 1 在生產跑 1-2 週、無 regression 後：
- `WalkingAgentEnabled` 預設改為 true
- 加 `--no-walking-agent` 旁路 flag（保留 escape hatch）
- 移除 ClearSessionForModel 在這條路徑的呼叫

### Phase 3: 擴展到 Codex /ghermes

- 補 Codex 的 walking 支援（`codex exec resume` 在 #145 已修，session reuse 可用）
- Codex pricing 對 cache_creation 沒懲罰、對 cache_read 折扣較淺，預期收益較小但仍有

## 成功指標

Phase 1 跑滿 1 週後評估：
- ✅ Walking 模式平均每 task cost vs baseline ↓ ≥ 30%
- ✅ Walking 模式平均每 task wallclock ↓ ≥ 20%
- ✅ Context window 強制重啟事件 ≤ 5%（避免變相退化為 baseline）
- ✅ 無新增 review_score 顯著下降（品質沒退步）
- ✅ 無新增 "Prompt too long" 錯誤（gladsheim 回歸守住）

## 範圍外

明確不在這份設計討論的：

- **Hermes v2 完整重新設計**（B 範圍）— Planner --resume、reviewer in-session、FSM 進一步簡化等。需先讓 walking agent 落地驗證假設。
- **Sub-task 平行化**（C 範圍）— 獨立 sub-task 並行跑（受外部 24/7 多 agent 架構啟發）。屬未來探索。
- **Loop coordinator pattern**（C 範圍）— 用 cheap Claude session 跑 retry 觀察 loop。
- **跨 chat / 跨 user 的 session warm pool**（C 範圍）。
- **Cache hit % dashboard 可視化**（屬 #148 Phase 3）。

## 相關 issue

- #145: codex exec resume `-C` flag 不相容（已修，walking agent 在 codex 端的前置條件）
- #146: direct_model_switch bridge 仍可能注入語意 memory（並行進行中）
- #147: review timeout 30s → 120s（已修，本設計不影響 reviewer timing）
- #148: token / cost 報表落差修正（Phase 1A/1B/1D/1E 已修；本設計依賴 1A 的 cache token 解析）
- #149: 本設計的 spike + decision 主 issue
