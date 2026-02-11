# Alice API TypeScript Types

TypeScript type definitions for the Alice AI Agent API, generated from Protocol Buffer definitions.

## Installation

```bash
# If published to npm
npm install @alice/api-types

# Or copy the files directly to your project
cp -r alice/ your-project/src/types/
```

## Usage

### Import Types

```typescript
import {
  AliceApiClient,
  AgentInfo,
  ToolExecution,
  DecisionLog,
  createAliceClient
} from '@alice/api-types';
```

### Create API Client

```typescript
const client = createAliceClient({
  baseUrl: 'http://localhost:8080',
  timeout: 10000
});
```

### Use the API

```typescript
// List all agents
const agentsResponse = await client.listAgents();
if (!isErrorResponse(agentsResponse)) {
  console.log('Agents:', agentsResponse.agents);
}

// Get recent tool executions
const toolsResponse = await client.getRecentTools(20);
console.log('Recent tools:', toolsResponse.executions);

// Get agent information
const agentResponse = await client.getAgent(123456, 1);
if (agentResponse.agent) {
  console.log('Agent status:', formatStatus(agentResponse.agent.status));
}
```

### Type-Safe Development

```typescript
// All responses are properly typed
interface MyDashboard {
  agents: AgentInfo[];
  recentTools: ToolExecution[];
  decisions: DecisionLog[];
}

async function buildDashboard(client: AliceApiClient): Promise<MyDashboard> {
  const [agentsRes, toolsRes, decisionsRes] = await Promise.all([
    client.listAgents(),
    client.getRecentTools(10),
    client.getRecentDecisions(10)
  ]);

  return {
    agents: agentsRes.agents || [],
    recentTools: toolsRes.executions || [],
    decisions: decisionsRes.decisions || []
  };
}
```

### Error Handling

```typescript
try {
  const response = await client.getAgent(123456);

  if (isErrorResponse(response)) {
    console.error('API Error:', response.error.message);
    return;
  }

  // TypeScript knows response.agent exists here
  console.log('Agent:', response.agent);
} catch (error) {
  console.error('Network Error:', error);
}
```

## API Reference

### Core Types

- `AgentInfo` - Agent instance information
- `TokenStats` - Token usage statistics
- `ToolExecution` - Tool execution record
- `DecisionLog` - AI decision logging
- `PerformanceMetric` - Performance monitoring data
- `SecurityEvent` - Security event logging

### Enums

- `Status` - Execution status (PENDING, RUNNING, SUCCESS, ERROR, CANCELLED)
- `Severity` - Event severity (LOW, MEDIUM, HIGH, CRITICAL)
- `AgentType` - Agent specialization type
- `PrivacyLevel` - Data privacy classification

### Client Methods

#### Agents
- `listAgents()` - List all agents
- `getAgent(chatId, threadId?)` - Get specific agent

#### Tools
- `getRecentTools(limit?)` - Get recent tool executions
- `getToolExecutions()` - Get tool execution statistics

#### Decisions
- `getDecisions()` - Get decision statistics
- `getRecentDecisions(limit?)` - Get recent decisions

#### MultiAgent
- `getMultiAgentStatus()` - Get coordination status
- `getMultiAgentStats()` - Get coordination statistics

#### Performance
- `getPerformanceAnalytics()` - Get performance analytics
- `getPerformanceMetrics()` - Get performance metrics

#### Security
- `getSecurityEvents()` - Get security events
- `getSecurityStats()` - Get security statistics

## Development

```bash
# Install dependencies
npm install

# Build TypeScript
npm run build

# The built files will be in ./dist/
```

## Proto-First Architecture

These types are automatically generated from Protocol Buffer definitions, ensuring:

- **Type Safety**: Compile-time validation of API contracts
- **Documentation**: Self-documenting API interfaces
- **Consistency**: Single source of truth for API schemas
- **Backward Compatibility**: Managed schema evolution

## Frontend Framework Examples

### React Hook

```typescript
import { useState, useEffect } from 'react';
import { AliceApiClient, AgentInfo } from '@alice/api-types';

function useAgents(client: AliceApiClient) {
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    client.listAgents()
      .then(response => {
        setAgents(response.agents || []);
        setLoading(false);
      })
      .catch(error => {
        console.error('Failed to load agents:', error);
        setLoading(false);
      });
  }, [client]);

  return { agents, loading };
}
```

### Vue 3 Composable

```typescript
import { ref, onMounted } from 'vue';
import { AliceApiClient, AgentInfo } from '@alice/api-types';

export function useAgents(client: AliceApiClient) {
  const agents = ref<AgentInfo[]>([]);
  const loading = ref(true);

  onMounted(async () => {
    try {
      const response = await client.listAgents();
      agents.value = response.agents || [];
    } catch (error) {
      console.error('Failed to load agents:', error);
    } finally {
      loading.value = false;
    }
  });

  return { agents, loading };
}
```

## License

MIT