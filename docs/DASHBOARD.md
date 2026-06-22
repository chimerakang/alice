# 🎨 Alice AI Agent Monitoring Dashboard

A comprehensive, real-time monitoring dashboard for Alice AI agents built with **Proto-First API** architecture and modern **Dark Mode OLED** design.

## 🌟 Features

### ✅ **Proto-First API Architecture**
- **Type-safe API contracts** using Protocol Buffers
- **Single source of truth** for all data structures
- **Automatic code generation** for Go backend and TypeScript frontend
- **Schema evolution** with backward compatibility

### 🎯 **Real-time Monitoring**
- **Live agent status** tracking (pending, running, success, error)
- **Tool execution monitoring** with success rates and performance metrics
- **Decision logging** with AI transparency and context capture
- **Multi-agent coordination** status and task distribution
- **Performance analytics** with resource usage and recommendations
- **Security event monitoring** with PII detection and audit trails

### 🎨 **Modern UI/UX Design**
- **Dark Mode (OLED)** theme optimized for developers
- **Fira Code + Fira Sans** typography for technical precision
- **Professional color palette** with status-aware indicators
- **Responsive layout** supporting desktop, tablet, and mobile
- **Accessible design** with WCAG AAA compliance
- **Smooth animations** with respect for `prefers-reduced-motion`

### 📊 **Data Visualization**
- **Real-time charts** using Chart.js with dark theme integration
- **Agent activity trends** with 24-hour time series
- **Tool usage distribution** with interactive doughnut charts
- **Performance metrics** with gradient fills and hover effects
- **Status indicators** with pulse animations and glow effects

## 🏗️ Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────────┐
│   Frontend      │    │   Go Backend     │    │   Proto Definitions │
│                 │    │                  │    │                     │
│ • TypeScript    │◄──►│ • HTTP Server    │◄──►│ • Schema Contracts  │
│ • Chart.js      │    │ • Proto API      │    │ • Type Generation   │
│ • Tailwind CSS  │    │ • Agent Core     │    │ • Version Control   │
│ • Dark Theme    │    │ • Tool Executor  │    │ • Documentation     │
└─────────────────┘    └──────────────────┘    └─────────────────────┘
```

## 🚀 Quick Start

### 1. **Build and Run Alice**

```bash
# Build the application
go build -o alice .

# Or run directly
go run .

# With proto generation (if needed)
make proto-gen && go run .
```

### 2. **Access the Dashboard**

```
🌐 Main Dashboard: http://localhost:8080
📊 API Health:     http://localhost:8080/api/health
⚙️  API Stats:      http://localhost:8080/api/stats
```

### 3. **Test API Endpoints**

```bash
# List all agents
curl http://localhost:8080/api/agents

# Get recent tools
curl http://localhost:8080/api/tools/recent?limit=10

# Get decisions
curl http://localhost:8080/api/decisions/recent

# Get performance metrics
curl http://localhost:8080/api/performance/analytics

# Get security events
curl http://localhost:8080/api/security/events
```

## 📱 UI Components

### **Key Metrics Cards**
- **Active Agents**: Real-time count with trend indicators
- **Tool Executions**: Total count with success rate percentage
- **Decisions Logged**: 24-hour count with timestamps
- **Token Usage**: Cumulative usage with cost tracking

### **Interactive Charts**
- **Agent Activity**: 24-hour line chart with dual datasets
- **Tool Usage**: Doughnut chart with hover interactions
- **Performance Trends**: Time-series analytics
- **Status Distribution**: Real-time status breakdown

### **Detail Lists**
- **Active Agents**: Live status with chat/thread IDs
- **Recent Tools**: Execution history with duration metrics
- **Recent Decisions**: AI transparency logs with outcomes

## 🛠️ Development

### **File Structure**
```
web/
├── index.html          # Main dashboard page
├── css/
│   └── dashboard.css   # Custom styles and animations
├── js/
│   ├── alice-api.js    # API client with type safety
│   ├── dashboard.js    # Dashboard controller logic
│   └── charts.js       # Chart.js visualization manager
└── demo.html           # Feature demonstration page

gen/ts/                 # TypeScript type definitions
├── package.json        # npm package configuration
├── tsconfig.json       # TypeScript compiler config
├── alice/v1/
│   └── types.ts        # Complete API type definitions
└── README.md           # Usage documentation
```

### **API Client Usage**

```typescript
import { createAliceClient, isErrorResponse } from '@alice/api-types';

const client = createAliceClient({
  baseUrl: 'http://localhost:8080'
});

// Type-safe API calls
const agents = await client.listAgents();
if (!isErrorResponse(agents)) {
  console.log('Active agents:', agents.agents);
}
```

### **Custom Styling**

```css
/* OLED Dark Theme Variables */
:root {
  --deep-black: #000000;
  --dark-grey: #121212;
  --midnight-blue: #0A0E27;
  --dashboard-primary: #3B82F6;
}

/* Glow effects for status indicators */
.status-glow {
  text-shadow: 0 0 10px currentColor;
}
```

## 🔧 Configuration

### **Environment Variables**
```bash
# Alice Configuration
export ANTHROPIC_API_KEY="sk-ant-xxxxx"
export TELEGRAM_BOT_TOKEN="123456:ABC-DEF"
export CLAUDE_MODEL="claude-sonnet-4-20250514"
export PROJECT_DIR="/path/to/your/project"
export ALLOWED_USER_IDS="123456789"

# Web Interface Configuration
export WEB_PORT="8080"
export WEB_HOST="localhost"
```

### **API Configuration**
```javascript
// Update API base URL
window.aliceApi = new AliceApiClient('http://your-server:8080');

// Configure refresh interval
window.dashboard.updateFrequency = 3000; // 3 seconds
```

## 📊 Monitoring Capabilities

### **Agent Monitoring**
- **Status Tracking**: pending, running, success, error, cancelled
- **Performance Metrics**: token usage, session duration, success rates
- **Project Association**: directory paths and git state
- **Session Management**: chat IDs, thread IDs, and unique identifiers

### **Tool Execution Tracking**
- **Execution History**: comprehensive logs with input/output data
- **Performance Analytics**: duration, success rates, error patterns
- **Tool Distribution**: usage patterns across different tool types
- **Resource Monitoring**: CPU, memory, and I/O metrics

### **Decision Logging**
- **AI Transparency**: complete decision context and reasoning
- **Outcome Tracking**: success/failure with detailed summaries
- **Privacy Controls**: configurable data retention and anonymization
- **Audit Trails**: compliance-ready logging with timestamps

### **Security Monitoring**
- **PII Detection**: automatic sensitive data identification
- **Access Monitoring**: user authentication and authorization logs
- **Threat Detection**: suspicious activity and anomaly detection
- **Compliance Tracking**: audit-ready security event logs

## 🎯 Performance Optimizations

### **Frontend Optimizations**
- **Lazy Loading**: charts and data loaded on demand
- **Virtualization**: efficient handling of large datasets
- **Debounced Updates**: reduced API calls with smart batching
- **Caching Strategy**: intelligent data caching with TTL
- **Bundle Optimization**: minified CSS/JS with CDN delivery

### **Backend Optimizations**
- **Proto Serialization**: efficient binary data transfer
- **Connection Pooling**: optimized database connections
- **Memory Management**: efficient Go struct handling
- **Concurrent Processing**: parallel API request handling
- **Response Compression**: gzipped API responses

## 🔐 Security Features

### **Authentication & Authorization**
- **User Whitelist**: configurable allowed user IDs
- **Session Management**: secure session handling
- **API Key Protection**: secure key storage and rotation
- **CORS Configuration**: controlled cross-origin requests

### **Data Protection**
- **PII Redaction**: automatic sensitive data masking
- **Secure Logging**: compliance-ready audit trails
- **Data Encryption**: encrypted data storage and transmission
- **Privacy Controls**: configurable data retention policies

## 🧪 Testing

### **Manual Testing**
```bash
# Start Alice with test configuration
cp config.test.json config.json
go run .

# Open dashboard in browser
open http://localhost:8080

# Trigger some agent activity via Telegram
# Monitor dashboard for real-time updates
```

### **API Testing**
```bash
# Test all API endpoints
curl -s http://localhost:8080/api/agents | jq
curl -s http://localhost:8080/api/tools/recent | jq
curl -s http://localhost:8080/api/decisions | jq
curl -s http://localhost:8080/api/performance/analytics | jq
curl -s http://localhost:8080/api/security/stats | jq
```

### **Load Testing**
```bash
# Simulate multiple concurrent requests
for i in {1..10}; do
  curl -s http://localhost:8080/api/agents &
done
wait

# Monitor dashboard performance during load
```

## 🚧 Future Enhancements

### **Phase 2: Advanced Features**
- [ ] **WebSocket Integration**: real-time push updates
- [ ] **Alert System**: configurable notifications and thresholds
- [ ] **Export Functionality**: PDF/CSV reports and data export
- [ ] **User Management**: multi-user access with role-based permissions
- [ ] **Custom Dashboards**: user-configurable widget layouts

### **Phase 3: Advanced Team Features**
- [ ] **SSO Integration**: enterprise authentication providers
- [ ] **Multi-tenant Support**: isolated environments per organization
- [ ] **Advanced Analytics**: ML-powered insights and predictions
- [ ] **Integration APIs**: third-party monitoring tool connections
- [ ] **Compliance Reporting**: automated audit and compliance reports

## 📚 Documentation

- **[Proto API Reference](gen/ts/README.md)**: Complete TypeScript API documentation
- **[Backend Architecture](CLAUDE.md)**: Go backend implementation details
- **[Build System](Makefile)**: proto generation and build automation
- **[Security Guide](/.claude/CLAUDE.md)**: security best practices and guidelines

## 🎉 Ready for Development!

Your Alice AI Agent Monitoring Dashboard is now complete with:

✅ **Proto-First API** with full type safety
✅ **Modern Dark Theme** optimized for developers
✅ **Real-time Monitoring** with comprehensive metrics
✅ **Professional UI/UX** following industry best practices
✅ **Responsive Design** supporting all device sizes
✅ **Performance Optimized** for production deployment

Start the server and navigate to `http://localhost:8080` to see your dashboard in action! 🚀