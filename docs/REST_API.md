# Maintenance HTTP API 명세

`maintenance/server/server.go`의 `Handler()`에 등록된 엔드포인트를 정리한다.  
**기본 URL**은 `http://<호스트>:<Maintenance.MaintenancePort>`이며, 경로 앞에는 설정값 **`Maintenance.APIPrefix`**(기본 `/api/v1`), **`Maintenance.WebPrefix`**(기본 `/web`)가 붙는다. 아래 표에서는 `{API}`, `{WEB}`로 표기한다.

---

## 공통

| 항목 | 설명 |
|------|------|
| **JSON 응답(대부분의 API)** | `Content-Type: application/json`. 본문 형식: `{"status":"success"\|"fail","data":<임의>}` (`APIResponse`). 일부 오류는 HTTP 4xx와 함께 동일 형식. |
| **원격 프록시** | `ip` 쿼리/바디로 원격 호스트를 지정하면, 서버는 **`Server.HTTPPort`(Gin 등 외부 포트)** 로 해당 에이전트에 HTTP 요청을 보내 응답을 그대로 전달한다(`remoteBaseURL`). `Server.HTTPPort`가 유효하지 않으면 원격 호출 실패. |
| **텍스트** | `GET /version`만 `text/plain` (JSON 아님). |

---

## 시스템·루트

| 메서드 | 경로 | 입력 | 응답 |
|--------|------|------|------|
| **GET** | `/version` | 없음 | **200** `text/plain`: `<BinaryName> <버전 키>` 한 줄. 경로는 `APIPrefix`와 무관(루트). |
| **GET** | `/` | 없음 | 브라우저로 추정되면 **302** → `{WEB}/`. 그 외 **404**. |

---

## 호스트·Discovery

| 메서드 | 경로 | 입력 | 응답 |
|--------|------|------|------|
| **GET** | `{API}/self` | 없음 | **200** `status: success`, `data`: 로컬 호스트 정보(DISCOVERY_RESPONSE 형). |
| **GET** | `{API}/host-info` | **Query**: `ip` (선택). 비어 있거나 `self`면 `/self`와 동일. 그 외 해당 IP로 **UDP 유니캐스트** Discovery. | **200** `success` + 단일 호스트 객체, 또는 `fail` + 메시지. |

### `GET {API}/discovery`

| 항목 | 설명 |
|------|------|
| **Query** | `exclude_self` 또는 `exclude-self`: `1`/`true`/`yes`/`on` → 자기 응답 제외. 생략 시 포함(`"self": true`). / `timeout`: 초 단위 정수 **1~600**, 해당 요청의 수집 시간만 재정의. 생략 시 `DiscoveryTimeoutSeconds`(0 이하이면 구현상 10초). |
| **응답** | **200** `success`, `data`: **배열** `[]` (발견 호스트·기본 시 자기 포함). 오류 시 **400** 또는 **500** 등 + `fail`. |

### `GET {API}/discovery/stream`

| 항목 | 설명 |
|------|------|
| **Query** | 위 `discovery`와 동일(`exclude_self`, `timeout`). |
| **응답** | **200** `Content-Type: text/event-stream`. 스트림 시작 전 실패 시에도 **200** + `event: discoveryfail` + JSON `data.message`. 정상 시 `data: <JSON 한 호스트>\n\n` 반복, 종료 시 `event: done`. 쿼리 파싱 오류도 `discoveryfail`로 안내할 수 있음. |

---

## 서비스(systemd)

| 메서드 | 경로 | 입력 | 응답 |
|--------|------|------|------|
| **GET** | `{API}/service-status` | **Query**: `ip` (선택). 없음/`self` → 로컬 `systemctl status`. 지정 시 원격 `GET {API}/service-status`(Gin 포트). | **200** `success`, `data`: `{ "output": "<systemctl 문자열>" }` 형 또는 원격과 동일 구조. 실패 시 `fail`. |
| **POST** | `{API}/service-control` | **Body JSON**: `{ "ip": "" \| "self" \| "<호스트IP>", "action": "start" \| "stop" \| "restart" }` | **200** `success` / `fail`. 원격 `restart`만 HTTP로, `start`/`stop`은 SSH. |

---

## 업로드·업데이트

| 메서드 | 경로 | 입력 | 응답 |
|--------|------|------|------|
| **POST** | `{API}/upload` | **multipart/form-data** (최대 64MB): 필드 **`agent`** (실행 파일), **`config`** (config.yaml). | **200** `success`, `data`: `{ "version": "<버전 키>" }`. 검증 실패 **400** `fail`. |
| **POST** | `{API}/upload/remove` | **Body JSON**: `{ "version": "<버전 키>" }` — 스테이징 디렉터리만 삭제. | **200** `success` / `fail`. |
| **GET** | `{API}/update-status` | **Query**: `ip` (선택). 비어 있거나 `self`면 **이 서버**의 `current`와 로컬 스테이징을 비교. **원격 IP**면 해당 호스트 `GET .../self`의 `version`과 **이 서버의 로컬 스테이징**을 비교해 원격에 적용 가능한지 판단. | **200** `success`, `data`: 로컬만일 때 `current_version`, 스테이징 `staging_versions`, `can_apply`, `apply_version`, `remove_version`, `update_in_progress`. 원격 `ip`일 때 추가로 `remote_ip`, `remote_current_version`(원격 현재 버전 키), `can_apply`/`apply_version`은 **원격 기준**으로 채움. 원격 조회 실패 시 `fail`. |
| **POST** | `{API}/apply-update` | **두 가지 모드**: (1) **JSON** `{"version":"<키>","ip":""\|"self"\|"<IP>"}` — 로컬이면 스테이징/versions에서 적용·`systemd-run` 비동기, 원격이면 해당 호스트로 업로드 API 후 apply. (2) **multipart/form-data** `ip`(필수, 원격), `agent`, `config` — 원격에만 업로드+적용. | **200** 성공 메시지 문자열 또는 `fail`. |

---

## 로그·설정·버전 목록

| 메서드 | 경로 | 입력 | 응답 |
|--------|------|------|------|
| **GET** | `{API}/update-log` | **Query**: `ip` (선택). 원격이면 프록시. | **200** `success`, `data`: `{ "output": "<최대 5줄>", "recent_rollback": <bool> }`. |
| **GET** | `{API}/current-config` | **Query**: `ip` (선택). | **200** `success`, `data`: `{ "content": "<yaml 문자열>" }`. |
| **POST** | `{API}/current-config` | **Body JSON**: `{ "content": "<yaml>", "ip": "<선택>" }` — `ip`로 원격 저장 프록시. | **200** `success`, `data`: null(로컬 저장 성공 시). 검증 실패 `fail`. |
| **GET** | `{API}/versions/list` | **Query**: `ip` (선택). | **200** `success`, `data`: `{ "versions": [ { "version", "is_current", "is_previous" }, ... ] }`. |
| **POST** | `{API}/versions/remove` | **Body JSON**: `{ "versions": ["<키>",...], "ip": "<선택>" }` | **200** `success`, `data`: 결과 메시지 문자열(삭제·제외 요약). current/previous 가리키는 버전은 삭제 안 함. |

---

## 웹 정적·런타임

| 메서드 | 경로 | 입력 | 응답 |
|--------|------|------|------|
| **GET** | `{WEB}/client-runtime.js` | 없음 | **200** `application/javascript`, `Cache-Control: no-store`. 본문: `window.__CONTRABASS_API_PREFIX__="<APIPrefix>";` |
| **GET** | `{WEB}/` 및 하위 | 경로 = embed된 `web/` 파일 (`index.html`, `app.js`, `style.css` 등) | 정적 파일 서빙 (`StripPrefix`). |

---

## curl 예제 (POST·업로드·업데이트)

아래는 **maintenance HTTP에 직접** 붙는 경우(`Maintenance.MaintenancePort`, 예: `8889`)를 가정한다.  
**`APIPrefix`가 `/api/v1`이 아니면** URL 경로만 바꾼다(예: `/maintenance/api/v1/upload`).  
**Gin(예: 8888)으로만 노출**하는 경우에도 동일한 경로·바디를 쓰면 된다.

```bash
# 공통: 베이스 URL (필요 시 호스트·포트만 변경)
BASE=http://127.0.0.1:8889
API=/api/v1
```

### 서비스 제어 `POST .../service-control`

로컬 서비스 재시작:

```bash
curl -sS -X POST "${BASE}${API}/service-control" \
  -H 'Content-Type: application/json' \
  -d '{"ip":"self","action":"restart"}'
```

`ip`를 빈 문자열로 두어도 로컬로 처리된다. `start` / `stop` 동일 형식.

---

### 업로드 `POST .../upload` (multipart)

실행 파일 필드명은 반드시 **`agent`**, config는 **`config`**. (동일 필드명이 **`POST .../apply-update`** 원격 multipart에도 적용된다.)

#### curl

`-F '필드명=@파일경로'` 에서 **`@` 뒤**는 **로컬 디스크에 있는 파일의 경로**이다. curl이 그 파일을 읽어 multipart 한 파트로 붙인다. 예시의 `/path/to/...` 는 **본인 PC의 실제 경로**로 바꾼다.

- **Windows**: Git Bash 등에서는 `C:/Users/이름/...` 또는 `/c/Users/...` 형식이 안전한 경우가 많다. PowerShell에서는 `curl.exe`를 쓰고 `-F "agent=@C:\work\agent"` 처럼 **경로를 따옴표로 감싸** 백슬래시 이스케이프에 주의한다.

```bash
curl -sS -X POST "${BASE}${API}/upload" \
  -F 'agent=@/path/to/contrabass-moleU' \
  -F 'config=@/path/to/config.yaml'
```

성공 시 `data.version`에 버전 키(예: `0.4.4_10`)가 온다.

#### Postman

경로 문자열을 직접 넣지 않는다. **Body → form-data** 에서:

1. **Key** `agent` — 타입을 **File**로 바꾼 뒤 **Select Files** 로 실행 파일 선택  
2. **Key** `config` — 타입 **File** — `config.yaml` 선택  

URL만 `http://127.0.0.1:8889/api/v1/upload` 등으로 맞추고 **Send** 하면 된다. OS가 Windows여도 **파일 선택 대화상자**가 경로를 처리한다.

#### curl vs Postman 요약

| 구분 | curl | Postman |
|------|------|---------|
| multipart 넣는 방식 | `-F 'name=@로컬파일경로'` (`@` = 그 경로의 파일 내용을 첨부) | form-data에서 필드 타입 **File**, **파일 선택** |
| 경로 | 터미널에 쓸 **실제 경로** 문자열 | GUI에서 파일만 고르면 됨(수동 경로 입력 불필요) |

---

### 스테이징 삭제 `POST .../upload/remove`

```bash
curl -sS -X POST "${BASE}${API}/upload/remove" \
  -H 'Content-Type: application/json' \
  -d '{"version":"0.4.4_10"}'
```

---

### 업데이트 적용 `POST .../apply-update`

**로컬** — 이미 스테이징 또는 `versions/`에 있는 버전 키를 적용(`ip` 생략 또는 `self`):

```bash
curl -sS -X POST "${BASE}${API}/apply-update" \
  -H 'Content-Type: application/json' \
  -d '{"version":"0.4.4_10","ip":"self"}'
```

**원격** — JSON만: 로컬에 해당 버전 디렉터리가 있어야 하며, 서버가 원격으로 업로드·적용 API를 호출한다.

```bash
curl -sS -X POST "${BASE}${API}/apply-update" \
  -H 'Content-Type: application/json' \
  -d '{"version":"0.4.4_10","ip":"192.168.0.42"}'
```

**원격** — 파일을 이 서버에서 골라 원격에 올리며 적용(multipart, `ip` 필수):

```bash
curl -sS -X POST "${BASE}${API}/apply-update" \
  -F 'ip=192.168.0.42' \
  -F 'agent=@/path/to/contrabass-moleU' \
  -F 'config=@/path/to/config.yaml'
```

---

### 현재 config 저장 `POST .../current-config`

로컬 `current/config.yaml` 덮어쓰기(내용은 **유효한 YAML**이어야 함):

```bash
curl -sS -X POST "${BASE}${API}/current-config" \
  -H 'Content-Type: application/json' \
  --data-binary @- <<'EOF'
{"content":"Server:\n  HTTPPort: 8888\nMaintenance:\n  MaintenancePort: 8889\n"}
EOF
```

한 줄로:

```bash
curl -sS -X POST "${BASE}${API}/current-config" \
  -H 'Content-Type: application/json' \
  -d '{"content":"# minimal\n"}'
```

---

### 설치된 버전 삭제 `POST .../versions/remove`

```bash
curl -sS -X POST "${BASE}${API}/versions/remove" \
  -H 'Content-Type: application/json' \
  -d '{"versions":["0.4.4_9","0.4.4_8"]}'
```

원격 호스트에 프록시:

```bash
curl -sS -X POST "${BASE}${API}/versions/remove" \
  -H 'Content-Type: application/json' \
  -d '{"versions":["0.4.4_9"],"ip":"192.168.0.42"}'
```

---

## 참고

- multipart 바이너리 필드명은 코드 상수 **`agent`** (`uploadBinaryField`).
- Discovery 쿼리 파싱은 `URL.RawQuery`가 비어 있으면 **`RequestURI`의 `?` 이후**를 보조로 사용한다.
- 상위 요구·동작 설명은 **`PRD.md`**를 본다.
