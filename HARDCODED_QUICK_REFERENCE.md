# 硬编码问题快速参考

## 📊 统计汇总

```
总硬编码数: 68 个 message keys
已本地化: 46 个
需要修复: 68 个

按优先级分布:
  🔴 P0 (立即修复): 31 个 - 图片(18) + 文件(10) + 任务(2) + 代理(1)
  🟠 P1 (本周修复): 28 个 - 多代理(9) + 检查点(11) + 任务统计(8)
  🟡 P2 (本月修复): 10 个 - 用量统计(4) + Web监控(6)
```

---

## 🔴 第一优先级 (P0) - 31个 keys

### 区域 1: 图片分析 (18个)
**位置**: `telegram.go` 行 1684-2147

关键消息:
- `photo_disabled` - 功能禁用提示
- `photo_analyzing_single` / `photo_analyzing_batch` - 处理中
- `photo_file_too_large` - 文件过大
- `photo_download_failed` / `photo_copy_failed` - 处理失败
- `photo_analyze_*_prompt` - 分析提示词
- `photo_*` 其他 (参考、引用等)

### 区域 2: 文件处理 (10个)
**位置**: `telegram.go` 行 2754-2853

关键消息:
- `document_file_too_large` - 文件过大
- `document_downloading` / `document_download_failed` - 下载状态
- `document_mkdir_failed` / `document_copy_failed` - 创建/复制失败
- `document_analyzing` / `document_analysis_failed` - 分析状态
- `document_*` 其他 (提示词等)

### 区域 3: 任务清单 (2个)
**位置**: `telegram.go` 行 1653-1675

关键消息:
- `tasks_read_failed` - 无法读取
- `tasks_format_invalid` - 格式不正确

### 区域 4: 使用代理 (1个)
**位置**: `telegram.go` 行 607

关键消息:
- `using_agent` - "🤖 使用 %s 代理处理此任务" (已有 key，但未使用)

---

## 🟠 第二优先级 (P1) - 28个 keys

### 区域 5: 多代理系统 (9个)
**位置**: `telegram.go` 行 1277-1361

关键消息:
- `multiagent_status_title` / `multiagent_status_stats` - 状态
- `multiagent_status_running` - 执行中任务
- `multiagent_usage_stats_title` - 使用统计
- `multiagent_agent_*` 其他 (代理信息)

### 区域 6: 检查点系统 (11个)
**位置**: `telegram.go` 行 1428-1491

关键消息:
- `checkpoint_list_*` - 列表显示
- `checkpoint_stats_*` - 统计显示
- `checkpoint_stats_error` - 错误消息

### 区域 7: 任务统计 (8个)
**位置**: `telegram.go` 行 305-348

关键消息:
- `task_savings_title` / `task_savings_cost_header` / `task_savings_method_header` - 标题
- `task_savings_model_breakdown` - 模型分解
- `task_savings_amount` - 节省金额
- `task_savings_*` 其他

---

## 🟡 第三优先级 (P2) - 10个 keys

### 区域 8: 用量统计 (4个)
**位置**: `telegram.go` 行 850-873

关键消息:
- `usage_stats_by_model` - "按模型分类"
- `usage_stats_model_item` - 模型项目行
- `usage_stats_routing_savings` - 路由节省
- `usage_stats_mode` - 订阅模式

### 区域 9: Web 监控 (6个)
**位置**: `telegram.go` 行 1372-1400

关键消息:
- `dashboard_status_connections` - 连接数
- `dashboard_title` / `dashboard_main` / `dashboard_timeline` / `dashboard_test` / `dashboard_database` - 监控链接

---

## 💾 文件修改清单

### 1. `locales/en.json`
- 添加 ~68 个新 message keys (英文版本)
- 参考: `hardcoded_text_fixes.md`

### 2. `locales/zh-TW.json`
- 添加 ~68 个新 message keys (繁体中文版本)
- 参考: `hardcoded_text_fixes.md`

### 3. `internal/app/telegram.go`
- 替换硬编码文本为 `t.getLocalizedMessage()` 调用
- 位置: 行 305-348, 607, 1277-1361, 1372-1400, 1428-1491, 1653-1675, 1684-2147, 2754-2853 等

---

## 🔍 快速搜索方法

### 查看所有硬编码中文
```bash
grep -nE '[\u4e00-\u9fa5]' internal/app/telegram.go | grep -v '//' | head -50
```

### 查看特定区域的硬编码
```bash
# 图片分析
sed -n '1684,2147p' internal/app/telegram.go | grep -nE 'fmt\.Sprintf|WriteString|t\.send'

# 文件处理
sed -n '2754,2853p' internal/app/telegram.go | grep -nE 'fmt\.Sprintf|WriteString|t\.send'
```

### 验证所有 keys 已定义
```bash
# 检查 message key 是否在 locale 文件中
grep '"task_savings_title"' locales/en.json locales/zh-TW.json
```

---

## ✅ 修复验证清单

### 代码修复验证
- [ ] 所有硬编码中文已被替换
- [ ] 所有 `fmt.Sprintf` 都使用了 message keys
- [ ] 没有混合硬编码和本地化的消息

### 编译验证
```bash
go build -o alice ./cmd/alice
# 应该编译成功，无错误
```

### 运行时验证 (Telegram Bot)
```
/lang en
/tasks               # 应显示英文
/photo <发送图片>    # 应显示英文提示

/lang zh-TW
/tasks               # 应显示繁体中文
/photo <发送图片>    # 应显示繁体中文提示
```

### Git 验证
```bash
git diff --stat        # 应显示 3 个文件修改
git diff locales/      # 检查新增的 message keys
git diff telegram.go   # 检查替换情况
```

---

## 📈 进度追踪

### Week 1 (P0 - 关键功能)
```
[ ] 图片分析 (18个 keys)
    - [ ] locales/en.json 添加
    - [ ] locales/zh-TW.json 添加
    - [ ] telegram.go 1684-2147 替换
    - [ ] 测试验证

[ ] 文件处理 (10个 keys)
    - [ ] locales/en.json 添加
    - [ ] locales/zh-TW.json 添加
    - [ ] telegram.go 2754-2853 替换
    - [ ] 测试验证

[ ] 任务清单 (2个 keys)
    - [ ] locales/en.json 添加
    - [ ] locales/zh-TW.json 添加
    - [ ] telegram.go 1653-1675 替换
    - [ ] 测试验证

[ ] 使用代理 (1个 key)
    - [ ] telegram.go 607 修复 (改用已有 key)
    - [ ] 测试验证

[ ] Git Commit & Push
```

### Week 2 (P1 - 重要功能)
```
[ ] 多代理系统 (9个 keys)
    - [ ] Keys 添加
    - [ ] telegram.go 替换
    - [ ] 测试验证

[ ] 检查点系统 (11个 keys)
    - [ ] Keys 添加
    - [ ] telegram.go 替换
    - [ ] 测试验证

[ ] 任务统计 (8个 keys)
    - [ ] Keys 添加
    - [ ] telegram.go 替换
    - [ ] 测试验证

[ ] Git Commit & Push
```

### Week 3 (P2 - 补充功能)
```
[ ] 用量统计 (4个 keys)
[ ] Web 监控 (6个 keys)
[ ] 最终验证
[ ] Git Commit & Push
```

---

## 🚀 实施步骤 (标准流程)

### Step 1: 准备
```bash
cd /Volumes/eclipse/projects/alice
git checkout -b i18n/complete-hardcoded-text
```

### Step 2: 编辑 locale 文件
```bash
# 使用编辑器编辑以下文件:
# - locales/en.json
# - locales/zh-TW.json
# 参考: hardcoded_text_fixes.md
```

### Step 3: 编辑 telegram.go
```bash
# 使用查找替换逐个替换硬编码文本
# 关键位置: 行 305-348, 607, 1277-1361 等

# 标准模式:
# 查找: fmt.Sprintf("硬編碼文字: %s", variable)
# 替换为: t.getLocalizedMessage(key.chatID, "message_key", map[string]string{"var": variable})
```

### Step 4: 编译测试
```bash
go build -o alice ./cmd/alice
./alice  # 启动 bot
```

### Step 5: 功能测试
```bash
# 在 Telegram 中测试所有相关命令
/lang en
/lang zh-TW
# ...验证所有功能的显示语言
```

### Step 6: Git 提交
```bash
git add locales/ internal/app/telegram.go
git commit -m "🌍 P13 #XX: 完成全量硬編碼文本國際化

完成 68 個 message keys 的添加和集成:
- 圖片分析 (18个)
- 文件處理 (10个)
- 任務清單 (2个)
- 多代理系統 (9个)
- 檢查點系統 (11个)
- 任務統計 (8个)
- 用量統計 (4个)
- Web 監控 (6个)

所有硬編碼文本已替換為 i18n message keys，支持英文和繁體中文顯示。"

git push origin i18n/complete-hardcoded-text
```

### Step 7: 创建 PR
```bash
gh pr create --title "完成全量硬編碼文本國際化" \
  --body "完成 68 個 message keys，支持完整的多語言交互"
```

---

## 📚 参考文档

| 文档 | 用途 |
|------|------|
| `HARDCODED_ISSUES.md` | 完整问题分析 |
| `hardcoded_text_audit.md` | 详细审计报告 |
| `hardcoded_text_fixes.md` | 逐个 key 的修复方案 |
| `i18n_guide.md` | i18n 开发指南 |
| `i18n_implementation.md` | i18n 实现细节 |

---

**快速问题查找**: 搜索这份文档中的"💾"、"🔍"、"✅" 符号找到相关步骤。

**需要帮助?** 参考 `hardcoded_text_fixes.md` 中的具体 message key 定义和代码示例。
