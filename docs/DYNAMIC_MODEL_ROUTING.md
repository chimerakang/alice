# Dynamic Model Routing Implementation

## Overview

Alice implements an intelligent model routing system to optimize token costs while maintaining quality by routing different types of tasks to appropriate Claude models:

- **Haiku**: Fast & cheap (~7-10x cheaper than Opus) - simple tasks
- **Sonnet**: Balanced default - general purpose
- **Opus**: Powerful & capable (10x more expensive) - complex tasks

## Three-Tier Routing Priority

The system uses sticky sessions by default. Once a Claude/Codex session is active, Alice keeps using the current model for follow-up turns so the CLI can preserve tool history and file-change context. Auto-triage only runs for a new conversation, after the idle timeout, or after the user explicitly resets/switches context.

The Telegram routing priority is:

### 1. User Command Override (Priority 1 - 0ms latency)

Explicit user commands take highest priority:

```
/fast  → Forces Haiku model (fast, cheap)
/smart → Forces Sonnet model (balanced)
/deep  → Forces Opus model (deep, powerful)
/clear → Clears the active session/context
/auto  → Returns to automatic routing
```

**Implementation**: Telegram handler detects commands and calls `agent.SetModelOverride(model)`

**Behavior**:
- Setting a new model clears the target backend session and recent bridge context unless the same active model is re-selected
- `/clear` clears the current session, sticky model, plan mode, and recent bridge context while preserving usage stats
- Command preference persists until user sends `/auto`

### 2. Sticky Session / Follow-up Detection (Priority 2 - 0ms latency)

When `sticky_session` is enabled and the current session has not exceeded `session_idle_timeout_min`, Alice keeps the current model and skips triage. If there is no active sticky session, short follow-up messages and messages starting with continuation words or pronouns also inherit the current model.

### 3. Hybrid Triage (Priority 3)

For new topics, Telegram first uses a local complexity heuristic. Ambiguous messages can be classified by the configured lightweight triage model:

```json
{
  "model_routing": {
    "enable_dynamic_routing": true,
    "sticky_session": true,
    "session_idle_timeout_min": 5,
    "use_gpt4o_mini_for_triage": true
  }
}
```

**Classification**:
- **Fast tasks** (→ Haiku): Translation, explanation, formatting, simple code viewing
- **Deep tasks** (→ Opus): Refactoring, architecture, debugging, complex algorithms

**Implementation**: `evaluateTaskComplexityScore()` and `triageWithHaiku()` in telegram.go
- Uses existing OpenAI API key (shared with voice transcription)
- Minimal cost: max_tokens=10, temperature=0
- Graceful fallback to "fast" if API key not configured

### 4. Static Rules-Based Routing (Agent Fallback - <5ms latency)

Keyword pattern matching provides fast, deterministic routing:

**Haiku Rules** (Priority 1-3):
- `translat*` → Translation services
- `summariz*` → Summarization, synopsis
- `explain*` → Explaining concepts
- `format|json|csv|xml` → Data format conversion
- `read|view|show|list` → File viewing, status checks
- `status` → Status queries
- `polish` → Minor code improvements

**Opus Rules** (Priority 20-24):
- `refactor*` → Code refactoring
- `architecture` → System design
- `multiple files` → Multi-file changes
- `debug|troubleshoot` → Debugging
- `algorithm|logic` → Algorithm design
- `optim*|performance` → Performance tuning

**Priority**: Lower number = higher precedence (1 > 20)

**Fallback**: Default to Sonnet if no rules match

## Implementation Architecture

### File Structure

**`internal/app/agent.go`**:
```go
func (a *Agent) selectModel(userMessage string) (model string, routingReason string)
```
- Analyzes user message using static rules
- Returns selected model ("haiku", "sonnet", "opus")
- Returns routing reason ("static_rule", "default")

**`internal/app/security.go`**:
```go
func GetDefaultModelRoutes() []ModelRoute
```
- Defines routing rules with patterns and priorities
- Returns list of ModelRoute structs

**`internal/app/config_types.go`**:
```go
type ModelRoutingConfig struct {
    StickySession         bool
    SessionIdleTimeoutMin int
}
```
- Controls sticky mode and the idle timeout for treating a message as a new topic

**`internal/app/telegram.go`**:
- Handles `/fast`, `/smart`, `/deep`, `/clear`, `/auto` commands
- Manages user model preferences with RWMutex for thread safety
- Applies sticky/follow-up routing before hybrid triage

### Decision Flow in Agent.Run()

```
Agent.Run(userMessage)
    ↓
1. Check currentModelOverride (set by Telegram /fast or /deep)
   ├─ If set → use it, reason = "user_command"
   └─ If empty → go to step 2
    ↓
2. Check active sticky session
   ├─ If lastUsedModel has a live backend session → use it, reason = "sticky_session"
   └─ If no sticky model/session → go to step 3
    ↓
3. Call selectModel(userMessage)
   ├─ Match against static rules
   ├─ Return selected model, reason = "static_rule"
   └─ Fallback to "sonnet", reason = "default"
    ↓
3. Check if model changed
   ├─ If yes → clear session (sessionID = "")
   └─ If no → continue
    ↓
4. Pass selected model to CallStream()
    ↓
5. Log decision with routing metadata:
   - Model: selected model
   - RoutingReason: how it was selected
   - RoutingLatency: how long selection took
```

### Decision Log Tracking

Each decision is logged with routing metadata in DecisionLog:

```go
type DecisionLog struct {
    // ... existing fields ...
    Model           string `json:"model"`           // "haiku", "sonnet", "opus"
    RoutingReason   string `json:"routing_reason"`  // "user_command", "static_rule", "default", "ai_router"
    RoutingLatency  int    `json:"routing_latency_ms"` // milliseconds
}
```

This enables:
- Audit trail of routing decisions
- Performance analysis (latency metrics)
- Cost attribution per model
- Refinement of rules based on actual usage

## Configuration

### Minimal Setup (Static Rules Only)

```json
{
  "model_routing": {
    "enable_dynamic_routing": false
  }
}
```
Uses Agent.selectModel() for all decisions, ignores `/fast` and `/deep` commands.

### Full Setup (All Three Tiers)

```json
{
  "model_routing": {
    "enable_dynamic_routing": true,
    "fast_model": "claude-haiku-4-5-20251001",
    "smart_model": "claude-sonnet-4-6",
    "deep_model": "claude-opus-4-6",
    "sticky_session": true,
    "session_idle_timeout_min": 5,
    "use_gpt4o_mini_for_triage": true
  }
}
```

Also requires: `OPENAI_API_KEY` environment variable

## Cost Optimization

### Expected Savings

Assuming typical usage:
- 70% simple tasks → Haiku (~$0.80 per M input tokens)
- 30% complex tasks → Opus (~$15.00 per M input tokens)

**Default (all Sonnet)**: $3.00 per M input tokens
**With Routing**: ~$4.50 per M input tokens on simple tasks vs. $15 on complex
- **Net savings**: ~40-50% overall token costs
- **Quality maintained**: Complex tasks use most capable model

## Testing

Comprehensive test suite in `internal/app/agent_test.go`:

```go
TestSelectModelHaikuRules()       // Validates Haiku rule detection
TestSelectModelOpusRules()        // Validates Opus rule detection
TestSelectModelDefaultFallback()  // Tests default Sonnet fallback
TestSelectModelCaseInsensitive()  // Confirms case-insensitive matching
TestSelectModelPriorityOrder()    // Verifies priority ordering
```

Run tests:
```bash
go test -v ./internal/app -run TestSelectModel
```

## Usage Examples

### User Command Override

```
User: /fast
Bot: ✅ 已切換至快速模式 (Switched to fast mode)

User: translating text (any message)
Agent: Routes to Haiku, responds with translation

User: /deep
Bot: ✅ 已切換至深度模式 (Switched to deep mode)

User: refactor my code (any message)
Agent: Routes to Opus, performs refactoring

User: 那 TV Timer RN 有沒有修？
Agent: Keeps the active Opus session; no auto-triage/model switch

User: /clear
Bot: 🔄 Session context cleared. The next message starts fresh

User: /auto
Bot: ✅ 已切換至自動路由模式 (Switched to auto mode)
Agent: Uses static rules for next message
```

### Automatic Static Rules

```
User: "Can you summarize this file?"
Agent Decision: Rule matches "summariz*" → Haiku
Logging: routing_reason="static_rule", routing_latency=2ms

User: "Help me debug this race condition"
Agent Decision: Rule matches "debug" → Opus
Logging: routing_reason="static_rule", routing_latency=3ms

User: "What's the weather?"
Agent Decision: No rules match → Default Sonnet
Logging: routing_reason="default", routing_latency=1ms
```

## Performance Characteristics

| Routing Method | Latency | Overhead |
|---|---|---|
| User Command | 0ms | None (already decided) |
| Static Rules | <5ms | Negligible |
| AI Triage | ~300ms | ~10-15ms per request |
| Default | <1ms | None (fallback) |

## Future Enhancements

1. **Learning-Based Routing**: Analyze decision logs to refine rules
2. **Cost-Based Routing**: Dynamic routing based on budget constraints
3. **Context-Aware Routing**: Consider conversation history
4. **A/B Testing**: Compare routing strategies
5. **Dashboard Integration**: Visualize routing decisions and cost savings

## Backward Compatibility

- No breaking changes to existing APIs
- Static rules are transparent (no user impact)
- Feature is disabled by default (`enable_dynamic_routing: false`)
- Users can ignore routing by not using `/fast` or `/deep`
- Graceful fallback to configured default model

## References

- Issue #72: Dynamic Model Routing - User Command & Static Rules (Phase 1)
- Issue #73: Per-Model Cost Tracking
- Issue #74: Savings Calculator Dashboard

---

**Implementation Status**: ✅ Phase 1 (User Commands + Static Rules) Complete
**Next Phase**: Phase 2 (AI Triage Integration with Dashboard)
