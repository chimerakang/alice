# Domain 管理和 VPN 管理系統設計方案研究報告

> 研究日期: 2026-02-20
>
> 目標: 設計企業級 Domain 和 VPN 管理系統，支持自動化配置、多區域部署和用戶自助服務

---

## 目錄

1. [WireGuard vs OpenVPN 技術對比](#1-wireguard-vs-openvpn-技術對比)
2. [開源 VPN 管理面板](#2-開源-vpn-管理面板)
3. [DNS 自動化管理方案](#3-dns-自動化管理方案)
4. [SSL 證書自動續期](#4-ssl-證書自動續期)
5. [企業級架構設計](#5-企業級架構設計)
6. [Go 語言實現方案](#6-go-語言實現方案)
7. [完整集成案例](#7-完整集成案例)

---

## 1. WireGuard vs OpenVPN 技術對比

### 1.1 性能對比 (2026 最新數據)

| 指標 | WireGuard | OpenVPN | 優勢 |
|------|-----------|---------|------|
| **下載速度** | 100% | 48% | WireGuard 快 **52%** |
| **上傳速度** | 100% | 83% | WireGuard 快 **17%** |
| **短距離連線** | 100% | 33% | WireGuard **幾乎 3 倍速** |
| **延遲** | < 10ms | 30-50ms | WireGuard **低 3-5 倍** |
| **CPU 使用率** | 較低 | 較高 | WireGuard 更節能 |

**來源**: [WireGuard vs OpenVPN: 7 Key Differences in 2026](https://cyberinsider.com/vpn/wireguard/wireguard-vs-openvpn/)

### 1.2 代碼複雜度

| 項目 | WireGuard | OpenVPN |
|------|-----------|---------|
| **代碼行數** | ~4,000 行 | ~400,000 行 |
| **可審計性** | 高（代碼少 100 倍） | 中（複雜度高） |
| **維護成本** | 低 | 高 |

**來源**: [WireGuard vs. OpenVPN 2026](https://www.safetydetectives.com/blog/wireguard-vs-openvpn/)

### 1.3 安全性

**WireGuard**:
- 使用最新加密算法 (ChaCha20, Poly1305, Curve25519)
- 代碼簡潔，易於安全審計
- 已通過多次獨立安全審計
- 內建密鑰輪換機制

**OpenVPN**:
- 成熟穩定，歷史悠久（20+ 年）
- 無已知重大漏洞
- 支持更多加密套件（靈活性高）
- 企業級防火牆穿透能力強

**來源**: [ExpressVPN - WireGuard vs OpenVPN](https://www.expressvpn.com/blog/wireguard-vs-openvpn/)

### 1.4 使用場景建議

| 場景 | 推薦方案 | 原因 |
|------|---------|------|
| 日常瀏覽、串流、遊戲 | **WireGuard** | 速度快、延遲低、適合移動設備 |
| 嚴格網路審查環境 | **OpenVPN** | 更好的防火牆穿透能力 |
| 企業遠程辦公 | **WireGuard** | 性能優越、易於管理 |
| 多平台兼容需求 | **OpenVPN** | 支持更多舊系統 |

---

## 2. 開源 VPN 管理面板

### 2.1 Firezone - 企業級 WireGuard 管理平台

**項目信息**:
- GitHub: [firezone/firezone](https://github.com/firezone/firezone)
- 官網: [firezone.dev](https://www.firezone.dev/)
- 授權: Apache 2.0
- 維護狀態: ✅ 積極維護（YC W22 孵化）

**核心功能**:
- ✅ 基於 WireGuard 的零信任訪問平台
- ✅ 速度比 OpenVPN 快 3-4 倍
- ✅ 延遲低於 10ms
- ✅ 基於群組的細粒度權限管理
- ✅ 支持單個應用級別訪問控制
- ✅ 支持整個子網段訪問控制
- ✅ 多平台客戶端（Windows, macOS, Linux, iOS, Android）

**適用場景**:
- 中大型企業遠程訪問
- 需要細粒度權限控制的場景
- 零信任架構部署

**來源**: [Firezone GitHub](https://github.com/firezone/firezone), [Hacker News Launch](https://news.ycombinator.com/item?id=41173330)

### 2.2 Wg-Easy - 輕量級 WireGuard Web UI

**項目信息**:
- 維護狀態: ✅ 積極維護
- 部署方式: Docker 容器

**核心功能**:
- ✅ 基於 Web 的圖形化界面
- ✅ 管理 WireGuard 配置和客戶端
- ✅ 創建多個 WireGuard 客戶端
- ✅ 分配多個網絡地址
- ✅ 啟用/禁用連接
- ✅ 單頁應用（SPA）界面

**適用場景**:
- 小型團隊或家庭使用
- 快速原型開發
- 簡單的 VPN 部署需求

**來源**: [Vultr - How to Install Wg-Easy](https://docs.vultr.com/how-to-install-wg-easy-an-opensource-web-ui-for-wireguard-vpn)

### 2.3 其他值得關注的項目

**NetBird**:
- GitHub: [netbirdio/netbird](https://github.com/netbirdio/netbird)
- 特點: 基於 WireGuard 的點對點 VPN
- 適用: 去中心化團隊協作

**Awesome WireGuard**:
- GitHub: [cedrickchee/awesome-wireguard](https://github.com/cedrickchee/awesome-wireguard)
- 資源: 精選 WireGuard 工具、項目和資源列表

---

## 3. DNS 自動化管理方案

### 3.1 Cloudflare API + Go 官方庫

**官方庫**: `github.com/cloudflare/cloudflare-go`

**核心功能**:
- ✅ DNS 記錄 CRUD 操作
- ✅ Zone（域名）管理
- ✅ 支持 API v4
- ✅ 自動生成（使用 Stainless）
- ✅ 需要 Go 1.22+

**基本用法**:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/cloudflare/cloudflare-go"
)

func main() {
    // 1. 初始化 API 客戶端
    api, err := cloudflare.NewWithAPIToken("YOUR_API_TOKEN")
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()
    zoneID := "your-zone-id" // 從 Cloudflare Dashboard 獲取

    // 2. 創建 DNS A 記錄
    record := cloudflare.CreateDNSRecordParams{
        Type:    "A",
        Name:    "vpn.example.com",
        Content: "203.0.113.10",
        TTL:     3600,
        Proxied: cloudflare.BoolPtr(false), // 不走 Cloudflare 代理（VPN 需要真實 IP）
    }

    result, err := api.CreateDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), record)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Created DNS record: %s -> %s\n", result.Name, result.Content)

    // 3. 查詢現有 DNS 記錄
    records, _, err := api.ListDNSRecords(ctx, cloudflare.ZoneIdentifier(zoneID), cloudflare.ListDNSRecordsParams{
        Type: "A",
    })
    if err != nil {
        log.Fatal(err)
    }

    for _, r := range records {
        fmt.Printf("Record: %s -> %s\n", r.Name, r.Content)
    }

    // 4. 更新 DNS 記錄
    updateParams := cloudflare.UpdateDNSRecordParams{
        ID:      result.ID,
        Type:    "A",
        Name:    "vpn.example.com",
        Content: "203.0.113.20", // 新 IP
        TTL:     3600,
    }

    _, err = api.UpdateDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), updateParams)
    if err != nil {
        log.Fatal(err)
    }

    // 5. 刪除 DNS 記錄
    err = api.DeleteDNSRecord(ctx, cloudflare.ZoneIdentifier(zoneID), result.ID)
    if err != nil {
        log.Fatal(err)
    }
}
```

**來源**: [cloudflare-go GitHub](https://github.com/cloudflare/cloudflare-go), [Cloudflare API Docs](https://developers.cloudflare.com/api/resources/dns/)

### 3.2 AWS Route53 + AWS SDK for Go

**官方庫**: `github.com/aws/aws-sdk-go-v2/service/route53`

**基本用法**:

```go
package main

import (
    "context"
    "fmt"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/route53"
    "github.com/aws/aws-sdk-go-v2/service/route53/types"
)

func main() {
    // 1. 載入 AWS 配置
    cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
    if err != nil {
        panic(err)
    }

    client := route53.NewFromConfig(cfg)
    ctx := context.Background()

    // 2. 創建或更新 DNS 記錄
    input := &route53.ChangeResourceRecordSetsInput{
        HostedZoneId: aws.String("Z1234567890ABC"), // Hosted Zone ID
        ChangeBatch: &types.ChangeBatch{
            Changes: []types.Change{
                {
                    Action: types.ChangeActionUpsert, // 如果存在則更新，否則創建
                    ResourceRecordSet: &types.ResourceRecordSet{
                        Name: aws.String("vpn.example.com"),
                        Type: types.RRTypeA,
                        TTL:  aws.Int64(300),
                        ResourceRecords: []types.ResourceRecord{
                            {
                                Value: aws.String("203.0.113.10"),
                            },
                        },
                    },
                },
            },
        },
    }

    result, err := client.ChangeResourceRecordSets(ctx, input)
    if err != nil {
        panic(err)
    }

    fmt.Printf("Change Info: %+v\n", result.ChangeInfo)
}
```

**來源**: [AWS SDK for Go - Route53](https://docs.aws.amazon.com/sdk-for-go/api/service/route53/)

### 3.3 DNSPod + Go 客戶端

**官方庫**: `github.com/nrdcg/dnspod-go`

**基本用法**:

```go
package main

import (
    "fmt"

    "github.com/nrdcg/dnspod-go"
)

func main() {
    client := dnspod.NewClient("YOUR_API_TOKEN")

    // 1. 列出所有域名
    domains, _, err := client.Domains.List()
    if err != nil {
        panic(err)
    }

    for _, domain := range domains {
        fmt.Printf("Domain: %s\n", domain.Name)
    }

    // 2. 創建 DNS 記錄
    record := &dnspod.Record{
        Name:  "vpn",
        Type:  "A",
        Line:  "默认",
        Value: "203.0.113.10",
        TTL:   600,
    }

    _, _, err = client.Records.Create("example.com", record)
    if err != nil {
        panic(err)
    }
}
```

**來源**: [nrdcg/dnspod-go GitHub](https://github.com/nrdcg/dnspod-go)

---

## 4. SSL 證書自動續期

### 4.1 Lego - Let's Encrypt ACME Go 客戶端

**項目信息**:
- GitHub: [go-acme/lego](https://github.com/go-acme/lego)
- 官網: [go-acme.github.io/lego](https://go-acme.github.io/lego/)
- 維護狀態: ✅ 積極維護（2026-01-08 最新版本）
- 支持年限: 約 10 年

**核心功能**:
- ✅ 支持 ACME v2 (RFC 8555)
- ✅ 支持 TLS-ALPN Challenge (RFC 8737)
- ✅ 支持 IP 地址證書 (RFC 8738)
- ✅ 支持續期信息擴展 (RFC 9773 ARI)
- ✅ 支持約 **180 個 DNS 提供商**
- ✅ 支持 HTTP-01, DNS-01, TLS-ALPN-01 Challenge

**支持的 DNS 提供商**（部分）:
- Cloudflare
- Route53 (AWS)
- Google Cloud DNS
- Azure DNS
- DNSPod
- Alibaba Cloud DNS
- 等 180+ 個

**命令行用法**:

```bash
# 1. 安裝
go install github.com/go-acme/lego/v4/cmd/lego@latest

# 2. 使用 Cloudflare DNS Challenge 申請證書
export CLOUDFLARE_DNS_API_TOKEN="your-token"

lego --email="your-email@example.com" \
     --domains="vpn.example.com" \
     --dns cloudflare \
     run

# 3. 續期證書
lego --email="your-email@example.com" \
     --domains="vpn.example.com" \
     --dns cloudflare \
     renew --days 30

# 4. 通配符證書
lego --email="your-email@example.com" \
     --domains="*.vpn.example.com" \
     --dns cloudflare \
     run
```

**Go 程序集成**:

```go
package main

import (
    "crypto"
    "crypto/ecdsa"
    "crypto/elliptic"
    "crypto/rand"
    "fmt"
    "log"

    "github.com/go-acme/lego/v4/certcrypto"
    "github.com/go-acme/lego/v4/certificate"
    "github.com/go-acme/lego/v4/lego"
    "github.com/go-acme/lego/v4/providers/dns/cloudflare"
    "github.com/go-acme/lego/v4/registration"
)

// 實現 ACME User 接口
type MyUser struct {
    Email        string
    Registration *registration.Resource
    key          crypto.PrivateKey
}

func (u *MyUser) GetEmail() string {
    return u.Email
}

func (u *MyUser) GetRegistration() *registration.Resource {
    return u.Registration
}

func (u *MyUser) GetPrivateKey() crypto.PrivateKey {
    return u.key
}

func main() {
    // 1. 創建私鑰
    privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
    if err != nil {
        log.Fatal(err)
    }

    myUser := MyUser{
        Email: "your-email@example.com",
        key:   privateKey,
    }

    // 2. 創建 ACME 配置
    config := lego.NewConfig(&myUser)
    config.CADirURL = lego.LEDirectoryProduction // 生產環境
    config.Certificate.KeyType = certcrypto.RSA2048

    // 3. 創建 ACME 客戶端
    client, err := lego.NewClient(config)
    if err != nil {
        log.Fatal(err)
    }

    // 4. 配置 DNS 提供商（Cloudflare）
    provider, err := cloudflare.NewDNSProvider()
    if err != nil {
        log.Fatal(err)
    }

    err = client.Challenge.SetDNS01Provider(provider)
    if err != nil {
        log.Fatal(err)
    }

    // 5. 註冊賬號
    reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
    if err != nil {
        log.Fatal(err)
    }
    myUser.Registration = reg

    // 6. 申請證書
    request := certificate.ObtainRequest{
        Domains: []string{"vpn.example.com"},
        Bundle:  true,
    }

    certificates, err := client.Certificate.Obtain(request)
    if err != nil {
        log.Fatal(err)
    }

    // 7. 保存證書
    fmt.Printf("Certificate Domain: %s\n", certificates.Domain)
    fmt.Printf("Certificate URL: %s\n", certificates.CertURL)
    // certificates.Certificate - PEM 格式證書
    // certificates.PrivateKey - PEM 格式私鑰
    // certificates.IssuerCertificate - 中間證書

    // 8. 續期證書（通常在到期前 30 天執行）
    certificates, err = client.Certificate.Renew(*certificates, true, false, "")
    if err != nil {
        log.Fatal(err)
    }
}
```

**自動續期 Cron Job**:

```go
package main

import (
    "log"
    "time"

    "github.com/robfig/cron/v3"
)

func renewCertificate() error {
    // 調用上面的證書續期邏輯
    log.Println("Checking certificate expiration...")
    // 實際續期邏輯
    return nil
}

func main() {
    c := cron.New()

    // 每天凌晨 2 點檢查證書是否需要續期
    _, err := c.AddFunc("0 2 * * *", func() {
        if err := renewCertificate(); err != nil {
            log.Printf("Certificate renewal failed: %v", err)
        } else {
            log.Println("Certificate renewed successfully")
        }
    })

    if err != nil {
        log.Fatal(err)
    }

    c.Start()

    // 保持程序運行
    select {}
}
```

**來源**: [Lego GitHub](https://github.com/go-acme/lego), [Lego Documentation](https://go-acme.github.io/lego/)

---

## 5. 企業級架構設計

### 5.1 2026 年企業 VPN 市場趨勢

**市場規模**:
- 2026 年市值: **18.67 億美元**
- 2035 年預測: **71.1 億美元**
- 年複合增長率: **16.2%**

**部署統計**:
- 大型企業占 VPN 部署的 **67%**
- **82%** 的企業運行多區域 VPN 架構
- **62%** 的千人以上企業在 **10+ 個站點**部署分布式 VPN 網關
- **59%** 的企業管理 **10,000+ 同時連接**
- **47%** 需要高級負載均衡

**技術趨勢（2023-2025）**:
- AI 驅動的 VPN 策略增長 **54%**
- 雲管理 VPN 解決方案增長 **47%**
- 5G 集成 VPN 增長 **43%**
- 端點自適應隧道增長 **39%**
- 容器化 VPN 部署增長 **31%**
- SASE 嵌入式 VPN 框架占 **37%**

**來源**: [Enterprise Infrastructure VPNs Market](https://www.marketgrowthreports.com/market-reports/enterprise-infrastructure-vpns-software-market-118677)

### 5.2 多區域 VPN 架構設計

**現代 Cloud VPN 架構**:

```
┌─────────────────────────────────────────────────────────────────┐
│                      Global VPN Backbone                         │
│                                                                  │
│  ┌─────────────┐   ┌─────────────┐   ┌─────────────┐           │
│  │   US East   │   │   EU West   │   │ Asia Pacific│           │
│  │     PoP     │   │     PoP     │   │     PoP     │           │
│  └──────┬──────┘   └──────┬──────┘   └──────┬──────┘           │
│         │                 │                 │                   │
└─────────┼─────────────────┼─────────────────┼───────────────────┘
          │                 │                 │
    ┌─────┴─────┐     ┌─────┴─────┐     ┌─────┴─────┐
    │  Branch   │     │  Branch   │     │  Branch   │
    │  Office   │     │  Office   │     │  Office   │
    │  New York │     │  London   │     │  Tokyo    │
    └───────────┘     └───────────┘     └───────────┘
          │                 │                 │
    ┌─────┴─────┐     ┌─────┴─────┐     ┌─────┴─────┐
    │  Remote   │     │  Remote   │     │  Remote   │
    │   Users   │     │   Users   │     │   Users   │
    └───────────┘     └───────────┘     └───────────┘
```

**核心設計原則**:
1. **就近連接**: 用戶自動連接到最近的 PoP（Point of Presence）
2. **負載均衡**: 每個 PoP 支持多台 VPN 服務器
3. **高可用性**: 跨可用區（AZ）冗餘部署
4. **自動故障轉移**: 健康檢查 + DNS 切換
5. **統一管理**: 中央控制平面管理所有區域

**來源**: [Cato Networks - Scalable VPN Connectivity](https://www.catonetworks.com/blog/rethinking-enterprise-remote-access-vpn-solutions-designing-scalable-vpn-connectivity/)

### 5.3 推薦架構：多區域 WireGuard + Cloudflare DNS

**架構圖**:

```
┌─────────────────────────────────────────────────────────────────┐
│                    Cloudflare DNS (Global)                       │
│  vpn.example.com → Load Balanced Geo-Routing                    │
│  ├─ us-east.vpn.example.com   → 203.0.113.10                    │
│  ├─ eu-west.vpn.example.com   → 198.51.100.20                   │
│  └─ ap-south.vpn.example.com  → 192.0.2.30                      │
└─────────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        │                     │                     │
┌───────▼────────┐    ┌───────▼────────┐    ┌───────▼────────┐
│   US-East-1    │    │   EU-West-1    │    │  AP-South-1    │
│  AWS/GCP/Azure │    │  AWS/GCP/Azure │    │  AWS/GCP/Azure │
├────────────────┤    ├────────────────┤    ├────────────────┤
│ WireGuard SRV  │    │ WireGuard SRV  │    │ WireGuard SRV  │
│ + Firezone UI  │    │ + Firezone UI  │    │ + Firezone UI  │
├────────────────┤    ├────────────────┤    ├────────────────┤
│  PostgreSQL    │◄───┤  PostgreSQL    │───►│  PostgreSQL    │
│  (Replicated)  │    │  (Replicated)  │    │  (Replicated)  │
└────────────────┘    └────────────────┘    └────────────────┘
        │                     │                     │
┌───────▼────────┐    ┌───────▼────────┐    ┌───────▼────────┐
│  US Users      │    │  EU Users      │    │  APAC Users    │
│  (Auto-routed) │    │  (Auto-routed) │    │  (Auto-routed) │
└────────────────┘    └────────────────┘    └────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│              Central Management & Monitoring                     │
│  ├─ User Provisioning API (Go)                                  │
│  ├─ Connection Monitoring (Prometheus + Grafana)                │
│  ├─ Automated Certificate Renewal (Lego Cron)                   │
│  └─ DNS Auto-Update Service (Cloudflare API)                    │
└─────────────────────────────────────────────────────────────────┘
```

**組件說明**:

1. **DNS 層（Cloudflare）**:
   - 全球負載均衡（Geo-Routing）
   - 健康檢查和自動故障轉移
   - DNSSEC 安全保護

2. **VPN 服務器層（WireGuard + Firezone）**:
   - 每個區域獨立部署
   - 使用 Firezone 提供 Web 管理界面
   - PostgreSQL 多區域複製（用戶數據同步）

3. **管理平面（Go Services）**:
   - 用戶自助註冊 API
   - 自動配置文件生成
   - 證書自動續期服務
   - 監控和告警系統

---

## 6. Go 語言實現方案

### 6.1 核心技術棧

| 組件 | 技術選型 | Go 庫 |
|------|---------|-------|
| **VPN 核心** | WireGuard | `golang.zx2c4.com/wireguard/wgctrl` |
| **VPN 管理界面** | Firezone | 外部服務（Docker） |
| **DNS 管理** | Cloudflare API | `github.com/cloudflare/cloudflare-go` |
| **SSL 證書** | Let's Encrypt | `github.com/go-acme/lego/v4` |
| **數據庫** | PostgreSQL | `github.com/lib/pq` |
| **Web 框架** | Gin | `github.com/gin-gonic/gin` |
| **任務調度** | Cron | `github.com/robfig/cron/v3` |
| **監控** | Prometheus | `github.com/prometheus/client_golang` |

### 6.2 WireGuard 配置管理（使用 wgctrl）

```go
package main

import (
    "fmt"
    "log"
    "net"

    "golang.zx2c4.com/wireguard/wgctrl"
    "golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type WireGuardManager struct {
    client *wgctrl.Client
}

func NewWireGuardManager() (*WireGuardManager, error) {
    client, err := wgctrl.New()
    if err != nil {
        return nil, err
    }
    return &WireGuardManager{client: client}, nil
}

func (wg *WireGuardManager) Close() error {
    return wg.client.Close()
}

// 獲取設備信息
func (wg *WireGuardManager) GetDevice(name string) (*wgtypes.Device, error) {
    return wg.client.Device(name)
}

// 添加 Peer
func (wg *WireGuardManager) AddPeer(deviceName string, publicKey string, allowedIPs []string) error {
    // 解析公鑰
    key, err := wgtypes.ParseKey(publicKey)
    if err != nil {
        return fmt.Errorf("invalid public key: %w", err)
    }

    // 解析允許的 IP 範圍
    var ipnets []net.IPNet
    for _, ip := range allowedIPs {
        _, ipnet, err := net.ParseCIDR(ip)
        if err != nil {
            return fmt.Errorf("invalid IP range %s: %w", ip, err)
        }
        ipnets = append(ipnets, *ipnet)
    }

    // 配置 Peer
    peerConfig := wgtypes.PeerConfig{
        PublicKey:  key,
        AllowedIPs: ipnets,
    }

    // 應用配置
    config := wgtypes.Config{
        Peers: []wgtypes.PeerConfig{peerConfig},
    }

    return wg.client.ConfigureDevice(deviceName, config)
}

// 移除 Peer
func (wg *WireGuardManager) RemovePeer(deviceName string, publicKey string) error {
    key, err := wgtypes.ParseKey(publicKey)
    if err != nil {
        return err
    }

    remove := true
    peerConfig := wgtypes.PeerConfig{
        PublicKey: key,
        Remove:    remove,
    }

    config := wgtypes.Config{
        Peers: []wgtypes.PeerConfig{peerConfig},
    }

    return wg.client.ConfigureDevice(deviceName, config)
}

// 列出所有 Peers
func (wg *WireGuardManager) ListPeers(deviceName string) ([]wgtypes.Peer, error) {
    device, err := wg.GetDevice(deviceName)
    if err != nil {
        return nil, err
    }
    return device.Peers, nil
}

func main() {
    wgm, err := NewWireGuardManager()
    if err != nil {
        log.Fatal(err)
    }
    defer wgm.Close()

    // 示例：添加 Peer
    err = wgm.AddPeer("wg0", "peer-public-key-here", []string{"10.0.0.2/32"})
    if err != nil {
        log.Fatal(err)
    }

    // 列出所有 Peers
    peers, err := wgm.ListPeers("wg0")
    if err != nil {
        log.Fatal(err)
    }

    for _, peer := range peers {
        fmt.Printf("Peer: %s\n", peer.PublicKey)
        fmt.Printf("  Last Handshake: %s\n", peer.LastHandshakeTime)
        fmt.Printf("  RX: %d bytes, TX: %d bytes\n", peer.ReceiveBytes, peer.TransmitBytes)
    }
}
```

**來源**: [wgctrl Go Package](https://pkg.go.dev/golang.zx2c4.com/wireguard/wgctrl)

### 6.3 用戶配置文件生成服務

```go
package vpn

import (
    "crypto/rand"
    "encoding/base64"
    "fmt"
    "net"
    "text/template"

    "golang.org/x/crypto/curve25519"
)

type WireGuardConfig struct {
    PrivateKey string
    PublicKey  string
    Address    string
    DNS        string
    ServerPublicKey string
    ServerEndpoint  string
    AllowedIPs string
}

// 生成 WireGuard 密鑰對
func GenerateKeyPair() (privateKey, publicKey string, err error) {
    var privKey [32]byte
    _, err = rand.Read(privKey[:])
    if err != nil {
        return "", "", err
    }

    // 生成公鑰
    var pubKey [32]byte
    curve25519.ScalarBaseMult(&pubKey, &privKey)

    privateKey = base64.StdEncoding.EncodeToString(privKey[:])
    publicKey = base64.StdEncoding.EncodeToString(pubKey[:])

    return privateKey, publicKey, nil
}

// 分配 IP 地址（從 IP 池中分配）
func AllocateIP(ipPool *net.IPNet, usedIPs []net.IP) (net.IP, error) {
    // 簡化實現：遍歷 IP 池，找到第一個未使用的 IP
    ip := ipPool.IP
    for {
        ip = nextIP(ip)
        if !ipPool.Contains(ip) {
            return nil, fmt.Errorf("IP pool exhausted")
        }

        if !contains(usedIPs, ip) {
            return ip, nil
        }
    }
}

func nextIP(ip net.IP) net.IP {
    next := make(net.IP, len(ip))
    copy(next, ip)
    for i := len(next) - 1; i >= 0; i-- {
        next[i]++
        if next[i] > 0 {
            break
        }
    }
    return next
}

func contains(ips []net.IP, ip net.IP) bool {
    for _, i := range ips {
        if i.Equal(ip) {
            return true
        }
    }
    return false
}

// 生成客戶端配置文件
func GenerateClientConfig(cfg WireGuardConfig) (string, error) {
    tmpl := `[Interface]
PrivateKey = {{.PrivateKey}}
Address = {{.Address}}
DNS = {{.DNS}}

[Peer]
PublicKey = {{.ServerPublicKey}}
Endpoint = {{.ServerEndpoint}}
AllowedIPs = {{.AllowedIPs}}
PersistentKeepalive = 25
`

    t, err := template.New("wg-config").Parse(tmpl)
    if err != nil {
        return "", err
    }

    var buf []byte
    writer := &stringWriter{buf: buf}
    err = t.Execute(writer, cfg)
    if err != nil {
        return "", err
    }

    return string(writer.buf), nil
}

type stringWriter struct {
    buf []byte
}

func (sw *stringWriter) Write(p []byte) (n int, err error) {
    sw.buf = append(sw.buf, p...)
    return len(p), nil
}

// 完整的用戶配置生成流程
func ProvisionUser(username string) (*WireGuardConfig, error) {
    // 1. 生成密鑰對
    privKey, pubKey, err := GenerateKeyPair()
    if err != nil {
        return nil, err
    }

    // 2. 分配 IP（示例：從 10.0.0.0/24 分配）
    _, ipPool, _ := net.ParseCIDR("10.0.0.0/24")
    usedIPs := []net.IP{} // 從數據庫查詢已使用的 IP

    clientIP, err := AllocateIP(ipPool, usedIPs)
    if err != nil {
        return nil, err
    }

    // 3. 組裝配置
    config := &WireGuardConfig{
        PrivateKey:      privKey,
        PublicKey:       pubKey,
        Address:         fmt.Sprintf("%s/32", clientIP.String()),
        DNS:             "1.1.1.1, 8.8.8.8",
        ServerPublicKey: "SERVER_PUBLIC_KEY_HERE",
        ServerEndpoint:  "vpn.example.com:51820",
        AllowedIPs:      "0.0.0.0/0", // 全流量通過 VPN
    }

    return config, nil
}
```

### 6.4 統一管理 API 服務

```go
package main

import (
    "database/sql"
    "fmt"
    "net/http"
    "time"

    "github.com/gin-gonic/gin"
    _ "github.com/lib/pq"
)

type User struct {
    ID           int       `json:"id"`
    Username     string    `json:"username"`
    Email        string    `json:"email"`
    PublicKey    string    `json:"public_key"`
    AllocatedIP  string    `json:"allocated_ip"`
    Subdomain    string    `json:"subdomain"`
    CreatedAt    time.Time `json:"created_at"`
}

type VPNService struct {
    db          *sql.DB
    wgManager   *WireGuardManager
    dnsManager  *DNSManager
    certManager *CertManager
}

type DNSManager struct {
    // Cloudflare API 封裝
}

type CertManager struct {
    // Lego ACME 封裝
}

// POST /api/users/register
func (s *VPNService) RegisterUser(c *gin.Context) {
    var req struct {
        Username string `json:"username" binding:"required"`
        Email    string `json:"email" binding:"required,email"`
    }

    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    // 1. 生成 WireGuard 配置
    config, err := ProvisionUser(req.Username)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to provision VPN"})
        return
    }

    // 2. 分配子域名
    subdomain := fmt.Sprintf("%s.vpn.example.com", req.Username)

    // 3. 創建 DNS 記錄（指向 VPN 服務器）
    err = s.dnsManager.CreateRecord(subdomain, config.Address)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create DNS record"})
        return
    }

    // 4. 申請 SSL 證書
    err = s.certManager.IssueCertificate(subdomain)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to issue certificate"})
        return
    }

    // 5. 添加 Peer 到 WireGuard
    err = s.wgManager.AddPeer("wg0", config.PublicKey, []string{config.Address})
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add VPN peer"})
        return
    }

    // 6. 保存到數據庫
    user := User{
        Username:    req.Username,
        Email:       req.Email,
        PublicKey:   config.PublicKey,
        AllocatedIP: config.Address,
        Subdomain:   subdomain,
        CreatedAt:   time.Now(),
    }

    err = s.saveUser(&user)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save user"})
        return
    }

    // 7. 生成配置文件
    configFile, err := GenerateClientConfig(*config)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate config"})
        return
    }

    // 8. 返回結果
    c.JSON(http.StatusOK, gin.H{
        "user":   user,
        "config": configFile,
    })
}

// GET /api/users/:id/config
func (s *VPNService) GetUserConfig(c *gin.Context) {
    userID := c.Param("id")

    // 從數據庫查詢用戶
    user, err := s.getUserByID(userID)
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
        return
    }

    // 重新生成配置文件
    config := WireGuardConfig{
        PublicKey:       user.PublicKey,
        Address:         user.AllocatedIP,
        DNS:             "1.1.1.1",
        ServerPublicKey: "SERVER_PUBLIC_KEY",
        ServerEndpoint:  "vpn.example.com:51820",
        AllowedIPs:      "0.0.0.0/0",
    }

    configFile, _ := GenerateClientConfig(config)

    c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.conf", user.Username))
    c.Data(http.StatusOK, "text/plain", []byte(configFile))
}

// GET /api/stats/connections
func (s *VPNService) GetConnectionStats(c *gin.Context) {
    peers, err := s.wgManager.ListPeers("wg0")
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get stats"})
        return
    }

    var stats []gin.H
    for _, peer := range peers {
        stats = append(stats, gin.H{
            "public_key":        peer.PublicKey.String(),
            "last_handshake":    peer.LastHandshakeTime,
            "rx_bytes":          peer.ReceiveBytes,
            "tx_bytes":          peer.TransmitBytes,
            "endpoint":          peer.Endpoint,
        })
    }

    c.JSON(http.StatusOK, gin.H{
        "total_peers": len(peers),
        "peers":       stats,
    })
}

func (s *VPNService) saveUser(user *User) error {
    query := `
        INSERT INTO users (username, email, public_key, allocated_ip, subdomain, created_at)
        VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING id
    `
    return s.db.QueryRow(query, user.Username, user.Email, user.PublicKey,
        user.AllocatedIP, user.Subdomain, user.CreatedAt).Scan(&user.ID)
}

func (s *VPNService) getUserByID(id string) (*User, error) {
    user := &User{}
    query := `SELECT id, username, email, public_key, allocated_ip, subdomain, created_at
              FROM users WHERE id = $1`
    err := s.db.QueryRow(query, id).Scan(&user.ID, &user.Username, &user.Email,
        &user.PublicKey, &user.AllocatedIP, &user.Subdomain, &user.CreatedAt)
    return user, err
}

func main() {
    // 初始化數據庫
    db, _ := sql.Open("postgres", "postgres://user:pass@localhost/vpndb?sslmode=disable")
    defer db.Close()

    // 初始化服務
    wgm, _ := NewWireGuardManager()
    defer wgm.Close()

    service := &VPNService{
        db:          db,
        wgManager:   wgm,
        dnsManager:  &DNSManager{},
        certManager: &CertManager{},
    }

    // 初始化 Gin 路由
    r := gin.Default()

    api := r.Group("/api")
    {
        api.POST("/users/register", service.RegisterUser)
        api.GET("/users/:id/config", service.GetUserConfig)
        api.GET("/stats/connections", service.GetConnectionStats)
    }

    r.Run(":8080")
}
```

---

## 7. 完整集成案例

### 7.1 用戶註冊自動化流程

**流程圖**:

```
User Registration
       │
       ▼
┌──────────────────┐
│ 1. API Request   │
│ POST /register   │
│ {username, email}│
└────────┬─────────┘
         │
         ▼
┌────────────────────────────┐
│ 2. Generate WireGuard Keys │
│ - Private Key (user keeps) │
│ - Public Key (server uses) │
└────────┬───────────────────┘
         │
         ▼
┌────────────────────────────┐
│ 3. Allocate IP from Pool   │
│ - Query DB for used IPs    │
│ - Assign next available IP │
└────────┬───────────────────┘
         │
         ▼
┌────────────────────────────┐
│ 4. Create Subdomain        │
│ - username.vpn.example.com │
│ - Cloudflare API call      │
└────────┬───────────────────┘
         │
         ▼
┌────────────────────────────┐
│ 5. Issue SSL Certificate   │
│ - Lego ACME DNS Challenge  │
│ - Save cert to storage     │
└────────┬───────────────────┘
         │
         ▼
┌────────────────────────────┐
│ 6. Add WireGuard Peer      │
│ - wgctrl API call          │
│ - Configure allowed IPs    │
└────────┬───────────────────┘
         │
         ▼
┌────────────────────────────┐
│ 7. Save to Database        │
│ - User info + credentials  │
│ - IP allocation record     │
└────────┬───────────────────┘
         │
         ▼
┌────────────────────────────┐
│ 8. Return Config File      │
│ - .conf for WireGuard      │
│ - QR Code for mobile       │
└────────────────────────────┘
```

### 7.2 完整 Go 實現（帶監控和自動續期）

**項目結構**:

```
vpn-manager/
├── cmd/
│   ├── api/
│   │   └── main.go              # API 服務入口
│   ├── cert-renewer/
│   │   └── main.go              # 證書續期 Cron Job
│   └── dns-updater/
│       └── main.go              # DNS 動態更新服務
├── internal/
│   ├── wireguard/
│   │   ├── manager.go           # WireGuard 管理
│   │   └── config.go            # 配置生成
│   ├── dns/
│   │   ├── cloudflare.go        # Cloudflare API 封裝
│   │   └── route53.go           # Route53 API 封裝
│   ├── cert/
│   │   ├── lego.go              # Lego ACME 封裝
│   │   └── storage.go           # 證書存儲
│   ├── api/
│   │   ├── handlers.go          # HTTP 處理器
│   │   └── middleware.go        # 認證/限流
│   ├── db/
│   │   ├── postgres.go          # PostgreSQL 客戶端
│   │   └── models.go            # 數據模型
│   └── monitoring/
│       ├── prometheus.go        # Prometheus metrics
│       └── alerting.go          # 告警規則
├── migrations/
│   └── 001_initial.sql          # 數據庫 schema
├── configs/
│   └── config.yaml              # 配置文件
├── docker-compose.yml           # Docker 部署配置
└── README.md
```

**數據庫 Schema**:

```sql
-- migrations/001_initial.sql

CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    public_key TEXT NOT NULL,
    private_key_hash TEXT, -- 用於驗證（不存明文）
    allocated_ip INET NOT NULL,
    subdomain VARCHAR(255) UNIQUE NOT NULL,
    status VARCHAR(20) DEFAULT 'active', -- active, suspended, deleted
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE ip_allocations (
    id SERIAL PRIMARY KEY,
    ip_address INET UNIQUE NOT NULL,
    user_id INTEGER REFERENCES users(id),
    region VARCHAR(50) NOT NULL, -- us-east, eu-west, ap-south
    allocated_at TIMESTAMP DEFAULT NOW(),
    released_at TIMESTAMP
);

CREATE TABLE certificates (
    id SERIAL PRIMARY KEY,
    domain VARCHAR(255) UNIQUE NOT NULL,
    cert_pem TEXT NOT NULL,
    key_pem TEXT NOT NULL,
    issuer_cert TEXT,
    expires_at TIMESTAMP NOT NULL,
    renewed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE connection_logs (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    connected_at TIMESTAMP NOT NULL,
    disconnected_at TIMESTAMP,
    bytes_received BIGINT DEFAULT 0,
    bytes_sent BIGINT DEFAULT 0,
    client_ip INET,
    server_region VARCHAR(50)
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_ip_allocations_user ON ip_allocations(user_id);
CREATE INDEX idx_ip_allocations_region ON ip_allocations(region);
CREATE INDEX idx_certificates_expires ON certificates(expires_at);
CREATE INDEX idx_connection_logs_user ON connection_logs(user_id);
```

**Prometheus Metrics**:

```go
// internal/monitoring/prometheus.go
package monitoring

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    // 活躍連接數
    ActiveConnections = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "vpn_active_connections",
            Help: "Number of active VPN connections",
        },
        []string{"region"},
    )

    // 流量統計
    BytesTransferred = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "vpn_bytes_transferred_total",
            Help: "Total bytes transferred",
        },
        []string{"direction", "region"}, // direction: rx/tx
    )

    // 用戶註冊數
    UserRegistrations = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "vpn_user_registrations_total",
            Help: "Total number of user registrations",
        },
        []string{"region"},
    )

    // 證書到期時間（天）
    CertificateExpiry = promauto.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "vpn_certificate_expiry_days",
            Help: "Days until certificate expiration",
        },
        []string{"domain"},
    )

    // API 請求延遲
    APILatency = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "vpn_api_request_duration_seconds",
            Help:    "API request latency in seconds",
            Buckets: prometheus.DefBuckets,
        },
        []string{"method", "endpoint", "status"},
    )
)
```

**證書自動續期服務**:

```go
// cmd/cert-renewer/main.go
package main

import (
    "context"
    "log"
    "time"

    "github.com/robfig/cron/v3"
    "vpn-manager/internal/cert"
    "vpn-manager/internal/db"
)

func main() {
    // 初始化數據庫
    database, err := db.NewPostgresDB("postgres://user:pass@localhost/vpndb")
    if err != nil {
        log.Fatal(err)
    }
    defer database.Close()

    // 初始化證書管理器
    certMgr := cert.NewLegoManager()

    // 創建 Cron 調度器
    c := cron.New()

    // 每天凌晨 2 點檢查證書
    _, err = c.AddFunc("0 2 * * *", func() {
        log.Println("Starting certificate renewal check...")

        // 查詢所有 30 天內到期的證書
        certs, err := database.GetExpiringCertificates(30)
        if err != nil {
            log.Printf("Failed to query certificates: %v", err)
            return
        }

        for _, cert := range certs {
            log.Printf("Renewing certificate for %s (expires: %s)",
                cert.Domain, cert.ExpiresAt)

            // 續期證書
            newCert, err := certMgr.RenewCertificate(context.Background(), cert.Domain)
            if err != nil {
                log.Printf("Failed to renew %s: %v", cert.Domain, err)
                continue
            }

            // 更新數據庫
            err = database.UpdateCertificate(cert.Domain, newCert)
            if err != nil {
                log.Printf("Failed to update certificate in DB: %v", err)
                continue
            }

            log.Printf("Successfully renewed certificate for %s", cert.Domain)
        }
    })

    if err != nil {
        log.Fatal(err)
    }

    c.Start()

    log.Println("Certificate renewal service started")
    select {} // 保持運行
}
```

### 7.3 Docker Compose 部署

```yaml
# docker-compose.yml
version: '3.8'

services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: vpndb
      POSTGRES_USER: vpnuser
      POSTGRES_PASSWORD: ${DB_PASSWORD}
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./migrations:/docker-entrypoint-initdb.d
    networks:
      - vpn-internal
    restart: unless-stopped

  wireguard:
    image: linuxserver/wireguard:latest
    cap_add:
      - NET_ADMIN
      - SYS_MODULE
    environment:
      - PUID=1000
      - PGID=1000
      - TZ=UTC
    volumes:
      - wireguard_config:/config
    ports:
      - "51820:51820/udp"
    sysctls:
      - net.ipv4.conf.all.src_valid_mark=1
    networks:
      - vpn-internal
    restart: unless-stopped

  firezone:
    image: firezone/firezone:latest
    depends_on:
      - postgres
    environment:
      DATABASE_URL: postgres://vpnuser:${DB_PASSWORD}@postgres/vpndb
      EXTERNAL_URL: https://admin.vpn.example.com
      DEFAULT_ADMIN_EMAIL: admin@example.com
      DEFAULT_ADMIN_PASSWORD: ${ADMIN_PASSWORD}
    ports:
      - "127.0.0.1:8080:8080"
    networks:
      - vpn-internal
    restart: unless-stopped

  vpn-api:
    build:
      context: .
      dockerfile: Dockerfile
    depends_on:
      - postgres
    environment:
      DATABASE_URL: postgres://vpnuser:${DB_PASSWORD}@postgres/vpndb
      CLOUDFLARE_API_TOKEN: ${CLOUDFLARE_TOKEN}
      WIREGUARD_INTERFACE: wg0
    ports:
      - "127.0.0.1:8082:8082"
    networks:
      - vpn-internal
    restart: unless-stopped

  cert-renewer:
    build:
      context: .
      dockerfile: Dockerfile.cert-renewer
    depends_on:
      - postgres
    environment:
      DATABASE_URL: postgres://vpnuser:${DB_PASSWORD}@postgres/vpndb
      CLOUDFLARE_API_TOKEN: ${CLOUDFLARE_TOKEN}
      ACME_EMAIL: admin@example.com
    networks:
      - vpn-internal
    restart: unless-stopped

  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - prometheus_data:/prometheus
    ports:
      - "127.0.0.1:9090:9090"
    networks:
      - vpn-internal
    restart: unless-stopped

  grafana:
    image: grafana/grafana:latest
    depends_on:
      - prometheus
    environment:
      GF_SECURITY_ADMIN_PASSWORD: ${GRAFANA_PASSWORD}
    volumes:
      - grafana_data:/var/lib/grafana
    ports:
      - "127.0.0.1:3000:3000"
    networks:
      - vpn-internal
    restart: unless-stopped

networks:
  vpn-internal:
    driver: bridge

volumes:
  postgres_data:
  wireguard_config:
  prometheus_data:
  grafana_data:
```

### 7.4 用戶自助服務前端（React 示例）

```tsx
// UserDashboard.tsx
import React, { useState } from 'react';
import { QRCodeSVG } from 'qrcode.react';

interface VPNConfig {
  config: string;
  subdomain: string;
  allocatedIP: string;
}

export function UserDashboard() {
  const [config, setConfig] = useState<VPNConfig | null>(null);

  const handleRegister = async (username: string, email: string) => {
    const response = await fetch('/api/users/register', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, email }),
    });

    const data = await response.json();
    setConfig(data);
  };

  const downloadConfig = () => {
    if (!config) return;

    const blob = new Blob([config.config], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'wireguard.conf';
    a.click();
  };

  return (
    <div className="dashboard">
      <h1>VPN 自助服務平台</h1>

      {!config ? (
        <RegistrationForm onSubmit={handleRegister} />
      ) : (
        <div className="config-display">
          <h2>您的 VPN 配置已就緒</h2>

          <div className="info-cards">
            <div className="card">
              <h3>子域名</h3>
              <p>{config.subdomain}</p>
            </div>

            <div className="card">
              <h3>分配 IP</h3>
              <p>{config.allocatedIP}</p>
            </div>
          </div>

          <div className="qr-code">
            <h3>掃描二維碼連接（移動設備）</h3>
            <QRCodeSVG value={config.config} size={256} />
          </div>

          <div className="actions">
            <button onClick={downloadConfig}>
              下載配置文件
            </button>
            <a href="/docs/setup-guide" target="_blank">
              查看設置指南
            </a>
          </div>

          <div className="config-preview">
            <h3>配置文件預覽</h3>
            <pre>{config.config}</pre>
          </div>
        </div>
      )}
    </div>
  );
}
```

---

## 總結與建議

### 推薦技術方案

**VPN 核心**:
- ✅ **選擇 WireGuard** 而非 OpenVPN（速度快 52%，代碼少 100 倍）
- ✅ **使用 Firezone** 作為企業級管理面板（零信任架構，細粒度權限）

**DNS 管理**:
- ✅ **Cloudflare API** + `cloudflare-go` 官方庫
- ✅ 支持 Geo-Routing 實現多區域負載均衡
- ✅ DNSSEC 安全保護

**SSL 證書**:
- ✅ **Lego** ACME 客戶端（支持 180+ DNS 提供商）
- ✅ 自動續期 Cron Job（每天檢查，到期前 30 天續期）
- ✅ 支持通配符證書

**架構設計**:
- ✅ 多區域部署（US-East, EU-West, APAC）
- ✅ PostgreSQL 跨區域複製（用戶數據同步）
- ✅ Prometheus + Grafana 監控（連接數、流量、證書到期）
- ✅ 容器化部署（Docker Compose）

### 開發優先級

**Phase 1 - MVP（2 週）**:
1. 基礎 WireGuard 配置管理
2. 單區域用戶註冊 API
3. Cloudflare DNS 自動創建
4. 配置文件生成和下載

**Phase 2 - 生產就緒（4 週）**:
1. SSL 證書自動申請和續期
2. PostgreSQL 持久化
3. Firezone 集成（Web UI）
4. 監控和告警系統

**Phase 3 - 多區域擴展（6 週）**:
1. 多區域 VPN 服務器部署
2. 地理路由和負載均衡
3. 跨區域數據同步
4. 高可用性設計

**Phase 4 - 企業功能（8 週）**:
1. 團隊和組織管理
2. 基於群組的訪問控制
3. 流量統計和計費
4. 合規審計日誌

### 關鍵注意事項

1. **安全性**:
   - 不要在數據庫存儲用戶的私鑰（只存公鑰和私鑰哈希用於驗證）
   - 使用 HTTPS 保護 API 端點
   - 實施速率限制防止濫用
   - 定期備份證書和用戶數據

2. **性能**:
   - 使用連接池（PostgreSQL）
   - 實施 DNS 查詢緩存
   - 監控 WireGuard 握手延遲
   - 設置合理的連接數上限

3. **可維護性**:
   - 使用結構化日誌（JSON）
   - 實施健康檢查端點
   - 編寫完整的單元測試
   - 提供詳細的 API 文檔（OpenAPI）

4. **合規性**:
   - 符合 GDPR（用戶數據刪除請求）
   - 記錄連接日誌（根據當地法律要求）
   - 提供數據導出功能
   - 實施數據保留策略

---

## 參考資源

### WireGuard & VPN Management
- [Firezone GitHub](https://github.com/firezone/firezone)
- [Firezone Official Site](https://www.firezone.dev/)
- [Awesome WireGuard](https://github.com/cedrickchee/awesome-wireguard)
- [WireGuard vs OpenVPN 2026](https://cyberinsider.com/vpn/wireguard/wireguard-vs-openvpn/)
- [ExpressVPN Protocol Comparison](https://www.expressvpn.com/blog/wireguard-vs-openvpn/)
- [Wg-Easy Installation Guide](https://docs.vultr.com/how-to-install-wg-easy-an-opensource-web-ui-for-wireguard-vpn)

### DNS Automation
- [Cloudflare Go Library](https://github.com/cloudflare/cloudflare-go)
- [Cloudflare API Documentation](https://developers.cloudflare.com/api/resources/dns/)
- [DNSPod Go Client](https://github.com/nrdcg/dnspod-go)
- [AWS Route53 SDK](https://docs.aws.amazon.com/sdk-for-go/api/service/route53/)

### SSL/TLS Certificates
- [Lego GitHub](https://github.com/go-acme/lego)
- [Lego Documentation](https://go-acme.github.io/lego/)

### Enterprise VPN Architecture
- [Enterprise Infrastructure VPN Market Report](https://www.marketgrowthreports.com/market-reports/enterprise-infrastructure-vpns-software-market-118677)
- [Cato Networks - Scalable VPN](https://www.catonetworks.com/blog/rethinking-enterprise-remote-access-vpn-solutions-designing-scalable-vpn-connectivity/)

### Best Practices
- [User Provisioning Best Practices](https://www.securends.com/blog/user-provisioning-best-practices/)
- [Automated User Provisioning Guide](https://blog.invgate.com/automated-user-provisioning)

### Go Libraries
- [wireguard-go](https://github.com/WireGuard/wireguard-go)
- [wgctrl](https://pkg.go.dev/golang.zx2c4.com/wireguard/wgctrl)

---

**報告編制時間**: 2026-02-20
**版本**: 1.0
**作者**: Claude Code Assistant
