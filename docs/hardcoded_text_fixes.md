# 硬编码文本修复清单

本文档列出所有需要添加的 message keys 及其修复方案。

## 第一批修复: 任务统计 (P0)

### 新增 Message Keys

在 `locales/en.json` 和 `locales/zh-TW.json` 中添加：

```json
{
  "task_savings_title": {
    "en": "📊 *Weekly Smart Routing Statistics*\n\n",
    "zh-TW": "📊 *本週智慧路由統計*\n\n"
  },
  "task_savings_model_breakdown": {
    "en": "{icon} *{model}*: {calls} times\n  Cost: ${cost} (Assumed Sonnet: ${sonnet_cost}) {status}\n\n",
    "zh-TW": "{icon} *{model}*: {calls} 次\n  成本: ${cost} (假設 Sonnet: ${sonnet_cost}) {status}\n\n"
  },
  "task_savings_cost_header": {
    "en": "💰 *Cost Savings Summary*\n",
    "zh-TW": "💰 *節省金額統計*\n"
  },
  "task_savings_actual_cost": {
    "en": "Actual Cost: ${cost}\n",
    "zh-TW": "實際花費: ${cost}\n"
  },
  "task_savings_assumed_cost": {
    "en": "Assumed Sonnet Cost: ${cost}\n",
    "zh-TW": "假設全用 Sonnet: ${cost}\n"
  },
  "task_savings_amount": {
    "en": "Savings: *${savings}* ({percent}%)\n\n",
    "zh-TW": "節省金額: *${savings}* ({percent}%)\n\n"
  },
  "task_savings_method_header": {
    "en": "🎯 *Routing Method Distribution*\n",
    "zh-TW": "🎯 *路由方式分佈*\n"
  },
  "task_savings_method_item": {
    "en": "• {method}: {count} times ({percent}%)\n",
    "zh-TW": "• {method}: {count} 次 ({percent}%)\n"
  }
}
```

### 代码修复位置

**文件**: `internal/app/telegram.go`

**行 305-348**: `handleTaskSavingsCommand()`

```go
// 现在:
msg.WriteString("📊 *本週智慧路由統計*\n\n")

// 修改为:
msg.WriteString(t.getLocalizedMessage(key.chatID, "task_savings_title", nil))

// 以此类推...
```

---

## 第二批修复: 多代理系统 (P1)

### 新增 Message Keys

```json
{
  "multiagent_status_title": {
    "en": "🤖 *Multi-Agent System Status*\n\n",
    "zh-TW": "🤖 *多代理系統狀態*\n\n"
  },
  "multiagent_status_stats": {
    "en": "📊 *Statistics*:\n  Total Agents: {count}\n\n",
    "zh-TW": "📊 *統計*:\n  總代理數: {count}\n\n"
  },
  "multiagent_status_running": {
    "en": "🔄 *Running Tasks*:\n  ID: {id}\n  Status: {status}\n\n",
    "zh-TW": "🔄 *執行中任務*:\n  ID: {id}\n  狀態: {status}\n\n"
  },
  "multiagent_usage_stats_title": {
    "en": "📊 *Multi-Agent Usage Statistics*\n\n",
    "zh-TW": "📊 *多代理使用統計*\n\n"
  },
  "multiagent_agent_header": {
    "en": "🤖 *{agent_type}*\n",
    "zh-TW": "🤖 *{agent_type}*\n"
  },
  "multiagent_agent_task_count": {
    "en": "  Task Count: {count}\n",
    "zh-TW": "  任務數: {count}\n"
  },
  "multiagent_agent_last_used": {
    "en": "  Last Used: {time}\n\n",
    "zh-TW": "  最後使用: {time}\n\n"
  },
  "multiagent_agent_description": {
    "en": "**{agent_name}**\n",
    "zh-TW": "**{agent_name}**\n"
  },
  "multiagent_agent_desc_text": {
    "en": "Description: {description}\n",
    "zh-TW": "描述: {description}\n"
  },
  "multiagent_agent_skills": {
    "en": "Skills: {skills}\n\n",
    "zh-TW": "技能: {skills}\n\n"
  }
}
```

### 代码修复位置

**文件**: `internal/app/telegram.go`

**行 1277-1361**: `handleMultiagentStatsCommand()` 和 `handleMultiagentDetailCommand()`

---

## 第三批修复: 检查点系统 (P1)

### 新增 Message Keys

```json
{
  "checkpoint_list_title": {
    "en": "📂 Project: `{path}`\n",
    "zh-TW": "📂 專案: `{path}`\n"
  },
  "checkpoint_list_count": {
    "en": "📊 Total: {count} checkpoints\n\n",
    "zh-TW": "📊 總數: {count} 個檢查點\n\n"
  },
  "checkpoint_list_item": {
    "en": "• `{id}`\n",
    "zh-TW": "• `{id}`\n"
  },
  "checkpoint_list_description": {
    "en": "  📝 {description}\n",
    "zh-TW": "  📝 {description}\n"
  },
  "checkpoint_list_timestamp": {
    "en": "  📅 {timestamp}\n",
    "zh-TW": "  📅 {timestamp}\n"
  },
  "checkpoint_list_size": {
    "en": "  💾 {size} bytes\n\n",
    "zh-TW": "  💾 {size} bytes\n\n"
  },
  "checkpoint_stats_title": {
    "en": "📂 Project: `{path}`\n\n",
    "zh-TW": "📂 專案: `{path}`\n\n"
  },
  "checkpoint_stats_total": {
    "en": "📊 Total Checkpoints: {count}\n",
    "zh-TW": "📊 總檢查點: {count}\n"
  },
  "checkpoint_stats_size": {
    "en": "💾 Total Size: {size} bytes\n",
    "zh-TW": "💾 總大小: {size} bytes\n"
  },
  "checkpoint_stats_avg_size": {
    "en": "📏 Average Size: {size} bytes\n",
    "zh-TW": "📏 平均大小: {size} bytes\n"
  },
  "checkpoint_stats_error": {
    "en": "❌ Failed to get checkpoint statistics: {error}",
    "zh-TW": "❌ 獲取檢查點統計失敗: {error}"
  }
}
```

### 代码修复位置

**文件**: `internal/app/telegram.go`

**行 1428-1491**: `handleCheckpointListCommand()` 和 `handleCheckpointStatsCommand()`

---

## 第四批修复: 图片分析 (P0)

### 新增 Message Keys

```json
{
  "photo_disabled": {
    "en": "📷 Photo analysis feature is currently disabled. Please contact the administrator to enable `enable_photo_support`.",
    "zh-TW": "📷 圖片分析功能目前未啟用。請聯繫管理員開啟 `enable_photo_support` 設定。"
  },
  "photo_analyzing_single": {
    "en": "📷 Analyzing photo...",
    "zh-TW": "📷 正在分析圖片..."
  },
  "photo_analyzing_batch": {
    "en": "📷 Analyzing {count} photos...",
    "zh-TW": "📷 正在分析 {count} 張圖片..."
  },
  "photo_mkdir_failed": {
    "en": "📷 Failed to create project temporary directory.",
    "zh-TW": "📷 建立專案臨時目錄失敗。"
  },
  "photo_file_too_large": {
    "en": "📷 Photo {index} file is too large ({size}). Limit is {limit}MB.",
    "zh-TW": "📷 第 {index} 張圖片檔案過大（{size}），限制為 {limit}MB。"
  },
  "photo_download_failed": {
    "en": "📷 Failed to download photo {index}",
    "zh-TW": "📷 第 {index} 張圖片下載失敗"
  },
  "photo_copy_failed": {
    "en": "📷 Failed to copy photo {index}",
    "zh-TW": "📷 第 {index} 張圖片複製失敗"
  },
  "photo_all_failed": {
    "en": "📷 All photos processing failed. Please try again later.",
    "zh-TW": "📷 所有圖片處理失敗，請稍後再試。"
  },
  "photo_list_item": {
    "en": "Photo {index}: {path}\n",
    "zh-TW": "圖片 {index}: {path}\n"
  },
  "photo_reference_batch": {
    "en": "\n\n(Reference {count} photos:\n{list})",
    "zh-TW": "\n\n（參考附件 {count} 張圖片：\n{list}）"
  },
  "photo_reference_single": {
    "en": "\n\n(Reference photo: {path})",
    "zh-TW": "\n\n（參考附件圖片: {path}）"
  },
  "photo_analyze_batch_prompt": {
    "en": "Analyze these {count} photos and perform comparative analysis:\n{list}",
    "zh-TW": "請分析這 {count} 張圖片，並進行比較分析：\n{list}"
  },
  "photo_analyze_single_prompt": {
    "en": "Analyze this photo: {path}",
    "zh-TW": "請分析這張圖片: {path}"
  },
  "photo_caption_pii": {
    "en": "⚠️ Sensitive information detected in photo caption and auto-filtered.",
    "zh-TW": "⚠️ 圖片說明中偵測到敏感資訊已自動過濾。"
  },
  "photo_analysis_failed": {
    "en": "📷 Photo analysis failed. Please try again later.",
    "zh-TW": "📷 圖片分析失敗，請稍後再試。"
  }
}
```

### 代码修复位置

**文件**: `internal/app/telegram.go`

**行 1684-2147**: 所有图片处理相关函数

---

## 第五批修复: 文件处理 (P0)

### 新增 Message Keys

```json
{
  "document_file_too_large": {
    "en": "📁 Document file is too large ({size}). Limit is {limit}MB.",
    "zh-TW": "📁 文件檔案過大（{size}），限制為 {limit}MB。"
  },
  "document_downloading": {
    "en": "📁 Downloading document...",
    "zh-TW": "📁 正在下載文件..."
  },
  "document_download_failed": {
    "en": "📁 Failed to download document. Please try again later.",
    "zh-TW": "📁 下載文件失敗，請稍後再試。"
  },
  "document_mkdir_failed": {
    "en": "📁 Failed to create temporary directory.",
    "zh-TW": "📁 建立臨時目錄失敗。"
  },
  "document_copy_failed": {
    "en": "📁 Failed to copy document to project directory.",
    "zh-TW": "📁 複製文件到專案目錄失敗。"
  },
  "document_analyzing": {
    "en": "📁 Analyzing document...",
    "zh-TW": "📁 正在分析文件..."
  },
  "document_analysis_failed": {
    "en": "📁 Document analysis failed. Please try again later.",
    "zh-TW": "📁 文件分析失敗，請稍後再試。"
  },
  "document_prompt_prefix": {
    "en": "User uploaded a document: {path}",
    "zh-TW": "用戶上傳了一個文件：{path}"
  },
  "document_user_note": {
    "en": "\nUser note: {note}",
    "zh-TW": "\n用戶說：{note}"
  },
  "document_file_type": {
    "en": "\nFile type: {type}",
    "zh-TW": "\n文件類型：{type}"
  }
}
```

### 代码修复位置

**文件**: `internal/app/telegram.go`

**行 2754-2853**: `handleDocumentMessage()` 相关函数

---

## 第六批修复: Web 监控界面 (P2)

### 新增 Message Keys

```json
{
  "dashboard_status_connections": {
    "en": "  🔌 Connected Clients: {count}\n",
    "zh-TW": "  🔌 連接數: {count}\n"
  },
  "dashboard_title": {
    "en": "\n🌐 *Web Monitoring Interface*:\n",
    "zh-TW": "\n🌐 *Web 監控介面*:\n"
  },
  "dashboard_main": {
    "en": "  📊 Main Dashboard: http://localhost:{port}/\n",
    "zh-TW": "  📊 主面板: http://localhost:{port}/\n"
  },
  "dashboard_timeline": {
    "en": "  📈 Timeline: http://localhost:{port}/timeline.html\n",
    "zh-TW": "  📈 Timeline: http://localhost:{port}/timeline.html\n"
  },
  "dashboard_test": {
    "en": "  🧪 Test Page: http://localhost:{port}/test-timeline.html\n",
    "zh-TW": "  🧪 測試頁面: http://localhost:{port}/test-timeline.html\n"
  },
  "dashboard_database": {
    "en": "  📁 Database: {path}\n",
    "zh-TW": "  📁 資料庫: {path}\n"
  }
}
```

### 代码修复位置

**文件**: `internal/app/telegram.go`

**行 1372-1400**: `handleStatusCommand()` 中的 dashboard 部分

---

## 第七批修复: 用量统计 (P1)

### 新增 Message Keys

```json
{
  "usage_stats_by_model": {
    "en": "\n📊 *Models (Last 7 Days):*\n",
    "zh-TW": "\n📊 *按模型分類（近 7 天）:*\n"
  },
  "usage_stats_model_item": {
    "en": "  {icon} {model}: {calls} calls | {input}K in / {output}K out | ${cost}\n",
    "zh-TW": "  {icon} {model}: {calls} 次 | {input}K in / {output}K out | ${cost}\n"
  },
  "usage_stats_routing_savings": {
    "en": "\n💡 *Routing Savings: {percent}%* (${actual} → ${default})\n",
    "zh-TW": "\n💡 *路由節省: {percent}%* (${actual} → ${default})\n"
  },
  "usage_stats_mode": {
    "en": "\n*Mode: Claude Max Subscription*\n  Monthly fee $200, no additional token cost",
    "zh-TW": "\n*模式: Claude Max 訂閱*\n  月費固定 $200，無額外 token 費用"
  }
}
```

### 代码修复位置

**文件**: `internal/app/telegram.go`

**行 850-873**: `handleStatusCommand()` 中的 usage stats 部分

---

## 第八批修复: 错误处理和其他 (P2)

### 新增 Message Keys

```json
{
  "tasks_read_failed": {
    "en": "❌ Failed to read task list\n\nPlease run /task-sync to generate MASTER_TASKS.md\n\nError: {error}",
    "zh-TW": "❌ 無法讀取任務清單\n\n請執行 /task-sync 生成 MASTER_TASKS.md\n\n錯誤: {error}"
  },
  "tasks_format_invalid": {
    "en": "❌ MASTER_TASKS.md format is invalid. Please run /task-sync to regenerate",
    "zh-TW": "❌ MASTER_TASKS.md 格式不正確，請執行 /task-sync 重新生成"
  },
  "i18n_not_initialized": {
    "en": "❌ i18n system is not initialized",
    "zh-TW": "❌ i18n 系統未初始化"
  }
}
```

### 代码修复位置

**文件**: `internal/app/telegram.go`

**行 1653-1675**: `handleTasksCommand()`
**行 2997**: `handleLangCommand()`

---

## 实施步骤

1. **第一步**: 在 `locales/en.json` 和 `locales/zh-TW.json` 中添加所有新的 message keys
2. **第二步**: 在 `internal/app/telegram.go` 中替换硬编码文本，改用 `t.getLocalizedMessage()`
3. **第三步**: 对于需要模板变量的消息，使用 `strings.ReplaceAll()` 替换 `{variable}` 占位符
4. **第四步**: 重新编译: `go build -o alice ./cmd/alice`
5. **第五步**: 分别用英文和繁体中文测试每个命令
6. **第六步**: 运行 `git diff` 确认没有其他意外改动

---

## 代码示例

### 替换硬编码的标准模式

**之前**:
```go
msg.WriteString("📊 *本週智慧路由統計*\n\n")
```

**之后**:
```go
msg.WriteString(t.getLocalizedMessage(key.chatID, "task_savings_title", nil))
```

### 带模板变量的替换

**之前**:
```go
fmt.Sprintf("🤖 使用 %s 代理處理此任務", agentType.String())
```

**之后**:
```go
msgTemplate := t.getLocalizedMessage(key.chatID, "using_agent", nil)
msg := strings.ReplaceAll(msgTemplate, "{agent}", agentType.String())
```

---

## 验证清单

- [ ] 所有 message keys 已添加到 `locales/en.json`
- [ ] 所有 message keys 已添加到 `locales/zh-TW.json`
- [ ] 所有硬编码的 `fmt.Sprintf` 都已被替换
- [ ] 英文测试通过 (`/lang en`)
- [ ] 繁体中文测试通过 (`/lang zh-TW`)
- [ ] 没有遗漏的中文字符
- [ ] 没有日志输出中混入硬编码文本
- [ ] Git 提交信息清晰明确
