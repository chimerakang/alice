#!/bin/bash

# Alice Dashboard Startup Script
# 🎨 Launches Alice AI Agent Monitor with Dashboard

set -e

echo "🚀 Starting Alice AI Agent Monitor..."
echo "======================================"

# Check if config exists
if [ ! -f "config.json" ]; then
    if [ -f "config.test.json" ]; then
        echo "📝 Using test configuration..."
        cp config.test.json config.json
    else
        echo "⚠️  No configuration found. Please create config.json"
        echo "   You can copy from config.example.json"
        exit 1
    fi
fi

# Build the application
echo "🔨 Building Alice..."
if ! go build -o alice .; then
    echo "❌ Build failed!"
    exit 1
fi

echo "✅ Build successful!"

# Check if web directory exists
if [ ! -d "web" ]; then
    echo "❌ Web directory not found! Dashboard files missing."
    exit 1
fi

echo "🎨 Dashboard files found:"
echo "   • HTML: $(ls web/*.html | wc -l) files"
echo "   • CSS:  $(ls web/css/*.css 2>/dev/null | wc -l) files"
echo "   • JS:   $(ls web/js/*.js 2>/dev/null | wc -l) files"

# Start the server
echo ""
echo "🌟 Starting Alice AI Agent Monitor..."
echo "📱 Dashboard: http://localhost:8080"
echo "🔧 API Docs:  http://localhost:8080/api/health"
echo ""
echo "Press Ctrl+C to stop the server"
echo "======================================"

# Function to handle cleanup
cleanup() {
    echo ""
    echo "🛑 Shutting down Alice Monitor..."
    exit 0
}

# Set trap to catch interrupt
trap cleanup INT

# Start Alice
./alice