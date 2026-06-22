# Security Policy

Alice Monitor handles agent logs, prompts, tool events, project paths, and optional PII detection records. Please treat security reports with care and avoid posting sensitive details in public issues.

## Reporting a Vulnerability

If you find a vulnerability, please open a private security advisory on GitHub when available, or contact the maintainer privately before publishing details. Include:

- A short description of the issue.
- Steps to reproduce.
- The affected version, commit, or configuration.
- Any impact on secrets, prompts, logs, local files, or user data.
- Suggested fixes or mitigations, if you have them.

Please do not include real tokens, credentials, private prompts, or personal data in the report. Use redacted examples whenever possible.

## Security Scope

Security-sensitive areas include:

- Secret and PII detection.
- Local database storage and retention.
- Telegram bot authorization and allowlists.
- Web dashboard authentication boundaries.
- Shell command execution and project directory access.
- Codex, Claude Code, and other CLI agent integrations.

## 安全政策

Alice Monitor 可能處理 agent logs、prompts、工具事件、專案路徑與 PII 偵測紀錄。若你發現安全問題，請避免直接在公開 issue 暴露可被濫用的細節。

### 回報漏洞

請優先使用 GitHub private security advisory，或先私下聯絡維護者。回報內容建議包含：

- 問題簡述。
- 重現步驟。
- 受影響的版本、commit 或設定。
- 對 secrets、prompts、logs、本機檔案或使用者資料的影響。
- 若你已知道修補方向，也歡迎附上建議。

請不要在回報中放入真實 token、credential、私人 prompt 或個資；能遮蔽就盡量遮蔽。
