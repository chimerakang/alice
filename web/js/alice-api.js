/**
 * Alice API Client
 * Type-safe API client for Alice AI Agent monitoring
 */

class AliceApiClient {
    constructor(baseUrl = 'http://localhost:8080') {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.requestCount = 0;
    }

    /**
     * Generic HTTP request method with error handling
     */
    async request(endpoint, options = {}) {
        const url = `${this.baseUrl}${endpoint}`;
        this.requestCount++;

        const defaultHeaders = {
            'Content-Type': 'application/json',
        };

        try {
            const response = await fetch(url, {
                ...options,
                headers: {
                    ...defaultHeaders,
                    ...(options.headers || {}),
                },
            });

            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }

            return await response.json();
        } catch (error) {
            console.error(`API Error [${endpoint}]:`, error);
            throw error;
        }
    }

    // ========== Agent API ==========

    async listAgents() {
        return this.request('/api/agents');
    }

    async getAgent(chatId, threadId) {
        const params = new URLSearchParams({ chat_id: chatId.toString() });
        if (threadId !== undefined) {
            params.set('thread_id', threadId.toString());
        }
        return this.request(`/api/agents?${params}`);
    }

    // ========== Tool API ==========

    async getRecentTools(limit = 20) {
        return this.request(`/api/tools/recent?limit=${limit}`);
    }

    async getToolExecutions() {
        return this.request('/api/tools/executions');
    }

    // ========== Decision API ==========

    async getDecisions() {
        return this.request('/api/decisions');
    }

    async getRecentDecisions(limit = 10) {
        return this.request(`/api/decisions/recent?limit=${limit}`);
    }

    // ========== MultiAgent API ==========

    async getMultiAgentStatus() {
        return this.request('/api/multiagent/status');
    }

    async getMultiAgentStats() {
        return this.request('/api/multiagent/stats');
    }

    // ========== Performance API ==========

    async getPerformanceAnalytics() {
        return this.request('/api/performance/analytics');
    }

    async getPerformanceMetrics() {
        return this.request('/api/performance/metrics');
    }

    // ========== Security API ==========

    async getSecurityEvents() {
        return this.request('/api/security/events');
    }

    async getSecurityStats() {
        return this.request('/api/security/stats');
    }

    // ========== Health Check ==========

    async getHealth() {
        return this.request('/api/health');
    }

    async getStats() {
        return this.request('/api/stats');
    }

    // ========== Git Integration API ==========

    async getGitStatus(projectDir) {
        const params = projectDir ? `?project_dir=${encodeURIComponent(projectDir)}` : '';
        return this.request(`/api/git/status${params}`);
    }

    async getGitEvents(limit = 20, projectDir) {
        const params = new URLSearchParams();
        if (limit) params.set('limit', limit.toString());
        if (projectDir) params.set('project_dir', projectDir);
        const query = params.toString() ? `?${params}` : '';
        return this.request(`/api/git/events${query}`);
    }

    async getGitOperations() {
        return this.request('/api/git/operations');
    }

    async updateGitOperations(settings) {
        return this.request('/api/git/operations', {
            method: 'POST',
            body: JSON.stringify(settings)
        });
    }
}

/**
 * Utility Functions
 */

// Format status enum for display
function formatStatus(status) {
    if (!status) return 'Unknown';
    return status.replace('STATUS_', '').toLowerCase().replace('_', ' ');
}

// Format timestamp to relative time
function formatTimeAgo(timestamp) {
    const now = new Date();
    const time = new Date(timestamp);
    const diffMs = now - time;
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMins / 60);
    const diffDays = Math.floor(diffHours / 24);

    if (diffMins < 1) return 'Just now';
    if (diffMins < 60) return `${diffMins}m ago`;
    if (diffHours < 24) return `${diffHours}h ago`;
    if (diffDays < 30) return `${diffDays}d ago`;
    return time.toLocaleDateString();
}

// Format numbers with thousands separator
function formatNumber(num) {
    if (num === undefined || num === null) return '0';
    return num.toLocaleString();
}

// Format bytes to human readable
function formatBytes(bytes, decimals = 2) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + ' ' + sizes[i];
}

// Get status color class
function getStatusColor(status) {
    const statusMap = {
        'STATUS_SUCCESS': 'text-status-success',
        'STATUS_RUNNING': 'text-dashboard-primary',
        'STATUS_PENDING': 'text-status-warning',
        'STATUS_ERROR': 'text-status-error',
        'STATUS_FAILED': 'text-status-error',
        'STATUS_CANCELLED': 'text-gray-400'
    };
    return statusMap[status] || 'text-gray-400';
}

// Get status background color class
function getStatusBgColor(status) {
    const statusMap = {
        'STATUS_SUCCESS': 'bg-status-success/10 border-status-success/20',
        'STATUS_RUNNING': 'bg-dashboard-primary/10 border-dashboard-primary/20',
        'STATUS_PENDING': 'bg-status-warning/10 border-status-warning/20',
        'STATUS_ERROR': 'bg-status-error/10 border-status-error/20',
        'STATUS_FAILED': 'bg-status-error/10 border-status-error/20',
        'STATUS_CANCELLED': 'bg-gray-800/10 border-gray-600/20'
    };
    return statusMap[status] || 'bg-gray-800/10 border-gray-600/20';
}

// Animate number counter
function animateNumber(element, startValue, endValue, duration = 1000) {
    const startTime = performance.now();
    const difference = endValue - startValue;

    function updateNumber(currentTime) {
        const elapsed = currentTime - startTime;
        const progress = Math.min(elapsed / duration, 1);

        // Easing function (ease-out)
        const easeOut = 1 - Math.pow(1 - progress, 3);
        const currentValue = Math.floor(startValue + difference * easeOut);

        element.textContent = formatNumber(currentValue);

        if (progress < 1) {
            requestAnimationFrame(updateNumber);
        }
    }

    requestAnimationFrame(updateNumber);
}

// Check if response has error
function isErrorResponse(response) {
    return response && response.error;
}

// Global API client instance
window.aliceApi = new AliceApiClient();