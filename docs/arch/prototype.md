# Prototype Feature Architecture

Issue #92 adds `/prototype` — a Telegram-native UI prototyping flow using Claude Code CLI for HTML generation and chromedp for server-side screenshots.

## Core Types (`internal/app/prototype.go`)

```
PrototypeManager
  ├── client    Client          // Claude Code CLI wrapper
  ├── outputDir string          // HTML + export file storage
  └── storage   Storage         // SQLite backend

Prototype
  ├── ID / ChatID / MessageID
  ├── OriginalPrompt / CurrentHTML
  ├── EditHistory []EditRecord   // append-only, drives versioning
  └── Version int                // increments on every Edit()

EditRecord { Prompt string; Timestamp time.Time }
```

## CLI Prompt Strategy

**Generate** (`generatePromptTmpl`): instructs Claude to output a complete single-page HTML using Tailwind CSS CDN, 1280 px fixed body, Traditional Chinese text, and placeholder images. Response is parsed by `extractHTML()` which tries ```` ```html ``` ```` block first, then bare `<!DOCTYPE…</html>`, then falls back to full text.

**Edit** (`editPromptTmpl`): passes the current HTML inline with the user's change request. Claude returns the full updated HTML; `extractHTML()` parses it the same way.

## Screenshot (`Screenshot` method)

Dependency: `github.com/chromedp/chromedp` v0.14.2 (already in `go.mod`).

Flow:
1. Write HTML to `os.CreateTemp` file.
2. `chromedp.NewExecAllocator` with `WindowSize(1280, 900)`, headless, no-sandbox.
3. Navigate to `file://` URL → `chromedp.FullScreenshot` at 90 % quality.
4. Return PNG bytes; caller sends as Telegram photo.

## Database Schema (`internal/app/storage.go`)

```sql
CREATE TABLE IF NOT EXISTS prototypes (
    id              TEXT PRIMARY KEY,
    chat_id         INTEGER NOT NULL,
    original_prompt TEXT NOT NULL,
    current_html    TEXT NOT NULL,
    edit_history    TEXT,          -- JSON []EditRecord
    version         INTEGER DEFAULT 1,
    message_id      INTEGER,       -- TG message ID for reply detection
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_prototypes_chat_id    ON prototypes(chat_id);
CREATE INDEX idx_prototypes_message_id ON prototypes(message_id);
```

Storage interface adds five methods: `CreatePrototype`, `GetPrototype`, `UpdatePrototype`, `ListPrototypesByChat`, `GetPrototypeByMessageID`.

## Telegram Integration (`internal/app/telegram.go`)

Commands: `/prototype`, `/prototype-edit`, `/prototype-list`, `/prototype-export`.

Inline Keyboard: main menu (🎨 配色 / 📐 佈局 / ✏️ 文字 / ➕ 加元素 / 📤 匯出 / 🔄 重做) → sub-menus expand per category → callback data format `proto:<action>:<protoID>`.

Reply detection: `handleMessage` receives `replyToMessageID int`; if the replied-to message is a prototype screenshot (`GetPrototypeByMessageID`), the text is routed to `handlePrototypeReplyEdit`.
