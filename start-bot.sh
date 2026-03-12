#!/bin/bash
set -e

echo "🔌 停止現有 bot 進程..."
pkill -f "^\./alice" || true
sleep 1

echo "🏗️  編譯 bot..."
go build -o alice ./cmd/alice

echo "✅ 啟動 bot..."
nohup ./alice >> alice.log 2>&1 &
BOT_PID=$!
sleep 2

# 驗證進程
if ps -p $BOT_PID > /dev/null; then
    echo "✨ Bot 已啟動 (PID: $BOT_PID)"
    ps aux | grep "^\./alice" | grep -v grep || true
else
    echo "❌ Bot 啟動失敗"
    tail -20 alice.log
    exit 1
fi

echo "🌐 Dashboard 應在 http://localhost:3939 可用"
