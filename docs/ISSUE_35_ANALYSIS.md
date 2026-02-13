# Issue #35: 圖片+文字組合處理問題分析

> **Issue #35 補充問題發現** (2026-02-13 15:40)

## 🚨 問題描述

**症狀**：圖片和文字說明組合時處理失敗

- **純圖片** ✅ 正常
- **純文字** ✅ 正常
- **圖片 + 文字說明** ❌ 失敗

## 🔍 根本原因分析

### 錯誤的處理路徑

```
圖片+文字 → handleSinglePhoto() → EnhancedCLIClient → 新 CLI 子進程 → 需要認證 ❌
```

**應該的處理路徑**：
```
圖片+文字 → handleSinglePhoto() → agent.Run() → 現有會話 ✅
```

### 具體錯誤日誌

```log
2026/02/13 15:34:37 enhanced_cli.go:67: [enhanced-cli] executing: claude -p --output-format json --model claude-sonnet-4-20250514 --dangerously-skip-permissions --max-turns 25 --file img_0:temp/photo_...jpg --resume 347e4ee0-06cf-45e2-a090-d9c9a1bb4e0b 請分析這張圖片: photo_...jpg

用戶說明: 圖片測試

2026/02/13 15:34:38 enhanced_cli.go:80: [enhanced-cli] stderr: Error: Session token required for file downloads. CLAUDE_CODE_SESSION_ACCESS_TOKEN must be set.
```

## 💡 解決方案

### 對比：語音處理（正確的方式）

**語音訊息處理** ✅：
1. 下載語音檔案
2. 使用 Whisper API 轉錄成文字
3. **使用 `agent.Run(transcribedText)` 通過現有會話處理**

### 修復建議

**圖片處理應該改為**：
1. 下載圖片到專案 temp 目錄 ✅ (已實作)
2. **使用 `agent.Run(prompt)` 通過現有會話處理** ❌ (需修復)
3. 移除 `EnhancedCLIClient` 的使用

### 程式碼修改位置

**檔案**: `/internal/app/telegram.go`

**函數**: `handleSinglePhoto()` 和 `handleMultiplePhotos()`

**目前錯誤的實作** (約第1580行):
```go
// 使用增強的多模態代理處理
agent = t.getAgent(key)
enhancedAgent := NewEnhancedAgent(agent)

// 構建多模態訊息內容
messageContent := CreateImageMessageContent("", []string{projectImagePath}, caption)

// 發送給增強代理處理
ctx := context.Background()
_, err = enhancedAgent.ProcessMessage(ctx, messageContent, func(update string, silent bool) {
    if silent {
        return
    }
    t.send(key, update)
})
```

**應該改為** (參考語音處理邏輯):
```go
// 發送給 Agent 處理（就像語音轉文字一樣）
agent = t.getAgent(key)

_, err = agent.Run(prompt, func(update string, silent bool) {
    if silent {
        return
    }
    t.send(key, update)
})
```

## 🎯 技術細節

### 為什麼會有這個設計錯誤？

1. **設計分離**：純文字和圖片使用不同的處理路徑
2. **過度工程化**：`EnhancedAgent` 和 `EnhancedCLIClient` 試圖解決不存在的問題
3. **認證混亂**：新子進程需要額外的認證，但 Alice bot 是轉傳工具

### 為什麼語音可以正常工作？

語音處理最終轉成文字後，使用 `agent.Run()` 通過現有的 Claude Code 會話，不需要啟動新進程。

## 📋 修復檢查清單

- [ ] 修改 `handleSinglePhoto()` 使用 `agent.Run()`
- [ ] 修改 `handleMultiplePhotos()` 使用 `agent.Run()`
- [ ] 移除 `EnhancedAgent` 和 `EnhancedCLIClient` 的依賴
- [ ] 測試圖片+文字組合功能
- [ ] 確保向後相容性（純圖片仍可正常工作）

## 🔗 相關檔案

- `/internal/app/telegram.go` - 主要修改目標
- `/internal/app/multimodal.go` - 可能需要簡化或移除
- `/internal/app/enhanced_cli.go` - 可能需要移除

## 📝 測試案例

1. **純圖片** - 應該繼續正常工作
2. **圖片 + 中文說明** - 目前失敗，修復後應正常
3. **圖片 + 英文說明** - 目前失敗，修復後應正常
4. **多張圖片 + 文字** - 目前失敗，修復後應正常

---

*記錄時間：2026-02-13 15:40*
*發現者：透過日誌分析和架構檢查*
*優先級：P0 (影響核心圖片分析功能)*