/**
 * Alice Terminal Component
 * 終端機模擬器：顯示 CLI 輸出、工具執行結果和系統訊息
 */

class AliceTerminal {
    constructor(container, options = {}) {
        this.container = typeof container === 'string' ? document.querySelector(container) : container;
        this.options = {
            maxLines: options.maxLines || 1000,
            autoScroll: options.autoScroll !== false,
            showTimestamps: options.showTimestamps !== false,
            theme: options.theme || 'dark',
            fontSize: options.fontSize || '12px',
            fontFamily: options.fontFamily || 'Fira Code, monospace',
            prompt: options.prompt || 'alice@ai-agent:~$',
            height: options.height || '400px',
            ...options
        };

        this.lines = [];
        this.isAutoScrolling = true;
        this.wsClient = null;
        this.filters = {
            showInfo: true,
            showWarning: true,
            showError: true,
            showSuccess: true,
            showDebug: false
        };

        this.init();
    }

    init() {
        this.createTerminalStructure();
        this.setupEventListeners();
        this.initializeWebSocketConnection();
        this.addWelcomeMessage();
    }

    createTerminalStructure() {
        if (!this.container) {
            console.error('Terminal container not found');
            return;
        }

        this.container.className = 'alice-terminal bg-black border border-gray-800 rounded-xl overflow-hidden font-mono';
        this.container.innerHTML = `
            <!-- Terminal Header -->
            <div class="terminal-header bg-gray-900 border-b border-gray-800 p-3">
                <div class="flex items-center justify-between">
                    <div class="flex items-center space-x-4">
                        <div class="flex items-center space-x-2">
                            <!-- Terminal Window Controls -->
                            <div class="flex space-x-1">
                                <div class="w-3 h-3 bg-red-500 rounded-full"></div>
                                <div class="w-3 h-3 bg-yellow-500 rounded-full"></div>
                                <div class="w-3 h-3 bg-green-500 rounded-full"></div>
                            </div>
                            <span class="text-gray-400 text-sm font-bold ml-2">Terminal</span>
                        </div>
                        <div class="text-xs text-gray-500 font-mono">
                            ${this.options.prompt}
                        </div>
                    </div>

                    <!-- Terminal Controls -->
                    <div class="flex items-center space-x-2">
                        <!-- Filter Toggle -->
                        <div class="flex items-center space-x-1">
                            <button id="terminalFilterToggle" class="text-gray-400 hover:text-white text-xs px-2 py-1 rounded transition-colors" title="Toggle Filters">
                                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 4a1 1 0 011-1h16a1 1 0 011 1v2.586a1 1 0 01-.293.707l-6.414 6.414a1 1 0 00-.293.707V17l-4 4v-6.586a1 1 0 00-.293-.707L3.293 7.707A1 1 0 013 7V4z"/>
                                </svg>
                            </button>
                        </div>

                        <!-- Auto-scroll Toggle -->
                        <button id="terminalAutoScroll" class="text-green-400 text-xs px-2 py-1 bg-green-400/10 rounded hover:bg-green-400/20 transition-colors">
                            Auto
                        </button>

                        <!-- Clear Terminal -->
                        <button id="terminalClear" class="text-gray-400 hover:text-white text-xs px-2 py-1 rounded transition-colors" title="Clear Terminal">
                            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1-1H8a1 1 0 00-1 1v3M4 7h16"/>
                            </svg>
                        </button>

                        <!-- Copy All -->
                        <button id="terminalCopy" class="text-gray-400 hover:text-white text-xs px-2 py-1 rounded transition-colors" title="Copy All">
                            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"/>
                            </svg>
                        </button>
                    </div>
                </div>

                <!-- Filter Controls (Hidden by default) -->
                <div id="terminalFilters" class="mt-3 pt-3 border-t border-gray-700 hidden">
                    <div class="flex items-center space-x-4 text-xs">
                        <span class="text-gray-400">Show:</span>
                        <label class="flex items-center space-x-1">
                            <input type="checkbox" id="showInfo" class="text-blue-400 bg-gray-800 border-gray-600 rounded" checked>
                            <span class="text-blue-400">Info</span>
                        </label>
                        <label class="flex items-center space-x-1">
                            <input type="checkbox" id="showWarning" class="text-yellow-400 bg-gray-800 border-gray-600 rounded" checked>
                            <span class="text-yellow-400">Warning</span>
                        </label>
                        <label class="flex items-center space-x-1">
                            <input type="checkbox" id="showError" class="text-red-400 bg-gray-800 border-gray-600 rounded" checked>
                            <span class="text-red-400">Error</span>
                        </label>
                        <label class="flex items-center space-x-1">
                            <input type="checkbox" id="showSuccess" class="text-green-400 bg-gray-800 border-gray-600 rounded" checked>
                            <span class="text-green-400">Success</span>
                        </label>
                        <label class="flex items-center space-x-1">
                            <input type="checkbox" id="showDebug" class="text-gray-500 bg-gray-800 border-gray-600 rounded">
                            <span class="text-gray-500">Debug</span>
                        </label>
                    </div>
                </div>
            </div>

            <!-- Terminal Content -->
            <div id="terminalContent" class="terminal-content p-4 font-mono text-sm leading-relaxed overflow-y-auto bg-black text-green-400" style="height: ${this.options.height};">
                <!-- Terminal lines will be inserted here -->
            </div>

            <!-- Terminal Status Bar -->
            <div class="terminal-status bg-gray-900 border-t border-gray-800 px-3 py-2">
                <div class="flex items-center justify-between text-xs text-gray-400">
                    <div class="flex items-center space-x-4">
                        <span>Lines: <span id="terminalLineCount">0</span></span>
                        <span class="flex items-center space-x-1">
                            <div class="w-2 h-2 bg-green-400 rounded-full animate-pulse" id="terminalStatusIndicator"></div>
                            <span id="terminalStatusText">Ready</span>
                        </span>
                    </div>
                    <div class="flex items-center space-x-2">
                        <span id="terminalLastUpdate">Never</span>
                    </div>
                </div>
            </div>
        `;
    }

    setupEventListeners() {
        // Auto-scroll toggle
        const autoScrollBtn = document.getElementById('terminalAutoScroll');
        autoScrollBtn?.addEventListener('click', () => {
            this.toggleAutoScroll();
        });

        // Clear terminal
        const clearBtn = document.getElementById('terminalClear');
        clearBtn?.addEventListener('click', () => {
            this.clear();
        });

        // Copy all content
        const copyBtn = document.getElementById('terminalCopy');
        copyBtn?.addEventListener('click', () => {
            this.copyAll();
        });

        // Filter toggle
        const filterToggleBtn = document.getElementById('terminalFilterToggle');
        filterToggleBtn?.addEventListener('click', () => {
            this.toggleFilters();
        });

        // Filter checkboxes
        ['Info', 'Warning', 'Error', 'Success', 'Debug'].forEach(type => {
            const checkbox = document.getElementById(`show${type}`);
            checkbox?.addEventListener('change', (e) => {
                this.filters[`show${type}`] = e.target.checked;
                this.applyFilters();
            });
        });

        // Terminal scroll detection
        const content = document.getElementById('terminalContent');
        content?.addEventListener('scroll', () => {
            this.handleUserScroll();
        });
    }

    initializeWebSocketConnection() {
        // Use global WebSocket client or create new one
        if (window.dashboard && window.dashboard.wsClient) {
            this.wsClient = window.dashboard.wsClient;
        } else {
            this.wsClient = new AliceWebSocketClient();
        }

        // Set up event listeners for terminal-relevant events
        this.wsClient.on('tool_execution_start', (event) => {
            this.handleToolStart(event.data);
        });

        this.wsClient.on('tool_execution', (event) => {
            this.handleToolComplete(event.data);
        });

        this.wsClient.on('decision_complete', (event) => {
            this.handleDecisionComplete(event.data);
        });

        this.wsClient.on('security_alert', (event) => {
            this.handleSecurityAlert(event.data);
        });

        this.wsClient.on('connected', () => {
            this.addSystemMessage('WebSocket connected', 'success');
            this.updateStatus('Connected', 'success');
        });

        this.wsClient.on('disconnected', () => {
            this.addSystemMessage('WebSocket disconnected', 'warning');
            this.updateStatus('Disconnected', 'warning');
        });
    }

    addWelcomeMessage() {
        this.addSystemMessage('Alice AI Terminal v1.0 initialized', 'info');
        this.addSystemMessage('Waiting for AI agent activity...', 'info');
        this.addLine('', { type: 'prompt', showCursor: true });
    }

    // Event Handlers
    handleToolStart(data) {
        const timestamp = this.formatTimestamp();
        const message = `Starting tool execution: ${data.tool_name}`;
        this.addLine(`${timestamp} ${message}`, {
            type: 'info',
            icon: '🔧',
            metadata: { chatId: data.chat_id, toolName: data.tool_name }
        });
    }

    handleToolComplete(data) {
        const timestamp = this.formatTimestamp();
        const duration = data.duration_ms ? ` (${data.duration_ms}ms)` : '';
        const status = data.status === 'success' ? 'success' : 'error';
        const icon = data.status === 'success' ? '✅' : '❌';

        let message = `Tool ${data.tool_name} completed: ${data.status.toUpperCase()}${duration}`;

        this.addLine(`${timestamp} ${message}`, {
            type: status,
            icon: icon,
            metadata: { chatId: data.chat_id, toolName: data.tool_name, duration: data.duration_ms }
        });

        // Show error details if present
        if (data.error) {
            this.addLine(`  └─ Error: ${data.error}`, {
                type: 'error',
                indent: true
            });
        }

        // Simulate some tool output based on tool type
        this.simulateToolOutput(data.tool_name, data.status === 'success');
    }

    handleDecisionComplete(data) {
        const timestamp = this.formatTimestamp();
        const status = data.success ? 'success' : 'warning';
        const icon = data.success ? '🧠' : '⚠️';
        const duration = data.duration_ms ? ` (${data.duration_ms}ms)` : '';

        const message = `AI Decision: ${data.task_type || 'task'} - ${data.success ? 'SUCCESS' : 'PARTIAL'}${duration}`;

        this.addLine(`${timestamp} ${message}`, {
            type: status,
            icon: icon,
            metadata: {
                chatId: data.chat_id,
                taskType: data.task_type,
                duration: data.duration_ms,
                tokens: {
                    input: data.tokens_input,
                    output: data.tokens_output
                }
            }
        });

        // Show token usage details
        if (data.tokens_input && data.tokens_output) {
            this.addLine(`  └─ Tokens: ${data.tokens_input} in, ${data.tokens_output} out`, {
                type: 'debug',
                indent: true
            });
        }
    }

    handleSecurityAlert(data) {
        const timestamp = this.formatTimestamp();
        const severityMap = {
            'low': { type: 'info', icon: 'ℹ️' },
            'medium': { type: 'warning', icon: '⚠️' },
            'high': { type: 'error', icon: '🚨' },
            'critical': { type: 'error', icon: '🚨' }
        };

        const config = severityMap[data.severity] || severityMap['medium'];
        const message = `SECURITY ALERT [${data.severity.toUpperCase()}]: ${data.event_type} - ${data.description}`;

        this.addLine(`${timestamp} ${message}`, {
            type: config.type,
            icon: config.icon,
            metadata: {
                severity: data.severity,
                eventType: data.event_type,
                ip: data.ip,
                userId: data.user_id
            }
        });

        // Show additional security details
        if (data.ip) {
            this.addLine(`  └─ Source IP: ${data.ip}`, { type: 'debug', indent: true });
        }
        if (data.user_id) {
            this.addLine(`  └─ User ID: ${data.user_id}`, { type: 'debug', indent: true });
        }
    }

    // Core Terminal Functions
    addLine(content, options = {}) {
        const line = {
            id: this.generateLineId(),
            content: content,
            type: options.type || 'info',
            timestamp: new Date(),
            icon: options.icon || null,
            indent: options.indent || false,
            metadata: options.metadata || {},
            showCursor: options.showCursor || false
        };

        this.lines.push(line);

        // Keep only the last N lines
        if (this.lines.length > this.options.maxLines) {
            this.lines = this.lines.slice(-this.options.maxLines);
        }

        this.renderLine(line);
        this.updateStats();

        if (this.isAutoScrolling) {
            this.scrollToBottom();
        }
    }

    addSystemMessage(message, type = 'info') {
        const timestamp = this.formatTimestamp();
        const icons = {
            'info': 'ℹ️',
            'success': '✅',
            'warning': '⚠️',
            'error': '❌',
            'debug': '🐛'
        };

        this.addLine(`${timestamp} SYSTEM: ${message}`, {
            type: type,
            icon: icons[type]
        });
    }

    renderLine(line) {
        const content = document.getElementById('terminalContent');
        if (!content) return;

        const lineElement = this.createLineElement(line);
        content.appendChild(lineElement);

        // Apply current filters
        if (!this.shouldShowLine(line)) {
            lineElement.style.display = 'none';
        }
    }

    createLineElement(line) {
        const div = document.createElement('div');
        div.className = `terminal-line terminal-${line.type}`;
        div.setAttribute('data-line-id', line.id);
        div.setAttribute('data-line-type', line.type);

        // Color classes based on type
        const typeClasses = {
            'info': 'text-blue-400',
            'success': 'text-green-400',
            'warning': 'text-yellow-400',
            'error': 'text-red-400',
            'debug': 'text-gray-500',
            'prompt': 'text-green-400'
        };

        const colorClass = typeClasses[line.type] || 'text-gray-400';

        let html = '';

        // Add indentation if specified
        if (line.indent) {
            html += '<span class="text-gray-600">  </span>';
        }

        // Add icon if present
        if (line.icon) {
            html += `<span class="terminal-icon mr-1">${line.icon}</span>`;
        }

        // Add content
        html += `<span class="${colorClass}">${this.escapeHtml(line.content)}</span>`;

        // Add cursor for prompt lines
        if (line.showCursor) {
            html += '<span class="terminal-cursor animate-pulse ml-1">█</span>';
        }

        div.innerHTML = html;
        return div;
    }

    simulateToolOutput(toolName, success) {
        // Add some realistic tool output simulation
        const outputs = {
            'Read': [
                '  Reading file: /path/to/file.txt',
                success ? '  File read successfully (1.2KB)' : '  ERROR: File not found'
            ],
            'Write': [
                '  Writing to file: /path/to/output.txt',
                success ? '  File written successfully (2.5KB)' : '  ERROR: Permission denied'
            ],
            'Bash': [
                '  Executing command: ls -la',
                success ? '  Command completed with exit code 0' : '  Command failed with exit code 1'
            ],
            'Grep': [
                '  Searching for pattern in files...',
                success ? '  Found 3 matches in 2 files' : '  No matches found'
            ],
            'Edit': [
                '  Applying edit to file...',
                success ? '  Edit applied successfully' : '  ERROR: Pattern not found'
            ]
        };

        const output = outputs[toolName] || ['  Tool executed'];
        output.forEach(line => {
            this.addLine(line, { type: 'debug', indent: true });
        });
    }

    // Utility Functions
    formatTimestamp() {
        return this.options.showTimestamps
            ? `[${new Date().toLocaleTimeString()}]`
            : '';
    }

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    generateLineId() {
        return Date.now().toString(36) + Math.random().toString(36).substr(2);
    }

    shouldShowLine(line) {
        const filterMap = {
            'info': this.filters.showInfo,
            'success': this.filters.showSuccess,
            'warning': this.filters.showWarning,
            'error': this.filters.showError,
            'debug': this.filters.showDebug
        };

        return filterMap[line.type] !== false;
    }

    // Control Functions
    toggleAutoScroll() {
        this.isAutoScrolling = !this.isAutoScrolling;
        const btn = document.getElementById('terminalAutoScroll');

        if (btn) {
            if (this.isAutoScrolling) {
                btn.textContent = 'Auto';
                btn.className = 'text-green-400 text-xs px-2 py-1 bg-green-400/10 rounded hover:bg-green-400/20 transition-colors';
                this.scrollToBottom();
            } else {
                btn.textContent = 'Manual';
                btn.className = 'text-gray-400 text-xs px-2 py-1 bg-gray-800/50 rounded hover:bg-gray-800 hover:text-white transition-colors';
            }
        }
    }

    toggleFilters() {
        const filtersDiv = document.getElementById('terminalFilters');
        if (filtersDiv) {
            filtersDiv.classList.toggle('hidden');
        }
    }

    applyFilters() {
        const content = document.getElementById('terminalContent');
        if (!content) return;

        const lines = content.querySelectorAll('.terminal-line');
        lines.forEach(lineElement => {
            const lineId = lineElement.getAttribute('data-line-id');
            const line = this.lines.find(l => l.id === lineId);

            if (line && !this.shouldShowLine(line)) {
                lineElement.style.display = 'none';
            } else {
                lineElement.style.display = 'block';
            }
        });
    }

    clear() {
        this.lines = [];
        const content = document.getElementById('terminalContent');
        if (content) {
            content.innerHTML = '';
        }
        this.updateStats();
        this.addWelcomeMessage();
    }

    copyAll() {
        const textContent = this.lines
            .filter(line => this.shouldShowLine(line))
            .map(line => `${this.formatTimestamp()} ${line.content}`)
            .join('\n');

        if (navigator.clipboard) {
            navigator.clipboard.writeText(textContent).then(() => {
                this.addSystemMessage('Terminal content copied to clipboard', 'success');
            }).catch(() => {
                this.addSystemMessage('Failed to copy content', 'error');
            });
        } else {
            // Fallback for older browsers
            const textArea = document.createElement('textarea');
            textArea.value = textContent;
            document.body.appendChild(textArea);
            textArea.select();
            document.execCommand('copy');
            document.body.removeChild(textArea);
            this.addSystemMessage('Terminal content copied', 'success');
        }
    }

    scrollToBottom() {
        const content = document.getElementById('terminalContent');
        if (content) {
            content.scrollTop = content.scrollHeight;
        }
    }

    handleUserScroll() {
        const content = document.getElementById('terminalContent');
        if (!content) return;

        // Check if user has scrolled away from bottom
        const isNearBottom = content.scrollTop + content.clientHeight >= content.scrollHeight - 50;

        if (!isNearBottom && this.isAutoScrolling) {
            // User scrolled up manually, temporarily disable auto-scroll
            // We don't change the button state, just stop scrolling
        } else if (isNearBottom && this.isAutoScrolling) {
            // User is back at bottom, resume auto-scrolling
        }
    }

    updateStats() {
        const lineCountElement = document.getElementById('terminalLineCount');
        if (lineCountElement) {
            lineCountElement.textContent = this.lines.length;
        }

        const lastUpdateElement = document.getElementById('terminalLastUpdate');
        if (lastUpdateElement) {
            lastUpdateElement.textContent = new Date().toLocaleTimeString();
        }
    }

    updateStatus(text, type = 'info') {
        const statusIndicator = document.getElementById('terminalStatusIndicator');
        const statusText = document.getElementById('terminalStatusText');

        if (statusIndicator && statusText) {
            statusText.textContent = text;

            // Update indicator color based on status
            statusIndicator.className = 'w-2 h-2 rounded-full animate-pulse';
            switch (type) {
                case 'success':
                    statusIndicator.classList.add('bg-green-400');
                    break;
                case 'warning':
                    statusIndicator.classList.add('bg-yellow-400');
                    break;
                case 'error':
                    statusIndicator.classList.add('bg-red-400');
                    break;
                default:
                    statusIndicator.classList.add('bg-blue-400');
            }
        }
    }

    // Public API
    destroy() {
        if (this.wsClient) {
            // Remove event listeners
            this.wsClient.off('tool_execution_start');
            this.wsClient.off('tool_execution');
            this.wsClient.off('decision_complete');
            this.wsClient.off('security_alert');
        }

        this.container.innerHTML = '';
        this.lines = [];
    }

    getLines() {
        return this.lines;
    }

    addCustomLine(content, type = 'info', icon = null) {
        this.addLine(content, { type, icon });
    }

    setFilters(filters) {
        Object.assign(this.filters, filters);
        this.applyFilters();
    }

    exportLog() {
        return {
            lines: this.lines,
            exported_at: new Date().toISOString(),
            total_lines: this.lines.length
        };
    }
}

// Global registration
window.AliceTerminal = AliceTerminal;