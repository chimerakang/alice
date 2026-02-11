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
        this.updateFrequency = 5000; // 5 seconds

        this.initializeEventListeners();
        this.startAutoRefresh();
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
                multiAgentStatus
            ] = await Promise.all([
                this.loadAgents(),
                this.loadTools(),
                this.loadDecisions(),
                this.loadToolStats(),
                this.loadMultiAgentStatus()
            ]);

            // Update metrics
            this.updateMetrics(agentsData, toolsData, decisionsData, toolStatsData);

            // Update lists
            this.updateAgentsList(agentsData);
            this.updateToolsList(toolsData);
            this.updateDecisionsList(decisionsData);

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