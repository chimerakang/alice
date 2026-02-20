# Enterprise IAM (Identity & Access Management) Architecture Research

**Research Date**: February 20, 2026
**Focus**: Multi-tenant systems, authentication, authorization, and Go ecosystem integration

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Multi-Tenant Member System](#multi-tenant-member-system)
3. [Authentication System](#authentication-system)
4. [Authorization & Permission Control](#authorization--permission-control)
5. [Industry Solutions Comparison](#industry-solutions-comparison)
6. [Data Model Design](#data-model-design)
7. [Security Best Practices](#security-best-practices)
8. [Go Ecosystem Integration](#go-ecosystem-integration)
9. [Telegram Bot Integration Patterns](#telegram-bot-integration-patterns)
10. [Implementation Recommendations](#implementation-recommendations)

---

## Executive Summary

Enterprise IAM systems in 2026 face increasing complexity due to multi-tenant SaaS architectures, distributed microservices, and evolving security requirements. Key trends include:

- **Multi-Tenant Isolation**: Row-Level Security (RLS) emerging as the preferred balance between complexity and security
- **Authentication Evolution**: WebAuthn/FIDO2 becoming the gold standard, OIDC replacing SAML for modern apps
- **Authorization Shift**: ReBAC (Relationship-Based Access Control) gaining traction over traditional RBAC for complex hierarchies
- **Go Ecosystem Maturity**: Strong libraries (Casbin, Ory) and frameworks (go-saas) available for enterprise IAM
- **Automation Priority**: 68.6% of organizations using generative AI for provisioning/deprovisioning workflows

---

## Multi-Tenant Member System

### 1.1 Data Isolation Strategies

#### Three Main Approaches:

| Strategy | Isolation Level | Complexity | Scalability | Use Case |
|----------|----------------|------------|-------------|----------|
| **Database per Tenant** | Highest | High | Low | High-value enterprise clients, strict compliance |
| **Schema per Tenant** | Medium | Medium | Medium | Mid-market SaaS, regulatory requirements |
| **Row-Level Security (RLS)** | Application-level | Low | High | Multi-tenant SaaS, cost-efficient scaling |

#### Row-Level Security (RLS) - Recommended for Most SaaS

PostgreSQL RLS emerged as the **balanced choice** in 2026:

**How it works**:
```sql
-- Add tenant_id to every table
ALTER TABLE documents ADD COLUMN tenant_id UUID NOT NULL;

-- Create RLS policy
CREATE POLICY tenant_isolation ON documents
    USING (tenant_id = current_setting('app.current_tenant')::UUID);

-- Enable RLS
ALTER TABLE documents ENABLE ROW LEVEL SECURITY;

-- Set tenant context at runtime
SET app.current_tenant = 'tenant-uuid-here';
```

**Benefits**:
- ✅ All tenants share the same tables → simple schema management
- ✅ Centralized enforcement at database level → reduces developer burden
- ✅ Cost-efficient scaling → single database instance
- ✅ Schema changes only need to be performed once

**Drawbacks**:
- ❌ Lower isolation than separate schemas/databases
- ❌ Performance can degrade with very large tenant counts (billions of rows)
- ❌ Requires careful configuration to avoid data leaks

**Implementation Methods**:

1. **Per-tenant database users** (not recommended):
   - Create a PostgreSQL role for every tenant
   - Hard to maintain and doesn't scale well

2. **Shared application user with runtime context** (recommended):
   - Use a single PostgreSQL login for the application
   - Set `app.current_tenant` parameter at runtime
   - Policies automatically filter rows based on context

### 1.2 Organization Architecture Management

#### LDAP/Active Directory Synchronization

**Security Best Practices**:
- ✅ Use LDAPS (port 636) for encrypted connections
- ✅ Leverage SASL (Kerberos) for enhanced authentication security
- ✅ Configure least-privilege access control for sync accounts
- ✅ Map LDAP schema to AD schema for compatibility

**Integration Workflow**:
```
1. Assess current directory services
2. Configure LDAP server to communicate with AD
3. Set up synchronization rules for data consistency
4. Map user/group attributes between systems
5. Test authentication, authorization, directory queries
6. Monitor and audit synchronization logs
```

**Access Control Configuration**:
- Configure AD groups based on role or access level
- Keep groups up-to-date
- Assign least amount of access necessary (principle of least privilege)

### 1.3 User Lifecycle Management

#### Modern Automation (2026 Trends)

**Key Statistics**:
- 68.6% of organizations using generative AI for provisioning/deprovisioning
- Organizations with mature lifecycle management experience:
  - 65% fewer privilege-based security incidents
  - Up to 45% reduction in identity administration costs

#### Joiner-Mover-Leaver Process:

| Stage | Actions | Automation Goal | Impact |
|-------|---------|----------------|--------|
| **Joiner** | Create accounts, assign roles, grant access | Reduce from days to minutes | Day-one productivity |
| **Mover** | Update roles, adjust permissions, transfer access | Real-time role changes | Zero disruption |
| **Leaver** | Revoke access, archive data, deactivate accounts | Immediate deprovisioning | Eliminate security gaps |

#### SCIM (System for Cross-domain Identity Management):

**What it is**: Open standard for automating user identity data exchange between IdP and cloud apps

**How it works**:
- Uses REST APIs for real-time provisioning, updates, and deprovisioning
- Standardized format reduces integration complexity
- Keeps identities in sync across systems without manual intervention

**Implementation**:
```
HR System (Trigger) → SCIM → Identity Provider → SCIM → Cloud Apps
     |                                                         |
     └─────── Automated End-to-End Lifecycle ────────────────┘
```

---

## Authentication System

### 2.1 OAuth 2.0 / OpenID Connect Best Practices

#### Protocol Selection (2026 Guidance):

| Use Case | Protocol | Reason |
|----------|----------|--------|
| Modern APIs, mobile apps, SPAs | **OIDC** | Lightweight JSON, built for REST |
| Legacy enterprise apps, corporate intranet | **SAML** | XML assertions, audit trail requirements |
| Hybrid enterprise (72% adoption) | **Multi-protocol SSO** | Best of both worlds |

#### OAuth 2.0 Flow Recommendations:

**Modern Security Enhancements (2026)**:
1. **Authorization Code + PKCE** — Works everywhere, prevents code interception
2. **PAR + JAR** — Pushed Authorization Requests + JWT-secured Authorization Requests for defense in depth
3. **DPoP** — Proof-of-possession for stronger token security
4. **mTLS** — Mutual TLS for machine-to-machine auth

#### OIDC vs SAML Technical Differences:

| Aspect | OIDC | SAML |
|--------|------|------|
| **Data Format** | JSON (lightweight, readable) | XML (verbose, complex) |
| **Token Type** | JWT | XML assertions |
| **Use Case** | Mobile, SPA, microservices, IoT | Browser-based enterprise apps |
| **Complexity** | Simple, RESTful | Complex, requires XML processing |
| **Industry Adoption** | Growing (modern stack) | Established (legacy systems) |

#### OIDC Architecture for Modern Apps:

```
┌─────────────┐
│   Browser   │
│  (SPA/App)  │
└──────┬──────┘
       │ 1. Login redirect
       ▼
┌─────────────────────┐
│  Identity Provider  │  ← OIDC Authorization Server
│  (Keycloak/Auth0)   │
└──────┬──────────────┘
       │ 2. User authenticates
       │ 3. Issue tokens (JWT)
       ▼
┌─────────────┐
│  HttpOnly   │  ← Store tokens in secure cookie
│   Cookie    │     (NOT localStorage!)
└──────┬──────┘
       │ 4. API requests with cookie
       ▼
┌─────────────────────┐
│   Backend API       │  ← Validate JWT signature
│   (Go service)      │     Extract user claims
└─────────────────────┘
```

### 2.2 Multi-Factor Authentication (MFA/2FA)

#### Factor Hierarchy (Security Level):

| Method | Security Level | Phishing Resistant | Implementation Complexity | 2026 Recommendation |
|--------|---------------|-------------------|--------------------------|---------------------|
| **WebAuthn/FIDO2** | ⭐⭐⭐⭐⭐ | ✅ Yes | Medium | **Gold standard** |
| **Passkeys** | ⭐⭐⭐⭐⭐ | ✅ Yes | Low | **Best UX + Security** |
| **TOTP (Authenticator App)** | ⭐⭐⭐⭐ | ❌ No | Low | **Good baseline** |
| **Push Notifications** | ⭐⭐⭐ | ❌ No | Medium | Use with caution |
| **SMS OTP** | ⭐⭐ | ❌ No | Low | **Avoid as primary** |
| **Email OTP** | ⭐⭐ | ❌ No | Low | **Avoid as primary** |

#### TOTP Implementation:

**How it works**:
1. User installs TOTP app (Google Authenticator, Authy, etc.)
2. Web app generates a secret key and displays QR code
3. User scans QR code → app stores seed
4. App generates 6-digit code every 60 seconds
5. User enters code during login → server validates against same algorithm

**Advantages**:
- ✅ No need for hardware tokens
- ✅ Works offline
- ✅ Cheap and easy to implement

**Security considerations**:
- Encrypt TOTP secrets at rest
- Never store secrets in plain text
- Implement rate limiting on verification attempts
- Provide backup codes for account recovery

#### WebAuthn/FIDO2 - The Future:

**Why it's superior**:
- Uses **public key cryptography** (more secure than shared secrets in TOTP)
- **Origin-bound credentials** — credentials tied to specific domain
- Phishing domains cannot use them (domain validation built-in)
- Best combination of security and user experience

**Implementation Strategy (2026)**:
```
Phase 1: Implement TOTP for all users (baseline MFA)
         ↓
Phase 2: Add WebAuthn for high-security users (admin, finance)
         ↓
Phase 3: Migrate all users to Passkeys (best UX)
         ↓
Phase 4: Deprecate SMS/email OTP
```

**Best Practices**:
- ✅ Require MFA for administrative or high-privileged users
- ✅ Implement secure MFA reset procedure
- ✅ Rate limit verification attempts aggressively
- ✅ Log all MFA events for audit trail
- ✅ Test thoroughly — MFA bugs can lock users out

### 2.3 Session Management & Token Strategy

#### Session Timeout Enforcement:

**Timeout Types**:
1. **Absolute timeout** — Maximum session duration regardless of activity
2. **Idle timeout** — Session expires after inactivity period

**Example configuration**:
```json
{
  "session": {
    "absolute_timeout": "8h",
    "idle_timeout": "30m",
    "extend_on_activity": true
  }
}
```

#### Three Logout Strategies:

| Strategy | How it Works | Pros | Cons | Use Case |
|----------|-------------|------|------|----------|
| **RP-Initiated Logout** | App redirects to IdP logout endpoint | Simple | No multi-app coordination | Single app deployments |
| **Front-Channel Logout** | Hidden iframes notify all apps | Works across browsers | Less reliable | Browser-based apps |
| **Back-Channel Logout** | Server-to-server notifications | Most reliable | Requires backend infrastructure | Enterprise SSO |

#### Token Storage Security (Critical):

**❌ NEVER DO THIS**:
```javascript
// WRONG - Vulnerable to XSS attacks
localStorage.setItem('access_token', token);
sessionStorage.setItem('refresh_token', token);
```

**✅ CORRECT APPROACH**:

| Platform | Recommended Storage | Reason |
|----------|-------------------|--------|
| **Web Apps** | HttpOnly + Secure cookie | JavaScript cannot access (XSS protection) |
| **Mobile (iOS)** | Keychain | Hardware-backed encryption at rest |
| **Mobile (Android)** | Keystore | Hardware-backed encryption at rest |

**HttpOnly Cookie Example**:
```go
// Go backend sets HttpOnly cookie
http.SetCookie(w, &http.Cookie{
    Name:     "access_token",
    Value:    jwtToken,
    HttpOnly: true,  // Prevent JavaScript access
    Secure:   true,  // HTTPS only
    SameSite: http.SameSiteStrictMode,
    MaxAge:   900,   // 15 minutes
})
```

#### Refresh Token Rotation (2026 Standard):

**Why it's critical**:
- Every time a refresh token is exchanged for a new access token, a **new refresh token is also returned**
- Old refresh token is immediately invalidated
- Prevents stolen refresh tokens from being reused

**How it protects against attacks**:
```
1. Attacker steals refresh token at time T0
2. Legitimate user uses refresh token at T1 → new token issued, old invalidated
3. Attacker tries to use stolen token at T2 → REJECTED (token already used)
4. System detects potential compromise → invalidate all tokens for that user
```

**Implementation with jti (JWT ID)**:
```go
type RefreshToken struct {
    JTI       string    // Unique token identifier
    UserID    string
    IssuedAt  time.Time
    ExpiresAt time.Time
    Used      bool      // Mark as used after rotation
}

// On refresh:
// 1. Check if token.Used == true → reject (replay attack detected)
// 2. Mark token as used
// 3. Issue new access + refresh tokens with new JTI
// 4. Return new tokens to client
```

**Additional Security Measures**:
- Use RS256 (asymmetric) instead of HS256 (symmetric) for signing
- Only holder of private key can sign tokens
- Anyone can verify with public key
- Avoid embedding sensitive data in JWT payload (JWTs are base64-encoded, not encrypted)

#### Session Store Architecture:

**Stateless BFF (Backend-for-Frontend)**:
```
┌─────────┐   HttpOnly Cookie (JWT)   ┌─────────┐
│ Browser │ ─────────────────────────→ │   BFF   │
│         │ ←───────────────────────── │ (Go API)│
└─────────┘                            └─────────┘
                                           │
                                           ▼
                                     Validate JWT
                                     (no DB lookup)
```

**Stateful BFF (Session ID)**:
```
┌─────────┐   HttpOnly Cookie (SessionID)   ┌─────────┐
│ Browser │ ────────────────────────────────→ │   BFF   │
│         │ ←──────────────────────────────── │ (Go API)│
└─────────┘                                   └────┬────┘
                                                   │
                                                   ▼
                                            ┌────────────┐
                                            │   Redis    │
                                            │ (Sessions) │
                                            └────────────┘
```

**Trade-offs**:

| Approach | Pros | Cons |
|----------|------|------|
| **Stateless (JWT)** | No database lookup, scales easily | Cannot revoke tokens until expiry |
| **Stateful (Session ID)** | Can revoke immediately, smaller cookies | Requires Redis/DB lookup on every request |

**Best Practice (2026)**: Use **short-lived JWTs (15 min) + refresh token rotation** for balance between performance and security.

---

## Authorization & Permission Control

### 3.1 RBAC vs ABAC vs ReBAC

#### Comprehensive Comparison Table:

| Aspect | RBAC | ABAC | ReBAC |
|--------|------|------|-------|
| **Decision Basis** | User's role(s) | User/resource/environment attributes | Relationships between entities |
| **Complexity** | Low | High | Medium-High |
| **Flexibility** | Low | Very High | High |
| **Scalability** | Breaks down with many roles | Scales with policies | Scales with graph complexity |
| **Use Case** | Stable roles, predictable access | Dynamic, context-aware scenarios | Hierarchical, interconnected data |
| **Example** | "Admin can edit posts" | "User can edit posts if they're the author AND it's a weekday AND their department matches" | "Users can access documents in projects they're assigned to" |
| **Best For** | Small teams, simple hierarchies | Enterprise with complex rules | SaaS, multi-tenant, document management |
| **Performance** | Fast (role lookup) | Slower (policy evaluation) | Medium (graph traversal) |

#### When to Use Each Model:

**RBAC** (Role-Based Access Control):
```
✅ Use when:
- You have stable, well-defined roles (Admin, Editor, Viewer)
- Access requirements are predictable
- Team size is small-to-medium
- Simplicity is a priority

❌ Breaks down when:
- Roles proliferate ("AdminForProjectA", "EditorForRegionB")
- Context matters (time, location, resource attributes)
- Multi-tenant SaaS with complex hierarchies
```

**ABAC** (Attribute-Based Access Control):
```
✅ Use when:
- Access depends on dynamic context (time of day, IP location, device type)
- You need fine-grained, policy-driven control
- Regulations require attribute-based decisions (GDPR, HIPAA)

❌ Watch out for:
- Complex policies become hard to debug
- Performance overhead from policy evaluation
- Requires centralized policy management
```

**ReBAC** (Relationship-Based Access Control):
```
✅ Use when:
- You have complex hierarchies (organizations → teams → projects → documents)
- Access is based on connections ("can view if member of parent organization")
- Building multi-tenant SaaS, document management, social platforms

❌ Challenges:
- Requires graph database or efficient graph storage
- Query performance depends on relationship depth
- More complex to reason about than RBAC
```

#### Hybrid Approach (Recommended for 2026):

**Policy-Based Access Control** = RBAC + ABAC + ReBAC:

```
Authorization Decision = (
    RBAC: Does user have baseline role?
    +
    ABAC: Do attributes refine permissions?
    +
    ReBAC: Do relationships scope access?
)
```

**Example**:
```
User: Alice
Role: Developer (RBAC)
Attribute: Department=Engineering, Location=US (ABAC)
Relationship: Member of Project "Phoenix" (ReBAC)

Decision:
✅ Can view code in Project Phoenix (relationship)
✅ Can deploy to staging (role + attribute: Developer in Engineering)
❌ Cannot deploy to production (role limitation)
❌ Cannot access Project "Athena" (no relationship)
```

### 3.2 Google Zanzibar - ReBAC at Scale

#### What is Zanzibar?

**Official Definition**: Google's centralized authorization system handling over **10 million checks/second** across Gmail, Drive, Calendar, YouTube, etc.

**Scale Metrics**:
- **Trillions** of access control lists
- **Millions** of authorization requests per second
- **95th percentile latency**: < 10ms
- **Availability**: > 99.999%

#### Core Concepts:

**Relation Tuples**:
```
Format: <object>#<relation>@<user>

Examples:
document:readme#viewer@user:alice
folder:2024#editor@user:bob
folder:2024#parent@document:readme
organization:acme#member@user:alice
```

**How it works**:
```
Query: "Can Alice view document:readme?"

Step 1: Check direct relationship
        document:readme#viewer@user:alice? → Not found

Step 2: Check inherited relationship
        document:readme#parent → folder:2024
        folder:2024#viewer@user:alice? → Not found

Step 3: Check through organization
        folder:2024#parent → organization:acme
        organization:acme#member@user:alice? → ✅ FOUND

Decision: ✅ ALLOW (via organization membership)
```

#### Consistency Guarantees:

**Problem**: Prevent false positives/negatives when permissions change rapidly

**Solution**: Zanzibar uses **Paxos-based distributed consistency** to ensure:
- Modifications and checks respect temporal order
- No race conditions between permission grant/revoke
- Global consistency across all services

#### ReBAC Authorization Model:

**Question Format**:
- ❌ Old (RBAC): "Does user X have role A?"
- ✅ New (ReBAC): "What relationship does user X have with resource Y?"

**Example Policies**:
```
# Direct access
document:readme#viewer@user:alice

# Group-based access
document:readme#viewer@group:engineers
group:engineers#member@user:alice

# Hierarchical access (via parent relationship)
document:readme#parent@folder:2024
folder:2024#viewer@user:alice
```

#### Open-Source Implementations:

| Project | Language | Features | Use Case |
|---------|----------|----------|----------|
| **SpiceDB** | Go | Full Zanzibar implementation, gRPC API | Production-ready, cloud-native |
| **Ory Keto** | Go | Zanzibar-inspired, headless, API-first | Microservices, self-hosted |
| **Permify** | Go | Developer-friendly, TypeScript SDK | Startups, rapid prototyping |

### 3.3 API-Level Permission Verification

#### Enforcement Points:

```
┌─────────────┐
│   Client    │
└──────┬──────┘
       │ 1. HTTP Request + JWT
       ▼
┌─────────────────────┐
│   API Gateway       │ ← 1st Check: Valid token?
│   (Rate limiting)   │
└──────┬──────────────┘
       │ 2. Forward to backend
       ▼
┌─────────────────────┐
│   Middleware        │ ← 2nd Check: Extract claims
│   (Authentication)  │
└──────┬──────────────┘
       │ 3. User context set
       ▼
┌─────────────────────┐
│   Authorization     │ ← 3rd Check: Permission check
│   Layer (Casbin)    │    (RBAC/ABAC/ReBAC)
└──────┬──────────────┘
       │ 4. Authorized request
       ▼
┌─────────────────────┐
│   Business Logic    │ ← 4th Check: Resource-level
│   (Row-Level)       │    (RLS, ownership)
└─────────────────────┘
```

#### Go Implementation Example (Casbin):

```go
// Load policy from database
enforcer, _ := casbin.NewEnforcer("model.conf", "policy.csv")

// Middleware for API authorization
func AuthzMiddleware(e *casbin.Enforcer) gin.HandlerFunc {
    return func(c *gin.Context) {
        // Extract user from JWT
        user := c.GetString("user_id")

        // Get requested resource and action
        obj := c.Request.URL.Path
        act := c.Request.Method

        // Check permission
        ok, err := e.Enforce(user, obj, act)
        if err != nil {
            c.AbortWithStatus(500)
            return
        }

        if !ok {
            c.AbortWithStatus(403)
            return
        }

        c.Next()
    }
}

// Usage
r := gin.Default()
r.Use(AuthzMiddleware(enforcer))
r.GET("/api/v1/documents/:id", GetDocument)
```

#### Policy Model (model.conf):

```ini
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && r.obj == p.obj && r.act == p.act
```

#### Policy Data (policy.csv):

```csv
p, alice, /api/v1/documents/*, GET
p, bob, /api/v1/documents/*, POST
p, admin, /api/v1/*, *
g, alice, user
g, bob, editor
g, charlie, admin
```

---

## Industry Solutions Comparison

### 4.1 Open-Source IAM Solutions

#### Comprehensive Comparison Table:

| Solution | Architecture | Deployment | Best For | Complexity | Resource Usage | Go Support |
|----------|-------------|-----------|----------|------------|---------------|------------|
| **Keycloak** | Monolith (Java) | Docker, K8s, VM | Enterprise, legacy systems | High | Heavy (1GB+ RAM) | Go client libs |
| **Authelia** | Forward Auth | Docker behind reverse proxy | Homelabs, personal projects | Low | Ultra-light (~30MB RAM) | Go-based |
| **Ory** | Microservices | Cloud-native (K8s) | Large-scale distributed systems | Medium-High | Medium (modular) | Native Go |
| **Authentik** | Monolith (Python) | Docker, K8s | SMBs, growing teams | Medium | Medium | API client |
| **ZITADEL** | Event-sourced | Cloud-native | Modern SaaS | Medium | Medium | Native Go |

#### Detailed Solution Profiles:

##### **Keycloak** (Enterprise Heavyweight)

**What it is**: Red Hat-backed, mature IAM platform (since 2014)

**Features**:
- ✅ OAuth 2.0, OIDC, SAML support
- ✅ LDAP/AD federation
- ✅ Social login (Google, Facebook, GitHub)
- ✅ Fine-grained permissions
- ✅ Identity brokering
- ✅ User federation
- ✅ Strong admin UI

**Pros**:
- Stable and widely supported in enterprise
- Rich feature set covers 90% of IAM needs
- Large community and extensive documentation
- Battle-tested in production at scale

**Cons**:
- Heavy to run (Java-based, 1GB+ memory)
- Complex to configure (steep learning curve)
- Overkill for simple use cases
- Slow startup times

**When to use**:
- Enterprise deployments with complex requirements
- Need for SAML + OIDC + LDAP in one system
- Large teams with dedicated ops resources
- Compliance requirements (HIPAA, SOC 2)

##### **Authelia** (Lightweight Specialist)

**What it is**: Forward authentication service (sits behind reverse proxy)

**Features**:
- ✅ TOTP, WebAuthn, Duo Push MFA
- ✅ Single Sign-On (SSO)
- ✅ Access control policies
- ✅ Session management
- ❌ NOT a full identity provider (uses external user directories)

**Pros**:
- Ultra-lightweight (container < 20MB, RAM < 30MB)
- Easy to deploy with Traefik/NGINX/Caddy
- Perfect for homelabs and self-hosters
- Simple YAML configuration

**Cons**:
- Not a standalone IdP (requires external user store)
- Limited to forward auth pattern
- Smaller community than Keycloak

**When to use**:
- Homelab or personal projects
- Protecting self-hosted apps (Nextcloud, Jellyfin, etc.)
- Resource-constrained environments
- Simple SSO needs with existing user directory

##### **Ory** (Modular Cloud-Native)

**What it is**: Composable IAM platform built on microservices

**Components**:
- **Kratos** — Identity management (user registration, login)
- **Hydra** — OAuth2 and OIDC server
- **Keto** — Permission management (Zanzibar-based)
- **Oathkeeper** — Identity & Access Proxy (API gateway)

**Pros**:
- Modular (use only what you need)
- Cloud-native design (Kubernetes-ready)
- Written in Go (native performance)
- Headless (API-first, bring your own UI)
- Control over every IAM layer

**Cons**:
- Requires assembling multiple services
- Steeper learning curve than monolithic solutions
- More operational overhead (multiple containers)
- Documentation can be fragmented

**When to use**:
- Microservices architectures
- Large-scale distributed systems
- Kubernetes deployments
- Teams that want full control and customization
- Go-native tech stacks

##### **ZITADEL** (Event-Sourced IAM)

**What it is**: Modern IAM built with event sourcing and CQRS

**Features**:
- ✅ Multi-tenancy built-in
- ✅ Event-sourced architecture (full audit trail)
- ✅ OAuth 2.0, OIDC
- ✅ SAML 2.0
- ✅ User management with self-service
- ✅ Strong admin console

**Pros**:
- Modern architecture (event sourcing = full history)
- Multi-tenancy as first-class feature
- Written in Go
- Strong observability and audit capabilities

**Cons**:
- Newer project (less battle-tested than Keycloak)
- Smaller community
- Event-sourced architecture has complexity trade-offs

**When to use**:
- Multi-tenant SaaS platforms
- Need for full audit trail (compliance)
- Modern cloud-native deployments
- Go-native projects

### 4.2 Commercial Solutions (For Reference)

| Solution | Pricing Model | Best For | Standout Feature |
|----------|--------------|----------|------------------|
| **Auth0** | Per MAU (Monthly Active User) | Startups, rapid development | Rich SDKs, extensive integrations |
| **Okta** | Per user/month | Large enterprises | Mature platform, enterprise support |
| **AWS Cognito** | Pay-per-use | AWS-native apps | Deep AWS integration |
| **Azure AD B2C** | Per authentication | Microsoft ecosystem | Seamless Office 365 integration |

### 4.3 Decision Matrix

#### Small Projects / Homelabs:
```
Authelia > Authentik > Keycloak
(lightweight, simple, sufficient)
```

#### Startups / SMBs:
```
Ory Kratos/Hydra > Authentik > Auth0 (commercial)
(scalable, cost-effective, modern)
```

#### Enterprise / Legacy Systems:
```
Keycloak > Okta (commercial) > Azure AD
(mature, feature-rich, enterprise support)
```

#### Cloud-Native / Microservices:
```
Ory (full stack) > ZITADEL > Keycloak
(modular, Kubernetes-native, Go-based)
```

#### Multi-Tenant SaaS:
```
ZITADEL > Ory Kratos+Keto > Custom (Casbin + go-saas)
(multi-tenancy first, event-sourced, ReBAC)
```

---

## Data Model Design

### 5.1 Core IAM Entities

#### Entity-Relationship Diagram (Conceptual):

```
┌──────────────┐
│    Tenant    │
└───────┬──────┘
        │ 1:N
        ▼
┌──────────────┐       ┌──────────────┐
│ Organization │───N:N─│     User     │
└───────┬──────┘       └───────┬──────┘
        │                      │
        │ 1:N                  │ N:N
        ▼                      ▼
┌──────────────┐       ┌──────────────┐
│     Team     │───N:N─│     Role     │
└──────────────┘       └───────┬──────┘
                               │ N:N
                               ▼
                       ┌──────────────┐
                       │  Permission  │
                       └───────┬──────┘
                               │ N:N
                               ▼
                       ┌──────────────┐
                       │   Resource   │
                       └──────────────┘
```

#### Schema Design (PostgreSQL):

```sql
-- Multi-tenancy base
CREATE TABLE tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    domain VARCHAR(255) UNIQUE,
    settings JSONB DEFAULT '{}',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Users table
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255),  -- NULL if external auth (OAuth, SAML)
    display_name VARCHAR(255),
    mfa_enabled BOOLEAN DEFAULT FALSE,
    mfa_secret VARCHAR(255),  -- TOTP secret
    status VARCHAR(50) DEFAULT 'active',  -- active, suspended, deleted
    last_login TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(tenant_id, email)
);

-- Roles table
CREATE TABLE roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    is_system BOOLEAN DEFAULT FALSE,  -- System roles (admin, user) vs custom
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(tenant_id, name)
);

-- Permissions table
CREATE TABLE permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    resource VARCHAR(255) NOT NULL,  -- e.g., "documents", "projects"
    action VARCHAR(100) NOT NULL,    -- e.g., "read", "write", "delete"
    description TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(tenant_id, resource, action)
);

-- Role-Permission mapping (RBAC)
CREATE TABLE role_permissions (
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    created_at TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (role_id, permission_id)
);

-- User-Role mapping
CREATE TABLE user_roles (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    assigned_at TIMESTAMP DEFAULT NOW(),
    assigned_by UUID REFERENCES users(id),
    PRIMARY KEY (user_id, role_id)
);

-- Organizations (for hierarchical structures)
CREATE TABLE organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    parent_id UUID REFERENCES organizations(id),  -- Self-referencing for hierarchy
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Organization membership
CREATE TABLE organization_members (
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID REFERENCES roles(id),
    joined_at TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (organization_id, user_id)
);

-- OAuth providers (for social login)
CREATE TABLE oauth_providers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider_name VARCHAR(100) NOT NULL,  -- google, github, microsoft
    client_id VARCHAR(255) NOT NULL,
    client_secret VARCHAR(255) NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    UNIQUE(tenant_id, provider_name)
);

-- OAuth connections (link users to external identities)
CREATE TABLE user_oauth_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_id UUID NOT NULL REFERENCES oauth_providers(id) ON DELETE CASCADE,
    external_user_id VARCHAR(255) NOT NULL,  -- ID from external provider
    access_token TEXT,  -- Encrypted
    refresh_token TEXT,  -- Encrypted
    expires_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(provider_id, external_user_id)
);

-- Sessions table (for stateful session management)
CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(255) NOT NULL,  -- SHA-256 hash of session token
    ip_address INET,
    user_agent TEXT,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    last_activity TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_sessions_token ON sessions(token_hash);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_expiry ON sessions(expires_at);

-- Audit logs
CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action VARCHAR(100) NOT NULL,  -- login, logout, permission_changed
    resource_type VARCHAR(100),
    resource_id UUID,
    ip_address INET,
    user_agent TEXT,
    details JSONB,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_audit_tenant ON audit_logs(tenant_id);
CREATE INDEX idx_audit_user ON audit_logs(user_id);
CREATE INDEX idx_audit_created ON audit_logs(created_at);

-- Row-Level Security policies
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE roles ENABLE ROW LEVEL SECURITY;
ALTER TABLE permissions ENABLE ROW LEVEL SECURITY;

-- Policy: Users can only see data from their tenant
CREATE POLICY tenant_isolation_users ON users
    USING (tenant_id = current_setting('app.current_tenant')::UUID);

CREATE POLICY tenant_isolation_roles ON roles
    USING (tenant_id = current_setting('app.current_tenant')::UUID);

CREATE POLICY tenant_isolation_permissions ON permissions
    USING (tenant_id = current_setting('app.current_tenant')::UUID);
```

### 5.2 ReBAC Data Model (Zanzibar-Style)

```sql
-- Relation tuples for ReBAC
CREATE TABLE relation_tuples (
    id BIGSERIAL PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    namespace VARCHAR(100) NOT NULL,      -- e.g., "document", "folder", "organization"
    object_id VARCHAR(255) NOT NULL,      -- e.g., "doc123", "folder456"
    relation VARCHAR(100) NOT NULL,       -- e.g., "viewer", "editor", "parent"
    subject_type VARCHAR(100) NOT NULL,   -- e.g., "user", "group", "team"
    subject_id VARCHAR(255) NOT NULL,     -- e.g., "user:alice", "team:engineering"
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(tenant_id, namespace, object_id, relation, subject_type, subject_id)
);

CREATE INDEX idx_relation_tuples_lookup ON relation_tuples(tenant_id, namespace, object_id, relation);
CREATE INDEX idx_relation_tuples_subject ON relation_tuples(tenant_id, subject_type, subject_id);

-- Example tuples:
-- document:readme#viewer@user:alice
-- folder:2024#parent@document:readme
-- organization:acme#member@user:alice
```

### 5.3 ABAC Policy Model

```sql
-- Attribute-based policies
CREATE TABLE abac_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    resource VARCHAR(255) NOT NULL,
    action VARCHAR(100) NOT NULL,
    conditions JSONB NOT NULL,  -- JSON-based policy rules
    priority INTEGER DEFAULT 0,
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Example conditions JSON:
{
  "all": [
    {"user.department": {"eq": "Engineering"}},
    {"user.location": {"in": ["US", "CA", "UK"]}},
    {"resource.classification": {"ne": "top-secret"}},
    {"time.hour": {"between": [9, 17]}}
  ]
}
```

---

## Security Best Practices

### 6.1 Password Security

#### Storage:

```go
import "golang.org/x/crypto/bcrypt"

// Hash password (use cost factor 12-14 for 2026)
func HashPassword(password string) (string, error) {
    hash, err := bcrypt.GenerateFromPassword([]byte(password), 14)
    return string(hash), err
}

// Verify password
func CheckPassword(password, hash string) bool {
    err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
    return err == nil
}
```

#### Password Policy (2026 Standards):

```go
type PasswordPolicy struct {
    MinLength int  // Minimum 12 characters
    RequireUppercase bool
    RequireLowercase bool
    RequireNumbers bool
    RequireSpecialChars bool
    PreventCommonPasswords bool  // Check against breach databases
    PreventReuse int  // Prevent last N passwords
    ExpiryDays int  // 0 = no expiry (recommended per NIST)
}
```

**NIST Guidelines (2026)**:
- ✅ Minimum 12 characters (prefer 16+)
- ✅ Allow all ASCII characters including spaces
- ✅ Check against breach databases (Have I Been Pwned)
- ❌ No mandatory periodic resets (causes weak passwords)
- ❌ No complex character requirements if using MFA

### 6.2 API Security

#### Rate Limiting:

```go
import "github.com/didip/tollbooth"

// Per-IP rate limiting
limiter := tollbooth.NewLimiter(10, nil)  // 10 requests/second
limiter.SetIPLookups([]string{"X-Forwarded-For", "X-Real-IP", "RemoteAddr"})

// Per-user rate limiting (after auth)
func UserRateLimiter(userID string, limit int) gin.HandlerFunc {
    limiters := make(map[string]*rate.Limiter)
    mu := &sync.Mutex{}

    return func(c *gin.Context) {
        mu.Lock()
        if _, exists := limiters[userID]; !exists {
            limiters[userID] = rate.NewLimiter(rate.Limit(limit), limit*2)
        }
        limiter := limiters[userID]
        mu.Unlock()

        if !limiter.Allow() {
            c.AbortWithStatus(429)
            return
        }
        c.Next()
    }
}
```

#### Input Validation:

```go
import "github.com/go-playground/validator/v10"

type CreateUserRequest struct {
    Email    string `json:"email" validate:"required,email"`
    Password string `json:"password" validate:"required,min=12,max=128"`
    Name     string `json:"name" validate:"required,min=2,max=100"`
}

validate := validator.New()
if err := validate.Struct(req); err != nil {
    // Return 400 Bad Request
}
```

#### SQL Injection Prevention:

```go
// ❌ NEVER do this
db.Exec("SELECT * FROM users WHERE email = '" + email + "'")

// ✅ Always use parameterized queries
db.Query("SELECT * FROM users WHERE email = $1", email)
```

### 6.3 Encryption at Rest

```go
import "golang.org/x/crypto/nacl/secretbox"

// Encrypt sensitive data before storing
func EncryptData(plaintext []byte, key *[32]byte) ([]byte, error) {
    var nonce [24]byte
    if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
        return nil, err
    }

    encrypted := secretbox.Seal(nonce[:], plaintext, &nonce, key)
    return encrypted, nil
}

// Decrypt when retrieving
func DecryptData(encrypted []byte, key *[32]byte) ([]byte, error) {
    var nonce [24]byte
    copy(nonce[:], encrypted[:24])

    decrypted, ok := secretbox.Open(nil, encrypted[24:], &nonce, key)
    if !ok {
        return nil, errors.New("decryption failed")
    }
    return decrypted, nil
}
```

**What to encrypt**:
- ✅ OAuth tokens (access/refresh)
- ✅ TOTP secrets
- ✅ API keys
- ✅ Personally Identifiable Information (PII)
- ✅ Sensitive configuration values

### 6.4 Security Headers

```go
func SecurityHeaders() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Header("X-Frame-Options", "DENY")
        c.Header("X-Content-Type-Options", "nosniff")
        c.Header("X-XSS-Protection", "1; mode=block")
        c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
        c.Header("Content-Security-Policy", "default-src 'self'")
        c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
        c.Next()
    }
}
```

### 6.5 Audit Logging

```go
func LogSecurityEvent(db *sql.DB, event SecurityEvent) error {
    query := `
        INSERT INTO audit_logs
        (tenant_id, user_id, action, resource_type, resource_id, ip_address, user_agent, details)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
    `

    details, _ := json.Marshal(event.Details)
    _, err := db.Exec(query,
        event.TenantID,
        event.UserID,
        event.Action,
        event.ResourceType,
        event.ResourceID,
        event.IPAddress,
        event.UserAgent,
        details,
    )
    return err
}

// Events to log:
// - Authentication (login, logout, failed attempts)
// - Authorization failures
// - Permission changes
// - User lifecycle events (create, update, delete)
// - Sensitive data access
// - Configuration changes
```

---

## Go Ecosystem Integration

### 7.1 Key Libraries for IAM

#### Authentication & Authorization:

| Library | Purpose | GitHub Stars | Use Case |
|---------|---------|--------------|----------|
| **casbin/casbin** | Authorization (RBAC/ABAC/ReBAC) | 18k+ | General-purpose policy enforcement |
| **ory/hydra** | OAuth2/OIDC server | 15k+ | Full-featured OAuth provider |
| **ory/kratos** | Identity management | 11k+ | User registration, login, recovery |
| **ory/keto** | Permissions (Zanzibar) | 4.8k+ | ReBAC at scale |
| **golang-jwt/jwt** | JWT parsing/validation | 7k+ | Token handling |
| **pquerna/otp** | TOTP/HOTP | 3.6k+ | 2FA implementation |

#### Session & State Management:

| Library | Purpose | Use Case |
|---------|---------|----------|
| **go-redis/redis** | Session store | Distributed session management |
| **gorilla/sessions** | Cookie sessions | Simple web app sessions |
| **alexedwards/scs** | Session management | Production-ready session handling |

#### Cryptography:

| Library | Purpose | Use Case |
|---------|---------|----------|
| **golang.org/x/crypto/bcrypt** | Password hashing | Secure password storage |
| **golang.org/x/crypto/nacl** | Encryption | Symmetric encryption |
| **google/tink** | Cryptographic library | Multi-language crypto primitives |

### 7.2 Casbin Integration Example

```go
package main

import (
    "github.com/casbin/casbin/v2"
    "github.com/casbin/casbin/v2/model"
    gormadapter "github.com/casbin/gorm-adapter/v3"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
)

func InitCasbin() (*casbin.Enforcer, error) {
    // Connect to PostgreSQL
    db, err := gorm.Open(postgres.Open("your-dsn"), &gorm.Config{})
    if err != nil {
        return nil, err
    }

    // Use GORM adapter for policy storage
    adapter, err := gormadapter.NewAdapterByDB(db)
    if err != nil {
        return nil, err
    }

    // Define RBAC model
    m := model.NewModel()
    m.AddDef("r", "r", "sub, obj, act")
    m.AddDef("p", "p", "sub, obj, act")
    m.AddDef("g", "g", "_, _")
    m.AddDef("e", "e", "some(where (p.eft == allow))")
    m.AddDef("m", "m", "g(r.sub, p.sub) && r.obj == p.obj && r.act == p.act")

    // Create enforcer
    enforcer, err := casbin.NewEnforcer(m, adapter)
    if err != nil {
        return nil, err
    }

    // Load policies from database
    enforcer.LoadPolicy()

    return enforcer, nil
}

func CheckPermission(e *casbin.Enforcer, user, resource, action string) (bool, error) {
    return e.Enforce(user, resource, action)
}

// Add role to user
func AssignRole(e *casbin.Enforcer, user, role string) error {
    _, err := e.AddGroupingPolicy(user, role)
    return err
}

// Add permission to role
func GrantPermission(e *casbin.Enforcer, role, resource, action string) error {
    _, err := e.AddPolicy(role, resource, action)
    return err
}
```

### 7.3 JWT Implementation

```go
package auth

import (
    "time"
    "github.com/golang-jwt/jwt/v5"
)

type Claims struct {
    UserID   string   `json:"user_id"`
    TenantID string   `json:"tenant_id"`
    Roles    []string `json:"roles"`
    jwt.RegisteredClaims
}

func GenerateJWT(userID, tenantID string, roles []string, secret []byte) (string, error) {
    claims := Claims{
        UserID:   userID,
        TenantID: tenantID,
        Roles:    roles,
        RegisteredClaims: jwt.RegisteredClaims{
            ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
            IssuedAt:  jwt.NewNumericDate(time.Now()),
            Issuer:    "your-app",
        },
    }

    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString(secret)
}

func ValidateJWT(tokenString string, secret []byte) (*Claims, error) {
    token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
        return secret, nil
    })

    if err != nil {
        return nil, err
    }

    if claims, ok := token.Claims.(*Claims); ok && token.Valid {
        return claims, nil
    }

    return nil, jwt.ErrInvalidKey
}
```

### 7.4 Multi-Tenant Context Middleware

```go
func TenantContextMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // Extract tenant from JWT claims
        claims := c.MustGet("claims").(*Claims)
        tenantID := claims.TenantID

        // Set PostgreSQL session variable for RLS
        db := c.MustGet("db").(*sql.DB)
        _, err := db.Exec("SET app.current_tenant = $1", tenantID)
        if err != nil {
            c.AbortWithStatus(500)
            return
        }

        // Store tenant in context for business logic
        c.Set("tenant_id", tenantID)
        c.Next()
    }
}
```

---

## Telegram Bot Integration Patterns

### 8.1 OAuth Integration with Telegram

#### Scenario 1: Use Telegram for Authentication

**Flow**:
```
1. User clicks "Login with Telegram" on your website
2. Website redirects to Telegram OAuth URL
3. User authorizes in Telegram app
4. Telegram redirects back with auth data
5. Verify hash using bot token
6. Create session for user
```

**Go Implementation**:

```go
import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "net/url"
)

type TelegramAuthData struct {
    ID        int64  `form:"id"`
    FirstName string `form:"first_name"`
    LastName  string `form:"last_name"`
    Username  string `form:"username"`
    PhotoURL  string `form:"photo_url"`
    AuthDate  int64  `form:"auth_date"`
    Hash      string `form:"hash"`
}

func VerifyTelegramAuth(data TelegramAuthData, botToken string) bool {
    // Create data check string
    dataCheckString := fmt.Sprintf(
        "auth_date=%d\nfirst_name=%s\nid=%d\nlast_name=%s\nphoto_url=%s\nusername=%s",
        data.AuthDate, data.FirstName, data.ID, data.LastName, data.PhotoURL, data.Username,
    )

    // Calculate secret key (SHA256 of bot token)
    h := sha256.New()
    h.Write([]byte(botToken))
    secretKey := h.Sum(nil)

    // Calculate HMAC-SHA256
    mac := hmac.New(sha256.New, secretKey)
    mac.Write([]byte(dataCheckString))
    expectedHash := hex.EncodeToString(mac.Sum(nil))

    return expectedHash == data.Hash
}

// Gin handler example
func TelegramOAuthCallback(c *gin.Context) {
    var authData TelegramAuthData
    if err := c.ShouldBind(&authData); err != nil {
        c.JSON(400, gin.H{"error": "invalid data"})
        return
    }

    // Verify authentication
    botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
    if !VerifyTelegramAuth(authData, botToken) {
        c.JSON(401, gin.H{"error": "authentication failed"})
        return
    }

    // Create or update user in database
    user, err := CreateOrUpdateUserFromTelegram(authData)
    if err != nil {
        c.JSON(500, gin.H{"error": "failed to create user"})
        return
    }

    // Generate JWT for your system
    token, err := GenerateJWT(user.ID, user.TenantID, user.Roles, []byte(secret))
    if err != nil {
        c.JSON(500, gin.H{"error": "failed to generate token"})
        return
    }

    // Set HttpOnly cookie
    c.SetCookie("access_token", token, 900, "/", "", true, true)
    c.Redirect(302, "/dashboard")
}
```

#### Scenario 2: Bot Requests External OAuth

**Use Case**: Telegram bot needs to access user's Google Drive / GitHub / etc.

**Flow**:
```
1. User sends /connect_google to bot
2. Bot generates OAuth URL with state parameter
3. Bot sends URL to user: "Click here to authorize"
4. User authorizes in browser
5. OAuth provider redirects to your callback URL
6. Your server saves tokens, notifies bot
7. Bot confirms to user in Telegram: "Connected!"
```

**Go Implementation**:

```go
import (
    "golang.org/x/oauth2"
    "golang.org/x/oauth2/google"
)

var googleOAuthConfig = &oauth2.Config{
    ClientID:     "your-client-id",
    ClientSecret: "your-client-secret",
    RedirectURL:  "https://your-domain.com/oauth/callback",
    Scopes:       []string{"https://www.googleapis.com/auth/drive.readonly"},
    Endpoint:     google.Endpoint,
}

// Telegram bot command handler
func HandleConnectCommand(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
    // Generate random state to prevent CSRF
    state := generateRandomState()

    // Store state with user ID in Redis (expires in 10 minutes)
    redis.Set(ctx, "oauth_state:"+state, msg.From.ID, 10*time.Minute)

    // Generate OAuth URL
    authURL := googleOAuthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)

    // Send to user
    reply := tgbotapi.NewMessage(msg.Chat.ID,
        "Click here to connect your Google account:\n" + authURL)
    bot.Send(reply)
}

// Web server callback handler
func OAuthCallback(c *gin.Context) {
    code := c.Query("code")
    state := c.Query("state")

    // Verify state
    userID, err := redis.Get(ctx, "oauth_state:"+state).Int64()
    if err != nil {
        c.String(400, "Invalid state")
        return
    }

    // Exchange code for token
    token, err := googleOAuthConfig.Exchange(ctx, code)
    if err != nil {
        c.String(500, "Failed to exchange token")
        return
    }

    // Save token to database (encrypted)
    SaveOAuthToken(userID, "google", token)

    // Notify user in Telegram
    msg := tgbotapi.NewMessage(userID, "✅ Google account connected successfully!")
    bot.Send(msg)

    c.String(200, "Success! You can close this window and return to Telegram.")
}
```

### 8.2 Integrating IAM with Existing Alice Bot

#### Architecture Integration:

```
┌──────────────────────────────────────────────────────────────┐
│                     Alice Bot (Existing)                      │
│                                                               │
│  ┌─────────────┐   ┌──────────────┐   ┌─────────────────┐   │
│  │  Telegram   │──→│    Agent     │──→│   Claude Code   │   │
│  │   Handler   │   │  (Per-chat)  │   │      CLI        │   │
│  └─────────────┘   └──────────────┘   └─────────────────┘   │
│         │                  │                                  │
│         │                  │                                  │
│         ▼                  ▼                                  │
│  ┌───────────────────────────────────────────────────────┐   │
│  │               IAM Layer (NEW)                         │   │
│  │                                                       │   │
│  │  ┌──────────────┐  ┌─────────────┐  ┌────────────┐  │   │
│  │  │ Auth Module  │  │  Casbin     │  │   User     │  │   │
│  │  │ (OAuth/JWT)  │  │ (Authz)     │  │ Management │  │   │
│  │  └──────────────┘  └─────────────┘  └────────────┘  │   │
│  └───────────────────────────────────────────────────────┘   │
│         │                  │                  │               │
│         ▼                  ▼                  ▼               │
│  ┌──────────────────────────────────────────────────────┐    │
│  │            SQLite Storage (Existing)                 │    │
│  │  + users, roles, permissions, oauth_tokens tables    │    │
│  └──────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────┘
```

#### Implementation Steps:

**Step 1**: Extend database schema
```sql
-- Add to existing alice.db
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    telegram_user_id INTEGER UNIQUE NOT NULL,
    email TEXT,
    display_name TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE roles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    description TEXT
);

CREATE TABLE permissions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    resource TEXT NOT NULL,
    action TEXT NOT NULL,
    UNIQUE(resource, action)
);

CREATE TABLE user_roles (
    user_id INTEGER REFERENCES users(id),
    role_id INTEGER REFERENCES roles(id),
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE role_permissions (
    role_id INTEGER REFERENCES roles(id),
    permission_id INTEGER REFERENCES permissions(id),
    PRIMARY KEY (role_id, permission_id)
);

-- Insert default roles
INSERT INTO roles (name, description) VALUES
    ('admin', 'Full access to all features'),
    ('user', 'Basic user access');

-- Insert default permissions
INSERT INTO permissions (resource, action) VALUES
    ('agent', 'execute'),
    ('tools', 'use'),
    ('settings', 'read'),
    ('settings', 'write');
```

**Step 2**: Create IAM module
```go
// internal/app/iam.go
package app

import (
    "database/sql"
    "github.com/casbin/casbin/v2"
    "github.com/casbin/casbin/v2/model"
    sqliteadapter "github.com/casbin/sqlite-adapter/v2"
)

type IAMManager struct {
    db       *sql.DB
    enforcer *casbin.Enforcer
}

func NewIAMManager(db *sql.DB) (*IAMManager, error) {
    // Initialize Casbin with SQLite adapter
    adapter, err := sqliteadapter.NewAdapter(db, "casbin_rules")
    if err != nil {
        return nil, err
    }

    // Define RBAC model
    m := model.NewModel()
    m.AddDef("r", "r", "sub, obj, act")
    m.AddDef("p", "p", "sub, obj, act")
    m.AddDef("g", "g", "_, _")
    m.AddDef("e", "e", "some(where (p.eft == allow))")
    m.AddDef("m", "m", "g(r.sub, p.sub) && r.obj == p.obj && r.act == p.act")

    enforcer, err := casbin.NewEnforcer(m, adapter)
    if err != nil {
        return nil, err
    }

    enforcer.LoadPolicy()

    return &IAMManager{
        db:       db,
        enforcer: enforcer,
    }, nil
}

func (iam *IAMManager) CheckPermission(userID int64, resource, action string) (bool, error) {
    return iam.enforcer.Enforce(fmt.Sprintf("user:%d", userID), resource, action)
}

func (iam *IAMManager) AssignRole(userID int64, role string) error {
    _, err := iam.enforcer.AddGroupingPolicy(fmt.Sprintf("user:%d", userID), role)
    return err
}
```

**Step 3**: Integrate with Telegram handler
```go
// internal/app/telegram.go (modified)

func (t *TelegramBot) handleCommand(update tgbotapi.Update) {
    msg := update.Message

    // Check permission before executing command
    if t.iam != nil {
        allowed, err := t.iam.CheckPermission(msg.From.ID, "agent", "execute")
        if err != nil || !allowed {
            t.send(ChatKey{chatID: msg.Chat.ID}, "❌ You don't have permission to use this command")
            return
        }
    }

    // Existing command handling logic...
}
```

**Step 4**: Add admin commands
```go
func (t *TelegramBot) handleAdminCommand(msg *tgbotapi.Message) {
    // Check if user is admin
    allowed, _ := t.iam.CheckPermission(msg.From.ID, "settings", "write")
    if !allowed {
        t.send(ChatKey{chatID: msg.Chat.ID}, "❌ Admin only")
        return
    }

    parts := strings.Split(msg.Text, " ")
    switch parts[0] {
    case "/grant":
        // /grant @username user
        if len(parts) != 3 {
            t.send(ChatKey{chatID: msg.Chat.ID}, "Usage: /grant @username <role>")
            return
        }
        username := parts[1]
        role := parts[2]

        // Get user ID from username
        userID := t.getUserIDFromUsername(username)
        if userID == 0 {
            t.send(ChatKey{chatID: msg.Chat.ID}, "User not found")
            return
        }

        // Assign role
        err := t.iam.AssignRole(userID, role)
        if err != nil {
            t.send(ChatKey{chatID: msg.Chat.ID}, "Failed to assign role")
            return
        }

        t.send(ChatKey{chatID: msg.Chat.ID}, fmt.Sprintf("✅ Assigned role '%s' to %s", role, username))
    }
}
```

---

## Implementation Recommendations

### 9.1 Recommended Stack for Alice Integration

Based on the research and Alice's existing architecture, here's the recommended approach:

#### **Lightweight IAM Integration** (Phase 1 - Quick Win):

| Component | Solution | Reason |
|-----------|----------|--------|
| **Authorization** | Casbin (RBAC) | Lightweight, SQLite-compatible, mature Go library |
| **Authentication** | Custom (Telegram OAuth) | Alice is Telegram-native, leverage existing bot auth |
| **Session Management** | JWT in SQLite | Stateless tokens, no Redis dependency |
| **User Storage** | SQLite (extend existing) | Keep architecture simple, no new databases |

**Benefits**:
- ✅ Minimal dependencies (no new services)
- ✅ Leverages existing SQLite storage
- ✅ Telegram-native authentication
- ✅ Quick implementation (~1-2 weeks)

#### **Enterprise-Ready IAM** (Phase 2 - Future):

| Component | Solution | Reason |
|-----------|----------|--------|
| **Full IAM Platform** | Ory Kratos + Keto | Cloud-native, Go-based, modular |
| **Multi-Tenant** | PostgreSQL + RLS | Better scalability than SQLite |
| **Authorization** | Ory Keto (Zanzibar) | ReBAC for complex hierarchies |
| **Cache** | Redis | Session + policy caching |

**When to migrate**:
- User base > 10,000
- Need for multi-tenancy
- Complex permission hierarchies
- Compliance requirements (SOC 2, HIPAA)

### 9.2 Implementation Roadmap

#### **Phase 1: Basic IAM (2-3 weeks)**

**Week 1: Database & Auth**
- [ ] Extend SQLite schema (users, roles, permissions tables)
- [ ] Implement Telegram OAuth verification
- [ ] Add JWT generation/validation
- [ ] Create IAMManager interface

**Week 2: Authorization**
- [ ] Integrate Casbin for RBAC
- [ ] Add permission checks to existing commands
- [ ] Implement admin commands (/grant, /revoke)
- [ ] Add audit logging

**Week 3: Testing & Docs**
- [ ] Write unit tests for IAM functions
- [ ] Integration tests for permission flows
- [ ] Document admin commands
- [ ] Create user guide

#### **Phase 2: Advanced Features (4-6 weeks)**

**Week 4-5: Multi-Factor Authentication**
- [ ] Add TOTP support (pquerna/otp)
- [ ] MFA enrollment flow in Telegram
- [ ] Backup codes generation
- [ ] Admin MFA enforcement

**Week 6-7: OAuth Provider Integration**
- [ ] Google OAuth integration
- [ ] GitHub OAuth integration
- [ ] OAuth token storage (encrypted)
- [ ] Token refresh logic

**Week 8-9: Dashboard Integration**
- [ ] Add IAM UI to React dashboard
- [ ] User management interface
- [ ] Role/permission editor
- [ ] Audit log viewer

#### **Phase 3: Enterprise Features (Optional)**

- [ ] Migrate to PostgreSQL + RLS
- [ ] Implement ReBAC with Ory Keto
- [ ] Add LDAP/AD sync
- [ ] SCIM provisioning support
- [ ] SSO (SAML/OIDC provider)

### 9.3 Code Structure

```
internal/app/
├── iam/
│   ├── iam.go              # IAMManager interface
│   ├── auth.go             # JWT, OAuth, TOTP
│   ├── authz.go            # Casbin wrapper
│   ├── user.go             # User management
│   ├── role.go             # Role management
│   ├── permission.go       # Permission management
│   └── audit.go            # Audit logging
├── telegram.go             # Modified to use IAM
├── web.go                  # Modified to use IAM
└── storage.go              # Extended with IAM tables

migrations/
├── 001_create_iam_tables.sql
├── 002_add_oauth_tables.sql
└── 003_add_mfa_tables.sql
```

### 9.4 Security Checklist

Before deploying IAM to production:

- [ ] All passwords hashed with bcrypt (cost >= 12)
- [ ] JWT secrets stored in environment variables
- [ ] OAuth tokens encrypted at rest
- [ ] TOTP secrets encrypted at rest
- [ ] Rate limiting on auth endpoints
- [ ] Audit logs for all permission changes
- [ ] HttpOnly cookies for web sessions
- [ ] CSRF protection for web endpoints
- [ ] SQL injection prevented (parameterized queries)
- [ ] Input validation on all user inputs
- [ ] Security headers configured
- [ ] HTTPS enforced (for web dashboard)
- [ ] Regular dependency updates (go mod tidy)
- [ ] Backup procedures for user database

---

## Sources

### Multi-Tenant Architecture:
- [Building scalable multi-tenant applications in Go | Atlas](https://atlasgo.io/blog/2025/05/26/gophercon-scalable-multi-tenant-apps-in-go)
- [GitHub - go-saas/saas](https://github.com/go-saas/saas)
- [Multi-Tenancy Database Patterns with examples in Go | Medium](https://medium.com/@rosgluk/multi-tenancy-database-patterns-with-examples-in-go-ade087d642c8)
- [Multi-tenant data isolation with PostgreSQL Row Level Security | AWS](https://aws.amazon.com/blogs/database/multi-tenant-data-isolation-with-postgresql-row-level-security/)
- [Row Level Security for Tenants in Postgres | Crunchy Data](https://www.crunchydata.com/blog/row-level-security-for-tenants-in-postgres)

### OAuth & Authentication:
- [OAuth 2.0 & OpenID Connect: The Complete Guide | Medium](https://mrutyunjaypatil.medium.com/oauth-2-0-openid-connect-the-complete-guide-to-what-the-standards-actually-say-e92f040a4251)
- [Managing Sessions with OpenID Connect | Medium](https://technospace.medium.com/managing-sessions-with-openid-connect-d3b6fb4f552b)
- [OIDC Session Timeout Enforcement: Best Practices | Hoop](https://hoop.dev/blog/oidc-session-timeout-enforcement-best-practices-and-pitfalls/)
- [Using OAuth for Single Page Applications | Curity](https://curity.io/resources/learn/spa-best-practices/)

### Authorization Models:
- [RBAC vs ABAC vs ReBAC | Oso](https://www.osohq.com/learn/rbac-vs-abac-vs-rebac-what-is-the-best-access-policy-paradigm)
- [RBAC vs ReBAC | Security Boulevard](https://securityboulevard.com/2026/01/rbac-vs-rebac-comparing-role-based-relationship-based-access-control/)
- [RBAC, ABAC, and ReBAC - Differences and Scenarios | Aserto](https://www.aserto.com/blog/rbac-abac-and-rebac-differences-and-scenarios)
- [RBAC vs ABAC & ReBAC | Permit.io](https://www.permit.io/blog/rbac-vs-abac-and-rebac-choosing-the-right-authorization-model)

### IAM Solutions:
- [Best Open Source Auth Tools 2026 | Cerbos](https://www.cerbos.dev/blog/best-open-source-auth-tools-and-software-for-enterprises-2026)
- [Top 10 IAM platforms | Medium](https://jewelhuq.medium.com/top-10-full-stack-self-hosted-iam-platforms-keycloak-peers-5b92a3a3426b)
- [Authentik vs Authelia vs Keycloak 2026](https://blog.elest.io/authentik-vs-authelia-vs-keycloak-choosing-the-right-self-hosted-identity-provider-in-2026/)
- [Best Keycloak Alternatives 2025 | Oso](https://www.osohq.com/learn/best-keycloak-alternatives-2025)

### MFA & Security:
- [Multifactor Authentication - OWASP Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Multifactor_Authentication_Cheat_Sheet.html)
- [From TOTP to Passkeys | FRC](https://fedresources.com/from-totp-to-phishing-resistant-passkeys-a-guide-to-multi-factor-authentication/)
- [Top Open-Source MFA Solutions 2026 | Authgear](https://www.authgear.com/post/top-open-source-mfa-solutions-for-enterprise-applications-2026)
- [JWT Security Best Practices | APIsec](https://www.apisec.ai/blog/jwt-security-vulnerabilities-prevention)
- [Refresh Token Rotation | Serverion](https://www.serverion.com/uncategorized/refresh-token-rotation-best-practices-for-developers/)

### SSO Protocols:
- [SAML vs OIDC 2026 | Security Boulevard](https://securityboulevard.com/2026/01/saml-vs-oidc-choosing-the-right-protocol-for-modern-single-sign-on/)
- [OIDC vs SAML | Authgear](https://www.authgear.com/post/oidc-vs-saml)
- [SSO: OAuth2 vs OIDC vs SAML | Pomerium](https://www.pomerium.com/blog/sso-oauth2-vs-oidc-vs-saml)

### Go Libraries:
- [GitHub - casbin/casbin](https://github.com/casbin/casbin)
- [GitHub - ory/keto](https://github.com/ory/keto)
- [Authorization in Golang Projects using Casbin | Medium](https://medium.com/wesionary-team/authorization-in-golang-projects-using-casbin-f8fad744dae5)

### Zanzibar & ReBAC:
- [Google Zanzibar | Authzed Docs](https://authzed.com/docs/spicedb/concepts/zanzibar)
- [Introduction to Google Zanzibar | Authzed](https://authzed.com/learn/google-zanzibar)
- [What is Google Zanzibar? | Permit.io](https://www.permit.io/blog/what-is-google-zanzibar)
- [Zanzibar: Google's Authorization System | Research](https://research.google/pubs/zanzibar-googles-consistent-global-authorization-system/)

### User Lifecycle:
- [User Provisioning: Lifecycle Management 2025-2026 | Avatier](https://www.avatier.com/blog/user-provisioning-lcm-enterprise-guide/)
- [What is User Provisioning and Deprovisioning? | Lumos](https://www.lumos.com/topic/lifecycle-management-user-provisioning-deprovisioning)
- [User Provisioning Best Practices 2026 | TechPrescient](https://www.techprescient.com/blogs/user-provisioning/)

### LDAP/AD Integration:
- [LDAP Integration with Active Directory | Parallels](https://www.parallels.com/blogs/ras/ldap-integration-with-active-directory/)
- [Active Directory LDAP Authentication | DNSstuff](https://www.dnsstuff.com/active-directory-ldap-authentication)
- [Connect App to Active Directory | Auth0](https://auth0.com/docs/authenticate/identity-providers/enterprise-identity-providers/active-directory-ldap)

### Telegram OAuth:
- [OAuth2.0 with Telegram | Medium](https://medium.com/@tech.engineer.jedi/oauth2-0-with-telegram-1c321a9dca27)
- [Authenticate users using OAuth in Telegram Bot | Medium](https://medium.com/@frederic.henri/authenticate-your-users-on-3rd-party-services-using-oauth-within-your-telegram-bot-b0003764e83e)
- [Telegram OAuth for Your Site | Medium](https://medium.com/@alexawesome/telegram-oauth-authorization-for-your-site-6d527fe212ab)

---

**End of Report**

*This research provides a comprehensive foundation for implementing enterprise-grade IAM in the Alice project with focus on Go ecosystem compatibility and Telegram bot integration.*
