# Alice Deployment Guide

This guide covers various deployment options for Alice (Claude TG Agent) from development to production environments.

## Quick Start

### Docker Compose (Recommended for Development)

```bash
# Clone the repository
git clone https://github.com/your-org/alice.git
cd alice

# Copy and configure environment variables
cp .env.example .env
# Edit .env with your configuration

# Start Alice with monitoring
docker-compose up -d

# Start with full monitoring stack
docker-compose --profile monitoring up -d
```

### Binary Deployment

```bash
# Build from source
go build -o alice .

# Set environment variables
export TELEGRAM_BOT_TOKEN="your-bot-token"
export CLAUDE_MODEL="sonnet"

# Run
./alice
```

## Production Deployment

### Docker Compose Production

```bash
# Use production configuration
docker-compose -f docker-compose.prod.yml up -d

# Monitor logs
docker-compose -f docker-compose.prod.yml logs -f alice
```

### Kubernetes Deployment

```bash
# Apply Kubernetes manifests
kubectl apply -f k8s/base/

# Or use Kustomize for overlays
kubectl apply -k k8s/overlays/production/

# Check deployment status
kubectl get pods -l app=alice-bot
kubectl logs deployment/alice-bot
```

### Systemd Service (Linux)

```bash
# Install binary
sudo cp alice /usr/local/bin/
sudo chmod +x /usr/local/bin/alice

# Create service file
sudo cp deploy/alice.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable alice
sudo systemctl start alice

# Check status
sudo systemctl status alice
```

## Configuration

### Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `TELEGRAM_BOT_TOKEN` | Telegram bot token | - | ✅ |
| `CLAUDE_MODEL` | Claude model name | `sonnet` | ❌ |
| `ENABLE_WEB_INTERFACE` | Enable web dashboard | `true` | ❌ |
| `WEB_PORT` | Web interface port | `8080` | ❌ |
| `ENABLE_RATE_LIMITING` | Enable rate limiting | `true` | ❌ |
| `RATE_LIMIT_RPM` | Requests per minute limit | `60` | ❌ |
| `ENABLE_PII_DETECTION` | Enable PII detection | `true` | ❌ |
| `ENCRYPTION_KEY` | Encryption key (32 chars) | - | ❌ |
| `ALLOWED_IPS` | Allowed IP addresses | - | ❌ |
| `DATA_RETENTION_DAYS` | Data retention period | `30` | ❌ |

### Web Preview Notes

- `/preview` 會透過 `npx playwright screenshot` 擷取截圖，因此主機需要可執行的 `Node.js` 與 `npx`。
- 第一次在乾淨主機執行時，`npx` 可能需要連到 npm 下載 Playwright 套件與瀏覽器資源；若要避免首次請求才下載，請先執行 `npx --yes playwright install chromium`。
- `/preview` 支援 `http://` 與 `https://`，包含外部網站與 `localhost` 類的本機服務。
- `/preview` 的實際截圖 timeout 是 20 秒；若 Playwright CLI 在這段時間內沒有產出圖片，Bot 會回覆 `截圖逾時`。
- 目前 repo 內沒有打包 Alice bot runtime 的 `Dockerfile`；`docker-compose.prod.yml` 只引用外部提供的 `alice:latest` image，所以若你採用容器部署，必須自行把 `Node.js`、`npx` 與 Playwright Chromium 一起包進該 image。

### Multi-Backend Hermes Configuration

Use `ai_backend: "multi"` when you want a single Alice instance to serve both the default Claude tier and the GPT/Codex tier used by `/ghermes`.

```json
{
  "ai_backend": "multi",
  "model": "sonnet",
  "model_routing": {
    "codex_smart_model": "gpt-5.4"
  },
  "multimedia": {
    "openai_api_key": "sk-..."
  },
  "hermes": {
    "enabled": true,
    "prompts_dir": "internal/app/hermes/prompts"
  }
}
```

OpenAI key resolution for the Codex backend follows this order:

1. `OPENAI_API_KEY` environment variable
2. `multimedia.openai_api_key` in `config.json`

If both are present, `OPENAI_API_KEY` wins. If neither is present, Alice still starts in `ai_backend: "multi"` mode, but the Codex/GPT tier is disabled and `/ghermes`-family commands will be rejected at runtime.

### `/hermes` vs `/ghermes`

Use `/hermes` for the default Claude tier and `/ghermes` for the GPT/Codex tier. Both commands enter the same Hermes planner-executor workflow and can target GitHub Issues, but they route to different model backends.

| Command | Backend | When to use |
|---------|---------|-------------|
| `/hermes` | Claude tier | Default choice when your deployment only has Anthropic configured, or when you want the standard Claude-backed Hermes path. |
| `/ghermes` | GPT/Codex tier | Use when `ai_backend` is `multi` and an OpenAI key is available, especially for validating or running the Codex-specific Hermes path. |

Operational notes:

- `/ghermes` requires `ai_backend: "multi"`; it is intentionally unavailable on `claude`, `api`, or other single-backend modes.
- `/hermes` remains the safer default for general deployments because it does not depend on the Codex backend being enabled.
- For issue-driven runs such as `/hermes #107` or `/ghermes #107`, GitHub checklist sync and Hermes lifecycle behavior are the same; the difference is only the model tier selected underneath.

### Codex Tier Known Limitations

When routing Hermes through `/ghermes`, keep these Codex backend constraints in mind:

- The executor toolset is limited to shell-style command execution; do not assume file edit, web fetch, or other MCP tools are available.
- Session resume is not guaranteed across machines, so resumed runs should not depend on prior local shell state.
- There is no direct `--max-turns` equivalent flag for Codex runs; rely on prompt-side guardrails and bounded task descriptions.
- Planner tool isolation is prompt-enforced only; `codexPlanGuard` reduces misuse risk but does not provide a hard runtime sandbox.

### Codex Session Observation

Alice can observe local Codex CLI / VS Code activity without intercepting or
blocking it. Enable the JSONL watcher with:

```json
{
  "codex_interception": {
    "enabled": true,
    "sessions_dir": "~/.codex/sessions"
  }
}
```

If `sessions_dir` is empty, Alice watches `~/.codex/sessions`. The watcher reads
new lines from `rollout-*.jsonl` files and persists normalized
`CodexSessionUpdate` runtime events. These events are visible from:

- `GET /api/runtime/events?type=CodexSessionUpdate`
- the dashboard Runtime page using the Codex filter
- WebSocket event type `codex_session_update`

External runners can send the same normalized payload directly:

```bash
curl -X POST http://localhost:8080/api/hooks/codex-session-update \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "thread-id",
    "event": "session_update",
    "source": "codex-vscode",
    "event_type": "item.completed",
    "message": "summary"
  }'
```

This is observe-only Phase 1. Alice does not block Codex prompts, alter VS Code,
or proxy the Codex CLI process.

### Security Configuration

```bash
# Generate encryption key
export ENCRYPTION_KEY=$(openssl rand -base64 32)

# Configure IP whitelist
export ALLOWED_IPS="127.0.0.1,10.0.0.0/8,192.168.0.0/16"

# Configure rate limiting
export RATE_LIMIT_RPM=120
export RATE_LIMIT_BURST=10
```

## Monitoring & Observability

### Prometheus Metrics

Access metrics at `http://localhost:8080/metrics`

Key metrics:
- `alice_requests_total` - Total requests processed
- `alice_success_rate` - Success rate percentage
- `alice_api_latency_seconds` - API latency
- `alice_memory_usage_bytes` - Memory usage
- `alice_security_events_total` - Security events

### Grafana Dashboard

1. Access Grafana at `http://localhost:3000`
2. Login with admin/admin (change password)
3. Import dashboard from `monitoring/grafana/dashboards/alice-dashboard.json`

### Alerting

Configure Alertmanager:
1. Edit `monitoring/alertmanager.yml`
2. Set up notification channels (email, Slack, webhooks)
3. Restart Alertmanager

### Log Aggregation

With Loki and Promtail:
```bash
# View logs in Grafana
# - Add Loki datasource: http://loki:3100
# - Explore logs with LogQL queries
```

## Health Checks

### HTTP Health Check
```bash
curl http://localhost:8080/api/health
```

### Container Health Check
```bash
docker exec alice-container /app/healthcheck.sh
```

### Kubernetes Health Check
```bash
kubectl get pods -l app=alice-bot
kubectl describe pod <pod-name>
```

## Scaling & Performance

### Horizontal Scaling

For high-traffic deployments:

```yaml
# Kubernetes HPA
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: alice-bot-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: alice-bot
  minReplicas: 2
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
```

### Performance Tuning

```bash
# Optimize Go runtime
export GOGC=100
export GOMEMLIMIT=512MiB

# Configure rate limiting based on load
export RATE_LIMIT_RPM=300
export RATE_LIMIT_BURST=50
```

## Security Best Practices

### Network Security
```bash
# Use firewall rules
sudo ufw allow 8080/tcp
sudo ufw enable

# Configure reverse proxy with SSL
# See nginx/nginx.conf for example
```

### Container Security
```bash
# Run as non-root user
# Use read-only filesystem
# Drop capabilities
# See Dockerfile for security configuration
```

### Data Protection
```bash
# Encrypt sensitive data
export ENCRYPTION_KEY=$(openssl rand -base64 32)

# Enable PII detection
export ENABLE_PII_DETECTION=true

# Configure data retention
export DATA_RETENTION_DAYS=30
```

## Troubleshooting

### Common Issues

1. **Bot not responding**
   ```bash
   # Check bot token
   curl -s "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/getMe"

   # Check logs
   docker-compose logs alice
   ```

2. **High memory usage**
   ```bash
   # Monitor memory
   curl http://localhost:8080/api/performance/analytics

   # Restart if needed
   docker-compose restart alice
   ```

3. **API rate limits**
   ```bash
   # Check rate limiting stats
   curl http://localhost:8080/api/security/stats

   # Adjust limits
   export RATE_LIMIT_RPM=120
   ```

4. **`/preview` 截圖失敗或逾時**
   ```bash
   # 確認 Node.js 與 npx 可用
   node -v
   npx --version

   # 確認 Playwright CLI 可執行
   npx --yes playwright --version

   # 在乾淨主機或新 image 中預裝 Chromium，避免首次請求時才下載
   npx --yes playwright install chromium

   # 查看 bot log
   docker-compose logs -f alice
   ```

   如果是第一次在乾淨主機執行，請確認機器可連外下載 Playwright 套件；若環境不允許連網，請在建置 image 或主機佈署階段先把相關依賴預裝好。

### Log Analysis

```bash
# Application logs
docker-compose logs -f alice

# Security events
curl "http://localhost:8080/api/security/events?severity=high"

# Performance metrics
curl "http://localhost:8080/api/performance/trends?hours=24"
```

### Performance Analysis

```bash
# Export metrics for analysis
curl http://localhost:8080/api/performance/export > metrics.json

# Check recommendations
curl http://localhost:8080/api/performance/recommendations
```

## Backup & Recovery

### Data Backup
```bash
# Backup persistent volumes
docker run --rm -v alice_data:/data -v $(pwd):/backup alpine tar czf /backup/alice-data.tar.gz -C /data .

# Backup configuration
cp config.json config.json.bak
```

### Database Migration
```bash
# If using external database
# Export decision logs and performance data
curl http://localhost:8080/api/decisions/export > decisions.json
curl http://localhost:8080/api/performance/export > performance.json
```

## Updates & Maintenance

### Update Process
```bash
# Pull latest image
docker-compose pull alice

# Recreate container
docker-compose up -d alice

# Verify health
curl http://localhost:8080/api/health
```

### Maintenance Mode
```bash
# Scale down
kubectl scale deployment alice-bot --replicas=0

# Perform maintenance
# ...

# Scale up
kubectl scale deployment alice-bot --replicas=2
```

## Support & Resources

- **Documentation**: [GitHub Repository](https://github.com/your-org/alice)
- **Issues**: [GitHub Issues](https://github.com/your-org/alice/issues)
- **Monitoring**: Access Grafana dashboard for real-time metrics
- **Logs**: Use Loki for centralized log aggregation
- **Alerts**: Configure Alertmanager for proactive monitoring

For additional support, please open an issue in the GitHub repository.
