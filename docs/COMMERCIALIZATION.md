# Alice Monitor — Commercialization Plan

## Executive Summary

Alice Monitor has a mature technical foundation as a local-first AI agent observability platform. This plan addresses three critical gaps:

1. **Incomplete licensing** — README references AGPL-3.0 but no LICENSE file exists
2. **Unrealized commercial potential** — Enterprise features are mentioned but not built
3. **Legal risk** — Missing formal copyright notices and commercial licensing terms

---

## Implementation Strategy

### Phase 1: Licensing & Legal Foundation (1 week)

- [ ] Create formal AGPL-3.0 `LICENSE` file in project root
- [ ] Add copyright headers to all source files
- [ ] Draft Contributor License Agreement (CLA) for external contributors
- [ ] Update README.md with commercial contact information
- [ ] Add `NOTICE` file for third-party attribution

### Phase 2: Core Commercial Features (3–4 weeks)

- [ ] **Custom PII Rules Engine** — Industry-specific patterns for healthcare (HIPAA), finance (PCI-DSS), and legal sectors
- [ ] **Team RBAC** — Multi-tenant role-based access control with project-level permissions
- [ ] **Advanced Analytics & Cost Optimization** — Budget alerts, usage forecasting, cost anomaly detection

### Phase 3: Enterprise Integration (2–3 weeks)

- [ ] **SSO Integration** — SAML 2.0, OIDC, Azure AD support
- [ ] **Audit Export** — Compliance-ready exports (CSV, JSON, SIEM integration)
- [ ] **License Key Verification** — Feature-gated activation via license keys

### Phase 4: Dual Licensing & Release (1 week)

- [ ] Establish clear open-source / commercial module separation using Go build tags
- [ ] Implement feature flags for commercial-only functionality
- [ ] Validate both open-source and commercial feature sets
- [ ] Set up separate release pipelines

**Total estimated timeline: 7–9 weeks**

---

## Business Model

### Pricing (USD)

| Tier | Price | Users | Features |
|------|-------|-------|----------|
| **Community** | Free | Unlimited | Core monitoring, basic PII detection, single-user dashboard |
| **Team** | $99/mo | Up to 20 users | Custom PII rules, Team RBAC, priority support |
| **Enterprise** | $299/mo | Unlimited | Full SSO, audit exports, SIEM integration, SLA guarantee |
| **Custom** | Contact us | — | On-premise deployment, custom integrations, dedicated support |

> Annual plans receive a 20% discount.

---

## Licensing Strategy

**Approach: Same repository, module separation via Go build tags**

- **Open-source modules** (`internal/app/`) — AGPL-3.0, public GitHub repository
- **Commercial modules** (`internal/pro/`) — Proprietary license, gated by build tags
- **CLA required** — All external contributors sign a CLA granting dual-licensing rights

### Why AGPL-3.0 + Commercial Dual License?

| Scenario | License |
|----------|---------|
| Personal / open-source use | AGPL-3.0 (free) |
| Internal company tool (no SaaS) | AGPL-3.0 (free, must share modifications) |
| Closed-source SaaS or commercial product | Commercial license required |

This model protects the open-source community while enabling sustainable commercial development.

---

## Key Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| External contributions complicate licensing | CLA requirement before merge |
| Build tag separation leaks commercial code | CI checks + `.gitignore` for pro modules |
| Phase 2–3 timeline overrun | Prioritize RBAC + Custom PII first; defer SSO |
| Community backlash on dual licensing | Transparent communication; generous Community tier |

---

## Next Steps

1. Create `LICENSE` file (immediate, blocks all other work)
2. Begin Phase 1 legal foundation
3. Open GitHub Issues for Phase 2–3 features with milestone tracking
4. Draft CLA document

---

---

# Alice Monitor — 商業化計畫

## 📋 計畫概要

Alice Monitor 作為 Local-First AI Agent 可觀測性平台已具備成熟的技術基礎。本計畫處理三個關鍵缺口：

1. **授權狀態不完整** — README 提及 AGPL-3.0 但缺少實際 LICENSE 檔案
2. **商業化潛力未釋放** — 已提及企業功能但尚未開發
3. **法律風險** — 缺少正式的版權聲明和商業授權條款

---

## 🎯 實施策略

### 階段一：授權與法律基礎（1 週）

- [ ] 在專案根目錄建立正式的 AGPL-3.0 `LICENSE` 檔案
- [ ] 在所有源代碼添加版權聲明
- [ ] 擬定貢獻者授權協議（CLA）供外部貢獻者簽署
- [ ] 更新 README.md 的商業聯絡資訊
- [ ] 新增 `NOTICE` 檔案列出第三方歸屬

### 階段二：核心商業功能（3–4 週）

- [ ] **自定義 PII 規則引擎** — 針對醫療（HIPAA）、金融（PCI-DSS）、法律等行業的專用規則
- [ ] **Team RBAC** — 多租戶角色權限管理，支援專案層級權限控制
- [ ] **進階分析與成本優化** — 預算警報、用量預測、成本異常偵測

### 階段三：企業整合（2–3 週）

- [ ] **SSO 集成** — SAML 2.0、OIDC、Azure AD 支援
- [ ] **稽核匯出** — 合規導向匯出（CSV、JSON、SIEM 整合）
- [ ] **License Key 驗證** — 透過授權金鑰啟用商業功能

### 階段四：雙重授權與發佈（1 週）

- [ ] 使用 Go build tags 建立清晰的開源 / 商業模組分離
- [ ] 實施功能開關控制商業專屬功能
- [ ] 驗證開源版和商業版功能完整性
- [ ] 建立分別的發佈流程

**預估總時程：7–9 週**

---

## 🏢 商業模式

### 定價（USD）

| 方案 | 價格 | 用戶數 | 功能 |
|------|------|--------|------|
| **Community** | 免費 | 不限 | 基礎監控、基本 PII 偵測、單用戶 Dashboard |
| **Team** | $99 USD/月 | 最多 20 人 | 自定義 PII 規則、Team RBAC、優先支援 |
| **Enterprise** | $299 USD/月 | 不限 | 完整 SSO、稽核匯出、SIEM 整合、SLA 保障 |
| **Custom** | 面議 | — | 私有部署、客製化整合、專屬支援 |

> 年繳方案享 8 折優惠。

---

## ⚖️ 授權策略

**策略：同一倉庫，透過 Go build tags 進行模組分離**

- **開源模組** (`internal/app/`) — AGPL-3.0，GitHub 公開倉庫
- **商業模組** (`internal/pro/`) — 商業授權，透過 build tags 控制
- **CLA 必須** — 所有外部貢獻者需簽署 CLA，授予雙重授權權利

### 為什麼選擇 AGPL-3.0 + 商業雙重授權？

| 使用情境 | 適用授權 |
|----------|---------|
| 個人 / 開源使用 | AGPL-3.0（免費） |
| 公司內部工具（非 SaaS） | AGPL-3.0（免費，需公開修改） |
| 閉源 SaaS 或商業產品 | 需購買商業授權 |

此模式保護開源社群，同時支撐可持續的商業開發。

---

## ⚠️ 關鍵風險與對策

| 風險 | 對策 |
|------|------|
| 外部貢獻增加授權複雜度 | 合併前必須簽署 CLA |
| Build tag 分離可能洩漏商業代碼 | CI 檢查 + `.gitignore` 排除 pro 模組 |
| 階段二、三時程超支 | 優先開發 RBAC + 自定義 PII；SSO 延後 |
| 社群對雙重授權反彈 | 透明溝通；Community 方案保持慷慨 |

---

## 📌 下一步行動

1. 建立 `LICENSE` 檔案（立即，阻擋所有後續工作）
2. 啟動階段一法律基礎建設
3. 在 GitHub Issues 建立階段二、三功能項目並關聯 Milestone
4. 擬定 CLA 文件
