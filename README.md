# mail-forwarder

监听 IMAP 未读邮件，按条件转发到钉钉群机器人。Go 重写版，支持多邮箱源、多钉钉机器人、条件路由。

## 功能

- 支持多个 IMAP 邮箱源同时监听
- 支持多个钉钉机器人，按条件路由
- IMAP IDLE 实时推送 + 启动时轮询
- 发送失败保持未读，下次重试
- 结构化 JSON 日志
- 跨平台二进制：Linux / macOS / Windows
- Docker 部署

## 安装

### 二进制

从 [Releases](../../releases) 下载对应平台的二进制文件。

### Docker

```bash
docker compose pull
docker compose up -d
```

### 本地编译

```bash
go build -ldflags="-s -w -X main.version=$(git describe --tags --always)" -o mail-forwarder .
```

## 配置

```bash
cp config.example.yaml config.yaml
```

编辑 `config.yaml`，配置 IMAP 源和钉钉机器人：

```yaml
imap:
  - name: hesi-mailbox
    host: imap.exmail.qq.com
    port: 993
    secure: true
    user: alert@example.com
    pass: your_password
    mailbox: INBOX
    filter:
      from: email@service.ekuaibao.biz
      subject_keyword: "合思工作流失败提醒"

dingtalk:
  - name: hesi-robot
    webhook: https://oapi.dingtalk.com/robot/send?access_token=xxx
    secret: ""
    title: "合思工作流失败提醒"

dry_run: false
max_text_length: 3200
```

### 环境变量引用

配置文件中可用 `${VAR_NAME}` 引用环境变量：

```yaml
pass: ${IMAP_PASS}
```

### 路由逻辑

编辑 `router/router.go` 中的 `Route` 函数来自定义路由逻辑：

```go
func Route(mail mailer.Mail, availableTargets []string) Result {
    // 按 from/subject/内容 判断发给哪些机器人
    return Result{Targets: availableTargets}
}
```

## 运行

```bash
# 直接运行
./mail-forwarder -config config.yaml

# Dry run（不发钉钉，不标记已读）
# 在 config.yaml 中设置 dry_run: true

# Docker
docker compose up -d
docker logs -f mail-forwarder
```

## 项目结构

```
main.go              # 入口，组装 + 优雅退出
config/              # YAML 配置加载
mailer/              # IMAP 连接、idle、邮件获取
dingtalk/            # 钉钉 webhook 签名 + 发送
router/              # ★ 路由逻辑（改这个文件）
config.example.yaml  # 配置模板
```

## GitHub Actions

推送到 `main` 或打 tag 时自动构建：

- **tag (`v*`)**：构建三平台二进制 + Docker 镜像，创建 GitHub Release
- **main**：构建 Docker 镜像

产物：

| 平台 | 文件名 |
|------|--------|
| Linux amd64 | `mail-forwarder-linux-amd64` |
| macOS amd64 | `mail-forwarder-darwin-amd64` |
| macOS arm64 | `mail-forwarder-darwin-arm64` |
| Windows amd64 | `mail-forwarder-windows-amd64.exe` |

## 从 Node.js 版迁移

v0.2.0 是 Go 重写版，主要变化：

- 配置从 `.env` 改为 `config.yaml`（支持多源、多目标）
- 二进制分发，不再需要 Node 运行时
- 内存占用从 ~20MB 降至 ~2-3MB
- 结构化 JSON 日志
