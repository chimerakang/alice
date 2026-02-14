#!/usr/bin/env bash
# One-time setup: Create milestones and assign existing issues
# Usage: bash .github/scripts/setup-milestones.sh
#
# This script:
# 1. Creates milestones for each phase (P1-P13)
# 2. Assigns existing issues to the correct milestones
# 3. Can be run multiple times safely (idempotent)

set -euo pipefail

REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)
echo "Repository: $REPO"

# --- Create Milestones ---
create_milestone() {
  local title="$1"
  local desc="$2"
  local state="${3:-open}"

  # Check if milestone already exists
  existing=$(gh api "repos/${REPO}/milestones?state=all&per_page=100" --jq ".[] | select(.title == \"${title}\") | .number" 2>/dev/null || echo "")

  if [ -n "$existing" ]; then
    echo "  ✓ Milestone '${title}' already exists (#${existing})"
    return
  fi

  gh api "repos/${REPO}/milestones" -X POST \
    -f title="${title}" \
    -f description="${desc}" \
    -f state="${state}" \
    --silent 2>/dev/null

  echo "  ✅ Created milestone: ${title}"
}

echo ""
echo "=== Creating Milestones ==="

create_milestone "P1 - Core Backend"              "Telegram Bot + Claude CLI 整合"                           "closed"
create_milestone "P2 - Monitoring"                 "Web Dashboard + API + 監控系統"                           "closed"
create_milestone "P3 - Data Layer"                 "持久化 + Git 整合 + Checkpoint"                          "closed"
create_milestone "P4 - Proto-First"                "Protocol Buffers 架構遷移"                               "closed"
create_milestone "P5 - Frontend Foundation"        "React + Vite 框架 + 基礎元件"                            "closed"
create_milestone "P6 - AI Audit System"            "AI 開發追蹤核心功能"                                      "closed"
create_milestone "P7 - Dashboard & Analytics"      "儀表板強化 + 分析圖表 + 歷史資料查詢"                      "closed"
create_milestone "P8 - Control API"                "遠端控制 + 中斷/回溯"                                    "closed"
create_milestone "P8.5 - TG 指令增強"              "/tasks 待辦清單 + Topic 設定持久化"                       "closed"
create_milestone "P9 - Multimedia Input"           "圖片分析 + 語音轉文字"                                    "closed"
create_milestone "P9.5 - Multimedia Enhancement"   "多張圖片批次處理 + 媒體群組支援"                           "closed"
create_milestone "P10 - Claude Code Hooks"         "攔截所有 Claude Code 互動（Terminal/VSCode/TG）"          "closed"
create_milestone "P11 - User Experience"           "指令健全性和用戶體驗改善"                                  "closed"
create_milestone "P12 - Dashboard Analytics"       "Claude Code Hooks UI 增強：統計圖表 + 用戶指南"            "closed"
create_milestone "P13 - Future Enhancements"       "未來功能增強與優化"                                       "open"

# --- Assign Issues to Milestones ---
assign_issue() {
  local issue_num="$1"
  local milestone_title="$2"

  # Get milestone number
  ms_num=$(gh api "repos/${REPO}/milestones?state=all&per_page=100" --jq ".[] | select(.title == \"${milestone_title}\") | .number" 2>/dev/null || echo "")

  if [ -z "$ms_num" ]; then
    echo "  ⚠ Milestone '${milestone_title}' not found for issue #${issue_num}"
    return
  fi

  # Check current milestone
  current=$(gh issue view "$issue_num" --json milestone --jq '.milestone.title // ""' 2>/dev/null || echo "")
  if [ "$current" = "$milestone_title" ]; then
    echo "  ✓ Issue #${issue_num} already in '${milestone_title}'"
    return
  fi

  gh issue edit "$issue_num" --milestone "$milestone_title" 2>/dev/null
  echo "  ✅ Issue #${issue_num} → ${milestone_title}"
}

echo ""
echo "=== Assigning Issues to Milestones ==="

# P2 - Monitoring
assign_issue 1  "P2 - Monitoring"
assign_issue 2  "P2 - Monitoring"
assign_issue 3  "P2 - Monitoring"
assign_issue 4  "P2 - Monitoring"
assign_issue 5  "P2 - Monitoring"
assign_issue 12 "P2 - Monitoring"

# P3 - Data Layer
assign_issue 6  "P3 - Data Layer"
assign_issue 7  "P3 - Data Layer"
assign_issue 8  "P3 - Data Layer"
assign_issue 9  "P3 - Data Layer"
assign_issue 10 "P3 - Data Layer"

# P4 - Proto-First
assign_issue 13 "P4 - Proto-First"

# P5 - Frontend Foundation
assign_issue 15 "P5 - Frontend Foundation"
assign_issue 16 "P5 - Frontend Foundation"
assign_issue 17 "P5 - Frontend Foundation"
assign_issue 18 "P5 - Frontend Foundation"

# P6 - AI Audit System
assign_issue 19 "P6 - AI Audit System"
assign_issue 20 "P6 - AI Audit System"
assign_issue 21 "P6 - AI Audit System"
assign_issue 22 "P6 - AI Audit System"
assign_issue 23 "P6 - AI Audit System"
assign_issue 27 "P6 - AI Audit System"

# P7 - Dashboard & Analytics
assign_issue 24 "P7 - Dashboard & Analytics"
assign_issue 25 "P7 - Dashboard & Analytics"
assign_issue 26 "P7 - Dashboard & Analytics"
assign_issue 30 "P7 - Dashboard & Analytics"

# P8 - Control API
assign_issue 11 "P8 - Control API"

# P8.5 - TG 指令增強
assign_issue 31 "P8.5 - TG 指令增強"
assign_issue 33 "P8.5 - TG 指令增強"

# P9 - Multimedia Input
assign_issue 28 "P9 - Multimedia Input"
assign_issue 29 "P9 - Multimedia Input"

# P9.5 - Multimedia Enhancement
assign_issue 34 "P9.5 - Multimedia Enhancement"
assign_issue 35 "P9.5 - Multimedia Enhancement"

# P10 - Claude Code Hooks
assign_issue 32 "P10 - Claude Code Hooks"

# P11 - User Experience
assign_issue 37 "P11 - User Experience"

# P12 - Dashboard Analytics
assign_issue 36 "P12 - Dashboard Analytics"

echo ""
echo "=== Done ==="
echo "Run '/task-sync' or 'gh workflow run sync-tasks.yml' to generate MASTER_TASKS.md"
