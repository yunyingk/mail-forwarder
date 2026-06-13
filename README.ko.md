# mail-forwarder

[简体中文](README.md) | [English](README.en.md) | [日本語](README.ja.md)

`mail-forwarder`는 IMAP 메일을 HTTP Webhook 호출로 바꾸는 가벼운 전달
서비스입니다. 하나 이상의 메일함 폴더에 연결하고, IMAP IDLE 장기 연결로 읽지 않은
메일을 감시합니다. 메일 본문, HTML, 헤더, 첨부 파일 메타데이터를 JSON으로 정리해
설정된 Webhook으로 전송합니다.

보낸 사람 필터링, 키워드 판단, HTML 파싱, 비즈니스 라우팅은 하지 않습니다. 이런
처리는 하위 HTTP Agent가 담당해야 합니다.

## 사용 사례

- 시스템은 HTTP Webhook을 받을 수 있지만 이메일을 직접 구독할 수 없다.
- 공개 이메일 전달 SaaS에 의존하고 싶지 않다.
- 로컬 PC, 서버, Docker에서 메일을 계속 감시하고 싶다.
- 하위 시스템으로 성공적으로 전달된 뒤에만 메일을 읽음 처리하고 싶다.

## 기능

- 여러 IMAP 메일 소스 지원
- 소스마다 하나의 Webhook URL 설정
- IMAP IDLE 장기 연결
- 명시적인 처리 모드
- checkpoint와 재시도 쿨다운을 로컬 상태 파일에 저장
- Webhook이 2xx를 반환한 뒤에만 읽음 처리
- 선택 가능한 로컬 읽기 전용 관리 API
- macOS, Linux, Windows 바이너리와 Docker 지원

## 빠른 시작

설정 템플릿을 생성합니다.

```bash
mail-forwarder init-config
```

`config.yaml`을 수정해 실제 IMAP 계정과 Webhook URL을 입력한 뒤 실행합니다.

```bash
mail-forwarder -config config.yaml
```

템플릿 설정은 자리 표시자 IMAP 인증 정보와 `dry_run: true`를 사용합니다. 운영 전에
반드시 실제 설정으로 바꾸세요.

## Docker

```bash
docker compose pull
docker compose up -d
```

이미지:

```text
ghcr.io/yunyingk/mail-forwarder:v0.2.2
crpi-yg3r8e9fvlik3jk0.cn-hangzhou.personal.cr.aliyuncs.com/yingqing/mail-forwarder:v0.2.2
```

## 설정 예시

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

운영 환경에서는 메일 서비스에서 전용 폴더를 만들고, 규칙으로 대상 메일을 그 폴더로
이동하는 구성을 권장합니다. `mail-forwarder`는 설정한 `mailbox`만 감시하며 메일
계정 전체를 스캔하지 않습니다.

## 처리 모드

`processing_mode`는 필수입니다.

- `unread_queue`: 읽지 않은 상태를 큐로 사용합니다. 기존 미독 메일과 새 미독 메일을 전송합니다. 수동으로 다시 미독 처리하면 다시 전송됩니다.
- `new_unread_queue`: 시작 전의 미독 메일은 건너뛰고, 시작 후의 새 미독 메일만 전송합니다. 재시작하면 시작 기준이 다시 계산됩니다.
- `checkpoint_from_now`: 첫 실행 시 기존 미독 메일을 건너뛰고, 이후 새 메일만 처리하며 checkpoint를 저장합니다.
- `checkpoint_from_unread`: 첫 실행 시 기존 미독 메일을 처리하고, 이후 checkpoint로 성공한 메일의 중복 전송을 막습니다.

자세한 내용은 [docs/processing-modes.md](docs/processing-modes.md)를 참고하세요.

## 전달 의미

- Webhook이 HTTP 2xx를 반환한 뒤에만 메일을 읽음 처리합니다.
- Webhook 실패 시 메일은 미독 상태로 남습니다.
- 실패한 메일은 로컬 쿨다운에 들어가며, 매번 즉시 재시도하지 않습니다.
- checkpoint 모드는 성공한 UID를 기록합니다.
- `unread_queue` 계열 모드는 사용자가 수동으로 미독 처리한 메일을 다시 전송할 수 있습니다.

## OpenAPI

OpenAPI는 두 가지 인터페이스를 설명합니다.

- `/mail-ingress`: 하위 Agent가 구현해야 하는 Webhook 요청 형식.
- `/healthz`, `/sources`, `/config`, `/openapi`, `/openapi.json`: 로컬 관리 API.

관리 API를 활성화하면 Postman, Swagger, AI Agent가 URL로 가져올 수 있습니다.

```text
http://127.0.0.1:6245/openapi.json
```

`/mail-ingress`는 이 프로그램이 제공하는 서버 API가 아닙니다. 이 프로그램이 외부로 보내는 요청 형식을 설명합니다.

## 빌드

```bash
go test ./...
go build -ldflags="-s -w -X main.version=$(git describe --tags --always)" -o mail-forwarder .
```

릴리스 워크플로는 Linux, macOS, Windows 바이너리를 빌드하고 GHCR 및 Aliyun ACR로 Docker 이미지를 푸시합니다.
