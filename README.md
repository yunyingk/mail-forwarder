# mail-forwarder

监听腾讯企业邮箱 IMAP 未读邮件，把合思工作流失败提醒原文转发到钉钉群机器人。

## 处理语义

- 只处理 `FILTER_FROM` 发来的未读邮件。
- 如果配置了 `FILTER_SUBJECT_KEYWORD`，主题必须包含该关键词。
- 钉钉消息标题固定使用 `DINGTALK_TITLE`，正文使用邮件原文。
- 钉钉 webhook 返回 2xx 后，才把邮件标记为已读。
- 钉钉发送失败时，邮件保持未读，容器重启或下次收到新邮件后会继续尝试。
- IMAP 连接出现致命异常时进程退出，由 Docker `restart: always` 拉起。
- 当前只支持钉钉群机器人，不抽象其他通知渠道。

## 配置

复制环境变量模板：

```bash
cp .env.example .env
```

腾讯企业邮 IMAP 常用配置：

```env
IMAP_HOST=imap.exmail.qq.com
IMAP_PORT=993
IMAP_SECURE=true
```

如果钉钉机器人开启加签，填写 `DINGTALK_SECRET`；未开启则留空。

## 本地调试

本地不需要 Docker，使用 Mac 上的 Node 直接跑：

```bash
npm install
npm run dev
```

建议第一次用：

```env
DRY_RUN=true
MARK_SEEN_ON_DRY_RUN=false
```

这样只连接邮箱、筛选未读邮件并打印 payload，不发钉钉，也不标记已读。确认输出正确后，再改成：

```env
DRY_RUN=false
```

合思工作流失败提醒可使用：

```env
FILTER_FROM=email@service.ekuaibao.biz
FILTER_SUBJECT_KEYWORD=合思工作流失败提醒
```

## GitHub Actions 构建镜像

本目录包含 workflow，会在推送到 `main` 或打 tag 时构建 Docker 镜像并推送到 GHCR：

```text
ghcr.io/<owner>/mail-forwarder:<tag>
```

首次使用需要在仓库设置里确认 Actions 有写入 Packages 的权限：

```text
Settings -> Actions -> General -> Workflow permissions -> Read and write permissions
```

## 云端运行

在服务器上准备 `.env` 后运行：

```bash
docker compose pull
docker compose up -d
docker logs -f mail-forwarder
```

生产部署使用明确版本号，不使用 `latest`。当前示例：

```text
ghcr.io/yunyingk/mail-forwarder:v0.1.0
```
