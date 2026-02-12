# 圖片訊息支援測試

## 測試目標
驗證 Telegram Bot 能正確處理圖片訊息並將其傳送給 Claude 進行分析。

## 測試用例

### 1. 基礎圖片訊息
- 傳送單張圖片（無文字說明）
- 預期：Bot 下載圖片並傳送給 Claude 分析

### 2. 圖片 + 文字說明
- 傳送圖片 + caption 文字
- 預期：Bot 結合圖片和用戶說明一起分析

### 3. 檔案大小限制
- 傳送超過 20MB 的圖片
- 預期：Bot 回覆檔案過大錯誤訊息

### 4. 功能關閉測試
- 設定 `enable_photo_support: false`
- 預期：Bot 回覆功能未啟用訊息

## 實現的功能
- ✅ Telegram Update 解析 Photo 和 Caption 欄位
- ✅ 圖片檔案下載到臨時目錄
- ✅ 組合 prompt 讓 Claude Read tool 讀取圖片
- ✅ 臨時檔案使用後自動清理
- ✅ 檔案大小和功能開關驗證
- ✅ PII 檢測對 caption 文字的支援
- ✅ 安全事件記錄

## 架構
```
用戶發送圖片 → TG Bot 解析 Photo → downloadFile() → 臨時檔案 → Agent.Run() → Claude 分析 → 清理檔案
```