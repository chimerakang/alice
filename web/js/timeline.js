/**
 * Alice Timeline Component
 * 時間軸組件：顯示 AI 決策過程的縱向時序視圖
 */

class AliceTimeline {
    constructor(container, options = {}) {
        this.container = typeof container === 'string' ? document.querySelector(container) : container;
        this.options = {
            maxItems: options.maxItems || 50,
            autoScroll: options.autoScroll !== false, // 預設啟用自動滾動
            showFilters: options.showFilters !== false, // 預設顯示過濾器
            theme: options.theme || 'dark', // OLED dark theme
            updateInterval: options.updateInterval || 1000, // 1秒更新一次
            ...options
        };

        this.timelineData = [];
        this.filteredData = [];
        this.filters = {
            eventTypes: new Set(['all']), // 事件類型過濾
            chatIds: new Set(['all']), // 聊天 ID 過濾
            statusFilter: 'all', // 狀態過濾
            timeRange: '24h' // 時間範圍過濾
        };

        this.isAutoScrolling = true;
        this.wsClient = null;

        this.init();
    }

    init() {
        this.createTimelineStructure();
        this.setupEventListeners();
        this.initializeWebSocketConnection();
        this.startPeriodicUpdate();
    }

    createTimelineStructure() {
        if (!this.container) {
            console.error('Timeline container not found');
            return;
        }

        this.container.className = 'alice-timeline bg-dark-grey border border-gray-800 rounded-xl overflow-hidden';
        this.container.innerHTML = `
            <!-- Timeline Header -->
            <div class="timeline-header bg-dark-grey/90 backdrop-blur border-b border-gray-800 p-4">
                <div class="flex items-center justify-between mb-4">
                    <h3 class="text-lg font-semibold text-white flex items-center space-x-2">
                        <svg class="w-5 h-5 text-dashboard-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/>
                        </svg>
                        <span>AI Decision Timeline</span>
                    </h3>
                    <div class="flex items-center space-x-2">
                        <div class="timeline-status flex items-center space-x-2">
                            <div class="w-2 h-2 bg-status-success rounded-full animate-pulse-slow"></div>
                            <span class="text-xs text-gray-400 font-mono" id="timelineStatus">Live</span>
                        </div>
                        <button id="timelineAutoScroll" class="px-3 py-1 bg-dashboard-primary/10 text-dashboard-primary text-xs rounded-md hover:bg-dashboard-primary/20 transition-colors">
                            Auto Scroll
                        </button>
                        <button id="timelineClear" class="px-3 py-1 bg-gray-800/50 text-gray-400 text-xs rounded-md hover:bg-gray-800 hover:text-white transition-colors">
                            Clear
                        </button>
                    </div>
                </div>

                ${this.options.showFilters ? this.createFiltersHTML() : ''}
            </div>

            <!-- Timeline Content -->
            <div class="timeline-content relative" style="height: ${this.options.height || '600px'};">
                <!-- Vertical Timeline Line -->
                <div class="timeline-line absolute left-8 top-0 bottom-0 w-0.5 bg-gradient-to-b from-dashboard-primary via-dashboard-secondary to-transparent opacity-50"></div>

                <!-- Timeline Items Container -->
                <div id="timelineItems" class="timeline-items h-full overflow-y-auto custom-scrollbar p-4">
                    <div class="text-center py-12 text-gray-400">
                        <svg class="w-12 h-12 mx-auto mb-3 opacity-50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"/>
                        </svg>
                        <p class="text-sm">Waiting for AI events...</p>
                        <p class="text-xs text-gray-500 mt-2">Timeline will populate as AI agents make decisions</p>
                    </div>
                </div>

                <!-- Auto-scroll indicator -->
                <div id="autoScrollIndicator" class="absolute bottom-4 right-4 hidden">
                    <div class="bg-dashboard-primary/90 text-white text-xs px-3 py-1 rounded-full flex items-center space-x-1 shadow-lg">
                        <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 14l-7 7m0 0l-7-7m7 7V3"/>
                        </svg>
                        <span>Auto-scrolling</span>
                    </div>
                </div>
            </div>

            <!-- Timeline Footer -->
            <div class="timeline-footer bg-dark-grey/50 border-t border-gray-800 px-4 py-2">
                <div class="flex items-center justify-between text-xs text-gray-400">
                    <div class="flex items-center space-x-4">
                        <span>Total Events: <span id="totalEventsCount">0</span></span>
                        <span>Filtered: <span id="filteredEventsCount">0</span></span>
                    </div>
                    <div class="flex items-center space-x-2">
                        <span>Last Update:</span>
                        <span id="timelineLastUpdate" class="font-mono">Never</span>
                    </div>
                </div>
            </div>
        `;
    }

    createFiltersHTML() {
        return `
            <div class="timeline-filters grid grid-cols-1 md:grid-cols-4 gap-3">
                <!-- Event Type Filter -->
                <div class="filter-group">
                    <label class="text-xs text-gray-400 mb-1 block">Event Type</label>
                    <select id="eventTypeFilter" class="w-full bg-gray-800 border border-gray-700 text-white text-xs rounded px-2 py-1 focus:border-dashboard-primary focus:outline-none">
                        <option value="all">All Events</option>
                        <option value="tool_execution">Tool Executions</option>
                        <option value="decision_complete">Decisions</option>
                        <option value="performance_metric">Performance</option>
                        <option value="security_alert">Security</option>
                        <option value="agent_status">Agent Status</option>
                    </select>
                </div>

                <!-- Chat ID Filter -->
                <div class="filter-group">
                    <label class="text-xs text-gray-400 mb-1 block">Chat ID</label>
                    <select id="chatIdFilter" class="w-full bg-gray-800 border border-gray-700 text-white text-xs rounded px-2 py-1 focus:border-dashboard-primary focus:outline-none">
                        <option value="all">All Chats</option>
                    </select>
                </div>

                <!-- Status Filter -->
                <div class="filter-group">
                    <label class="text-xs text-gray-400 mb-1 block">Status</label>
                    <select id="statusFilter" class="w-full bg-gray-800 border border-gray-700 text-white text-xs rounded px-2 py-1 focus:border-dashboard-primary focus:outline-none">
                        <option value="all">All Status</option>
                        <option value="success">Success</option>
                        <option value="error">Error</option>
                        <option value="running">Running</option>
                        <option value="pending">Pending</option>
                    </select>
                </div>

                <!-- Time Range Filter -->
                <div class="filter-group">
                    <label class="text-xs text-gray-400 mb-1 block">Time Range</label>
                    <select id="timeRangeFilter" class="w-full bg-gray-800 border border-gray-700 text-white text-xs rounded px-2 py-1 focus:border-dashboard-primary focus:outline-none">
                        <option value="1h">Last Hour</option>
                        <option value="6h">Last 6 Hours</option>
                        <option value="24h" selected>Last 24 Hours</option>
                        <option value="7d">Last 7 Days</option>
                        <option value="all">All Time</option>
                    </select>
                </div>
            </div>
        `;
    }

    setupEventListeners() {
        // Auto-scroll toggle
        const autoScrollBtn = document.getElementById('timelineAutoScroll');
        autoScrollBtn?.addEventListener('click', () => {
            this.toggleAutoScroll();
        });

        // Clear timeline
        const clearBtn = document.getElementById('timelineClear');
        clearBtn?.addEventListener('click', () => {
            this.clearTimeline();
        });

        // Filter listeners
        document.getElementById('eventTypeFilter')?.addEventListener('change', (e) => {
            this.setFilter('eventTypes', new Set([e.target.value]));
        });

        document.getElementById('chatIdFilter')?.addEventListener('change', (e) => {
            this.setFilter('chatIds', new Set([e.target.value]));
        });

        document.getElementById('statusFilter')?.addEventListener('change', (e) => {
            this.setFilter('statusFilter', e.target.value);
        });

        document.getElementById('timeRangeFilter')?.addEventListener('change', (e) => {
            this.setFilter('timeRange', e.target.value);
        });

        // Timeline scroll detection (disable auto-scroll if user manually scrolls)
        const timelineItems = document.getElementById('timelineItems');
        timelineItems?.addEventListener('scroll', () => {
            this.handleUserScroll();
        });
    }

    initializeWebSocketConnection() {
        // 使用全局 WebSocket 客戶端或創建新的
        if (window.dashboard && window.dashboard.wsClient) {
            this.wsClient = window.dashboard.wsClient;
        } else {
            this.wsClient = new AliceWebSocketClient();
        }

        // 設置事件監聽器
        this.wsClient.on('tool_execution_start', (event) => {
            this.addTimelineEvent('tool_execution_start', event);
        });

        this.wsClient.on('tool_execution', (event) => {
            this.addTimelineEvent('tool_execution', event);
        });

        this.wsClient.on('decision_complete', (event) => {
            this.addTimelineEvent('decision_complete', event);
        });

        this.wsClient.on('performance_metric', (event) => {
            this.addTimelineEvent('performance_metric', event);
        });

        this.wsClient.on('security_alert', (event) => {
            this.addTimelineEvent('security_alert', event);
        });

        this.wsClient.on('agent_status', (event) => {
            this.addTimelineEvent('agent_status', event);
        });

        // 連接狀態監聽
        this.wsClient.on('connected', () => {
            this.updateConnectionStatus(true);
        });

        this.wsClient.on('disconnected', () => {
            this.updateConnectionStatus(false);
        });
    }

    addTimelineEvent(eventType, eventData) {
        const timelineEvent = {
            id: this.generateEventId(),
            type: eventType,
            timestamp: new Date(),
            data: eventData.data || eventData,
            processed: false
        };

        // 添加到數據陣列
        this.timelineData.unshift(timelineEvent);

        // 限制數據量
        if (this.timelineData.length > this.options.maxItems) {
            this.timelineData = this.timelineData.slice(0, this.options.maxItems);
        }

        // 更新過濾器選項
        this.updateFilterOptions();

        // 應用過濾器並更新顯示
        this.applyFiltersAndUpdate();

        // 標記為已處理
        timelineEvent.processed = true;

        console.log('Timeline event added:', eventType, timelineEvent);
    }

    applyFiltersAndUpdate() {
        this.filteredData = this.timelineData.filter(event => {
            // Event type filter
            if (!this.filters.eventTypes.has('all') && !this.filters.eventTypes.has(event.type)) {
                return false;
            }

            // Chat ID filter
            const chatId = this.extractChatId(event);
            if (!this.filters.chatIds.has('all') && chatId && !this.filters.chatIds.has(chatId.toString())) {
                return false;
            }

            // Status filter
            if (this.filters.statusFilter !== 'all') {
                const status = this.extractStatus(event);
                if (status !== this.filters.statusFilter) {
                    return false;
                }
            }

            // Time range filter
            if (this.filters.timeRange !== 'all') {
                const eventTime = event.timestamp;
                const now = new Date();
                const timeDiff = now - eventTime;

                let maxAge;
                switch (this.filters.timeRange) {
                    case '1h': maxAge = 60 * 60 * 1000; break;
                    case '6h': maxAge = 6 * 60 * 60 * 1000; break;
                    case '24h': maxAge = 24 * 60 * 60 * 1000; break;
                    case '7d': maxAge = 7 * 24 * 60 * 60 * 1000; break;
                    default: maxAge = Infinity;
                }

                if (timeDiff > maxAge) {
                    return false;
                }
            }

            return true;
        });

        this.updateTimelineDisplay();
        this.updateFooterStats();
    }

    updateTimelineDisplay() {
        const container = document.getElementById('timelineItems');
        if (!container) return;

        if (this.filteredData.length === 0) {
            container.innerHTML = `
                <div class="text-center py-12 text-gray-400">
                    <svg class="w-12 h-12 mx-auto mb-3 opacity-50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 4a1 1 0 011-1h16a1 1 0 011 1v2.586a1 1 0 01-.293.707l-6.414 6.414a1 1 0 00-.293.707V17l-4 4v-6.586a1 1 0 00-.293-.707L3.293 7.707A1 1 0 013 7V4z"/>
                    </svg>
                    <p class="text-sm">No events match your filters</p>
                    <p class="text-xs text-gray-500 mt-2">Try adjusting your filter settings</p>
                </div>
            `;
            return;
        }

        // 生成時間軸項目 HTML
        const timelineHTML = this.filteredData.map((event, index) => {
            return this.generateTimelineItemHTML(event, index);
        }).join('');

        container.innerHTML = timelineHTML;

        // 如果啟用自動滾動，滾動到底部
        if (this.isAutoScrolling) {
            setTimeout(() => {
                container.scrollTop = container.scrollHeight;
            }, 100);
        }
    }

    generateTimelineItemHTML(event, index) {
        const eventConfig = this.getEventConfig(event.type);
        const relativeTime = this.formatRelativeTime(event.timestamp);
        const absoluteTime = event.timestamp.toLocaleTimeString();

        // 提取事件特定數據
        const chatId = this.extractChatId(event);
        const status = this.extractStatus(event);
        const description = this.generateEventDescription(event);
        const details = this.generateEventDetails(event);

        return `
            <div class="timeline-item relative flex items-start space-x-4 mb-6 animate-fade-in" data-event-id="${event.id}">
                <!-- Timeline Node -->
                <div class="timeline-node relative z-10">
                    <div class="w-10 h-10 ${eventConfig.bgColor} ${eventConfig.borderColor} border-2 rounded-full flex items-center justify-center shadow-lg">
                        ${eventConfig.icon}
                    </div>
                    ${index < this.filteredData.length - 1 ? `
                        <div class="timeline-connector absolute top-10 left-1/2 transform -translate-x-px w-0.5 h-6 ${eventConfig.connectorColor}"></div>
                    ` : ''}
                </div>

                <!-- Timeline Content -->
                <div class="timeline-content flex-1 min-w-0">
                    <!-- Header -->
                    <div class="timeline-header flex items-start justify-between mb-2">
                        <div class="flex-1">
                            <h4 class="text-sm font-semibold text-white mb-1">${eventConfig.title}</h4>
                            <p class="text-xs ${eventConfig.textColor}">${description}</p>
                        </div>
                        <div class="timeline-meta text-right ml-4 flex-shrink-0">
                            <p class="text-xs text-gray-400" title="${absoluteTime}">${relativeTime}</p>
                            ${chatId ? `<p class="text-xs text-gray-500 font-mono">Chat ${chatId}</p>` : ''}
                            ${status ? `<span class="inline-block mt-1 px-2 py-0.5 ${this.getStatusStyle(status)} text-xs rounded-full">${status}</span>` : ''}
                        </div>
                    </div>

                    <!-- Details -->
                    ${details ? `
                        <div class="timeline-details bg-gray-800/30 border border-gray-700/50 rounded-lg p-3 mt-2">
                            ${details}
                        </div>
                    ` : ''}
                </div>
            </div>
        `;
    }

    getEventConfig(eventType) {
        const configs = {
            'tool_execution_start': {
                title: 'Tool Execution Started',
                icon: '<svg class="w-5 h-5 text-dashboard-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 100 4m0-4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 100 4m0-4v2m0-6V4"/></svg>',
                bgColor: 'bg-dashboard-primary/10',
                borderColor: 'border-dashboard-primary',
                textColor: 'text-dashboard-primary',
                connectorColor: 'bg-dashboard-primary/30'
            },
            'tool_execution': {
                title: 'Tool Execution Completed',
                icon: '<svg class="w-5 h-5 text-status-success" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>',
                bgColor: 'bg-status-success/10',
                borderColor: 'border-status-success',
                textColor: 'text-status-success',
                connectorColor: 'bg-status-success/30'
            },
            'decision_complete': {
                title: 'AI Decision Made',
                icon: '<svg class="w-5 h-5 text-dashboard-secondary" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.663 17h4.673M12 3v1m6.364 1.636l-.707.707M21 12h-1M4 12H3m3.343-5.657l-.707-.707m2.828 9.9a5 5 0 117.072 0l-.548.547A3.374 3.374 0 0014 18.469V19a2 2 0 11-4 0v-.531c0-.895-.356-1.754-.988-2.386l-.548-.547z"/></svg>',
                bgColor: 'bg-dashboard-secondary/10',
                borderColor: 'border-dashboard-secondary',
                textColor: 'text-dashboard-secondary',
                connectorColor: 'bg-dashboard-secondary/30'
            },
            'performance_metric': {
                title: 'Performance Metric',
                icon: '<svg class="w-5 h-5 text-dashboard-cta" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"/></svg>',
                bgColor: 'bg-dashboard-cta/10',
                borderColor: 'border-dashboard-cta',
                textColor: 'text-dashboard-cta',
                connectorColor: 'bg-dashboard-cta/30'
            },
            'security_alert': {
                title: 'Security Alert',
                icon: '<svg class="w-5 h-5 text-status-error" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"/></svg>',
                bgColor: 'bg-status-error/10',
                borderColor: 'border-status-error',
                textColor: 'text-status-error',
                connectorColor: 'bg-status-error/30'
            },
            'agent_status': {
                title: 'Agent Status Change',
                icon: '<svg class="w-5 h-5 text-purple-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z"/></svg>',
                bgColor: 'bg-purple-500/10',
                borderColor: 'border-purple-400',
                textColor: 'text-purple-400',
                connectorColor: 'bg-purple-400/30'
            }
        };

        return configs[eventType] || {
            title: 'Unknown Event',
            icon: '<svg class="w-5 h-5 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8.228 9c.549-1.165 2.03-2 3.772-2 2.21 0 4 1.343 4 3 0 1.4-1.278 2.575-3.006 2.907-.542.104-.994.54-.994 1.093m0 3h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>',
            bgColor: 'bg-gray-500/10',
            borderColor: 'border-gray-400',
            textColor: 'text-gray-400',
            connectorColor: 'bg-gray-400/30'
        };
    }

    generateEventDescription(event) {
        const data = event.data;

        switch (event.type) {
            case 'tool_execution_start':
                return `Started executing ${data.tool_name || 'tool'}`;

            case 'tool_execution':
                const duration = data.duration_ms ? ` (${data.duration_ms}ms)` : '';
                return `Completed ${data.tool_name || 'tool'} - ${data.status || 'unknown'}${duration}`;

            case 'decision_complete':
                const outcome = data.success ? 'successful' : 'failed';
                const taskType = data.task_type || 'task';
                return `${outcome} ${taskType} decision in ${data.duration_ms || 0}ms`;

            case 'performance_metric':
                const latency = data.api_latency_ms || 0;
                const toolType = data.tool_execution_type || 'unknown';
                return `${toolType} performance: ${latency}ms API latency`;

            case 'security_alert':
                const severity = data.severity || 'unknown';
                const eventType = data.event_type || 'alert';
                return `${severity.toUpperCase()} - ${eventType}`;

            case 'agent_status':
                return `Agent status changed to: ${data.status || 'unknown'}`;

            default:
                return 'Unknown event occurred';
        }
    }

    generateEventDetails(event) {
        const data = event.data;

        switch (event.type) {
            case 'tool_execution':
                if (data.error) {
                    return `<div class="text-xs text-status-error"><strong>Error:</strong> ${data.error}</div>`;
                }
                break;

            case 'decision_complete':
                const tokenInfo = data.tokens_input && data.tokens_output
                    ? `<div class="text-xs text-gray-400">Tokens: ${data.tokens_input} in, ${data.tokens_output} out</div>`
                    : '';
                return tokenInfo;

            case 'performance_metric':
                return `
                    <div class="grid grid-cols-2 gap-2 text-xs text-gray-300">
                        <div>Memory: ${this.formatBytes(data.memory_usage || 0)}</div>
                        <div>Tokens: ${data.tokens_used || 0}</div>
                    </div>
                `;

            case 'security_alert':
                return `
                    <div class="text-xs text-gray-300">
                        <div><strong>Description:</strong> ${data.description || 'N/A'}</div>
                        ${data.ip ? `<div><strong>IP:</strong> ${data.ip}</div>` : ''}
                        ${data.user_id ? `<div><strong>User:</strong> ${data.user_id}</div>` : ''}
                    </div>
                `;
        }

        return null;
    }

    // Utility Methods
    extractChatId(event) {
        return event.data.chat_id || null;
    }

    extractStatus(event) {
        return event.data.status || (event.data.success !== undefined ? (event.data.success ? 'success' : 'error') : null);
    }

    getStatusStyle(status) {
        const styles = {
            'success': 'bg-status-success/10 text-status-success border border-status-success/20',
            'error': 'bg-status-error/10 text-status-error border border-status-error/20',
            'running': 'bg-status-warning/10 text-status-warning border border-status-warning/20',
            'pending': 'bg-status-info/10 text-status-info border border-status-info/20'
        };
        return styles[status] || 'bg-gray-500/10 text-gray-400 border border-gray-500/20';
    }

    formatRelativeTime(timestamp) {
        const now = new Date();
        const diff = now - timestamp;

        if (diff < 1000) return 'just now';
        if (diff < 60000) return `${Math.floor(diff / 1000)}s ago`;
        if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
        if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`;
        return `${Math.floor(diff / 86400000)}d ago`;
    }

    formatBytes(bytes) {
        if (bytes === 0) return '0 B';
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
    }

    generateEventId() {
        return Date.now().toString(36) + Math.random().toString(36).substr(2);
    }

    // Control Methods
    toggleAutoScroll() {
        this.isAutoScrolling = !this.isAutoScrolling;
        const btn = document.getElementById('timelineAutoScroll');
        const indicator = document.getElementById('autoScrollIndicator');

        if (btn) {
            btn.textContent = this.isAutoScrolling ? 'Auto Scroll' : 'Manual';
            btn.className = this.isAutoScrolling
                ? 'px-3 py-1 bg-dashboard-primary/10 text-dashboard-primary text-xs rounded-md hover:bg-dashboard-primary/20 transition-colors'
                : 'px-3 py-1 bg-gray-800/50 text-gray-400 text-xs rounded-md hover:bg-gray-800 hover:text-white transition-colors';
        }

        if (indicator) {
            indicator.classList.toggle('hidden', !this.isAutoScrolling);
        }

        if (this.isAutoScrolling) {
            // 立即滾動到底部
            const container = document.getElementById('timelineItems');
            if (container) {
                container.scrollTop = container.scrollHeight;
            }
        }
    }

    clearTimeline() {
        this.timelineData = [];
        this.filteredData = [];
        this.updateTimelineDisplay();
        this.updateFooterStats();
    }

    setFilter(filterName, value) {
        this.filters[filterName] = value;
        this.applyFiltersAndUpdate();
    }

    updateFilterOptions() {
        // 更新聊天 ID 選項
        const chatIds = new Set();
        this.timelineData.forEach(event => {
            const chatId = this.extractChatId(event);
            if (chatId) chatIds.add(chatId.toString());
        });

        const chatIdSelect = document.getElementById('chatIdFilter');
        if (chatIdSelect) {
            const currentValue = chatIdSelect.value;
            chatIdSelect.innerHTML = '<option value="all">All Chats</option>';

            Array.from(chatIds).sort().forEach(chatId => {
                const option = document.createElement('option');
                option.value = chatId;
                option.textContent = `Chat ${chatId}`;
                chatIdSelect.appendChild(option);
            });

            // 恢復之前的選擇
            if (chatIds.has(currentValue)) {
                chatIdSelect.value = currentValue;
            }
        }
    }

    updateFooterStats() {
        const totalElement = document.getElementById('totalEventsCount');
        const filteredElement = document.getElementById('filteredEventsCount');
        const lastUpdateElement = document.getElementById('timelineLastUpdate');

        if (totalElement) totalElement.textContent = this.timelineData.length;
        if (filteredElement) filteredElement.textContent = this.filteredData.length;
        if (lastUpdateElement) lastUpdateElement.textContent = new Date().toLocaleTimeString();
    }

    updateConnectionStatus(connected) {
        const statusElement = document.getElementById('timelineStatus');
        if (statusElement) {
            statusElement.textContent = connected ? 'Live' : 'Disconnected';
            statusElement.className = connected
                ? 'text-xs text-status-success font-mono'
                : 'text-xs text-status-error font-mono';
        }
    }

    handleUserScroll() {
        const container = document.getElementById('timelineItems');
        if (!container) return;

        // 如果用戶滾動到接近底部，重新啟用自動滾動
        const isNearBottom = container.scrollTop + container.clientHeight >= container.scrollHeight - 50;

        if (!isNearBottom && this.isAutoScrolling) {
            // 用戶手動滾動離開底部，暫時停用自動滾動
            const indicator = document.getElementById('autoScrollIndicator');
            if (indicator) indicator.classList.add('hidden');
        } else if (isNearBottom && this.isAutoScrolling) {
            // 用戶滾動回到底部，恢復自動滾動指示器
            const indicator = document.getElementById('autoScrollIndicator');
            if (indicator) indicator.classList.remove('hidden');
        }
    }

    startPeriodicUpdate() {
        setInterval(() => {
            this.updateFooterStats();
            // 更新相對時間顯示
            this.updateRelativeTimes();
        }, this.options.updateInterval);
    }

    updateRelativeTimes() {
        const timelineItems = document.querySelectorAll('.timeline-item');
        timelineItems.forEach((item, index) => {
            if (index < this.filteredData.length) {
                const event = this.filteredData[index];
                const timeElement = item.querySelector('.timeline-meta p:first-child');
                if (timeElement) {
                    timeElement.textContent = this.formatRelativeTime(event.timestamp);
                }
            }
        });
    }

    // Public API
    destroy() {
        if (this.wsClient) {
            // 移除事件監聽器
            this.wsClient.off('tool_execution_start');
            this.wsClient.off('tool_execution');
            this.wsClient.off('decision_complete');
            this.wsClient.off('performance_metric');
            this.wsClient.off('security_alert');
            this.wsClient.off('agent_status');
        }

        this.container.innerHTML = '';
    }

    getEventCount() {
        return {
            total: this.timelineData.length,
            filtered: this.filteredData.length
        };
    }

    exportTimeline() {
        return {
            events: this.timelineData,
            filters: this.filters,
            exported_at: new Date().toISOString()
        };
    }
}

// 全局註冊
window.AliceTimeline = AliceTimeline;