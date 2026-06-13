# mail-forwarder

[English](README.en.md) | [日本語](README.ja.md) | [한국어](README.ko.md)

`mail-forwarder` 是一个轻量的 IMAP 到 HTTP Webhook 转发服务。它连接一个
或多个邮箱文件夹，使用 IMAP IDLE 长连接监听未读邮件，把邮件正文、HTML、头信
息和附件元数据整理成 JSON，然后发送到你配置的 Webhook。

它不做发件人过滤、关键词判断、HTML 解析或业务路由。下游 HTTP Agent 负责这些
业务逻辑。

## 适用场景

- 你的系统只会接 HTTP Webhook，但邮件是事件入口。
- 你不想依赖公网邮件转发 SaaS。
- 你希望在本机、服务器或 Docker 中长期监听邮箱。
- 你希望邮件成功送达下游后，才把邮件标记为已读。

## 功能

- 支持多个 IMAP 邮箱来源
- 每个来源配置一个 Webhook 地址
- 使用 IMAP IDLE 长连接监听
- 显式处理模式，避免默认误清空未读邮件
- 本地状态文件记录 checkpoint 和失败重试冷却
- Webhook 返回 2xx 后才标记邮件已读
- 可选本地只读管理 API
- 支持 macOS、Linux、Windows 二进制和 Docker

## 快速开始

生成配置模板：

```bash
mail-forwarder init-config
```

编辑 `config.yaml`，填入真实 IMAP 账号和 Webhook 地址，然后运行：

```bash
mail-forwarder -config config.yaml
```

模板配置默认使用占位账号，并且 `dry_run: true`。正式使用前需要改成真实配置。

## Docker

```bash
docker compose pull
docker compose up -d
```

镜像地址：

```text
ghcr.io/yunyingk/mail-forwarder:v0.2.2
crpi-yg3r8e9fvlik3jk0.cn-hangzhou.personal.cr.aliyuncs.com/yingqing/mail-forwarder:v0.2.2
```

## 配置示例

```yaml
processing_mode: checkpoint_from_now
state:
  path: ./mail-forwarder-state.json
imap:
  - name: example-inbox
    host: imap.example.com
    port: 993
    secure: true
    user: user@example.com
    pass: ${IMAP_PASS}
    mailbox: INBOX
    webhook:
      url: http://127.0.0.1:3000/mail-ingress
dry_run: true
```

推荐在邮箱服务商里创建独立文件夹，并用邮箱规则把目标邮件移动到该文件夹。
`mail-forwarder` 只扫描你配置的 `mailbox`，不会扫描整个邮箱账号。

## 处理模式

`processing_mode` 是必填项：

- `unread_queue`：把未读状态当作队列，历史未读和新未读都会发送。手动改回未读会再次发送。
- `new_unread_queue`：启动前已有未读跳过，启动后的新未读会发送。重启后重新计算启动基线。
- `checkpoint_from_now`：首次启动跳过已有未读，只处理之后的新邮件，并持久化 checkpoint。
- `checkpoint_from_unread`：首次启动处理已有未读，之后通过 checkpoint 避免成功邮件重复发送。

详细语义见 [docs/processing-modes.md](docs/processing-modes.md)。

## 发送语义

- Webhook 返回 HTTP 2xx 后，才把邮件标记为已读。
- Webhook 失败时，邮件保持未读。
- 失败邮件会进入本地冷却，不会在每次扫描时立刻重复请求。
- checkpoint 模式会记录已成功处理的 UID。
- `unread_queue` 类模式允许用户手动改回未读后再次发送。

## OpenAPI

OpenAPI 同时描述两个接口面：

- `/mail-ingress`：下游 Agent 需要实现的 Webhook 请求格式。
- `/healthz`、`/sources`、`/config`、`/openapi`、`/openapi.json`：本地管理 API。

启用管理 API 后，Postman、Swagger 或 AI Agent 可以通过 URL 导入：

```text
http://127.0.0.1:6245/openapi.json
```

`/mail-ingress` 不是本程序提供的服务端接口，它描述的是本程序向外发送的请求体。

## 构建

```bash
go test ./...
go build -ldflags="-s -w -X main.version=$(git describe --tags --always)" -o mail-forwarder .
```

发布工作流会构建 Linux、macOS、Windows 二进制，并推送 GHCR 和阿里云 ACR 镜像。
