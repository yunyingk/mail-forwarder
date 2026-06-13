# mail-forwarder

[简体中文](README.md) | [English](README.en.md) | [한국어](README.ko.md)

`mail-forwarder` は軽量な IMAP から HTTP Webhook への転送サービスです。
1 つ以上のメールボックスフォルダに接続し、IMAP IDLE の長時間接続で未読メール
を監視します。本文、HTML、ヘッダー、添付ファイルのメタデータを JSON に整形し、
設定された Webhook に送信します。

送信者フィルタ、キーワード判定、HTML 解析、業務ルーティングは行いません。
それらの処理は下流の HTTP Agent が担当します。

## ユースケース

- システムは HTTP Webhook を受け取れるが、メールを直接購読できない。
- 公開 SaaS のメール転送サービスに依存したくない。
- ローカル PC、サーバー、Docker でメールを常時監視したい。
- 下流への送信成功後にだけメールを既読にしたい。

## 機能

- 複数の IMAP メールソースに対応
- ソースごとに 1 つの Webhook URL を設定
- IMAP IDLE による長時間接続
- 明示的な処理モード
- checkpoint とリトライ冷却をローカル状態ファイルに保存
- Webhook が 2xx を返した後にのみ既読化
- 任意のローカル読み取り専用管理 API
- macOS、Linux、Windows バイナリと Docker に対応

## クイックスタート

設定テンプレートを生成します。

```bash
mail-forwarder init-config
```

`config.yaml` を編集し、実際の IMAP アカウントと Webhook URL を設定して実行します。

```bash
mail-forwarder -config config.yaml
```

テンプレート設定にはダミーの IMAP 認証情報が入り、`dry_run: true` になっています。
本番利用前に必ず実際の設定へ変更してください。

## Docker

```bash
docker compose pull
docker compose up -d
```

イメージ:

```text
ghcr.io/yunyingk/mail-forwarder:v0.2.2
crpi-yg3r8e9fvlik3jk0.cn-hangzhou.personal.cr.aliyuncs.com/yingqing/mail-forwarder:v0.2.2
```

## 設定例

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

本番では、メールサービス側に専用フォルダを作成し、ルールで対象メールをその
フォルダへ移動する構成を推奨します。`mail-forwarder` は設定された `mailbox`
だけを監視し、アカウント全体はスキャンしません。

## 処理モード

`processing_mode` は必須です。

- `unread_queue`: 未読状態をキューとして扱います。既存の未読と新しい未読を送信します。手動で未読に戻すと再送信されます。
- `new_unread_queue`: 起動前の未読はスキップし、起動後の新しい未読だけを送信します。再起動時に起動基準が再計算されます。
- `checkpoint_from_now`: 初回起動時に既存の未読をスキップし、その後の新規メールだけを処理して checkpoint を永続化します。
- `checkpoint_from_unread`: 初回起動時に既存の未読を処理し、その後は checkpoint で成功済みメールの重複送信を抑制します。

詳細は [docs/processing-modes.md](docs/processing-modes.md) を参照してください。

## 配信セマンティクス

- Webhook が HTTP 2xx を返した後にのみメールを既読にします。
- Webhook が失敗した場合、メールは未読のままです。
- 失敗したメールはローカルの冷却期間に入り、毎回すぐには再送信されません。
- checkpoint モードは成功済み UID を記録します。
- `unread_queue` 系のモードでは、手動で未読に戻したメールを再送信できます。

## OpenAPI

OpenAPI は 2 つの面を記述します。

- `/mail-ingress`: 下流 Agent が実装する Webhook リクエスト形式。
- `/healthz`、`/sources`、`/config`、`/openapi`、`/openapi.json`: ローカル管理 API。

管理 API を有効にすると、Postman、Swagger、AI Agent は URL からインポートできます。

```text
http://127.0.0.1:6245/openapi.json
```

`/mail-ingress` はこのプログラムが提供するサーバー API ではありません。このプログラムが外部へ送信するリクエスト形式を示します。

## ビルド

```bash
go test ./...
go build -ldflags="-s -w -X main.version=$(git describe --tags --always)" -o mail-forwarder .
```

リリースワークフローは Linux、macOS、Windows バイナリをビルドし、GHCR と Aliyun ACR に Docker イメージを送信します。
