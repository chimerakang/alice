FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -o claude-tg-agent .

FROM alpine:3.21

RUN apk add --no-cache \
    bash \
    git \
    curl \
    ripgrep \
    findutils \
    openssh-client \
    nodejs \
    npm \
    python3

# 安裝 Claude Code CLI
RUN npm install -g @anthropic-ai/claude-code

# 建立非 root 用戶（Claude CLI 禁止 root 使用 --dangerously-skip-permissions）
RUN addgroup -S claude && adduser -S claude -G claude

WORKDIR /app
COPY --from=builder /app/claude-tg-agent .

# 專案目錄掛載點
VOLUME ["/project"]

ENV HOME=/home/claude

# Claude CLI 認證目錄
RUN mkdir -p /home/claude/.claude && chown -R claude:claude /home/claude

USER claude

ENTRYPOINT ["./claude-tg-agent"]
