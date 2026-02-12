# Web Assets 編譯檔案

這個目錄包含 Vite 編譯後的前端資源檔案。

## 當前檔案

- **index-B9TV7aqA.css**: 編譯後的 CSS 檔案，包含 Tailwind CSS 和自訂樣式
- **index-CYzL3SIB.js**: 編譯後的 JavaScript 檔案，包含 React 應用程式和所有依賴項

## 檔案命名規則

- 檔案名包含內容雜湊值 (如 `B9TV7aqA`)，確保瀏覽器快取正確失效
- 每次編譯時，如果內容有變更，雜湊值會改變
- 雜湊值確保部署後瀏覽器載入最新版本

## 自動管理

- 這些檔案由 Vite 自動生成和管理
- 不應手動編輯或修改
- `index.html` 會自動引用正確的雜湊檔名