#!/bin/bash

# Alice Installation Script
# This script installs Alice (Claude Telegram Agent) on Linux systems

set -euo pipefail

# Configuration
ALICE_USER="alice"
ALICE_GROUP="alice"
ALICE_HOME="/opt/alice"
ALICE_BINARY="/usr/local/bin/alice"
SERVICE_FILE="/etc/systemd/system/alice.service"
CONFIG_DIR="/etc/alice"
LOG_DIR="/var/log/alice"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root"
        exit 1
    fi
}

# Create alice user and group
create_user() {
    if ! id "$ALICE_USER" &>/dev/null; then
        log_info "Creating user: $ALICE_USER"
        useradd --system --shell /bin/false --home-dir "$ALICE_HOME" \
                --create-home --user-group "$ALICE_USER"
    else
        log_info "User $ALICE_USER already exists"
    fi
}

# Create directories
create_directories() {
    log_info "Creating directories"

    directories=("$ALICE_HOME" "$ALICE_HOME/data" "$ALICE_HOME/config"
                "$ALICE_HOME/logs" "$CONFIG_DIR" "$LOG_DIR")

    for dir in "${directories[@]}"; do
        mkdir -p "$dir"
        chown "$ALICE_USER:$ALICE_GROUP" "$dir"
        chmod 755 "$dir"
    done

    # Secure config directory
    chmod 750 "$CONFIG_DIR"
}

# Install binary
install_binary() {
    if [[ ! -f "alice" ]]; then
        log_error "alice binary not found in current directory"
        log_info "Please run: go build -o alice ."
        exit 1
    fi

    log_info "Installing alice binary to $ALICE_BINARY"
    cp alice "$ALICE_BINARY"
    chmod 755 "$ALICE_BINARY"
    chown root:root "$ALICE_BINARY"
}

# Install service file
install_service() {
    if [[ ! -f "deploy/alice.service" ]]; then
        log_error "Service file not found: deploy/alice.service"
        exit 1
    fi

    log_info "Installing systemd service"
    cp deploy/alice.service "$SERVICE_FILE"
    chmod 644 "$SERVICE_FILE"
    chown root:root "$SERVICE_FILE"

    systemctl daemon-reload
}

# Create configuration file
create_config() {
    local config_file="$CONFIG_DIR/alice.env"

    if [[ ! -f "$config_file" ]]; then
        log_info "Creating configuration file: $config_file"
        cat > "$config_file" << 'EOF'
# Alice Configuration
TELEGRAM_BOT_TOKEN=your-telegram-bot-token-here
CLAUDE_MODEL=sonnet
ENABLE_WEB_INTERFACE=true
WEB_PORT=8080
ENABLE_RATE_LIMITING=true
RATE_LIMIT_RPM=60
ENABLE_PII_DETECTION=true
ENABLE_AUDIT_LOGGING=true
DATA_RETENTION_DAYS=30
EOF

        chmod 600 "$config_file"
        chown "$ALICE_USER:$ALICE_GROUP" "$config_file"

        log_warn "Please edit $config_file with your configuration"
    else
        log_info "Configuration file already exists: $config_file"
    fi
}

# Configure firewall
configure_firewall() {
    if command -v ufw >/dev/null 2>&1; then
        log_info "Configuring UFW firewall"
        ufw allow 8080/tcp comment "Alice Web Interface"
    else
        log_warn "UFW not found, please configure firewall manually"
        log_warn "Allow port 8080/tcp for web interface"
    fi
}

# Install monitoring (optional)
install_monitoring() {
    read -p "Install monitoring stack (Prometheus, Grafana)? [y/N]: " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        if command -v docker-compose >/dev/null 2>&1; then
            log_info "Installing monitoring stack"

            # Copy monitoring configuration
            cp -r monitoring /opt/alice/
            chown -R "$ALICE_USER:$ALICE_GROUP" /opt/alice/monitoring

            # Create monitoring docker-compose
            cat > /opt/alice/docker-compose.monitoring.yml << 'EOF'
version: '3.8'
services:
  prometheus:
    image: prom/prometheus:latest
    container_name: alice-prometheus
    restart: unless-stopped
    volumes:
      - ./monitoring/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus_data:/prometheus
    ports:
      - "127.0.0.1:9090:9090"
    networks:
      - alice-monitoring

  grafana:
    image: grafana/grafana:latest
    container_name: alice-grafana
    restart: unless-stopped
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
    volumes:
      - grafana_data:/var/lib/grafana
      - ./monitoring/grafana:/etc/grafana/provisioning:ro
    ports:
      - "127.0.0.1:3000:3000"
    networks:
      - alice-monitoring

volumes:
  prometheus_data:
  grafana_data:

networks:
  alice-monitoring:
    driver: bridge
EOF

            log_info "Monitoring stack installed. Start with:"
            log_info "cd /opt/alice && docker-compose -f docker-compose.monitoring.yml up -d"
        else
            log_warn "Docker Compose not found, skipping monitoring installation"
        fi
    fi
}

# Enable and start service
start_service() {
    log_info "Enabling and starting alice service"
    systemctl enable alice

    if systemctl start alice; then
        log_info "Alice service started successfully"
    else
        log_error "Failed to start alice service"
        log_info "Check logs with: journalctl -u alice -f"
        return 1
    fi
}

# Main installation function
main() {
    log_info "Starting Alice installation"

    check_root
    create_user
    create_directories
    install_binary
    install_service
    create_config
    configure_firewall
    install_monitoring

    log_info "Installation completed!"
    log_info ""
    log_info "Next steps:"
    log_info "1. Edit configuration: $CONFIG_DIR/alice.env"
    log_info "2. Set your Telegram bot token"
    log_info "3. Start the service: systemctl start alice"
    log_info "4. Check status: systemctl status alice"
    log_info "5. View logs: journalctl -u alice -f"
    log_info ""
    log_info "Web interface: http://localhost:8080 (if enabled)"
    log_info "Metrics endpoint: http://localhost:8080/metrics"
}

# Run installation
main "$@"