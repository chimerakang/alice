/**
 * Alice WebSocket Client
 * Real-time event handling for dashboard
 */

class AliceWebSocketClient {
    constructor() {
        this.ws = null;
        this.reconnectAttempts = 0;
        this.maxReconnectAttempts = 10;
        this.reconnectInterval = 1000; // Start with 1 second
        this.maxReconnectInterval = 30000; // Max 30 seconds
        this.isConnected = false;
        this.eventListeners = new Map();
        this.messageQueue = [];

        // Event buffer for recent events
        this.eventBuffer = [];
        this.maxBufferSize = 100;

        this.connect();
        this.setupHeartbeat();
    }

    connect() {
        try {
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            const wsUrl = `${protocol}//${window.location.host}/ws`;

            console.log(`Connecting to WebSocket: ${wsUrl}`);
            this.ws = new WebSocket(wsUrl);

            this.ws.onopen = this.onOpen.bind(this);
            this.ws.onmessage = this.onMessage.bind(this);
            this.ws.onclose = this.onClose.bind(this);
            this.ws.onerror = this.onError.bind(this);

        } catch (error) {
            console.error('WebSocket connection failed:', error);
            this.scheduleReconnect();
        }
    }

    onOpen() {
        console.log('WebSocket connected successfully');
        this.isConnected = true;
        this.reconnectAttempts = 0;
        this.reconnectInterval = 1000;

        // Update connection status in UI
        this.updateConnectionStatus(true);

        // Send any queued messages
        this.flushMessageQueue();

        // Emit connected event
        this.emit('connected', {});
    }

    onMessage(event) {
        try {
            const message = JSON.parse(event.data);

            // Add to event buffer
            this.eventBuffer.push({
                ...message,
                received_at: Date.now()
            });

            // Keep buffer size under control
            if (this.eventBuffer.length > this.maxBufferSize) {
                this.eventBuffer.shift();
            }

            // Emit the specific event
            this.emit(message.type, message);

            // Also emit a generic 'event' for all messages
            this.emit('event', message);

            console.log('WebSocket message received:', message.type, message);

        } catch (error) {
            console.error('Failed to parse WebSocket message:', error, event.data);
        }
    }

    onClose(event) {
        console.log('WebSocket connection closed:', event.code, event.reason);
        this.isConnected = false;
        this.updateConnectionStatus(false);

        if (!event.wasClean) {
            this.scheduleReconnect();
        }

        // Emit disconnected event
        this.emit('disconnected', { code: event.code, reason: event.reason });
    }

    onError(error) {
        console.error('WebSocket error:', error);
        this.emit('error', error);
    }

    scheduleReconnect() {
        if (this.reconnectAttempts >= this.maxReconnectAttempts) {
            console.error('Max reconnection attempts reached. Giving up.');
            this.updateConnectionStatus(false, 'Max reconnection attempts reached');
            return;
        }

        this.reconnectAttempts++;
        const delay = Math.min(
            this.reconnectInterval * Math.pow(2, this.reconnectAttempts - 1),
            this.maxReconnectInterval
        );

        console.log(`Scheduling reconnection attempt ${this.reconnectAttempts} in ${delay}ms`);

        setTimeout(() => {
            if (!this.isConnected) {
                this.connect();
            }
        }, delay);
    }

    setupHeartbeat() {
        setInterval(() => {
            if (this.isConnected) {
                this.send('ping', {});
            }
        }, 30000); // Send ping every 30 seconds
    }

    send(type, data) {
        const message = {
            type: type,
            timestamp: new Date().toISOString(),
            data: data
        };

        if (this.isConnected && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify(message));
        } else {
            // Queue message for later
            this.messageQueue.push(message);
        }
    }

    flushMessageQueue() {
        while (this.messageQueue.length > 0 && this.isConnected) {
            const message = this.messageQueue.shift();
            this.ws.send(JSON.stringify(message));
        }
    }

    // Event emitter methods
    on(eventType, callback) {
        if (!this.eventListeners.has(eventType)) {
            this.eventListeners.set(eventType, []);
        }
        this.eventListeners.get(eventType).push(callback);
    }

    off(eventType, callback) {
        if (this.eventListeners.has(eventType)) {
            const listeners = this.eventListeners.get(eventType);
            const index = listeners.indexOf(callback);
            if (index !== -1) {
                listeners.splice(index, 1);
            }
        }
    }

    emit(eventType, data) {
        if (this.eventListeners.has(eventType)) {
            this.eventListeners.get(eventType).forEach(callback => {
                try {
                    callback(data);
                } catch (error) {
                    console.error(`Error in event listener for ${eventType}:`, error);
                }
            });
        }
    }

    // UI Status updates
    updateConnectionStatus(connected, message = '') {
        const statusIndicator = document.querySelector('.websocket-status');
        if (statusIndicator) {
            statusIndicator.className = `websocket-status ${connected ? 'connected' : 'disconnected'}`;
            statusIndicator.textContent = connected ? '🟢 Live' : '🔴 Disconnected';
            if (message) {
                statusIndicator.title = message;
            }
        }

        // Update connection info in stats
        const connectionInfo = document.querySelector('.connection-info');
        if (connectionInfo) {
            connectionInfo.textContent = connected ?
                `Connected • ${this.eventBuffer.length} events` :
                'Disconnected';
        }
    }

    // Get recent events
    getRecentEvents(type = null, count = 10) {
        let events = this.eventBuffer;

        if (type) {
            events = events.filter(event => event.type === type);
        }

        return events.slice(-count).reverse(); // Most recent first
    }

    // Get connection stats
    getStats() {
        return {
            connected: this.isConnected,
            reconnect_attempts: this.reconnectAttempts,
            event_buffer_size: this.eventBuffer.length,
            message_queue_size: this.messageQueue.length
        };
    }

    // Disconnect
    disconnect() {
        if (this.ws) {
            this.ws.close(1000, 'User initiated disconnect');
            this.ws = null;
        }
        this.isConnected = false;
    }
}

// Export for global use
window.AliceWebSocketClient = AliceWebSocketClient;