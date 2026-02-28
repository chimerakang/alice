# Telegram 線程恢復機制根本性分析

## 問題陳述

當用戶在 Telegram forum 中使用 `@project_mimir_bot /command` 時，Telegram API 返回 `message_thread_id=0`，導致 bot 無法確定命令的實際來源 topic。

## 當前恢復策略的根本問題

### recoverActualThreadID 邏輯缺陷

```go
// 行 274-280：單一 topic 假設（✗ 錯誤）
if len(knownTopics) == 1 {
    for tid := range knownTopics {
        log.Printf("[telegram] recoverThreadID: single topic found, recovering threadID=%d", tid)
        return chatKey{chatID: zeroKey.chatID, threadID: tid}
    }
}
```

### 問題根源

在多 topic 論壇（5-10 個 topics）中：

```
論壇中的所有 topics:  [17, 89, 3631, 7745, ...]
在 memory 中的:      [17]          ← 只有最後使用的
在 database 中的:    [17]          ← 只有被保存過的
```

當代碼發現 `len(knownTopics) == 1`，就**錯誤地假設** topic 17 是唯一的 topic，而實際上論壇有多個 topics。

### 為什麼現有解決方案失敗

| 來源 | 數據來源 | 覆蓋範圍 |
|------|---------|---------|
| `agents` map | 當前 session 的 agent instances | 只有被使用過的 topics |
| `topic_settings` 表 | 已保存的 topic 設置 | 只有被主動配置過的 topics |
| `lastUsedThreadID` map | 當前 session 的最後使用記錄 | 只有當前 bot 進程中使用過的 topic |

**結論**: 沒有任何數據來源能告訴 bot "這個 forum 有哪些 topics"。

## Telegram API 限制

```
正常消息:  message_thread_id = 89 ✓ 正確
@mention:  message_thread_id = 0  ✗ 丟失信息

reply_to_message: 也無法幫助
- 當 @mention 時，message 不是 reply，而是新發送
- reply_to_message 為 null
```

## 證據：日誌中的失敗案例

```
2026/02/28 20:13:24 [telegram] Received message: threadID=0 (from @mention)
2026/02/28 20:13:24 [telegram] recoverThreadID: single topic found, recovering threadID=17
2026/02/28 20:13:24 [telegram] handleCommand: /help@bot → sent to threadID=17

但用戶實際是在 threadID=3631 中發送的 @mention！
```

## 可行的解決方案對比

### 方案 1：禁用 @mention 命令（✓ 推薦）

**優點**:
- 完全消除歧義
- 強制用戶在正確的 topic 中發送命令
- 簡單可靠

**實現**:
```go
// 在 recoverActualThreadID 或 handleMessage 中
if key.threadID == 0 {
    t.send(key, "❌ @mention 命令在 forum 中不支援\n請在對應的 topic 中直接輸入命令，例如:\n/help")
    return zeroKey  // 不恢復，直接拒絕
}
```

**用戶體驗影響**:
- 😞 需要改變使用習慣
- ✓ 但保證正確的 topic 響應

### 方案 2：在 General topic 中響應（✗ 不推薦）

**問題**:
- 響應出現在錯誤的位置
- 用戶困惑加倍

### 方案 3：完整的 topic 列表（✗ 不可行）

**為什麼**:
- Telegram API 沒有 "列出 forum 中的所有 topics" 端點
- 無法實現

### 方案 4：使用 InlineQuery 或 Webhooks（✗ 複雜）

**為什麼**:
- Telegram 的 Inline Query 也無法傳遞 topic context
- WebHook 與 getUpdates polling 有不同問題

## 推薦的修復步驟

### Phase 1：診斷確認（已完成）
- [x] 驗證 Telegram API 確實發送 threadID=0
- [x] 確認 reply_to_message 無法幫助
- [x] 確認沒有完整 topic list 數據來源

### Phase 2：立即修復（應該做）
1. 修改 `recoverActualThreadID()` 返回 `threadID=0` 當找不到確定答案時
2. 在 `sendMarkdown()` 檢測 `threadID==0` 的情況
3. 在 general topic 中發送提示消息，告訴用戶改用正常命令

### Phase 3：用戶告知（通告）
1. 在 Telegram 中公告：@mention 命令已禁用
2. 指導用戶在對應 topic 中直接輸入命令

## 關鍵代碼位置

| 文件 | 行 | 問題 |
|------|----|----|
| `telegram.go` | 249-306 | recoverActualThreadID 邏輯 |
| `telegram.go` | 650 | handleMessage 中的 threadID=0 恢復調用 |
| `telegram.go` | 1394 | sendMarkdown 發送消息 |

## 對已提交代碼的影響

當前未提交的更改（包含診斷日誌和修復嘗試）仍有缺陷。建議：
1. 回滾當前更改（沒有根本解決問題）
2. 採用 "禁用 @mention" 方案
3. 在 general topic 中給出清晰提示
