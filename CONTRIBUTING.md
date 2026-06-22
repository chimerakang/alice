# Contributing to Alice Monitor

Thanks for helping improve Alice Monitor. This project is focused on local-first AI agent observability, maintainer automation, and privacy-conscious security workflows for open-source repositories.

## How to Contribute

1. Open an issue for larger changes before starting implementation.
2. Keep pull requests focused on one behavior or workflow.
3. Include tests or a clear manual verification plan when changing behavior.
4. Update docs when a command, API, configuration field, or workflow changes.
5. Avoid committing secrets, local databases, screenshots with private data, or generated artifacts that are not required.

## Development Setup

```bash
go test ./...
cd frontend
npm install
npm run lint
npm run build
```

Run the commands that match the area you changed. If a command cannot run in your environment, explain why in the pull request.

## Pull Request Checklist

- [ ] The change is scoped and explained.
- [ ] Tests or manual verification steps are included.
- [ ] User-facing text is available in English and Traditional Chinese when applicable.
- [ ] New security-sensitive behavior is documented.
- [ ] No secrets, private logs, or local-only config files are included.

## 開源貢獻說明

感謝你協助改進 Alice Monitor。本專案聚焦於本地優先的 AI agent 可觀測性、開源維護者自動化，以及重視隱私的安全工作流。

### 貢獻方式

1. 較大的變更請先開 issue 討論。
2. Pull request 請聚焦在單一行為或工作流。
3. 行為變更請附上測試或清楚的手動驗證步驟。
4. 若新增指令、API、設定欄位或工作流，請同步更新文件。
5. 請勿提交 secrets、本機資料庫、含私人資訊的截圖，或不必要的產物。

### PR 檢查清單

- [ ] 變更範圍清楚且有說明。
- [ ] 已附測試或手動驗證步驟。
- [ ] 使用者可見文字在適用時提供英文與繁體中文。
- [ ] 新增的安全敏感行為已文件化。
- [ ] 未包含 secrets、私人 log 或本機專用設定。
