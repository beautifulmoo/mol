# Maintenance HTTP API 명세

**CLI(명령줄)** 는 **[CLI.md](./CLI.md)** 를, **대화형 REPL**은 **[REPL.md](./REPL.md)** (`contrabass-moleU agent`, 인자 없음)를 참고한다.

`maintenance/server/server.go`의 `Handler()`에 등록된 엔드포인트를 정리한다. 핸들러 구현은 **`handlers_*.go`**(업로드·apply·discovery 등), 원격 HTTP는 **`remotehttp.go`**·**`remoteapply.go`**, bulk body 파싱은 **`bulkhosts.go`** 등으로 분리되어 있다.  
**기본 URL**은 `http://<호스트>:<Maintenance.MaintenancePort>`이며, 경로 앞에는 설정값 **`Maintenance.APIPrefix`**(기본 **`/maintenance/api/v1`**), **`Maintenance.WebPrefix`**(기본 **`/maintenance`**)가 붙는다. 생략 시 `maintenance/agentcfg`·`ginproxy.RegisterMaintenanceProxy` 가 동일 fallback 을 쓴다. 예시 `cfg/agent.local.yml` 은 fleet 일치를 위해 두 항목을 **주석 처리**해 코드 기본값만 사용한다. 아래 표에서는 `{API}`, `{WEB}`로 표기한다.

---

## 공통

| 항목 | 설명 |
|------|------|
| **JSON 응답(대부분의 API)** | `Content-Type: application/json`. 본문 형식: `{"status":"success"\|"fail","data":<임의>}` (`APIResponse`). 일부 오류는 HTTP 4xx와 함께 동일 형식. **`data`가 문자열인 경우(적용·전환·삭제 안내 등)는 영문** — CLI가 원격 응답을 그대로 출력하고, 웹 UI도 동일 API를 사용한다. |
| **원격 프록시** | `ip` 쿼리/바디로 원격 호스트를 지정하면, 서버는 **`Server.HTTPPort`(Gin 등 외부 포트)** 로 해당 에이전트에 HTTP 요청을 보내 응답을 그대로 전달한다(`remoteBaseURL`). `Server.HTTPPort`가 유효하지 않으면 원격 호출 실패. |
| **텍스트** | `GET /version`만 `text/plain` (JSON 아님). |
| **일괄 원격 NDJSON** | `push-local-all`·`restart-all`·`apply-update-all`·`rollback-all` — `Content-Type: application/x-ndjson`. 호스트별 `progress` 스트림. **CLI** 대응은 아래 §「일괄 원격 API·CLI」·[CLI.md](./CLI.md). |

---

## 일괄 원격 API·CLI

오케스트레이터 **maintenance HTTP**(`Maintenance.MaintenancePort`, 기본 8889)에서 호출한다. Body의 **`hosts[]`** 는 화면 카드 1장 = 호스트 1대(`primary_ip`, `hostname`, `cpu_uuid`, `ips[]`). **웹 UI**는 **도달 가능(reachable)** 원격 카드만 수집해 body로 보낸다(`hosts.js` `collectRemoteHostsFromDOM({ reachableOnly: true })`). **`ips[]`** 는 Discovery 응답별 **`host_ip`**·**`responded_from_ip`** 병합(웹 **IP**·CLI `--discovery` 대괄호와 동일). **`primary_ip`** 는 **`responded_from_ip`** 대표값. body 생략 시 서버 **remoteregistry** fallback(웹·CLI는 Discovery로 만든 `hosts` 전달 권장).

| API (`POST {API}/…`) | CLI | 완료 시 `update_history.log` 요약 |
|----------------------|-----|-----------------------------------|
| `current-config/push-local-all` | `agent --push-config-all-remotes` | `config push-all finished succeeded=N failed=M` |
| `service-control/restart-all` | `agent --restart-all-remotes` | `service restart-all finished succeeded=N failed=M` |
| `apply-update-all` | `agent --apply-update-all-remotes <bundle.tar.gz>` | `apply-update-all finished succeeded=N failed=M skipped=K` |
| `versions/rollback-all` | `agent --rollback-all-remotes` | `rollback-all finished succeeded=N failed=M skipped=K` |

CLI는 각 명령 **내부**에서 UDP Discovery(기본값) 후 위 API를 호출한다(`--discovery` 선행 불필요). **REPL** bulk는 **`discovery` 캐시**의 `hosts[]` 사용(재-discovery 없음; `discover` 별칭). 공통 플래그·help: **`maintenance/bulkcli/flags.go`** (`-apiprefix`, `-maintenance-port`). 상세는 **CLI.md** 「리모트 일괄 CLI (공통)」·**REPL.md**.

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
| **GET** | `{API}/self` | 없음 | **200** `status: success`, `data`: 로컬 호스트 정보(DISCOVERY_RESPONSE 형, `version`·**`build_variant`** 등). **CLI** `agent --host-info`·REPL `host-info`는 이 응답을 표로 출력하며, **`HOST_IP`**(응답 IP)·**`HOST_IPS`**(발견 IP) 열은 추가 **UDP Discovery**(~3s, REPL은 **`discovery` 캐시** 우선)로 보강 — 웹 카드 **IP**·**응답한 IP** 규칙과 동일. |
| **GET** | `{API}/health` | 없음 | **200** `success`, `data`: `{ "ok": true }` — HTTP 헬스(원격 에이전트 `Server.HTTPPort` 경로 동일). |
| **GET** | `{API}/remote-health-check` | **Query**: `ip` (필수, 원격 호스트 IP). 이 서버가 `http://<ip>:Server.HTTPPort` + `{APIPrefix}/health` 로 HTTP GET(타임아웃은 `Maintenance.RemoteHealth.TimeoutSeconds`). | **200** `success` (원격 헬스 OK) / `fail` (연결·HTTP·응답 형식 오류). |
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
| **POST** | `{API}/service-control/restart-all` | **Body JSON**(선택): `{ "hosts": [{ "primary_ip", "hostname", "cpu_uuid", "ips":[] }, …] }` — **화면 카드 1장 = 호스트 1대**(권장). 호스트별로 `POST …/service-control` restart 프록시 후 **2초 대기·최대 45초** 동안 `GET …/health` 또는 `GET …/service-status`의 `Active: active (running)` 으로 재기동 확인. 연결 끊김(connection reset·EOF 등)은 재시작 진행 중으로 간주. 완료 시 `update_history.log`에 **요약 1줄**(`service restart-all finished succeeded=N failed=M`). 응답 **`application/x-ndjson`**. progress 필드: `verify_ok`, `verify_detail`, `connect_ip`, `tried_ips`. **CLI**: `agent --restart-all-remotes` — UDP Discovery(기본값)로 `hosts` 구성 후 동일 API 호출(`docs/CLI.md`). | **200** NDJSON 스트림 / 사전 검증 실패 시 JSON `fail`. |

---

## 업로드·업데이트

| 메서드 | 경로 | 입력 | 응답 |
|--------|------|------|------|
| **POST** | `{API}/upload` | **multipart/form-data**: 필드 **`bundle`** — **tar.gz** 배포 번들(`contrabass.manifest.yaml` + 에이전트 + config 등, `maintenance/scripts/pack-agent-tarball.sh` 참고). 본문 상한은 설정 `Maintenance.MaxUploadBytes`(기본 64MiB). | **200** `success`, `data`: `{ "version": "<버전 키>" }`. 검증 실패 **400** `fail`. |
| **POST** | `{API}/upload/remove` | **Body JSON**: `{ "version": "<버전 키>" }` — 스테이징 디렉터리만 삭제. | **200** `success` / `fail`. |
| **GET** | `{API}/update-status` | **Query**: `ip` (선택). 비어 있거나 `self`면 **이 서버**의 `current`와 로컬 스테이징을 비교. **원격 IP**면 해당 호스트 `GET .../self`의 `version`과 **이 서버의 로컬 스테이징**을 비교해 원격에 적용 가능한지 판단(`StagingUpdateAvailable`, 설정 `AllowSameVersionUpdate` 반영). | **200** `success`, `data`: 로컬만일 때 `current_version`, 스테이징 `staging_versions`, `can_apply`, `apply_version`, `remove_version`, `update_in_progress`, `staging_dual_agents`. 원격 `ip`일 때 추가로 `remote_ip`, `remote_current_version`, `can_apply`/`apply_version`은 **원격 기준**. 웹 UI는 원격 카드마다 이 API를 호출해 적용 버튼·variant 표시를 맞춘다(적용 후 해당 `ip` 재조회). 원격 조회 실패 시 `fail`. |
| **POST** | `{API}/apply-update-all` | **Body JSON**(선택): `{ "hosts": […], "version": "<키>" (생략 시 로컬 스테이징 최신), "agent_variant": "control"\|"compute", "reuse_previous_config": true\|false }` — 호스트별로 원격 `GET …/self` 버전 조회 후 `StagingUpdateAvailable`이면 `apply-update`와 동일 경로(업로드+적용). 미충족 호스트는 **`skipped`**. 완료 시 `update_history.log`에 `apply-update-all finished succeeded=N failed=M skipped=K`. 응답 **`application/x-ndjson`**. **CLI**: `agent --apply-update-all-remotes <bundle.tar.gz>` — 로컬 `POST …/upload` 후 Discovery(기본값)로 `hosts` 구성·동일 API(`docs/CLI.md`). | **200** NDJSON / 사전 검증 실패 시 JSON `fail`. |
| **POST** | `{API}/apply-update` | **두 가지 모드**: (1) **JSON** `{"version":"<키>","ip":""\|"self"\|"<IP>", "agent_variant":"control"\|"compute" (선택), "reuse_previous_config":true\|false (선택)}` — 로컬이면 스테이징/versions에서 적용·`MaterializeCanonicalAgent`·(재사용 시 **적용 전 `current` config** 복사)·`systemd-run` 비동기, 원격이면 **`reuse_previous_config`가 true**일 때 원격 `GET …/current-config`로 config를 주입한 뒤 업로드 API·apply(self). JSON에서 **`agent_variant` 생략·빈 문자열**이면 서버는 **`compute`** 로 처리(CLI `--apply-update` 생략 시는 설치 `build_variant` 따름). **`reuse_previous_config` 생략**이면 서버는 **false**; **웹 UI·CLI**(`agent --apply-update`, 기본 재사용·`-use-bundle-config` 시 false)는 의도를 **명시**한다. (2) **multipart/form-data** `ip`(필수, 원격), **`bundle`**(tar.gz), **`agent_variant`**(선택), **`reuse_previous_config`**(선택, 기본 true) — 로컬 스테이징 없이 원격에만 번들 업로드+적용. | **200** 성공 메시지 문자열 또는 `fail`. |

적용·롤백 기록은 `{DeployBase}/update_history.log`(append, API는 **tail 10**). 동시 기록은 **`flock`**(`update_history.log.lock`, 0바이트로 잔존 가능, 다음 적용 차단 아님).

업로드 성공 시 스테이징 `{DeployBase}/staging/<버전 키>/` 에는 풀린 에이전트(**manifest의 `agent.path` basename**)·config(**manifest의 `config.path` basename**, 예: `agent.local.yml`) 외에 **원본 번들**이 `upload.bundle.tar.gz` 로 함께 저장된다. 로컬 적용으로 `versions/<키>/` 로 옮길 때는 **스테이징 디렉터리 전체를 그대로 복사**한 뒤 `upload.bundle.tar.gz`만 삭제한다(향후 번들에 추가 파일이 있어도 설치 트리에 반영됨). 원격 `apply-update`(JSON)는 **`reuse_previous_config`가 true**이면 원격 current config로 스테이징 트리 config를 덮어쓰고 `upload.bundle.tar.gz`를 제거한 뒤 트리에서 tar.gz를 재생성해 `POST .../upload`에 실어 보낸다. 재사용 off이고 스테이징에 `upload.bundle.tar.gz`가 남아 있으면 그 파일을 그대로 보내고, 스테이징만 지운 뒤 `versions/`에만 있으면 바이너리·config로 최소 번들을 만든다.

---

## 로그·설정·버전 목록

| 메서드 | 경로 | 입력 | 응답 |
|--------|------|------|------|
| **GET** | `{API}/update-log` | **Query**: `ip` (선택, 원격이면 `Server.HTTPPort`로 프록시). | **200** `success`, `data`: `{ "output": "<최근 10줄, 파일 맨 아래 tail, 오래된 줄→새 줄>", "recent_rollback": <bool> }`. 로컬·프록시 응답 모두 **`Cache-Control: no-store`** 및 tail 10 정규화. `recent_rollback`은 **파일 맨 아래 줄**이 `update … failed`·`rollback failed` 등 **실제 업데이트/롤백 실패**일 때만 true(`config push-all finished`·`service restart-all finished`·`apply-update-all finished`·`rollback-all finished` 요약의 `failed=N` 은 무시). `contrabass-mole-update.service` active이면 `recent_rollback`은 false. 웹 UI는 로컬·원격 **업데이트 적용·switch-current** 중 **2초 간격**으로 이 API를 호출하며(원격은 `?ip=`, `&_=`·`cache: 'no-store'`), 이번 run의 `update <버전> started` 확인 후 **마지막 줄**이 `success`/`failed`일 때까지 폴링(host-info/`/self` 폴링과 독립). 웹 UI는 `output` 줄을 **역순 표시**(최신이 위). 수동 갱신: **「로그 새로고침」**. |
| **GET** | `{API}/current-config` | **Query**: `ip` (선택). | **200** `success`, `data`: `{ "content": "<yaml 문자열>" }`. |
| **POST** | `{API}/current-config` | **Body JSON**: `{ "content": "<yaml>", "ip": "<선택>", "backup_before_write": true\|false (선택) }` — `ip`로 원격 저장 프록시(프록시 시 원격에 **`backup_before_write`: true** 전달). 로컬 직접 저장 시 기존 파일은 **`backup_before_write`: true**일 때만 `current/` 아래 **`agent.local.yml.backup`** 으로 백업 후 덮어쓴다. | **200** `success`, `data`: null(로컬 저장 성공 시). 검증 실패 `fail`. |
| **POST** | `{API}/current-config/push-local` | **Body JSON**: `{ "ip": "<원격 IP>" }` — **이 서버(로컬) `current`의 `agent.local.yml`** 내용을 읽어 해당 원격의 `POST …/current-config`로 전송(`backup_before_write`: true). | **200** `success`, `data`: `{ "message": "…" }` / `fail`. |
| **POST** | `{API}/current-config/push-local-all` | **Body JSON**(선택): `{ "hosts": [{ "primary_ip", "hostname", "cpu_uuid", "ips":[] }, …] }` — **화면 카드 1장 = 호스트 1대**(권장). `ips`는 해당 카드의 접속 후보 IP(첫 성공 시 종료). 레거시 `{ "ips":[] }` 도 지원. body 생략 시 **remoteregistry**·필요 시 서버 측 Discovery fallback. 완료 시 로컬 `update_history.log`에 **요약 1줄**만 append(`config push-all finished succeeded=N failed=M`). 응답 **`application/x-ndjson`**(호스트별 progress는 스트림·UI 「결과 보기」). **CLI**: `agent --push-config-all-remotes` — UDP Discovery(기본값)로 `hosts` 구성 후 동일 API 호출(`docs/CLI.md`). | **200** NDJSON 스트림 / 사전 검증 실패 시 JSON `fail`. |
| **GET** | `{API}/discovered-remotes` | 없음. | **200** `success`, `data`: `{ "remotes": [ { "primary_ip", "hostname", "health_dead", … } ] }` — 서버 메모리 레지스트리 스냅샷(헬스 실패 포함). |
| **GET** | `{API}/versions/list` | **Query**: `ip` (선택). | **200** `success`, `data`: `{ "versions": [ { "version", "is_current", "is_previous" }, ... ], "can_rollback": <bool> }`. **`can_rollback`**: `versionsapi.CanRollbackFromEntries` — `previous` 존재且 `current`≠`previous`. 로컬·원격 프록시 응답 모두 enrichment. |
| **POST** | `{API}/versions/remove` | **Body JSON**: `{ "versions": ["<키>",...], "ip": "<선택>" }` | **200** `success`, `data`: 결과 메시지 문자열(삭제·제외 요약). current/previous 가리키는 버전은 삭제 안 함. |
| **POST** | `{API}/versions/switch-current` | **Body JSON**: `{ "version": "<버전 키>", "ip": "<선택>" }` — 로컬에서 `versions/`(또는 스테이징)에 있는 버전을 **current**로 두기 위해 내장 `update.sh`를 `systemd-run`으로 실행(`apply-update` 로컬과 동일). `ip`가 원격이면 해당 호스트 API로 프록시. | **200** `success`, `data`: 안내 문자열 / `fail`. |
| **POST** | `{API}/versions/rollback` | **Body JSON**(선택): `{ "ip": "<선택>" }` — 로컬(또는 원격 프록시)에서 embedded **`rollback.sh`** 실행: `previous` → `current` 심링크 전환·서비스 재시작. `previous` 없으면 `fail`. | **200** `success` / `fail`. |
| **POST** | `{API}/versions/rollback-all` | **Body JSON**(선택): `{ "hosts": […] }` — 호스트별 원격 `versions/list`로 `is_current`·`is_previous` 버전 키를 비교. **`previous` 없음** 또는 **`current`·`previous` 동일**(이미 롤백됨)이면 **`skipped`**. 그 외 `POST …/versions/rollback` 프록시(embedded `rollback.sh`: `previous`→`current`·서비스 재시작). 완료 시 `rollback-all finished succeeded=N failed=M skipped=K`. 응답 **`application/x-ndjson`**. **CLI**: `agent --rollback-all-remotes` — UDP Discovery(기본값)로 `hosts` 구성 후 동일 API 호출(`docs/CLI.md`). | **200** NDJSON / 사전 검증 실패 시 JSON `fail`. |

---

## 웹 정적·런타임

| 메서드 | 경로 | 입력 | 응답 |
|--------|------|------|------|
| **GET** | `{WEB}/client-runtime.js` | 없음 | **200** `application/javascript`, `Cache-Control: no-store`. 본문: `window.__CONTRABASS_API_PREFIX__`, `window.__CONTRABASS_REMOTE_HEALTH__`(원격 헬스 폴링 간격·타임아웃·임계값·지터, 설정 반영). |
| **GET** | `{WEB}/` 및 하위 | 경로 = embed된 `web/` 파일 (`index.html`, `style.css`, `js/core.js`, `js/hosts.js`, `js/card-panels.js`, `js/card-apply.js`, `js/card-health.js`, `js/card.js`, `js/discovery.js`, `js/bulk.js`, `js/app.js` 등) | 정적 파일 서빙 (`StripPrefix`). `client-runtime.js`는 API prefix·RemoteHealth 주입 후 위 JS 모듈을 순서대로 로드한다. |

---

## curl 예제 (POST·업로드·업데이트)

아래는 **maintenance HTTP에 직접** 붙는 경우(`Maintenance.MaintenancePort`, 예: `8889`)를 가정한다.  
**`APIPrefix` 기본값은 `/maintenance/api/v1`** 이다. 다른 값을 쓰면 URL 경로만 바꾼다.  
**Gin(예: 8888)으로만 노출**하는 경우에도 동일한 경로·바디를 쓰면 된다.

```bash
# 공통: 베이스 URL (필요 시 호스트·포트만 변경)
BASE=http://127.0.0.1:8889
API=/maintenance/api/v1
```

### 서비스 제어 `POST .../service-control`

로컬 서비스 재시작:

```bash
curl -sS -X POST "${BASE}${API}/service-control" \
  -H 'Content-Type: application/json' \
  -d '{"ip":"self","action":"restart"}'
```

`ip`를 빈 문자열로 두어도 로컬로 처리된다. `start` / `stop` 동일 형식.

원격 **일괄 재시작** (`restart-all`, NDJSON):

```bash
curl -sS -N -X POST "${BASE}${API}/service-control/restart-all" \
  -H 'Content-Type: application/json' \
  -d '{"hosts":[{"primary_ip":"10.0.0.2","hostname":"node-b","cpu_uuid":"…","ips":["10.0.0.2"]}]}'
```

Body 생략 시 서버 **remoteregistry** fallback. 완료 시 `update_history.log`에 `service restart-all finished succeeded=N failed=M` 한 줄.

**CLI**: `contrabass-moleU agent --restart-all-remotes`.

---

### 업로드 `POST .../upload` (multipart)

필드 **`bundle`** 하나에 **tar.gz** 배포 번들을 첨부한다(`maintenance/packaging/contrabass.manifest.yaml.template`, `maintenance/scripts/pack-agent-tarball.sh`). 원격 전용 **`POST .../apply-update`** multipart도 동일하게 **`ip`** + **`bundle`**.

#### curl

`-F 'bundle=@파일경로'` — 번들은 로컬에서 `make` 후 `./maintenance/scripts/pack-agent-tarball.sh` 로 만든 `.tar.gz` 등.

```bash
curl -sS -X POST "${BASE}${API}/upload" \
  -F 'bundle=@/path/to/contrabass-agent-0.4.4-1-gabc1234.tar.gz'
```

성공 시 `data.version`에 버전 키가 온다.

#### Postman

**Body → form-data**: **Key** `bundle`, 타입 **File**, tar.gz 선택.

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
  -d '{"version":"0.4.4-10"}'
```

---

### 업데이트 적용 `POST .../apply-update`

**로컬** — 이미 스테이징 또는 `versions/`에 있는 버전 키를 적용(`ip` 생략 또는 `self`). 기본은 **current config 재사용**(`reuse_previous_config: true`, 웹·CLI와 동일):

```bash
curl -sS -X POST "${BASE}${API}/apply-update" \
  -H 'Content-Type: application/json' \
  -d '{"version":"0.4.4-10","ip":"self","agent_variant":"control","reuse_previous_config":true}'
```

번들에 포함된 config를 쓰려면 `reuse_previous_config`를 `false`로 보낸다. JSON에서 필드를 **생략**하면 서버는 **false**로 처리한다.

**원격** — JSON만: 로컬에 해당 버전 디렉터리가 있어야 하며, 서버가 원격으로 업로드·적용 API를 호출한다.

```bash
curl -sS -X POST "${BASE}${API}/apply-update" \
  -H 'Content-Type: application/json' \
  -d '{"version":"0.4.4-10","ip":"192.168.0.42","reuse_previous_config":true}'
```

**원격** — tar.gz 번들을 이 서버에서 골라 원격에 올리며 적용(multipart, `ip` 필수). multipart는 `reuse_previous_config` 생략 시 **true**:

```bash
curl -sS -X POST "${BASE}${API}/apply-update" \
  -F 'ip=192.168.0.42' \
  -F 'agent_variant=compute' \
  -F 'reuse_previous_config=true' \
  -F 'bundle=@/path/to/contrabass-agent-0.4.4.tar.gz'
```

**CLI** (`docs/CLI.md`): `agent --apply-update`는 기본 재사용; **`-use-bundle-config`** 로 번들 config.

---

### 현재 config 저장 `POST .../current-config`

로컬 `current/agent.local.yml` 덮어쓰기(내용은 **유효한 YAML**이어야 함):

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

원격 `current`에 저장(기존 `agent.local.yml`은 **`agent.local.yml.backup`** 으로 백업):

```bash
curl -sS -X POST "${BASE}${API}/current-config" \
  -H 'Content-Type: application/json' \
  -d '{"content":"Server:\n  HTTPPort: 8888\n","ip":"192.168.0.42"}'
```

---

### 로컬 current config → 원격 복사 `POST .../current-config/push-local`

로컬 `current/agent.local.yml` 전체를 지정 원격의 `current`에 복사한다. 원격 측은 저장 전 기존 파일을 **`agent.local.yml.backup`** 으로 백업한다.

```bash
curl -sS -X POST "${BASE}${API}/current-config/push-local" \
  -H 'Content-Type: application/json' \
  -d '{"ip":"192.168.0.42"}'
```

레지스트리의 **모든 원격**에 순차 복사(진행 NDJSON):

```bash
curl -sS -N -X POST "${BASE}${API}/current-config/push-local-all"
```

화면과 동일하게 **호스트 목록**을 지정(권장):

```bash
curl -sS -N -X POST "${BASE}${API}/current-config/push-local-all" \
  -H 'Content-Type: application/json' \
  -d '{"hosts":[{"primary_ip":"10.0.0.2","hostname":"node-b","cpu_uuid":"…","ips":["10.0.0.2","10.0.0.3"]}]}'
```

완료 시 `update_history.log`에 `config push-all finished succeeded=N failed=M` 한 줄.

**CLI**: `contrabass-moleU agent --push-config-all-remotes` — Discovery(기본값)로 `hosts` 구성 후 동일 API.

---

### 일괄 업데이트 적용 `POST .../apply-update-all` (NDJSON)

```bash
curl -sS -N -X POST "${BASE}${API}/apply-update-all" \
  -H 'Content-Type: application/json' \
  -d '{
    "hosts":[{"primary_ip":"10.0.0.2","hostname":"node-b","cpu_uuid":"…","ips":["10.0.0.2"]}],
    "version":"0.4.4-10",
    "agent_variant":"compute",
    "reuse_previous_config":true
  }'
```

`version` 생략 시 로컬 스테이징 최신. `StagingUpdateAvailable` 미충족 호스트는 스트림에서 `skipped`. 완료 시 `apply-update-all finished succeeded=N failed=M skipped=K` 한 줄.

**CLI**: `contrabass-moleU agent --apply-update-all-remotes <bundle.tar.gz>` — 먼저 `POST …/upload`로 스테이징.

---

### 일괄 롤백 `POST .../versions/rollback-all` (NDJSON)

```bash
curl -sS -N -X POST "${BASE}${API}/versions/rollback-all" \
  -H 'Content-Type: application/json' \
  -d '{"hosts":[{"primary_ip":"10.0.0.2","hostname":"node-b","cpu_uuid":"…","ips":["10.0.0.2"]}]}'
```

원격에서 `previous` 없음 또는 `current`·`previous`가 같은 버전이면 `skipped`. 완료 시 `rollback-all finished succeeded=N failed=M skipped=K` 한 줄.

**CLI**: `contrabass-moleU agent --rollback-all-remotes`.

---

### 설치된 버전 삭제 `POST .../versions/remove`

```bash
curl -sS -X POST "${BASE}${API}/versions/remove" \
  -H 'Content-Type: application/json' \
  -d '{"versions":["0.4.4-9","0.4.4-8"]}'
```

원격 호스트에 프록시:

```bash
curl -sS -X POST "${BASE}${API}/versions/remove" \
  -H 'Content-Type: application/json' \
  -d '{"versions":["0.4.4-9"],"ip":"192.168.0.42"}'
```

---

## 참고

- multipart 바이너리 필드명은 코드 상수 **`agent`** (`uploadBinaryField`).
- Discovery 쿼리 파싱은 `URL.RawQuery`가 비어 있으면 **`RequestURI`의 `?` 이후**를 보조로 사용한다.
- 상위 요구·동작 설명은 **`PRD.md`**를 본다.
