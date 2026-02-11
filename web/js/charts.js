/**
 * Alice Charts Manager
 * Handles all data visualization and charting
 */

class AliceChartsManager {
    constructor() {
        this.charts = {};
        this.chartColors = {
            primary: '#3B82F6',
            secondary: '#60A5FA',
            success: '#22C55E',
            warning: '#F59E0B',
            error: '#EF4444',
            info: '#3B82F6',
            gradient: {
                primary: ['#3B82F6', '#60A5FA'],
                success: ['#22C55E', '#34D399'],
                warning: ['#F59E0B', '#FBBF24'],
                error: ['#EF4444', '#F87171']
            }
        };

        this.initializeCharts();
    }

    initializeCharts() {
        // Configure Chart.js defaults for dark theme
        Chart.defaults.color = '#9CA3AF';
        Chart.defaults.backgroundColor = 'rgba(59, 130, 246, 0.1)';
        Chart.defaults.borderColor = '#374151';
        Chart.defaults.plugins.legend.labels.usePointStyle = true;
        Chart.defaults.plugins.legend.labels.padding = 20;
        Chart.defaults.scale.grid.color = '#374151';
        Chart.defaults.scale.ticks.color = '#9CA3AF';

        this.createActivityChart();
        this.createToolChart();
    }

    createActivityChart() {
        const ctx = document.getElementById('activityChart');
        if (!ctx) return;

        // Initialize with empty data - will be populated from real-time data
        const now = new Date();
        const labels = [];
        const agentData = [];
        const toolData = [];

        // Create 24 hourly data points
        for (let i = 23; i >= 0; i--) {
            const time = new Date(now.getTime() - i * 60 * 60 * 1000);
            labels.push(time.toLocaleTimeString([], { hour: '2-digit' }));
            agentData.push(0); // Will be populated from real data
            toolData.push(0);  // Will be populated from real data
        }

        this.charts.activity = new Chart(ctx, {
            type: 'line',
            data: {
                labels: labels,
                datasets: [
                    {
                        label: 'Active Agents',
                        data: agentData,
                        borderColor: this.chartColors.primary,
                        backgroundColor: this.createGradient(ctx, this.chartColors.gradient.primary),
                        borderWidth: 2,
                        fill: true,
                        tension: 0.4,
                        pointBackgroundColor: this.chartColors.primary,
                        pointBorderColor: '#000000',
                        pointBorderWidth: 2,
                        pointRadius: 4,
                        pointHoverRadius: 6
                    },
                    {
                        label: 'Tool Executions',
                        data: toolData,
                        borderColor: this.chartColors.success,
                        backgroundColor: this.createGradient(ctx, this.chartColors.gradient.success, 0.3),
                        borderWidth: 2,
                        fill: true,
                        tension: 0.4,
                        pointBackgroundColor: this.chartColors.success,
                        pointBorderColor: '#000000',
                        pointBorderWidth: 2,
                        pointRadius: 4,
                        pointHoverRadius: 6
                    }
                ]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: {
                    intersect: false,
                    mode: 'index'
                },
                plugins: {
                    legend: {
                        display: true,
                        position: 'top',
                        labels: {
                            color: '#F3F4F6',
                            font: {
                                family: 'Fira Sans',
                                size: 12
                            }
                        }
                    },
                    tooltip: {
                        backgroundColor: 'rgba(17, 24, 39, 0.9)',
                        titleColor: '#F3F4F6',
                        bodyColor: '#D1D5DB',
                        borderColor: '#374151',
                        borderWidth: 1,
                        cornerRadius: 8,
                        displayColors: true,
                        titleFont: {
                            family: 'Fira Sans',
                            size: 12,
                            weight: 'bold'
                        },
                        bodyFont: {
                            family: 'Fira Code',
                            size: 11
                        }
                    }
                },
                scales: {
                    x: {
                        grid: {
                            color: '#374151',
                            drawBorder: false
                        },
                        ticks: {
                            color: '#9CA3AF',
                            font: {
                                family: 'Fira Code',
                                size: 10
                            }
                        }
                    },
                    y: {
                        grid: {
                            color: '#374151',
                            drawBorder: false
                        },
                        ticks: {
                            color: '#9CA3AF',
                            font: {
                                family: 'Fira Code',
                                size: 10
                            }
                        },
                        beginAtZero: true
                    }
                },
                elements: {
                    line: {
                        borderJoinStyle: 'round'
                    },
                    point: {
                        hoverBorderWidth: 3
                    }
                }
            }
        });
    }

    createToolChart() {
        const ctx = document.getElementById('toolChart');
        if (!ctx) return;

        // Sample tool usage data
        const toolData = [
            { name: 'file_read', count: 45, color: this.chartColors.primary },
            { name: 'bash', count: 32, color: this.chartColors.success },
            { name: 'file_write', count: 28, color: this.chartColors.warning },
            { name: 'grep', count: 22, color: this.chartColors.error },
            { name: 'file_patch', count: 18, color: '#8B5CF6' },
            { name: 'other', count: 12, color: '#6B7280' }
        ];

        this.charts.tool = new Chart(ctx, {
            type: 'doughnut',
            data: {
                labels: toolData.map(t => t.name),
                datasets: [{
                    data: toolData.map(t => t.count),
                    backgroundColor: toolData.map(t => t.color),
                    borderColor: '#000000',
                    borderWidth: 2,
                    hoverBorderWidth: 3,
                    hoverBackgroundColor: toolData.map(t => t.color + 'DD'),
                    hoverOffset: 8
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                cutout: '60%',
                plugins: {
                    legend: {
                        display: true,
                        position: 'bottom',
                        labels: {
                            color: '#F3F4F6',
                            font: {
                                family: 'Fira Sans',
                                size: 11
                            },
                            padding: 15,
                            usePointStyle: true,
                            pointStyle: 'circle'
                        }
                    },
                    tooltip: {
                        backgroundColor: 'rgba(17, 24, 39, 0.9)',
                        titleColor: '#F3F4F6',
                        bodyColor: '#D1D5DB',
                        borderColor: '#374151',
                        borderWidth: 1,
                        cornerRadius: 8,
                        titleFont: {
                            family: 'Fira Sans',
                            size: 12,
                            weight: 'bold'
                        },
                        bodyFont: {
                            family: 'Fira Code',
                            size: 11
                        },
                        callbacks: {
                            label: function(context) {
                                const total = context.dataset.data.reduce((sum, value) => sum + value, 0);
                                const percentage = ((context.parsed * 100) / total).toFixed(1);
                                return `${context.label}: ${context.parsed} (${percentage}%)`;
                            }
                        }
                    }
                },
                animation: {
                    animateScale: true,
                    animateRotate: true,
                    duration: 1000,
                    easing: 'easeOutQuart'
                }
            }
        });
    }

    createGradient(ctx, colors, opacity = 0.6) {
        const gradient = ctx.getContext('2d').createLinearGradient(0, 0, 0, 400);
        gradient.addColorStop(0, colors[0] + Math.floor(opacity * 255).toString(16).padStart(2, '0'));
        gradient.addColorStop(1, colors[1] + '00');
        return gradient;
    }

    updateCharts(data) {
        this.updateActivityChart(data);
        this.updateToolChart(data);
    }

    updateActivityChart(data) {
        if (!this.charts.activity) return;

        // Process real data for activity chart
        const chart = this.charts.activity;

        // Generate time series data from agents and tools
        if (data.agents && data.tools) {
            // Count active agents over time
            const activeAgentsCount = data.agents.filter(agent =>
                agent.status === 'STATUS_RUNNING' || agent.status === 'STATUS_PENDING'
            ).length;

            // Update last data point with real data
            const agentDataset = chart.data.datasets[0];
            const toolDataset = chart.data.datasets[1];

            // Shift data left and add new point
            agentDataset.data.shift();
            agentDataset.data.push(activeAgentsCount);

            toolDataset.data.shift();
            toolDataset.data.push(data.tools.length);

            // Update time labels
            chart.data.labels.shift();
            chart.data.labels.push(new Date().toLocaleTimeString([], { hour: '2-digit' }));

            chart.update('none');
        }
    }

    updateToolChart(data) {
        if (!this.charts.tool || !data.toolStats) return;

        const chart = this.charts.tool;
        const toolCounts = data.toolStats.tool_counts || {};

        // Update chart data with real tool counts
        const toolNames = Object.keys(toolCounts);
        const toolValues = Object.values(toolCounts);

        if (toolNames.length > 0) {
            chart.data.labels = toolNames;
            chart.data.datasets[0].data = toolValues;

            // Generate colors for tools
            const colors = [
                this.chartColors.primary,
                this.chartColors.success,
                this.chartColors.warning,
                this.chartColors.error,
                '#8B5CF6',
                '#F472B6',
                '#06B6D4',
                '#84CC16'
            ];

            chart.data.datasets[0].backgroundColor = toolNames.map((_, i) =>
                colors[i % colors.length]
            );

            chart.update('active');
        }
    }

    // Utility method to destroy charts (for cleanup)
    destroy() {
        Object.values(this.charts).forEach(chart => {
            if (chart) chart.destroy();
        });
        this.charts = {};
    }

    // Method to create additional charts dynamically
    createCustomChart(containerId, type, data, options = {}) {
        const ctx = document.getElementById(containerId);
        if (!ctx) return null;

        const chart = new Chart(ctx, {
            type: type,
            data: data,
            options: {
                responsive: true,
                maintainAspectRatio: false,
                ...options
            }
        });

        return chart;
    }

    // Real-time data update methods
    addRealtimePerformanceData(performanceData) {
        if (!this.charts.activity) return;

        const chart = this.charts.activity;
        const now = new Date();
        const timeLabel = now.toLocaleTimeString([], { hour: '2-digit' });

        // Add new data point
        chart.data.labels.push(timeLabel);

        // Add tool execution data (count tools in last hour)
        const toolCount = performanceData.tool_execution_type ? 1 : 0;
        chart.data.datasets[1].data.push(toolCount);

        // Add agent activity (simplified - could be enhanced)
        const agentActivity = performanceData.chat_id ? 1 : 0;
        chart.data.datasets[0].data.push(agentActivity);

        // Keep only last 24 data points
        if (chart.data.labels.length > 24) {
            chart.data.labels.shift();
            chart.data.datasets.forEach(dataset => {
                dataset.data.shift();
            });
        }

        chart.update('none'); // Update without animation for real-time feel
    }

    updatePerformanceMetrics(data) {
        // Update performance chart if available
        if (this.charts.performance && data) {
            const chart = this.charts.performance;

            // Update performance data based on real metrics
            if (data.api_latency_ms !== undefined) {
                // Update API latency
                const latencyDataset = chart.data.datasets.find(d => d.label.includes('Latency'));
                if (latencyDataset && latencyDataset.data.length > 0) {
                    latencyDataset.data[latencyDataset.data.length - 1] = data.api_latency_ms;
                }
            }

            if (data.memory_usage !== undefined) {
                // Update memory usage
                const memoryDataset = chart.data.datasets.find(d => d.label.includes('Memory'));
                if (memoryDataset && memoryDataset.data.length > 0) {
                    memoryDataset.data[memoryDataset.data.length - 1] = Math.round(data.memory_usage / 1024 / 1024); // Convert to MB
                }
            }

            chart.update('none');
        }
    }

    updateToolUsageData(toolData) {
        if (!this.charts.tools || !toolData.length) return;

        const chart = this.charts.tools;

        // Count tool usage from real data
        const toolCounts = {};
        toolData.forEach(tool => {
            toolCounts[tool.tool_name] = (toolCounts[tool.tool_name] || 0) + 1;
        });

        // Update chart data
        chart.data.labels = Object.keys(toolCounts);
        chart.data.datasets[0].data = Object.values(toolCounts);

        chart.update();
    }

    // Load historical data for charts
    async loadHistoricalData() {
        try {
            // Load performance metrics for the last 24 hours
            const endTime = new Date();
            const startTime = new Date(endTime.getTime() - 24 * 60 * 60 * 1000);

            const response = await window.aliceApi.request(`/api/performance/metrics?start_time=${startTime.toISOString()}&end_time=${endTime.toISOString()}&limit=100`);

            if (response && response.metrics) {
                this.populateChartsWithHistoricalData(response.metrics);
            }
        } catch (error) {
            console.error('Failed to load historical data for charts:', error);
        }
    }

    populateChartsWithHistoricalData(metrics) {
        if (!metrics.length) return;

        // Group metrics by hour
        const hourlyData = {};
        metrics.forEach(metric => {
            const hour = new Date(metric.timestamp).toLocaleTimeString([], { hour: '2-digit' });
            if (!hourlyData[hour]) {
                hourlyData[hour] = { tools: 0, agents: new Set() };
            }
            hourlyData[hour].tools++;
            if (metric.chat_id) {
                hourlyData[hour].agents.add(metric.chat_id);
            }
        });

        // Update activity chart with historical data
        if (this.charts.activity) {
            const chart = this.charts.activity;
            chart.data.labels.forEach((label, index) => {
                if (hourlyData[label]) {
                    chart.data.datasets[0].data[index] = hourlyData[label].agents.size;
                    chart.data.datasets[1].data[index] = hourlyData[label].tools;
                }
            });
            chart.update();
        }
    }
}

// Initialize charts when DOM is loaded
document.addEventListener('DOMContentLoaded', () => {
    window.chartManager = new AliceChartsManager();
});