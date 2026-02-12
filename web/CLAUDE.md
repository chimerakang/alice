# Web Assets 目錄

這個目錄包含靜態網頁資源和編譯後的前端檔案。

## 目錄結構

- **index.html**: 主要的 HTML 入口點
- **assets/**: Vite 編譯後的 JavaScript 和 CSS 檔案
  - 檔案名包含 hash 確保快取失效
  - CSS 檔案包含 Tailwind 編譯後的樣式
  - JS 檔案包含 React 應用程式和依賴項

## 編譯流程

前端 React 應用程式透過 Vite 編譯後放置在此目錄：
1. `frontend/` 目錄包含原始碼
2. `vite build` 編譯並輸出到 `web/` 目錄
3. Go HTTP 伺服器提供這些靜態檔案

## 注意事項

- 此目錄內的檔案是自動生成的，不應手動編輯
- 編譯時會自動清理並重新生成
- 檔案名的 hash 值會在每次編譯時改變

<claude-mem-context>

</claude-mem-context>