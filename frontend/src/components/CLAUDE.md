# Components 元件庫

這個目錄包含可重用的 React 元件。

## 元件清單

- **DateRangeFilter.tsx**: 時間範圍篩選元件，支援歷史資料查詢
  - 支援預設時間範圍 (今天/昨天/本週/上週/本月/上月)
  - 支援自訂時間範圍選擇
  - 與 API 時間範圍查詢端點整合

## 設計原則

- 元件應該是純函數式元件 (functional components)
- 使用 TypeScript 確保型別安全
- 採用 Tailwind CSS 進行樣式設計
- 遵循統一的設計語言和配色方案