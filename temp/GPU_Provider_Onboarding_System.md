# GPU 提供者上線系統詳細設計

**核心目標**: 讓全球任何人都能輕鬆將閒置的 GPU 上線賺取收入

**文檔版本**: v1.0
**更新日期**: 2026-03-23

---

## 📋 第一部分：系統總體流程

### 1.1 GPU 提供者上線完整流程圖

```
┌────────────────────────────────────────────────────────────────┐
│              GPU 提供者上線五步驟完整流程                        │
└────────────────────────────────────────────────────────────────┘

Step 1️⃣ - 註冊與身份驗證 (5-10 分鐘)
└─────────────────────────────────────────
  │
  ├─ 訪問平台網站 (https://alice.ai)
  │   └─ 點擊 "Share Your GPU" 按鈕
  │
  ├─ 郵箱註冊或 Web3 連錢包
  │   ├─ 選項 A：傳統郵箱 + 密碼
  │   ├─ 選項 B：Web3 錢包 (MetaMask/WalletConnect)
  │   └─ 平台檢查重複帳號 (防止 Sybil 攻擊)
  │
  ├─ KYC 身份驗證 (必須)
  │   ├─ 上傳身份證明（護照 / ID / 駕照）
  │   ├─ 人臉識別驗證
  │   └─ 處理時間：5-15 分鐘 (自動)
  │
  ├─ 銀行/支付信息綁定
  │   ├─ 銀行賬戶或加密錢包地址
  │   └─ 用於接收收益
  │
  └─ ✅ 帳號激活
      └─ 發送確認郵件，進入 Step 2

Step 2️⃣ - 安裝客戶端程序 (5 分鐘)
└──────────────────────────────────
  │
  ├─ 下載適合你的操作系統的客戶端
  │   ├─ Windows: alice-gpu-client-v1.0-windows.exe
  │   ├─ macOS: alice-gpu-client-v1.0-macos.dmg
  │   ├─ Linux: alice-gpu-client-v1.0-linux.tar.gz
  │   └─ 文件大小：~50MB
  │
  ├─ 驗證完整性
  │   ├─ 下載 checksum 文件
  │   ├─ 驗證 SHA-256 hash
  │   └─ 防止供應鏈攻擊
  │
  ├─ 安裝程序
  │   ├─ ./install.sh (Linux/macOS)
  │   ├─ 或雙擊 .exe (Windows)
  │   └─ 自動檢測依賴項 (NVIDIA 驅動、CUDA 等)
  │
  ├─ 系統檢查
  │   ├─ 檢查 NVIDIA 驅動版本 (>= 525)
  │   ├─ 檢查 CUDA 版本 (>= 11.0)
  │   ├─ 檢查 cuDNN (>= 8.0)
  │   └─ 給出升級建議（如需）
  │
  └─ ✅ 安裝完成
      └─ 應用會提示下一步

Step 3️⃣ - GPU 檢測與配置 (2-5 分鐘)
└─────────────────────────────────────
  │
  ├─ 自動掃描本地 GPU
  │   ├─ 執行：nvidia-smi
  │   ├─ 檢測所有 GPU：型號、vRAM、計算能力
  │   └─ 結果展示給用戶
  │
  ├─ 用戶確認要分享的 GPU
  │   ├─ 可選擇分享全部 GPU
  │   ├─ 或只分享某些 GPU（保留部分用於個人用途）
  │   └─ 設置每個 GPU 的配置
  │
  ├─ 配置每個 GPU 的參數
  │   │
  │   └─ 對每個 GPU：
  │       ├─ 名稱（自定義）："My A100 #1"
  │       ├─ 最大並發任務數
  │       │   ├─ 默認值：(vRAM / 2) / 平均任務 vRAM 需求
  │       │   ├─ 例如：80GB A100 → 最多 8 個並發任務
  │       │   └─ 防止記憶體溢出
  │       │
  │       ├─ 接受的工作負載類型
  │       │   ├─ ✓ LLM 推理
  │       │   ├─ ✓ 視覺模型 (ComfyUI, Stable Diffusion)
  │       │   ├─ ✓ 數據處理
  │       │   ├─ ✓ 科學計算
  │       │   └─ ✓ 自定義容器
  │       │
  │       ├─ 可用時間範圍
  │       │   ├─ 24/7 (全天候)
  │       │   ├─ 或特定時間範圍（9am-6pm）
  │       │   └─ 系統尊重用戶的時間表
  │       │
  │       ├─ 最大租用時長
  │       │   ├─ 例如：每個任務最長 12 小時
  │       │   └─ 防止資源被長期佔用
  │       │
  │       └─ 小時價格（USD）
  │           ├─ 根據 GPU 型號提示市價
  │           ├─ RTX 4090：$0.30/小時
  │           ├─ A100：$0.80/小時
  │           ├─ H100：$1.50/小時
  │           └─ 用戶可自定義
  │
  └─ ✅ 配置保存
      └─ 進入 Step 4

Step 4️⃣ - 客戶端啟動與驗證 (1 分鐘)
└──────────────────────────────────────
  │
  ├─ 啟動客戶端程序
  │   ├─ 命令：alice-gpu-client start
  │   ├─ 或點擊應用圖標
  │   └─ 背景運行
  │
  ├─ 初始化連接
  │   ├─ 讀取配置文件 (~/.alice/config.yaml)
  │   ├─ 生成 GPU 設備簽名
  │   │   ├─ 簽名包含：GPU UUID + 序列號 + MAC 地址
  │   │   └─ 用於防止設備冒充
  │   │
  │   └─ 連接到平台服務器
  │       ├─ Protocol: TLS 1.3
  │       ├─ 身份驗證：API key + 簽名驗證
  │       └─ 建立會話
  │
  ├─ 上報 GPU 信息到平台
  │   ├─ GPU 基本信息
  │   │   ├─ 型號、vRAM、計算能力
  │   │   ├─ 驅動版本、CUDA 版本
  │   │   └─ 設備的全局唯一識別碼 (UUID)
  │   │
  │   ├─ 當前狀態
  │   │   ├─ 利用率、溫度、功耗
  │   │   └─ 可用內存
  │   │
  │   ├─ 配置信息
  │   │   ├─ 並發任務數上限
  │   │   ├─ 支持的工作負載
  │   │   └─ 小時價格
  │   │
  │   └─ 平台驗證並存儲到數據庫
  │
  ├─ 啟動心跳信號
  │   ├─ 每 10 秒發送一次心跳
  │   └─ 內容：GPU 狀態、利用率、可用性
  │
  └─ ✅ 驗證完成
      └─ 進入 Step 5

Step 5️⃣ - 上線與開始接單 (1 分鐘)
└─────────────────────────────────────
  │
  ├─ 平台顯示 GPU 已上線
  │   ├─ 用戶儀表板顯示 "ONLINE ✓"
  │   ├─ 客戶端顯示連接成功
  │   └─ 發送確認郵件給用戶
  │
  ├─ 收到第一個任務
  │   ├─ 客戶端下載任務代碼和數據
  │   ├─ 在容器中執行任務
  │   └─ 上報結果給平台
  │
  └─ ✅ 開始賺取收入！

⏱️ 總耗時：15-30 分鐘 (首次上線)
💰 準備工作：無 (客戶端自動檢測 GPU)

```

---

## 🖥️ 第二部分：GPU 自動檢測與配置

### 2.1 GPU 硬體檢測模塊

#### 2.1.1 檢測算法

```python
"""
GPU 自動檢測模塊
功能：掃描系統中所有可用的 GPU
支持平台：Windows, macOS, Linux
"""

class GPUDetector:
    def detect_all_gpus(self):
        """
        掃描系統中所有 GPU
        返回：GPU 列表 with 詳細信息
        """
        gpus = []

        # 方法 1：NVIDIA GPU (CUDA)
        nvidia_gpus = self._detect_nvidia_gpus()
        gpus.extend(nvidia_gpus)

        # 方法 2：AMD GPU (ROCm) - 未來支持
        # amd_gpus = self._detect_amd_gpus()
        # gpus.extend(amd_gpus)

        # 方法 3：Intel GPU (oneAPI) - 未來支持
        # intel_gpus = self._detect_intel_gpus()
        # gpus.extend(intel_gpus)

        return gpus

    def _detect_nvidia_gpus(self):
        """
        使用 nvidia-smi 檢測 NVIDIA GPU
        """
        import subprocess
        import json

        try:
            # 執行 nvidia-smi，以 JSON 格式返回
            cmd = [
                'nvidia-smi',
                '--query-gpu=index,name,driver_version,vram,compute_cap',
                '--format=csv,noheader,nounits'
            ]

            result = subprocess.run(cmd, capture_output=True, text=True)
            if result.returncode != 0:
                return []  # nvidia-smi 失敗

            gpus = []
            for line in result.stdout.strip().split('\n'):
                parts = line.split(',')
                gpu = {
                    'index': int(parts[0]),
                    'name': parts[1].strip(),
                    'driver_version': parts[2].strip(),
                    'vram_mb': int(parts[3]),  # MB
                    'compute_capability': parts[4].strip(),
                    'type': 'nvidia',
                }

                # 轉換為 GB
                gpu['vram_gb'] = gpu['vram_mb'] / 1024

                # 計算 TFLOPS（浮點運算能力）
                gpu['tflops'] = self._calculate_tflops(
                    gpu['name'],
                    gpu['compute_capability']
                )

                gpus.append(gpu)

            return gpus

        except Exception as e:
            print(f"GPU 檢測失敗: {e}")
            return []

    def _calculate_tflops(self, gpu_name, compute_cap):
        """
        根據 GPU 型號和計算能力計算 TFLOPS
        """
        # NVIDIA GPU 的標稱 TFLOPS
        tflops_map = {
            'RTX 4090': 1456,      # FP32
            'RTX 4080': 728,
            'RTX 4070': 546,
            'RTX 4060': 280,
            'A100': 312,           # FP32, 40/80GB
            'H100': 756,           # FP32, 80GB
            'L40S': 728,           # 48GB
            'L40': 728,            # 48GB
            'V100': 130,           # 32GB
            'A10': 150,
        }

        for model, tflops in tflops_map.items():
            if model in gpu_name:
                return tflops

        # 如果不在預定義列表中，根據計算能力估算
        # 計算能力 8.0 (GA102, RTX 40xx) → ~高性能
        if compute_cap.startswith('9.0'):  # Ada
            return 800
        elif compute_cap.startswith('8.9'):  # Ada
            return 650
        elif compute_cap.startswith('8.0'):  # Ampere (A100, RTX 30xx)
            return 300
        else:
            return 100  # 保守估計

    def validate_gpu_requirements(self):
        """
        驗證系統是否滿足最低需求
        """
        checks = {
            'nvidia_smi': False,
            'nvidia_driver_version': None,
            'cuda_version': None,
            'cudnn_version': None,
            'issues': []
        }

        # 檢查 1：nvidia-smi
        try:
            result = subprocess.run(['nvidia-smi', '--version'],
                                  capture_output=True, text=True)
            if result.returncode == 0:
                checks['nvidia_smi'] = True
                # 解析驅動版本
                version_line = result.stdout.strip().split('\n')[0]
                checks['nvidia_driver_version'] = version_line
            else:
                checks['issues'].append("❌ NVIDIA 驅動未安裝")
        except Exception:
            checks['issues'].append("❌ nvidia-smi 命令不可用")

        # 檢查 2：CUDA
        try:
            result = subprocess.run(['nvidia-smi', '--query-gpu=driver_version',
                                   '--format=csv,noheader'],
                                  capture_output=True, text=True)
            driver_ver = float(result.stdout.strip())
            # 推算 CUDA 版本（驅動版本 >= 525 → CUDA 12.x）
            if driver_ver >= 525:
                checks['cuda_version'] = "12.0+"
            else:
                checks['issues'].append(f"⚠️ NVIDIA 驅動版本較舊 ({driver_ver})")
        except Exception:
            pass

        # 檢查 3：cuDNN
        import os
        cudnn_paths = [
            '/usr/lib/x86_64-linux-gnu/libcudnn.so',  # Linux
            '/usr/local/cuda/lib64/libcudnn.so',
            'C:\\Program Files\\NVIDIA\\CUDNN\\bin\\cudnn64_x.dll',  # Windows
        ]

        for path in cudnn_paths:
            if os.path.exists(path):
                checks['cudnn_version'] = "Found"
                break

        if not checks['cudnn_version']:
            checks['issues'].append("⚠️ cuDNN 未檢測到（可選）")

        return checks

```

#### 2.1.2 GPU 信息展示界面

```
┌────────────────────────────────────────────────────┐
│        GPU 檢測結果 (自動掃描)                      │
├────────────────────────────────────────────────────┤
│                                                    │
│ ✓ 已檢測到 3 個 GPU：                               │
│                                                    │
│ ┌─ GPU #0 ─────────────────────────────────────┐  │
│ │ 型號：NVIDIA H100 PCIe 80GB                   │  │
│ │ vRAM：80 GB                                  │  │
│ │ 計算能力：9.0 (Ada)                          │  │
│ │ 計算能力：756 TFLOPS (FP32)                  │  │
│ │ 驅動版本：551.76                              │  │
│ │ CUDA：12.1                                   │  │
│ │ 狀態：✓ 可用                                  │  │
│ │                                              │  │
│ │ [✓] 分享此 GPU  [⚙️ 配置]                    │  │
│ └──────────────────────────────────────────────┘  │
│                                                    │
│ ┌─ GPU #1 ─────────────────────────────────────┐  │
│ │ 型號：NVIDIA RTX 4090                        │  │
│ │ vRAM：24 GB                                  │  │
│ │ 計算能力：8.9 (Ada)                          │  │
│ │ 計算能力：1456 TFLOPS (FP32)                 │  │
│ │ 驅動版本：551.76                              │  │
│ │ CUDA：12.1                                   │  │
│ │ 狀態：✓ 可用                                  │  │
│ │                                              │  │
│ │ [✓] 分享此 GPU  [⚙️ 配置]                    │  │
│ └──────────────────────────────────────────────┘  │
│                                                    │
│ ┌─ GPU #2 ─────────────────────────────────────┐  │
│ │ 型號：NVIDIA L40S                            │  │
│ │ vRAM：48 GB                                  │  │
│ │ 計算能力：8.9 (Ada)                          │  │
│ │ 計算能力：728 TFLOPS (FP32)                  │  │
│ │ 驅動版本：551.76                              │  │
│ │ CUDA：12.1                                   │  │
│ │ 狀態：✓ 可用                                  │  │
│ │                                              │  │
│ │ [  ] 分享此 GPU  [⚙️ 配置]  ← 用戶可選擇    │  │
│ └──────────────────────────────────────────────┘  │
│                                                    │
├────────────────────────────────────────────────────┤
│ 系統需求檢查：                                      │
│ ✓ NVIDIA 驅動版本 551.76 (>= 525)                 │
│ ✓ CUDA 12.1 (>= 11.0)                             │
│ ✓ cuDNN 已安裝                                     │
│                                                    │
│                    [下一步] →                     │
└────────────────────────────────────────────────────┘
```

---

### 2.2 GPU 配置表單

```
每個 GPU 的配置參數
═════════════════════════════════════════════════════

基本信息：
├─ GPU 名稱（自定義）
│   └─ 默認："NVIDIA H100 #0"
│   └─ 可改為："My Fast Compute"
│
├─ 描述（可選）
│   └─ "Optimized for LLM inference"
│
└─ 位置
    ├─ 城市：San Francisco
    ├─ 國家：USA
    └─ 用於計算延遲，匹配本地用戶

工作負載配置：
├─ 接受的工作負載類型 (複選)
│   ├─ ✓ LLM 推理（Transformers）
│   ├─ ✓ 圖像生成（Stable Diffusion, SDXL）
│   ├─ ✓ 文字到語音（TTS）
│   ├─ ✓ 視頻處理（FFmpeg 編碼）
│   ├─ ✓ 數據處理（Pandas, Spark）
│   ├─ ✓ 科學計算（PyTorch, TensorFlow）
│   ├─ ✓ 自定義容器（Docker）
│   └─ [ ] 暫時不接受任何工作負載
│
├─ 拒絕的工作負載類型
│   ├─ 例如：挖礦、DDoS 攻擊、非法內容
│   └─ 平台會自動過濾
│
└─ 任務超時配置
    ├─ 最大任務執行時間：12 小時
    ├─ 默認值：24 小時
    └─ 超時後自動中止任務

資源限制配置：
├─ 最大並發任務數
│   ├─ 建議值：auto (自動根據 vRAM 計算)
│   │   └─ 計算：vRAM / 2 / 平均任務大小
│   │   └─ 例如：80GB / 2 / 5GB = 8 個任務
│   │
│   ├─ 或手動設置：1 - 16
│   │   ├─ 1：保證 vRAM 充足，速度最快
│   │   ├─ 8：均衡模式（推薦）
│   │   └─ 16：最大吞吐，但可能 OOM
│   │
│   └─ 說明：
│       ├─ 並發 1：單個任務用 80GB
│       ├─ 並發 2：每個任務用 40GB
│       ├─ 並發 8：每個任務用 10GB
│       └─ 更多並發 = 更低總費用，但速度更慢
│
├─ 保留給個人使用的 vRAM
│   ├─ 默認：0%（100% 用於平台）
│   ├─ 或保留 10 GB（例如）
│   │   └─ 這樣平台只能用 70GB
│   │
│   └─ 允許 0-100%
│
└─ 帶寬限制（可選）
    ├─ 上傳速度限制
    ├─ 下載速度限制
    └─ 防止影響日常網絡使用

可用時間配置：
├─ 可用性時表
│   ├─ 預設選項：
│   │   ├─ ○ 24/7 (全天候)
│   │   ├─ ○ 工作時間外 (下午 6 點 - 上午 9 點)
│   │   ├─ ○ 周末 (週六、日)
│   │   └─ ○ 自定義時間表
│   │
│   └─ 自定義時間 (選擇後)
│       ├─ 周一：9am - 6pm
│       ├─ 周二：9am - 6pm
│       ├─ 周三：9am - 6pm
│       ├─ 周四：9am - 6pm
│       ├─ 周五：9am - 6pm
│       ├─ 周六：24 小時
│       └─ 周日：24 小時
│
├─ 時區設置
│   └─ 自動檢測為：America/Los_Angeles
│
└─ 說明：系統會尊重你的時間表，只在可用時
    分配任務。超過時間會自動轉移到其他 GPU。

價格配置：
├─ 小時租賃價格 (USD)
│   ├─ 市場參考價格：
│   │   ├─ H100：$1.50/小時
│   │   ├─ RTX 4090：$0.30/小時
│   │   ├─ L40S：$0.60/小時
│   │   └─ 等等...
│   │
│   ├─ 輸入框：$___.__ / 小時
│   │
│   └─ 價格建議：
│       ├─ 競爭力價格：吸引更多任務
│       ├─ 高價格：只接受高價值客戶
│       └─ 動態調整：根據市場需求自動調整
│
├─ 按月訂閱模式（可選）
│   ├─ 提供固定的月度租賃方案
│   ├─ 例如：$500/月無限使用
│   └─ 吸引企業級客戶
│
└─ 最小租賃時長
    ├─ 例如：最少租賃 1 小時（不分割成秒級）
    └─ 或允許 1 分鐘的 micro-tasks

性能和穩定性設置：
├─ GPU 功耗限制（可選）
│   ├─ 默認：不限制
│   ├─ 或限制：80% max power
│   └─ 保護 GPU 壽命
│
├─ GPU 溫度警告
│   ├─ 默認閾值：85°C
│   ├─ 超過時自動降頻或停止接單
│   └─ 通知用戶
│
└─ 自動更新設置
    ├─ ✓ 自動更新客戶端
    │   └─ 每周檢查一次，自動安裝
    │
    ├─ ✓ 自動更新 NVIDIA 驅動
    │   └─ 選中後，發現新驅動會提示
    │
    └─ [ ] 禁用自動更新 (手動更新)

社交和推廣設置：
├─ 公開 GPU 信息
│   ├─ ✓ 在全球排行榜上顯示
│   ├─ ✓ 讓其他用戶看到你的 GPU
│   └─ 可增加租賃機會
│
├─ 顯示主機名稱
│   ├─ ✓ 使用真名或昵稱
│   └─ [ ] 匿名 (XXXXX)
│
└─ 推廣代碼
    ├─ 分享：alice.ai/ref/USER123456
    └─ 朋友註冊時享受折扣

驗證和安全：
├─ 設備簽名
│   ├─ 當前設備 UUID：device_550e8400e29b41d4a716446655440000
│   ├─ 簽名：已驗證 ✓
│   └─ 用於防止設備冒充
│
├─ API 密鑰
│   ├─ 當前密鑰：sk_live_abc123...xyz (隱藏)
│   ├─ [重新生成] [顯示] [複製]
│   └─ 用於 API 集成
│
└─ 兩因素認證
    ├─ [ ] 已啟用
    └─ [設置 2FA]

═════════════════════════════════════════════════════

[保存配置並啟動客戶端]

```

---

## 🚀 第三部分：客戶端安裝與啟動

### 3.1 客戶端架構

```
alice-gpu-client 應用架構
═════════════════════════════════════════════════════

┌─────────────────────────────────────┐
│   用戶界面層 (UI)                    │
│   ├─ 系統托盤應用 (macOS/Windows)   │
│   ├─ 終端命令行 (Linux)             │
│   └─ Web Dashboard (可選)           │
│       └─ localhost:9090             │
└──────────────┬──────────────────────┘
               │
┌──────────────▼──────────────────────┐
│   客戶端核心服務 (Go Binary)         │
│   ├─ Connection Manager             │
│   │   ├─ 保持到平台的 TCP 連接     │
│   │   ├─ 自動重連 (指數退避)      │
│   │   └─ WebSocket 心跳            │
│   │                                 │
│   ├─ Task Executor                  │
│   │   ├─ 下載任務代碼和數據        │
│   │   ├─ 創建 Docker 容器          │
│   │   ├─ 執行任務（GPU 隔離）      │
│   │   ├─ 監控進度和資源使用       │
│   │   └─ 上傳結果                  │
│   │                                 │
│   ├─ Resource Monitor               │
│   │   ├─ 監控 GPU 使用情況        │
│   │   ├─ 監控溫度、功耗           │
│   │   ├─ 監控網絡帶寬             │
│   │   └─ 每 1 秒採集一次          │
│   │                                 │
│   ├─ Security Manager               │
│   │   ├─ 驗證任務簽名             │
│   │   ├─ 驗證代碼沙箱             │
│   │   └─ 防止資源逃逸             │
│   │                                 │
│   ├─ Configuration Manager          │
│   │   ├─ 讀取配置文件             │
│   │   ├─ 持久化設置               │
│   │   └─ 動態更新配置             │
│   │                                 │
│   └─ Log Manager                    │
│       ├─ 記錄所有操作             │
│       ├─ 上傳日誌到平台           │
│       └─ 本地保留 7 天            │
└──────────────┬──────────────────────┘
               │
┌──────────────▼──────────────────────┐
│   系統集成層                          │
│   ├─ GPU 驅動 (nvidia-smi)           │
│   ├─ 容器引擎 (Docker)               │
│   ├─ 網絡堆棧 (TCP/IP)               │
│   └─ 文件系統                        │
└─────────────────────────────────────┘

內存架構：
├─ 靜態內存：~50MB (Go binary)
├─ 監控動態內存：~100-200MB
├─ 任務執行時的額外內存：<=500MB
│   (每個並發任務)
│
└─ 總計：<1GB (平台級別)
    (GPU vRAM 由容器隔離管理)

```

### 3.2 客戶端啟動流程

```bash
# 1. 安裝
$ wget https://alice.ai/download/client/v1.0/linux
$ chmod +x alice-gpu-client
$ sudo ./alice-gpu-client install

# 2. 配置（首次執行）
$ alice-gpu-client setup
  ↓ 交互式引導
  ├─ 登錄（郵箱或錢包）
  ├─ 選擇 GPU
  ├─ 設置參數
  └─ 保存配置到 ~/.alice/config.yaml

# 3. 啟動
$ alice-gpu-client start

# 服務啟動日誌：
═════════════════════════════════════════════════════
2026-03-23 10:30:45 [INFO] Alice GPU Client v1.0.0
2026-03-23 10:30:45 [INFO] Reading config from ~/.alice/config.yaml
2026-03-23 10:30:46 [INFO] Detected 3 GPUs:
                          - GPU #0: NVIDIA H100 (80GB) ✓
                          - GPU #1: NVIDIA RTX 4090 (24GB) ✓
                          - GPU #2: NVIDIA L40S (48GB) ✓
2026-03-23 10:30:47 [INFO] Initializing Docker...
2026-03-23 10:30:48 [INFO] Connecting to platform server...
2026-03-23 10:30:49 [INFO] TLS handshake completed ✓
2026-03-23 10:30:49 [INFO] Authenticating...
2026-03-23 10:30:50 [INFO] Authentication successful ✓
2026-03-23 10:30:50 [INFO] Uploading GPU information...
2026-03-23 10:30:51 [INFO] GPU registration successful ✓
2026-03-23 10:30:52 [INFO] Starting heartbeat signal (10s interval)
2026-03-23 10:30:52 [INFO] Listening for tasks...
2026-03-23 10:30:52 [INFO] ★★★ READY TO RECEIVE TASKS ★★★
═════════════════════════════════════════════════════

# 4. 系統托盤應用（可選）
$ alice-gpu-client gui

  ┌──────────────────────────────┐
  │ Alice GPU Provider           │
  ├──────────────────────────────┤
  │ Status: 🟢 ONLINE            │
  │ Connected: Connected         │
  │                              │
  │ GPU Status:                  │
  │  H100: 45% utilization       │
  │  RTX4090: 0% utilization     │
  │  L40S: 62% utilization       │
  │                              │
  │ Tasks: 3 running             │
  │ Today Earnings: $23.45       │
  │                              │
  │ [View Details] [Quit]        │
  └──────────────────────────────┘

# 5. 查看詳細狀態
$ alice-gpu-client status

Current Status:
═════════════════════════════════════════════════════
Device ID: device_550e8400e29b41d4a716446655440000
Status: ONLINE ✓
Connected: Yes
Uptime: 2 hours 15 minutes

GPUs:
┌─────────────────────────────────────────┐
│ GPU #0: H100 (80GB)                     │
├─────────────────────────────────────────┤
│ Utilization: 45%                        │
│ Temperature: 62°C                       │
│ Power: 250W / 700W (36%)                │
│ vRAM: 64GB / 80GB (80%)                 │
│ Active Tasks: 2 / 8                     │
│ Price: $1.50/hour                       │
└─────────────────────────────────────────┘

Earnings:
├─ Today: $23.45
├─ This Week: $156.78
├─ This Month: $645.32
└─ Total: $4,532.10

Tasks Queue:
├─ Running: 3
│   ├─ Task #1001: LLM inference (32% done)
│   ├─ Task #1002: Image generation (78% done)
│   └─ Task #1003: Data processing (5% done)
│
└─ Pending: 2
    ├─ Task #1004: Queued
    └─ Task #1005: Queued

═════════════════════════════════════════════════════

```

---

## 💰 第四部分：GPU 算力分享與動態定價

### 4.1 算力市場與定價模型

```
GPU 算力分享市場
═════════════════════════════════════════════════════

定價基礎：

基於以下因素計算基准價格：
├─ GPU 型號（計算能力）
│   ├─ H100: 756 TFLOPS → $1.50/h
│   ├─ A100: 312 TFLOPS → $0.80/h
│   ├─ RTX 4090: 1456 TFLOPS → $0.30/h
│   │   (雖然 TFLOPS 更高，但業界普遍認為 RTX 系列便宜)
│   └─ RTX 4070: 546 TFLOPS → $0.15/h
│
├─ 地理位置（網絡延遲）
│   ├─ 美國西部 (矽谷): +0% (基准)
│   ├─ 美國東部: +5%
│   ├─ 歐洲: +10%
│   ├─ 亞洲: +15%
│   └─ 其他: +20%
│
├─ 供需關係
│   ├─ 供應充足 (>1000 GPU): -10%
│   ├─ 供應緊張 (100-1000 GPU): +0%
│   ├─ 供應稀缺 (<100 GPU): +20%
│   └─ 實時調整，每小時更新一次
│
└─ 信譽係數
    ├─ 信譽評分 > 90: -5%
    ├─ 信譽評分 70-90: +0%
    └─ 信譽評分 < 70: +10% (風險溢價)

定價公式：
─────────────────────────────────────────
基础价格 = [GPU基准价格] × [地理位置系数]
          × [供需系数] × [信誉系数]

例子：
├─ H100 在矽谷, 供應充足, 信譽 92
│   = $1.50 × 1.0 × 0.9 × 0.95 = $1.28/h
│
├─ RTX 4090 在歐洲, 供應緊張, 信譽 75
│   = $0.30 × 1.10 × 1.0 × 1.10 = $0.36/h
│
└─ L40S 在亞洲, 供應稀缺, 信譽 88
    = $0.60 × 1.15 × 1.20 × 0.95 = $0.79/h

市場實時信息面板：
═════════════════════════════════════════════════════
                                    排名  供應  平均價格
GPU 型號              基础价格      改變  (個)  (range)
────────────────────────────────────────────────────
H100 80GB             $1.50         ↑2%   342   $1.35-$1.68
A100 80GB             $0.80         ↓1%   156   $0.74-$0.92
RTX 4090              $0.30         →0%   1248  $0.26-$0.35
L40S 48GB             $0.60         ↑5%   89    $0.58-$0.71
RTX 4080 Super        $0.25         ↓2%   567   $0.22-$0.28
────────────────────────────────────────────────────

✓ 你的 H100 當前建議價格：$1.32/h (基于市場)
  用户设定: $1.50/h (高于市場 14%)
  → 可能降低租賃率，考慮降價

```

### 4.2 動態定價系統

```
動態定價算法 (自動調整)
═════════════════════════════════════════════════════

時間維度：

Peak Hours (09:00-18:00 US Eastern):
├─ 需求: 高
├─ 供應: 相對不足
├─ 價格調整: +15%
└─ 例如: $1.50 × 1.15 = $1.73/h

Off-Peak Hours (18:00-09:00):
├─ 需求: 低
├─ 供應: 充足
├─ 價格調整: -10%
└─ 例如: $1.50 × 0.90 = $1.35/h

Weekend:
├─ 需求: 中等
├─ 價格調整: -5%
└─ 例如: $1.50 × 0.95 = $1.43/h

可靠性維度：

高信譽 GPU (評分 > 90):
├─ 優勢: 任務成功率高
├─ 價格調整: -5% (競爭優勢)
├─ 可獲得長期客戶
└─ 推薦價格: $1.43/h

中信譽 GPU (評分 70-90):
├─ 中等可靠性
├─ 價格調整: +0%
└─ 基础价格: $1.50/h

低信譽 GPU (評分 < 70):
├─ 較高故障率
├─ 價格調整: +15% (風險溢價)
└─ 但仍有客戶願意用於非關鍵任務
    └─ 例如: $1.50 × 1.15 = $1.73/h

GPU 配置維度：

獨佔使用 (並發 1):
├─ 優勢: 性能最好
├─ 價格調整: +20%
├─ 適合需要高效能的任務
└─ 例如: $1.50 × 1.20 = $1.80/h

共享使用 (並發 8):
├─ 優勢: 成本低
├─ 價格調整: -10%
├─ 適合批量、非實時任務
└─ 例如: $1.50 × 0.90 = $1.35/h

實時價格競爭引擎：
─────────────────────────────────────────
1. 監測區域 GPU 平均價格
   ├─ 美國西部 H100: $1.32/h (平均)
   └─ 你的 H100: $1.50/h (高于平均 14%)

2. 分析你的 GPU 相對優勢
   ├─ 信譽評分: 92/100 (前 15%)
   ├─ 正常運行時間: 99.8%
   └─ 平均完成時間: 95% of estimated

3. 建議調整
   ├─ 若要吸引更多訂單: 降至 $1.35/h
   ├─ 若要優化收益: 保持 $1.42/h
   └─ 若只想接高價任務: 維持 $1.50/h

4. 自動調整（可選啟用）
   ├─ 智能模式：根據需求自動調整 ±15%
   ├─ 目標：維持 70% 利用率
   ├─ 例如：
   │   - 利用率 < 50% → 自動降價 5%
   │   - 利用率 > 90% → 自動漲價 5%
   │   - 每 15 分鐘評估一次
   │
   └─ 用戶可設置價格上限/下限
       ├─ Min: $1.00/h
       └─ Max: $2.00/h

```

### 4.3 賺取收入追蹤

```
收入儀表板
═════════════════════════════════════════════════════

                    今日    本週    本月    總計
──────────────────────────────────────────────────
實際收入            $23.45  $156.78 $645.32 $4,532.10
平台費用 (15%)      -$3.52  -$23.52 -$96.80 -$680.00
淨收入              $19.93  $133.26 $548.52 $3,852.10

任務統計
───────────────────────────────────────────────────
已完成任務          5       31      129     892
平均執行時間        42min   56min   48min   45min
成功率              100%    99.7%   99.2%   98.9%
平均評分            4.9/5   4.8/5   4.7/5   4.6/5

小時利用率
───────────────────────────────────────────────────
H100:               45%     58%     62%     65%
RTX 4090:           0%      12%     15%     18%
L40S:               62%     71%     68%     60%

平均小時費率        $1.45   $1.42   $1.39   $1.35

收益預測
───────────────────────────────────────────────────
基于當前速度：
├─ 按月預計: $645 × 3 = $1,935/month
├─ 按年預計: $1,935 × 12 = $23,220/year
│
└─ 若提升 50% 利用率:
   ├─ 按月預計: $2,902/month
   └─ 按年預計: $34,830/year

收益提升建議
───────────────────────────────────────────────────
✓ 降低 H100 價格 10% (從 $1.50 → $1.35)
  → 預計增加租賃機會 +30%
  → 預計月增收入：$194

✓ 改進 RTX 4090 配置 (目前 0% 利用率)
  → 考慮降價至 $0.25/h
  → 預計利用率: +30% (0% → 30%)
  → 預計月增收入：$54

✓ 升級 L40S 的評分
  → 當前評分: 4.7/5 (需改進)
  → 目標: 4.9/5
  → 可獲得 +10% 價格溢價
  → 預計月增收入：$32

✓ 提升可用時間 (目前 16/24)
  → 改為 20/24 (24/7 + 降價 5%)
  → 預計月增收入：$128

───────────────────────────────────────────────────
總預計月增收入: $408 (基于優化)
```

---

## 🔄 第五部分：GPU 調度與分派系統

### 5.1 任務分配的核心算法

```python
"""
GPU 調度與分派算法
核心: 在眾多可用 GPU 中選擇最優的來執行任務
"""

class GPUScheduler:
    def select_best_gpu(self, task, available_gpus):
        """
        為任務選擇最佳 GPU

        參數：
        - task: 任務對象 (包含需求: vRAM, timeout, budget, etc)
        - available_gpus: 可用 GPU 列表

        返回：
        - best_gpu: 最優 GPU
        - backup_gpus: 備選 GPU 列表 [2, 3]
        """

        # 第 1 步：硬性條件過濾
        candidates = []
        for gpu in available_gpus:
            # 條件 1: vRAM 足夠
            if gpu.available_vram < task.required_vram:
                continue

            # 條件 2: GPU 狀態正常
            if gpu.status not in ['ONLINE', 'IDLE']:
                continue

            # 條件 3: 信譽評分達標
            if gpu.reputation_score < 0.7:  # 70 分以上
                continue

            # 條件 4: 可以按時完成
            if gpu.max_concurrent_tasks >= 10:  # 已滿載
                continue

            # 通過所有硬性條件
            candidates.append(gpu)

        if not candidates:
            return None, []  # 沒有合適的 GPU

        # 第 2 步：軟性條件計分
        scored_list = []
        for gpu in candidates:
            score = self._calculate_suitability_score(gpu, task)
            scored_list.append((gpu, score))

        # 按分數降序排列
        scored_list.sort(key=lambda x: x[1], reverse=True)

        # 第 3 步：返回最優和備選
        best_gpu = scored_list[0][0]
        backup_gpus = [gpu for gpu, _ in scored_list[1:4]]

        return best_gpu, backup_gpus

    def _calculate_suitability_score(self, gpu, task):
        """
        計算 GPU 對任務的適合度評分 (0-100)
        """
        weights = {
            'latency': 0.25,       # 網絡延遲
            'availability': 0.25,  # 當前可用性
            'reputation': 0.20,    # 信譽評分
            'price': 0.20,         # 價格競爭力
            'speed': 0.10,         # GPU 計算速度
        }

        # ──────────────────────────────────────
        # 計分 1：延遲 (0-100)
        # ──────────────────────────────────────
        latency = self._calculate_network_latency(
            gpu.location, task.preferred_location
        )

        if latency < 50:      # < 50ms 最優
            latency_score = 100
        elif latency > 500:   # > 500ms 差
            latency_score = 0
        else:
            latency_score = 100 * (1 - latency / 500)

        # ──────────────────────────────────────
        # 計分 2：可用性 (0-100)
        # ──────────────────────────────────────
        current_load = gpu.active_tasks_count / gpu.max_concurrent_tasks
        availability_score = (1 - current_load) * 100
        # 例如: 2/8 任務在運行 → 75% 可用 → 75 分

        # ──────────────────────────────────────
        # 計分 3：信譽 (0-100)
        # ──────────────────────────────────────
        reputation_score = gpu.reputation_score * 100
        # GPU 信譽 0.92 → 92 分

        # ──────────────────────────────────────
        # 計分 4：價格 (0-100)
        # ──────────────────────────────────────
        if gpu.hourly_price <= task.budget_per_hour:
            # 在預算內，價格越低越好
            market_avg_price = self._get_market_average_price(gpu.type)
            price_score = 100 * (1 - gpu.hourly_price / market_avg_price)
            price_score = min(100, max(0, price_score))
        else:
            # 超過預算，根據超額程度扣分
            excess = gpu.hourly_price - task.budget_per_hour
            price_score = max(0, 100 - (excess / task.budget_per_hour) * 100)

        # ──────────────────────────────────────
        # 計分 5：速度 (0-100)
        # ──────────────────────────────────────
        max_tflops = max([g.tflops for g in available_gpus])
        speed_score = (gpu.tflops / max_tflops) * 100

        # ──────────────────────────────────────
        # 加權平均
        # ──────────────────────────────────────
        total_score = (
            weights['latency'] * latency_score +
            weights['availability'] * availability_score +
            weights['reputation'] * reputation_score +
            weights['price'] * price_score +
            weights['speed'] * speed_score
        )

        return total_score

    def _calculate_network_latency(self, gpu_location, task_location):
        """
        計算 GPU 位置到客戶位置的網絡延遲 (毫秒)
        使用大圓距離 + 經驗公式
        """
        # 這裡簡化，實際應使用 GeoIP + 實測延遲數據
        from math import radians, cos, sin, asin, sqrt

        lat1, lon1 = gpu_location
        lat2, lon2 = task_location

        # Haversine 公式計算距離
        lon1, lat1, lon2, lat2 = map(radians, [lon1, lat1, lon2, lat2])
        dlon = lon2 - lon1
        dlat = lat2 - lat1
        a = sin(dlat/2)**2 + cos(lat1) * cos(lat2) * sin(dlon/2)**2
        c = 2 * asin(sqrt(a))
        km = 6371 * c

        # 經驗公式: 延遲 ≈ 距離 / 速度
        # 光速 ~= 30万 km/s，但實際延遲更高
        # 經驗: 1000km ≈ 50ms
        latency_ms = (km / 1000) * 50

        return latency_ms

```

### 5.2 任務分派的時序圖

```
任務分派的完整時序
═════════════════════════════════════════════════════

客戶端                   平台                    GPU 客戶端
  │                       │                         │
  ├─ 提交任務 ────────────→ │                        │
  │  (代碼+數據)          │                         │
  │                       ├─ 驗證任務                │
  │                       │  ├─ 檢查簽名 ✓          │
  │                       │  ├─ 檢查預算 ✓          │
  │                       │  ├─ 檢查數據安全 ✓      │
  │                       │  └─ 存儲到 PostgreSQL  │
  │                       │                        │
  │                       ├─ 執行匹配算法           │
  │                       │  ├─ 掃描可用 GPU       │
  │                       │  ├─ 過濾 (vRAM, etc)   │
  │                       │  ├─ 計分              │
  │                       │  └─ 選出最優 GPU       │
  │                       │                        │
  │                       ├─ 發送分配請求 ────────→ │
  │                       │  (分配 request)        │
  │                       │                        │
  │                       │                   ├─ 收到分配
  │                       │                   ├─ 校驗簽名
  │                       │                   ├─ 檢查資源
  │                       │                   │
  │                       │            (接受) │
  │                       │ ←─────────────────┤
  │                       │  (確認接受)        │
  │                       │                   ├─ 下載任務
  │                       ├─ 狀態: ASSIGNED   │  (代碼+數據)
  │                       │                   │
  │                       │                   ├─ 創建容器
  │                       │                   ├─ 配置 GPU
  │                       │                   └─ 開始執行
  │                       │                        │
  │ ← [任務已分配] ←──────┤                        │
  │   (立即返回給客戶)    │                        │
  │                       │                        │
  │                       │ ←─ 進度上報 (每 1s) ───┤
  │                       │  (進度 %，資源使用)     │
  │                       │                        │
  │  (客戶可輪詢)         │                        │
  ├─ 查詢進度 ────────────→ │                        │
  │                       ├─ 查詢 Redis 快取        │
  │ ← [進度: 45%] ────────┤                        │
  │                       │                        │
  │                       │ ←─ 進度上報 ─────────→ │
  │                       │                        │
  │  (假設進度停滯)       │                        │
  │  (3 分鐘無更新)       │                        │
  │                       ├─ 異常檢測觸發           │
  │                       │  ├─ 發送 HEALTHCHECK   │
  │                       │  ├─ 等待 5s 回應       │
  │                       │  └─ 無回應 → 故障 !    │
  │                       │                        │
  │                       ├─ 啟動故障轉移           │
  │                       │  ├─ 標記 GPU OFFLINE   │
  │                       │  ├─ 下載檢查點 ✓       │
  │                       │  ├─ 選擇新 GPU         │
  │                       │  └─ 從檢查點恢復       │
  │                       │                        │
  │                       ├─ 發送轉移請求 ──────→  │ (新 GPU)
  │                       │                        │
  │                       │                   ├─ 加載檢查點
  │                       │                   ├─ 繼續執行
  │                       │                   └─ 進度 45% → 100%
  │                       │                        │
  │                       │ ←─ 結果上報 ────────────┤
  │                       │  (最終結果, 執行時間)   │
  │                       │                        │
  │                       ├─ 結果驗證               │
  │                       │  ├─ 校驗簽名 ✓         │
  │                       │  ├─ 驗證格式 ✓         │
  │                       │  ├─ 隨機驗證 (10%)     │
  │                       │  └─ 結果 ✓ 有效        │
  │                       │                        │
  │                       ├─ 計算費用并結算         │
  │                       │  ├─ 費用 = $5.50       │
  │                       │  ├─ 手續費 = $0.825    │
  │                       │  ├─ GPU 收入 = $4.675  │
  │                       │  └─ 批處理上鏈         │
  │                       │                        │
  │ ← [結果準備好] ←──────┤                        │
  │   (可下載)            │                        │
  │                       │                    ├─ 錢包收到 $4.675
  │                       │                    └─ 更新收入統計
  │                       │                        │
  ├─ 下載結果 ────────────→ │                        │
  │                       ├─ 檢查授權              │
  │ ← [result.zip] ←──────┤ (從 S3)                │
  │                       │                        │
  └─ (完成) ────────────→ └─ (完成) ──────────────→ └

流程亮點：
├─ 端到端簽名驗證 (防止篡改)
├─ 自動故障轉移 (99.99% 成功率)
├─ 實時進度監控 (1 秒間隔)
├─ 區塊鏈支付 (無需信任)
└─ 檢查點恢復 (可恢復任務)

```

---

## ✅ 總結：GPU 管理系統的核心價值

```
為什麼這個系統是平台的基礎：

1️⃣ 供應端 (GPU 提供者)
   ├─ 簡單上線：15-30 分鐘自動完成
   ├─ 被動收益：無需主動管理
   ├─ 透明計費：實時看到收入
   ├─ 自動支付：每小時自動入帳
   └─ 全球市場：訪問全球客戶

2️⃣ 平台端
   ├─ 完整的 GPU 資源池：>10,000 個 GPU
   ├─ 實時資源調度：智能匹配任務到最優 GPU
   ├─ 高可用性：故障轉移、冗余備份
   ├─ 成本優化：動態定價、利用率最大化
   └─ 可擴展性：輕鬆支持 100 倍增長

3️⃣ 需求端 (客戶)
   ├─ 廉價算力：比雲廠商便宜 30-50%
   ├─ 全球分佈：選擇最近的 GPU (低延遲)
   ├─ 高可靠性：99.99% SLA
   ├─ 透明定價：實時報價，無隱藏費用
   └─ 簡單使用：API 或 Web UI

實現這個系統後，你將擁有：
┌─────────────────────────────────────────┐
│ ✓ 去中心化計算網絡                      │
│ ✓ 自動化資源分配引擎                    │
│ ✓ 區塊鏈支付基礎設施                    │
│ ✓ 全球 GPU 資源市場                     │
│                                        │
│ = 真正的分散式雲計算平台                │
└─────────────────────────────────────────┘
```

