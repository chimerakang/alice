/**
 * Alice Dashboard Controller
 * Main dashboard logic and data management
 */

class AliceDashboard {
    constructor() {
        this.isLoading = false;
        this.refreshInterval = null;
        this.lastUpdate = null;
        this.cache = new Map();
        this.updateFrequency = 30000; // 30 seconds (reduced since we have real-time updates)

        // Initialize WebSocket client
        this.wsClient = null;
        this.realtimeData = {
            tools: [],
            decisions: [],
            performance: [],
            security: [],
            agents: new Map()
        };

        this.initializeWebSocket();
        this.initializeEventListeners();
        this.startAutoRefresh();
    }

    initializeWebSocket() {
        this.wsClient = new AliceWebSocketClient();

        // Set up real-time event handlers
        this.wsClient.on('tool_execution_start', (event) => {
            this.handleToolExecutionStart(event.data);
        });

        this.wsClient.on('tool_execution', (event) => {
            this.handleToolExecutionComplete(event.data);
        });

        this.wsClient.on('decision_complete', (event) => {
            this.handleDecisionComplete(event.data);
        });

        this.wsClient.on('performance_metric', (event) => {
            this.handlePerformanceMetric(event.data);
        });

        this.wsClient.on('security_alert', (event) => {
            this.handleSecurityAlert(event.data);
        });

        this.wsClient.on('agent_status', (event) => {
            this.handleAgentStatusChange(event.data);
        });

        this.wsClient.on('connected', () => {
            console.log('Dashboard connected to WebSocket');
            this.showNotification('Connected to real-time events', 'success');
        });

        this.wsClient.on('disconnected', () => {
            console.log('Dashboard disconnected from WebSocket');
            this.showNotification('Disconnected from real-time events', 'warning');
        });
    }

    initializeEventListeners() {
        // Refresh button
        document.getElementById('refreshBtn')?.addEventListener('click', () => {
            this.forceRefresh();
        });

        // Escape key to force refresh
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                this.forceRefresh();
            }
        });

        // Visibility API for pausing when tab is hidden
        document.addEventListener('visibilitychange', () => {
            if (document.hidden) {
                this.stopAutoRefresh();
            } else {
                this.startAutoRefresh();
                this.refresh();
            }
        });

        // Window focus/blur
        window.addEventListener('focus', () => this.refresh());

        // Git refresh button
        document.getElementById('refreshGitBtn')?.addEventListener('click', () => {
            this.refreshGitInfo();
        });
    }

    async loadDashboard() {
        if (this.isLoading) return;

        this.isLoading = true;
        this.showLoading(true);

        try {
            // Load all data in parallel for better performance
            const [
                agentsData,
                toolsData,
                decisionsData,
                toolStatsData,
                multiAgentStatus,
                gitStatusData,
                gitEventsData
            ] = await Promise.all([
                this.loadAgents(),
                this.loadTools(),
                this.loadDecisions(),
                this.loadToolStats(),
                this.loadMultiAgentStatus(),
                this.loadGitStatus(),
                this.loadGitEvents()
            ]);

            // Update metrics
            this.updateMetrics(agentsData, toolsData, decisionsData, toolStatsData);

            // Update lists
            this.updateAgentsList(agentsData);
            this.updateToolsList(toolsData);
            this.updateDecisionsList(decisionsData);
            this.updateGitInfo(gitStatusData, gitEventsData);

            // Update charts
            window.chartManager?.updateCharts({
                agents: agentsData,
                tools: toolsData,
                decisions: decisionsData,
                toolStats: toolStatsData
            });

            this.lastUpdate = new Date();
            this.updateLastUpdatedTime();

        } catch (error) {
            console.error('Dashboard load error:', error);
            this.showError('Failed to load dashboard data');
        } finally {
            this.isLoading = false;
            this.showLoading(false);
        }
    }

    async loadAgents() {
        try {
            const response = await window.aliceApi.listAgents();
            return response.agents || [];
        } catch (error) {
            console.warn('Failed to load agents:', error);
            return [];
        }
    }

    async loadTools() {
        try {
            const response = await window.aliceApi.getRecentTools(20);
            return response.executions || [];
        } catch (error) {
            console.warn('Failed to load tools:', error);
            return [];
        }
    }

    async loadDecisions() {
        try {
            const response = await window.aliceApi.getRecentDecisions(10);
            return response.decisions || [];
        } catch (error) {
            console.warn('Failed to load decisions:', error);
            return [];
        }
    }

    async loadToolStats() {
        try {
            const response = await window.aliceApi.getToolExecutions();
            return response;
        } catch (error) {
            console.warn('Failed to load tool stats:', error);
            return { total_executions: 0, success_rate: 0, tool_counts: {} };
        }
    }

    async loadMultiAgentStatus() {
        try {
            return await window.aliceApi.getMultiAgentStatus();
        } catch (error) {
            console.warn('Failed to load multi-agent status:', error);
            return { coordinator_status: 'unknown', active_tasks: 0 };
        }
    }

    async loadGitStatus() {
        try {
            return await window.aliceApi.getGitStatus();
        } catch (error) {
            console.warn('Failed to load Git status:', error);
            return { git_state: null };
        }
    }

    async loadGitEvents() {
        try {
            return await window.aliceApi.getGitEvents(10);
        } catch (error) {
            console.warn('Failed to load Git events:', error);
            return { events: [] };
        }
    }

    updateMetrics(agents, tools, decisions, toolStats) {
        // Count active agents
        const activeAgents = agents.filter(agent =>
            agent.status === 'STATUS_RUNNING' || agent.status === 'STATUS_PENDING'
        ).length;

        // Calculate total tokens
        const totalTokens = agents.reduce((sum, agent) => {
            if (agent.token_stats) {
                return sum + (agent.token_stats.total_input_tokens || 0) +
                       (agent.token_stats.total_output_tokens || 0);
            }
            return sum;
        }, 0);

        // Animate metric updates
        this.animateMetricUpdate('activeAgents', activeAgents);
        this.animateMetricUpdate('toolExecutions', toolStats.total_executions || tools.length);
        this.animateMetricUpdate('decisionsLogged', decisions.length);
        this.animateMetricUpdate('tokensUsed', totalTokens);
    }

    animateMetricUpdate(elementId, newValue) {
        const element = document.getElementById(elementId);
        if (!element) return;

        const oldValue = parseInt(element.textContent.replace(/,/g, '')) || 0;
        if (oldValue !== newValue) {
            animateNumber(element, oldValue, newValue, 800);
        }
    }

    updateAgentsList(agents) {
        const container = document.getElementById('agentsList');
        if (!container) return;

        if (agents.length === 0) {
            container.innerHTML = `
                <div class="text-center py-8 text-gray-400">
                    <svg class="w-12 h-12 mx-auto mb-3 opacity-50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4"/>
                    </svg>
                    <p class="text-sm">No active agents</p>
                </div>
            `;
            return;
        }

        const html = agents.slice(0, 5).map(agent => `
            <div class="flex items-center justify-between p-3 ${getStatusBgColor(agent.status)} border rounded-lg hover:bg-gray-800/20 transition-colors">
                <div class="flex items-center space-x-3">
                    <div class="w-2 h-2 rounded-full ${getStatusColor(agent.status)} opacity-75"></div>
                    <div>
                        <p class="text-sm font-medium text-white font-mono">Chat ${agent.chat_id}</p>
                        <p class="text-xs text-gray-400">Thread ${agent.thread_id || 0}</p>
                    </div>
                </div>
                <div class="text-right">
                    <p class="text-xs ${getStatusColor(agent.status)} font-semibold">${formatStatus(agent.status)}</p>
                    <p class="text-xs text-gray-400">${formatTimeAgo(agent.last_active)}</p>
                </div>
            </div>
        `).join('');

        container.innerHTML = html;
    }

    updateToolsList(tools) {
        const container = document.getElementById('toolsList');
        if (!container) return;

        if (tools.length === 0) {
            container.innerHTML = `
                <div class="text-center py-8 text-gray-400">
                    <svg class="w-12 h-12 mx-auto mb-3 opacity-50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"/>
                    </svg>
                    <p class="text-sm">No recent tools</p>
                </div>
            `;
            return;
        }

        const html = tools.slice(0, 5).map(tool => `
            <div class="flex items-center justify-between p-3 ${getStatusBgColor(tool.status)} border rounded-lg hover:bg-gray-800/20 transition-colors">
                <div class="flex items-center space-x-3">
                    <div class="w-8 h-8 ${getStatusBgColor(tool.status)} border rounded-lg flex items-center justify-center">
                        <svg class="w-4 h-4 ${getStatusColor(tool.status)}" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"/>
                        </svg>
                    </div>
                    <div>
                        <p class="text-sm font-medium text-white font-mono">${tool.tool_name}</p>
                        <p class="text-xs text-gray-400">Chat ${tool.chat_id}</p>
                    </div>
                </div>
                <div class="text-right">
                    <p class="text-xs ${getStatusColor(tool.status)} font-semibold">${formatStatus(tool.status)}</p>
                    <p class="text-xs text-gray-400">${formatTimeAgo(tool.timestamp)}</p>
                </div>
            </div>
        `).join('');

        container.innerHTML = html;
    }

    updateDecisionsList(decisions) {
        const container = document.getElementById('decisionsList');
        if (!container) return;

        if (decisions.length === 0) {
            container.innerHTML = `
                <div class="text-center py-8 text-gray-400">
                    <svg class="w-12 h-12 mx-auto mb-3 opacity-50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5H7a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-3 7h3m-3 4h3m-6-4h.01M9 16h.01"/>
                    </svg>
                    <p class="text-sm">No recent decisions</p>
                </div>
            `;
            return;
        }

        const html = decisions.slice(0, 5).map(decision => {
            const success = decision.outcome?.success;
            const statusColor = success ? 'text-status-success' : 'text-status-error';
            const statusBg = success ? 'bg-status-success/10 border-status-success/20' : 'bg-status-error/10 border-status-error/20';

            return `
                <div class="p-3 ${statusBg} border rounded-lg hover:bg-gray-800/20 transition-colors">
                    <div class="flex items-start justify-between mb-2">
                        <div class="flex-1">
                            <p class="text-sm font-medium text-white line-clamp-2">${decision.user_prompt?.substring(0, 50) || 'Decision logged'}...</p>
                            <p class="text-xs text-gray-400 mt-1">Chat ${decision.chat_id}</p>
                        </div>
                        <div class="ml-3 text-right">
                            <p class="text-xs ${statusColor} font-semibold">${success ? 'Success' : 'Failed'}</p>
                            <p class="text-xs text-gray-400">${formatTimeAgo(decision.timestamp)}</p>
                        </div>
                    </div>
                    ${decision.outcome?.summary ? `
                        <p class="text-xs text-gray-400 mt-2">${decision.outcome.summary.substring(0, 100)}...</p>
                    ` : ''}
                </div>
            `;
        }).join('');

        container.innerHTML = html;
    }

    updateGitInfo(gitStatusData, gitEventsData) {
        this.updateGitStatusCard(gitStatusData);
        this.updateGitRepositoryInfo(gitStatusData);
        this.updateGitEventsList(gitEventsData);
    }

    updateGitStatusCard(gitStatusData) {
        const branchElement = document.getElementById('gitBranch');
        const statusElement = document.getElementById('gitStatus');

        if (!branchElement || !statusElement) return;

        if (gitStatusData && gitStatusData.git_state) {
            const gitState = gitStatusData.git_state;
            branchElement.textContent = gitState.branch || 'Unknown';

            let statusText = 'Clean';
            let statusClass = 'text-status-success';

            if (gitState.is_dirty) {
                statusText = `${gitState.modified_files?.length || 0} modified`;
                statusClass = 'text-status-warning';
            }

            statusElement.textContent = statusText;
            statusElement.className = `text-xs mt-1 ${statusClass}`;
        } else {
            branchElement.textContent = 'No Repository';
            statusElement.textContent = 'Not a Git repository';
            statusElement.className = 'text-xs mt-1 text-gray-400';
        }
    }

    updateGitRepositoryInfo(gitStatusData) {
        const container = document.getElementById('gitRepositoryInfo');
        if (!container) return;

        if (!gitStatusData || !gitStatusData.git_state) {
            container.innerHTML = `
                <div class="text-center py-6 text-gray-400">
                    <svg class="w-12 h-12 mx-auto mb-3 opacity-50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"/>
                    </svg>
                    <p class="text-sm">No Git repository found</p>
                    <p class="text-xs text-gray-500">Initialize Git or check project directory</p>
                </div>
            `;
            return;
        }

        const gitState = gitStatusData.git_state;
        const isDirty = gitState.is_dirty;
        const statusColor = isDirty ? 'text-status-warning' : 'text-status-success';
        const statusIcon = isDirty ? '⚠️' : '✅';

        const html = `
            <div class="space-y-3">
                <div class="flex items-center justify-between p-3 bg-gray-800/30 rounded-lg">
                    <span class="text-sm text-gray-400">Branch</span>
                    <span class="font-mono text-white">${gitState.branch || 'Unknown'}</span>
                </div>
                <div class="flex items-center justify-between p-3 bg-gray-800/30 rounded-lg">
                    <span class="text-sm text-gray-400">Commit</span>
                    <span class="font-mono text-gray-300 text-sm">${gitState.commit_hash || 'N/A'}</span>
                </div>
                <div class="flex items-center justify-between p-3 bg-gray-800/30 rounded-lg">
                    <span class="text-sm text-gray-400">Status</span>
                    <span class="${statusColor} text-sm font-medium">${statusIcon} ${isDirty ? 'Modified' : 'Clean'}</span>
                </div>
                ${gitState.remote_url ? `
                <div class="flex items-center justify-between p-3 bg-gray-800/30 rounded-lg">
                    <span class="text-sm text-gray-400">Remote</span>
                    <span class="font-mono text-gray-300 text-xs truncate max-w-32" title="${gitState.remote_url}">
                        ${gitState.remote_url.split('/').pop() || gitState.remote_url}
                    </span>
                </div>
                ` : ''}
                ${isDirty && gitState.modified_files?.length ? `
                <div class="mt-3">
                    <p class="text-xs text-gray-400 mb-2">Modified Files (${gitState.modified_files.length})</p>
                    <div class="max-h-20 overflow-y-auto">
                        ${gitState.modified_files.slice(0, 5).map(file => `
                            <div class="text-xs font-mono text-gray-300 py-1">${file}</div>
                        `).join('')}
                        ${gitState.modified_files.length > 5 ?
                            `<div class="text-xs text-gray-400 py-1">... and ${gitState.modified_files.length - 5} more</div>`
                            : ''
                        }
                    </div>
                </div>
                ` : ''}
            </div>
        `;

        container.innerHTML = html;
    }

    updateGitEventsList(gitEventsData) {
        const container = document.getElementById('gitEventsList');
        if (!container) return;

        if (!gitEventsData || !gitEventsData.events || gitEventsData.events.length === 0) {
            container.innerHTML = `
                <div class="text-center py-8 text-gray-400">
                    <svg class="w-12 h-12 mx-auto mb-3 opacity-50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5H7a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2"/>
                    </svg>
                    <p class="text-sm">No Git operations recorded</p>
                </div>
            `;
            return;
        }

        const html = gitEventsData.events.slice(0, 8).map(event => {
            const success = event.success;
            const statusColor = success ? 'text-status-success' : 'text-status-error';
            const statusIcon = success ? '✓' : '✗';
            const timestamp = new Date(event.timestamp);

            return `
                <div class="flex items-center justify-between p-3 bg-gray-800/20 border border-gray-700/50 rounded-lg hover:bg-gray-800/40 transition-colors">
                    <div class="flex items-center space-x-3">
                        <div class="w-6 h-6 ${success ? 'bg-status-success/10' : 'bg-status-error/10'} border ${success ? 'border-status-success/20' : 'border-status-error/20'} rounded flex items-center justify-center">
                            <span class="${statusColor} text-xs font-bold">${statusIcon}</span>
                        </div>
                        <div>
                            <p class="text-sm font-medium text-white font-mono">git ${event.operation}</p>
                            <p class="text-xs text-gray-400">
                                ${event.args?.join(' ') || ''}
                                ${event.chat_id ? `• Chat ${event.chat_id}` : ''}
                            </p>
                        </div>
                    </div>
                    <div class="text-right">
                        <p class="text-xs ${statusColor}">${success ? 'Success' : 'Failed'}</p>
                        <p class="text-xs text-gray-400">${formatTimeAgo(timestamp)}</p>
                    </div>
                </div>
            `;
        }).join('');

        container.innerHTML = html;
    }

    async refreshGitInfo() {
        try {
            const [gitStatusData, gitEventsData] = await Promise.all([
                this.loadGitStatus(),
                this.loadGitEvents()
            ]);
            this.updateGitInfo(gitStatusData, gitEventsData);
        } catch (error) {
            console.error('Failed to refresh Git info:', error);
        }
    }

    // ========== Real-time Event Handlers ==========

    handleToolExecutionStart(data) {
        // Add to real-time data
        this.realtimeData.tools.unshift({
            ...data,
            status: 'running'
        });

        // Keep only recent 20 items
        if (this.realtimeData.tools.length > 20) {
            this.realtimeData.tools.pop();
        }

        // Update running tools counter
        this.updateRunningToolsCounter();

        // Update tools feed display
        this.updateRealtimeToolsFeed();

        console.log('Tool execution started:', data.tool_name);
    }

    handleToolExecutionComplete(data) {
        // Update existing entry or add new one
        const existingIndex = this.realtimeData.tools.findIndex(
            tool => tool.tool_name === data.tool_name &&
                   tool.chat_id === data.chat_id &&
                   tool.timestamp === data.timestamp
        );

        if (existingIndex !== -1) {
            this.realtimeData.tools[existingIndex] = data;
        } else {
            this.realtimeData.tools.unshift(data);
        }

        // Keep only recent items
        if (this.realtimeData.tools.length > 20) {
            this.realtimeData.tools.pop();
        }

        // Update counters and displays
        this.updateRunningToolsCounter();
        this.updateRealtimeToolsFeed();

        console.log('Tool execution completed:', data.tool_name, data.status);
    }

    handleDecisionComplete(data) {
        this.realtimeData.decisions.unshift(data);

        // Keep only recent 10 decisions
        if (this.realtimeData.decisions.length > 10) {
            this.realtimeData.decisions.pop();
        }

        // Update decision metrics
        this.updateDecisionMetrics();

        console.log('Decision completed:', data.success ? 'Success' : 'Failed');
    }

    handlePerformanceMetric(data) {
        this.realtimeData.performance.unshift(data);

        // Keep only recent 50 metrics
        if (this.realtimeData.performance.length > 50) {
            this.realtimeData.performance.pop();
        }

        // Update performance charts if available
        if (window.chartManager) {
            window.chartManager.addRealtimePerformanceData(data);
        }

        console.log('Performance metric received:', data.tool_execution_type);
    }

    handleSecurityAlert(data) {
        this.realtimeData.security.unshift(data);

        // Keep only recent 20 alerts
        if (this.realtimeData.security.length > 20) {
            this.realtimeData.security.pop();
        }

        // Show notification for high severity alerts
        if (data.severity === 'high' || data.severity === 'critical') {
            this.showNotification(
                `Security Alert: ${data.description}`,
                'error'
            );
        }

        // Update security metrics
        this.updateSecurityMetrics();

        console.log('Security alert:', data.severity, data.event_type);
    }

    handleAgentStatusChange(data) {
        // Update agent status map
        this.realtimeData.agents.set(data.chat_id, {
            chat_id: data.chat_id,
            status: data.status,
            details: data.details,
            last_seen: data.timestamp
        });

        // Update agent status displays
        this.updateAgentStatusDisplays();

        console.log('Agent status change:', data.chat_id, data.status);
    }

    // ========== Real-time Display Updates ==========

    updateRunningToolsCounter() {
        const runningCount = this.realtimeData.tools.filter(
            tool => tool.status === 'running'
        ).length;

        const element = document.getElementById('runningToolsCount');
        if (element) {
            element.textContent = runningCount;
        }
    }

    updateRealtimeToolsFeed() {
        const feedContainer = document.querySelector('.tools-feed');
        if (!feedContainer) return;

        const recentTools = this.realtimeData.tools.slice(0, 10);

        const html = recentTools.map(tool => {
            const statusClass = tool.status === 'success' ? 'text-status-success' :
                              tool.status === 'error' ? 'text-status-error' :
                              'text-status-warning';

            const statusIcon = tool.status === 'success' ? '✓' :
                             tool.status === 'error' ? '✗' : '⟳';

            const timestamp = new Date(tool.timestamp);
            const timeAgo = formatTimeAgo(timestamp);

            return `
                <div class="flex items-center justify-between p-3 bg-gray-800/20 border border-gray-700/50 rounded-lg">
                    <div class="flex items-center space-x-3">
                        <div class="w-6 h-6 bg-blue-600/10 border border-blue-600/20 rounded flex items-center justify-center">
                            <span class="${statusClass} text-xs font-bold">${statusIcon}</span>
                        </div>
                        <div>
                            <p class="text-sm font-medium text-white font-mono">${tool.tool_name}</p>
                            <p class="text-xs text-gray-400">Chat ${tool.chat_id}${tool.duration_ms ? ` • ${tool.duration_ms}ms` : ''}</p>
                        </div>
                    </div>
                    <div class="text-right">
                        <p class="text-xs ${statusClass}">${tool.status}</p>
                        <p class="text-xs text-gray-400">${timeAgo}</p>
                    </div>
                </div>
            `;
        }).join('');

        feedContainer.innerHTML = html || '<p class="text-gray-400 text-center py-4">No recent tool executions</p>';
    }

    updateDecisionMetrics() {
        const successfulDecisions = this.realtimeData.decisions.filter(d => d.success).length;
        const totalDecisions = this.realtimeData.decisions.length;

        const successRate = totalDecisions > 0 ? (successfulDecisions / totalDecisions * 100).toFixed(1) : 0;

        const successRateElement = document.getElementById('decisionSuccessRate');
        if (successRateElement) {
            successRateElement.textContent = `${successRate}%`;
        }

        const totalDecisionsElement = document.getElementById('totalDecisions');
        if (totalDecisionsElement) {
            totalDecisionsElement.textContent = totalDecisions;
        }
    }

    updateSecurityMetrics() {
        const criticalAlerts = this.realtimeData.security.filter(s => s.severity === 'critical').length;
        const highAlerts = this.realtimeData.security.filter(s => s.severity === 'high').length;

        const criticalElement = document.getElementById('criticalAlerts');
        if (criticalElement) {
            criticalElement.textContent = criticalAlerts;
        }

        const highAlertsElement = document.getElementById('highAlerts');
        if (highAlertsElement) {
            highAlertsElement.textContent = highAlerts;
        }
    }

    updateAgentStatusDisplays() {
        const activeAgents = Array.from(this.realtimeData.agents.values())
            .filter(agent => agent.status === 'active').length;

        const activeAgentsElement = document.getElementById('activeAgentsCount');
        if (activeAgentsElement) {
            activeAgentsElement.textContent = activeAgents;
        }
    }

    updateLastUpdatedTime() {
        const element = document.getElementById('lastUpdated');
        if (element && this.lastUpdate) {
            element.textContent = this.lastUpdate.toLocaleTimeString();
        }
    }

    showLoading(show) {
        const overlay = document.getElementById('loadingOverlay');
        if (overlay) {
            if (show) {
                overlay.classList.remove('opacity-0', 'pointer-events-none');
                overlay.classList.add('opacity-100');
            } else {
                overlay.classList.add('opacity-0', 'pointer-events-none');
                overlay.classList.remove('opacity-100');
            }
        }
    }

    showError(message) {
        // Simple error notification
        console.error('Dashboard Error:', message);

        // Could implement toast notifications here
        const notification = document.createElement('div');
        notification.className = 'fixed top-4 right-4 bg-status-error text-white px-4 py-3 rounded-lg shadow-lg z-50 animate-slide-up';
        notification.innerHTML = `
            <div class="flex items-center space-x-2">
                <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/>
                </svg>
                <span>${message}</span>
            </div>
        `;

        document.body.appendChild(notification);

        setTimeout(() => {
            notification.remove();
        }, 5000);
    }

    startAutoRefresh() {
        if (this.refreshInterval) return;

        this.refreshInterval = setInterval(() => {
            if (!document.hidden) {
                this.refresh();
            }
        }, this.updateFrequency);
    }

    stopAutoRefresh() {
        if (this.refreshInterval) {
            clearInterval(this.refreshInterval);
            this.refreshInterval = null;
        }
    }

    async refresh() {
        await this.loadDashboard();
    }

    async forceRefresh() {
        this.cache.clear();
        await this.loadDashboard();
    }
}

// Initialize dashboard when DOM is loaded
document.addEventListener('DOMContentLoaded', () => {
    window.dashboard = new AliceDashboard();
    window.dashboard.loadDashboard();
});