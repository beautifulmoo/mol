# 변경 이력 (mol)

## Maintenance 리팩터·문서 현행화 (2026-05)

서버·웹·CLI·REPL 정리(코드 감사 Large/Medium 항목 및 후속 스프린트).

| 영역 | 요약 |
|------|------|
| **서버** | `server.go`는 라우팅·핵심만; 핸들러는 `handlers_*.go`. 원격 JSON·URL은 `remotehttp.go`(`fetchRemoteAPI`, `joinRemoteAPIURL`). 원격 apply/rollback은 `remoteapply.go`. ELF·버전 키는 `agentbinary.go`. bulk body 파서는 `bulkhosts.go`. 일괄 호스트 목록 헬퍼는 `bulkRemoteHosts`. Discovery SSE 실패는 `writeDiscoverySSEFail`. |
| **웹 JS** | 단일 `app.js` → `web/js/` 모듈: `core` → `hosts` → `card-panels` → `card-apply` → `card-health` → `card` → `discovery` → `bulk` → `app` (`window.MolMaintenance`). `hosts.js`: IP 병합·`collectRemoteHostsFromDOM({ reachableOnly })`. |
| **웹 일괄** | push/restart도 apply/rollback과 같이 **도달 가능(reachable)** 호스트만 body·버튼 카운트. `GET …/versions/list` 응답 **`can_rollback`** 우선(구버전 에이전트는 클라이언트 폴백). |
| **CLI 일괄** | `bulkcli/flags.go` — 공통 `-apiprefix`/`-maintenance-port`·help(`WriteDiscoveryBuiltInHelp`). 4× `*clicli` + REPL bulk runner가 `bulkcli` 공유. help 문구: **명령 내부 UDP discovery**(standalone `--discovery` 선행 불필요). |
| **REPL** | Discovery 명령 **`discovery`**(CLI `--discovery`와 동일 철자). **`discover`** 는 하위 호환 별칭. |

**문서**: **PRD.md** §1.1·§4·§6·구현 표, **docs/CLI.md**, **docs/REPL.md**, **docs/REST_API.md**, **docs/OVERVIEW.md**, **README.md**.

## REPL·CLI 번들 경로 (2026-06)

| 항목 | 요약 |
|------|------|
| **`~` 확장** | 번들 업로드(`UploadBundle`·`UploadBundleMaintenance`)·REPL `apply-update` / `apply-update-all`에서 **`~/…` 경로를 홈 디렉터리로 확장** 후 `os.Open` (`clirest.ExpandLocalPath`) |
| **REPL Tab 경로 완성** | `apply-update`·`apply-update-all` — `./dist/…`, `~/…`, 절대 경로. 플래그(`-use-bundle-config` 등) 뒤에도 동작. **Tab 두 번** 후보 목록(bash 유사) |
| **CWD** | REPL·CLI 번들 상대 경로는 **프로세스 시작 시 CWD** 기준(셸에서 `agent` 실행한 디렉터리) |

구현: `maintenance/clirest/path.go`, `maintenance/replcli/complete.go`. **문서**: **docs/REPL.md**, **docs/CLI.md**.

## Agent CLI·REPL (2026-06)

한 세션에서 추가·정리된 agent 하위 기능.

| 항목 | 요약 |
|------|------|
| **리모트 일괄 CLI 4종** | `--push-config-all-remotes` 등 — 웹 §6.6과 동일 NDJSON API. Discovery 내부 1회. **docs/CLI.md** 「리모트 일괄 CLI」 |
| **Discovery IP 표시** | `--discovery`·`discovery`(REPL): 대괄호 IP = `host_ip`·`responded_from_ip` 병합(웹 **IP**). 대표 IP = **응답한 IP**. 일괄 `hosts[]`·`BulkPushHostsFromDiscovery` 동일 |
| **REPL** | **`contrabass-moleU agent`**(인자 없음) → `Mole-Agent>`. **`discovery`** 캐시 → bulk·`host-info` IP 보강(`discover` 별칭). TTY: readline(↑/↓ 히스토리, Tab 완성). `agent repl` 별칭 |
| **`--host-info`** | `HOST_IP`/`HOST_IPS` = 응답 IP / 발견 IP(UDP ~3s 보강). REPL은 **discovery 캐시** 우선 |
| **로컬 대상** | CLI·REPL에서 **`local`** = **`self`** (argv만; REST JSON은 `self`) |
| **코드 정리** | `discoverycli` 그룹/IP 헬퍼·`DiscoverToStdout` 공유; `replcli`; dead code 제거 |

구현: `maintenance/replcli`, `maintenance/discoverycli`, `maintenance/hostinfocli`, `*clicli` 패키지. **문서**: **docs/CLI.md**, **docs/REPL.md**, **PRD.md** §4.1·§4.1.2, **README.md**, **docs/REST_API.md**.

## CLI 리모트 일괄 명령 4종 (2026-06)

웹 UI §6.6 사이드바 4버튼과 동일 maintenance NDJSON API를 **`agent` CLI**로 제공한다. `contrabass-moleU agent --help` 에서는 **`--versions-*` 다음 맨 아래**에 나열된다.

| CLI | 요약 |
|-----|------|
| **`--push-config-all-remotes`** | UDP Discovery(기본값) → `POST …/current-config/push-local-all` |
| **`--restart-all-remotes`** | Discovery → `POST …/service-control/restart-all` |
| **`--apply-update-all-remotes <bundle>`** | `POST …/upload` → Discovery → `POST …/apply-update-all` |
| **`--rollback-all-remotes`** | Discovery → `POST …/versions/rollback-all` |

- **공통**: orchestrator **`-cfg`** 필수; **`-apiprefix`**·**`-maintenance-port`**(기본 8889); Discovery는 명령 **내부 1회**(dest 9999, src 9998, timeout 10s) — **`--discovery` 선행 불필요**; `hosts[]`는 메모리 병합(`BulkPushHostsFromDiscovery`).
- **코드**: `maintenance/bulkcli`(`Run`/`RunDiscovery`, **`flags.go`** 공통 플래그·help); `discoverycli`; `clirest`; `configpushclicli` / `restartallclicli` / `applyupdateallclicli` / `rollbackallclicli`.
- **`--apply-update-all-remotes`**: **`-agent-variant`**, **`-use-bundle-config`**(`--apply-update` 와 동일 의미).
- **문서**: **docs/CLI.md**(「리모트 일괄 CLI (공통)」), **docs/REST_API.md**, **PRD.md** §4.1.1·§6.6, **README.md**.

## CLI `--apply-update` 환경설정 재사용 (2026-06)

- **`agent --apply-update`**: 기본 **`reuse_previous_config: true`** — 대상 호스트 **current** `agent.local.yml` 재사용(웹 「이전버전의 환경설정 파일 재사용」 체크 기본 on과 동일). **`-use-bundle-config`** 지정 시 번들 config 적용.
- **문서**: **docs/CLI.md**, **PRD.md** §4.1·§5.5.3, **docs/REST_API.md**, **README.md**.

## 웹 UI · 일괄 원격 작업 (2026-06)

- **레이아웃**: **「모든 리모트 호스트 일괄 작업」** 섹션은 Discovery 옆이 아니라 **오른쪽 sticky 사이드바**(「업데이트」 패널 아래)에 둔다. 호스트 카드는 가운데 열, 업데이트·일괄 작업은 오른쪽 열.
- **일괄 버튼(4개)**: 「**로컬 설정을 리모트 호스트에 일괄 복사**」(`POST …/current-config/push-local-all`), 「**리모트 호스트 일괄 재시작**」(`POST …/service-control/restart-all`), 「**리모트 호스트에 일괄 업데이트 적용**」(`POST …/apply-update-all`), 「**리모트 호스트 일괄 롤백**」(`POST …/versions/rollback-all`). NDJSON per-host 결과, 공용 **「결과 보기」** 모달.
- **도달 가능 호스트**: push/restart/apply/rollback **모두** HTTP 헬스 dead·이번 Discovery 미응답 카드는 body·버튼 카운트에서 제외(`collectRemoteHostsFromDOM({ reachableOnly: true })`).
- **결과·상태 UX**: 버튼 클릭 **순서대로** 상태 줄이 `#bulk-remote-status-list`에 **추가**된다(고정 슬롯 없음). 상태 줄 접두는 짧은 라벨(**설정 복사**·**서비스 재시작**·**업데이트 적용**·**롤백**). **「결과 보기」** 와 **×** 는 같은 줄 오른쪽에 배치; × 클릭 시 해당 줄 전체 제거.
- **일괄 업데이트 적용(버튼)**: 로컬 `can_apply`가 아니라 **호스트별** `GET …/update-status?ip=` 의 `can_apply`로 판단. 스테이징 버전이 있고 **도달 가능한 원격 중 적용 가능 ≥1** 일 때만 활성. 라벨 **`(적용가능/전체)`** 표시. 확인 모달은 **오른쪽 업데이트 패널**의 「이전버전의 환경설정 파일 재사용」을 모든 원격에 동일 적용(카드별 체크박스 무시). 성공 호스트는 완료 후 **카드 자동 갱신**(host-info 폴링·패널 refresh).
- **일괄 롤백(버튼)**: 서버 `GET …/versions/list`의 **`can_rollback`**(또는 `is_current`·`is_previous` 비교 폴백). **도달 가능한 롤백 가능 호스트 ≥1** 일 때만 버튼 활성.
- **일괄 API 공통**: 호스트별 `StagingUpdateAvailable` / 롤백 가능 여부 미충족은 **`skipped`**(실패 아님). 진행 중 해당 버튼 **`N/M`**·비활성. `hosts` body = **화면 카드 1장 = 호스트 1대**(`primary_ip`, `hostname`, `cpu_uuid`, `ips[]`).
- **호스트 목록**: **`remoteregistry`**(volatile) + **`GET …/discovered-remotes`**; UI는 DOM 카드 목록을 body로 보냄(권장).
- **재시작 확인**: restart-all — 프록시 restart 후 **2s 대기·최대 45s** 폴링(`GET …/health` 또는 `service-status`의 `Active: active (running)`).
- **업데이트 기록**: bulk 완료 시 **`config push-all` / `service restart-all` / `apply-update-all` / `rollback-all finished`** 요약 1줄 append(`appendDeployHistory`).
- **`recent_rollback`**: 맨 아래 줄의 `failed=N` 카운트(일괄 요약)만으로는 롤백 경고를 띄우지 않음 — **`update … failed`**·**`rollback failed`** 등 실제 실패만.
- **Discovery 미응답**: run 완료 시 **이번 UDP run에 응답하지 않은 기존 원격 카드**에 「**이번 Discovery 미응답**」 배지·펼친 카드 안내 배너(카드는 유지). `discoveryfail`·시작 직후에는 이전 run 표시를 지우지 않음.
- **원격 조작 가드**: HTTP 헬스체크 실패·이번 Discovery 미응답 카드에서는 **「업데이트 적용」·「서비스 재시작」** 비활성(일괄 버튼의 도달 가능 판단에도 동일 규칙).
- **Discovery 진행 표시**: 상태 줄 `Discovery 진행 중… N초 (호스트 M개, …)` — 타임아웃은 `client-runtime`의 `discovery.timeoutSec`.
- **문서**: **PRD.md** §5.4.1·§5.5.3.1·§6.6, **docs/REST_API.md**.

## 웹 UI · 일괄 원격 config·재시작 (초기, 2026-06)

- 위 **「일괄 원격 작업 (2026-06)」** 절에 통합·확장됨. 초기에는 Discovery 아래 2버튼(config·restart)만 있었음.

## 문서 현행화 (2026-05)

- **업데이트 config 재사용**: `POST …/apply-update`의 **`reuse_previous_config`** — 적용 전 **`current` config**를 `versions/<키>/`에 복사(원격은 orchestrator가 `GET …/current-config` 주입). 웹 체크박스(스테이징 있을 때만, 로컬 패널·원격 카드 각각, 기본 on).
- **업데이트 기록 UI**: 로컬·원격 공통 tail 10·역순 표시·적용 중 2초 폴링; 원격 `update-log` 프록시 tail·`no-store` 정규화.
- **빌드**: `make build` — dual binary + **`strip`**, 루트 `./contrabass-moleU`는 **control** 복사(README·PRD §5.5.0).
- **CLI `--discovery`**: `version=<키> (control|compute)` (웹 UI와 동일).
- **CLI `--host-info`**: `BUILD_VARIANT` 행.
- **CLI `--apply-update`**: `-agent-variant` 생략 시 설치 variant 유지; **기본 `reuse_previous_config: true`**(웹과 동일), **`-use-bundle-config`** 시 번들 config; REST JSON 빈 `agent_variant`는 `compute`(PRD §5.5.3).
- **`update_history.log.lock`**: flock용 0바이트 파일, 업데이트 후 잔존 가능·다음 업데이트 차단 아님.
- **Ubuntu**: `bin/ubuntu/contrabass-agent-install.sh` / `contrabass-agent-uninstall.sh`.

## CLI `--discovery` build variant

- Result lines show variant like the web UI: **`version=<key> (control|compute)`** when `DISCOVERY_RESPONSE.build_variant` is set; otherwise `version=<key>` only (or `version=?`).

## CLI `--host-info` build variant

- **`BUILD_VARIANT`** row in tabular output (`control` / `compute`, or `-` if unknown).
- **`self`**: same resolution as `GET /self` — installed `current` binary `--version` suffix, else CLI ldflags `BuildVariant`.
- **`DISCOVERY_RESPONSE`**: `build_variant` field already used by HTTP/discovery; PRD §3.4 example updated.

## Ubuntu install/uninstall scripts (`bin/ubuntu/`)

- **Rename**: `contrabass-mole-new-install.sh` → **`contrabass-agent-install.sh`** (behavior unchanged).
- **Uninstall**: **`contrabass-agent-uninstall.sh`** — root, no args; stops/disables `contrabass-mole.service`, removes unit file, deletes `/var/lib/contrabass/mole` and `/var/log/contrabass/mole`.
- Documented in **README.md**, **PRD.md §5.5.0.1–§5.5.0.2**.

## Greenfield install script (`bin/ubuntu/contrabass-agent-install.sh`)

- **Ubuntu initial install**: manifest v2 tar.gz → `versions/<version-key>/`, `current`, `staging/`, materialize `control|compute` → `contrabass-moleU`, systemd `contrabass-mole.service`.
- Version key from binary **`agent --version`** (not tarball filename). Optional manifest SHA256 when `sha256sum` is available. English messages; root required (`id -u`); prints Usage when not root or wrong args.
- Documented in **README.md**, **PRD.md §5.5.0.1**.

## Maintenance API messages (English)

- **`maintenance/server`**: JSON `data` strings for apply-update, switch-current, upload/remove, remote proxy errors, update-log empty text, etc. are **English** (CLI and web consume the same API).
- **`versionsapi`**: errors returned through apply/switch paths are English.

## CLI · REST 전환 (`-cfg` 제거, `-apiprefix`)

- **`--host-info`**, **`--apply-update`**, **`--versions-list`**, **`--versions-switch`**: 설정 파일 **`-cfg` 제거**. 대상 에이전트 **Gin(기본 8888)** 의 **`{APIPrefix}`** REST만 사용(`maintenance/clirest`). **`-apiprefix`** 선택(기본 **`/maintenance/api/v1`**). 대상 **`self`/원격 IP** 서비스 미기동 시 `agent service is not running at …` (영문).
- **서비스 기동** **`<bin> -cfg <file>`** / **`agent -cfg <file>`** 는 변경 없음.

## 유지보수 웹 UI · 업데이트 기록

- **로그/목록 버튼**: 업데이트 기록 블록 **「로그 새로고침」**, 설치된 버전 블록 **「목록 새로고침」**.
- **로컬 적용·로컬 switch-current**: `update-log` **2초 자동 갱신** — 이번 run의 `started` 확인 후 **마지막 줄** `success`/`failed`까지. `/self` 폴링과 **분리**; 진행 중 패널 일괄 갱신은 기록 fetch 생략. 요청은 캐시 무효화(`no-store`, `&_=`). 로그 tail 10줄 **역순 표시**(최신이 위).
- **적용 버튼**: `can_apply` 시 **초록색** 스타일.
- **switch-current (로컬)**: `apply-update`와 동일하게 버전 트리 준비·`MaterializeCanonicalAgent` 후 `update.sh`.
- **`update_history.log`**: `append_history`에 **`flock`**(`update_history.log.lock`). lock 파일은 0바이트로 남을 수 있으나 잠금은 프로세스 종료 시 해제되어 다음 업데이트를 막지 않는다.

## update.sh · rollback 경로·헬스 대기

- **헬스 기본값 확대**: 느린 병합 바이너리 기동 오탐 롤백 방지(기본 HTTP 재시도 약 6분, service active 약 90초). `HEALTH_*`·`SERVICE_ACTIVE_*` 환경 변수로 조정.

## Web UI · update.sh 헬스체크

- **CLI `--apply-update` `-agent-variant`**: 생략 시 웹 UI와 같이 적용 대상의 설치된 `build_variant`를 따름(self: `current` 바이너리 `--version`, remote: `GET …/self`; 미상이면 `compute`). **`-use-bundle-config`** 로 번들 config 적용; 생략 시 **`reuse_previous_config: true`**.

- **원격 「업데이트 적용」**: `GET …/update-status?ip=`의 `can_apply`가 확정되면 업로드 파일 선택만으로 버튼을 켜지 않음. 적용 성공·실패·host-info 폴링 후 해당 IP에 대해 `update-status`를 재조회해 `AllowSameVersionUpdate: false`일 때 원격이 스테이징과 같으면 적용 버튼·variant 라디오를 비활성·숨김.
- **Agent variant (웹)**: 로컬·리모트 라디오 기본값 = 설치된 `build_variant`(`data-build-variant`). 리모트 variant는 적용 버튼이 활성일 때만 표시.
- **update.sh 헬스**: `systemctl is-active`·`GET /version` 재시도(환경 변수로 조정 가능). `invoke_rollback`으로 롤백 성공/실패 로그 구분. 루트·`maintenance/updatescripts/` 동기화.
- **Makefile**: `make build` 후 **`strip`**, **`contrabass-moleU-control`** → `./contrabass-moleU` 복사.

## Manifest v2 · Agent Variant (dual-binary)

- **Manifest v2**: 배포 번들이 `manifestVersion: 2`를 지원한다. `agent_control`·`agent_compute` 두 바이너리를 각각의 `path`·`sha256`으로 선언하여 번들에 포함. 레거시 `manifestVersion: 1`(단일 `agent`)도 계속 지원.
- **BuildVariant 주입**: `Makefile`이 `go build`를 두 번 수행하여 `-X main.BuildVariant=control`·`compute`를 각각 주입. `contrabass-moleU --version` 출력에 `(control)` / `(compute)` 표시. variant 미지정 빌드는 빈 문자열(정상 동작).
- **Agent Variant 선택**: 적용(`apply-update`) 시 `agent_variant` 파라미터로 어떤 바이너리를 `contrabass-moleU`(BinaryName)로 설치할지 결정. 기본값 `compute`.
  - **웹 UI**: 로컬 패널·각 리모트 카드에 라디오 버튼. 스테이징에 dual agent가 있을 때만 표시.
  - **CLI**: `--apply-update -agent-variant=compute|control` (생략 시 설치된 `build_variant` 따름). **`reuse_previous_config`**: 기본 true; **`-use-bundle-config`** 시 false.
  - **REST**: `POST …/apply-update` JSON/multipart `agent_variant`·`reuse_previous_config` 필드.
- **MaterializeCanonicalAgent** (`server/agentvariant.go`): 선택된 variant를 canonical `contrabass-moleU`로 복사. 스테이징 시점에는 canonical 파일 미생성, 적용 시점에만 수행.
- **AllowSameVersionUpdate**: `agent.local.yml`의 `AllowSameVersionUpdate: true`로 동일 버전 재적용 허용 (기본 false).
- **pack-agent-tarball.sh**: manifest v2 번들 생성. control·compute·config SHA-256. 기본 출력 파일명은 두 바이너리 **`agent --version`** 키(일치 필수), `build-version.sh` 미사용.
- **버전 키 검증**: `--version` 출력의 `(control)` / `(compute)` 접미사를 검증 시 제거.
- **UI Build Variant 표시**: 로컬·리모트 호스트 카드에 현재 실행 중인 variant를 badge로 표시.
- **관련 파일**: `maintenance/appmeta/agentvariant.go`, `maintenance/versionsapi/staging.go`, `maintenance/server/agentvariant.go`.

## 레이아웃

- **`build/`**: **`build/build.sh`** 가 루트에서 `make "$@"` 를 호출한다. **`make build`** 산출: **`build/image/contrabass-moleU-control`**·**`contrabass-moleU-compute`**(+ **`strip`**), 루트 **`./contrabass-moleU`** 는 control 복사본.
- **`cfg/`**: 예시 에이전트 설정 **`agent.local.yml`** (`pack-agent-tarball.sh` 기본 `--config` 소스 경로).
- **`maintenance/`**: `maintenance.go`에 **`Run(binVersion, args []string) int`**(서비스·CLI 진입; `args`는 보통 `os.Args`), `discovery`, `discoverycli`(`--discovery`), `applycli`, `versionscli`(`--versions-list` / `--versions-switch`), **`cliutil`**(CLI 공용: 원격 Gin URL·`APIPrefix`·TCP 확인), `versionsapi`(로컬 `versions/`·로컬 switch/apply 공통), `hostinfoapi`, `hostinfocli`(`--host-info`), `hostinfo`, `server`(HTTP·`applylocal` 로컬 번들 스테이징), `svcstatus`, `web` 패키지가 여기에 있다. 루트 Gin용 **`GinProxyConfig`** 는 **`ginproxy_config.go`**, 바깥 Gin→maintenance HTTP 프록시 구현은 **`ginproxy/`** 서브패키지이며 **`RegisterMaintenanceProxy`** 는 `maintenance` 패키지가 `ginproxy` 로 브리지한다(**임베드 시 `contrabass-agent/maintenance` import 하나**로 `GinProxyConfig`·`RegisterMaintenanceProxy` 사용). **`maintenance/scripts/`**·**`maintenance/packaging/`**(빌드·번들 보조). Go import는 `contrabass-agent/maintenance/<패키지>` 형태.
- **루트 `main.go`**: **`<bin> -cfg …`**(`IsServiceModeRootCfg`)일 때만 **`MyGIN()`** + **`router.Run(Server.HTTPPort)`**(메인 고루틴) + **`go func() { os.Exit(maintenance.Run(…)) }()`**; **`<bin> agent …`** 전체는 Gin 없이 **`os.Exit(Run(…))`** 만. **시그널**(`SIGINT`/`SIGTERM`)은 **`maintenance.runServiceWithConfigPath`** 의 `signal.Notify`만 — **`main`은 시그널 미등록**. 바깥 Gin JSON API는 **`routerGroupJSON`** 그룹에만 `Content-Type: application/json`(전역 미들웨어는 maintenance UI MIME 깨짐).
- **`maintenance/agentcfg/`**: YAML 설정 로드·검증(`Config`, `Load`, `LoadFromBytes` 등). 구현 파일은 `maintenance_config.go`. **`ClampMaxUploadBytes`** 로 업로드/번들 크기 한도를 서버와 apply CLI가 공유. Go import는 **`contrabass-agent/maintenance/agentcfg`** (패키지명 **`agentcfg`**).

## Discovery / CLI (최근)

- **에이전트 CLI**: HTTP·Discovery **서비스**는 **`contrabass-moleU -cfg /path/to/agent.local.yml`** 또는 **`contrabass-moleU agent -cfg /path/to/agent.local.yml`** 로 기동한다(`Run` 동작 동일). 그 외 Discovery·host-info 등은 **`agent`** 다음에 옵션(예: `contrabass-moleU agent --discovery`).
- **argv·Gin**: 바깥 Gin 은 **`IsServiceModeRootCfg`**(`<bin> -cfg …`)일 때만 `main`에서 `router.Run`. **`<bin> agent …`** 는 **`IsAgentSubcommand`** 로 먼저 분기되어 **Gin 없음**(`agent -cfg` 서비스 포함). **`IsServiceModeAgentCfg`** 는 `Run`·`ConfigPathForServiceMode` 판별용.

### 유지보수 REST 대응 CLI (`agent` + `-cfg` 등)

- **`--versions-list`**: **`self`** 는 **`versionsapi`** 로 디스크만 읽음; **원격 IP** 는 **`http://<ip>:Server.HTTPPort` + `APIPrefix` + `GET …/versions/list`** (대상 Gin에 직접, 로컬 에이전트·maintenance 불필요). `maintenance/versionscli`, 공용 주소·TCP 확인은 **`cliutil`**.
- **`--versions-switch`**: **`self`** 는 **`versionsapi.RunSwitchCurrentWithRoots`**(로컬 maintenance HTTP 불필요, 보통 sudo); **원격** 은 동일 Gin에 `POST …/versions/switch-current`. `maintenance/versionscli`.
- **`--host-info`**: `GET …/host-info` 와 동일 규칙 — `self`는 로컬 hostinfo, 원격은 UDP 유니캐스트; **로컬 maintenance HTTP 불필요**. 핵심 로직은 **`maintenance/hostinfoapi`** 에서 HTTP 핸들러와 공유. `maintenance/hostinfocli`.
- 위 CLI는 **`APIPrefix`**·**`Server.HTTPPort`**(원격 호출 시) 등을 설정 YAML에서 읽는다. `-h` 옵션 나열 순서에서 **`--host-info`** 는 **`--version`과 `--nic-brd` 사이**.

### Discovery 유니캐스트(멀티홈)

- **`DoDiscoveryUnicast`**: 응답의 `host_ip`가 유니캐스트 목적지 IP와 다를 수 있음(동일 호스트·다중 NIC). **`request_id`로만** 응답을 매칭하고 `host_ip` 문자열 일치를 요구하지 않는다.

### `mol --apply-update` (번들 한 번에 검증·적용)

- **`-cfg`**, **`[-agent-variant=compute|control]`**, **`<self|remote-ip>`**, **`<bundle.tar.gz>`** — **로컬 maintenance(8889) 불필요.**
- `-agent-variant`: manifest v2 번들에서 canonical name으로 설치할 variant 선택. 생략 시 대상 호스트의 설치 variant (`build_variant`) 따름.
- 번들은 서버와 동일하게 임시 풀기·검증 후 **`StagingUpdateAvailable`** 으로만 진행. `AllowSameVersionUpdate: true`이면 동일 버전도 허용. **self** 의 "현재 버전" 비교는 **`DeployBase/current` 심볼릭** 우선.
- **self**: **`ApplyUpdateSelfFromBundleExtract`** 로 `MaterializeCanonicalAgent`(variant→`BinaryName` 복사) + 스테이징 후 **`RunSwitchCurrentWithRoots`**.
- **remote**: **`http://<ip>:Server.HTTPPort` + `APIPrefix` + `POST …/apply-update`** multipart(`ip`, `bundle`, `agent_variant`) — 요청은 **원격 Gin**에서 처리(§5.5.3).
- **업로드 한도**: **`config.ClampMaxUploadBytes`** 를 서버와 공유.
- **CLI 출력**: 영문(로캘 없는 OS 대비). 원격 주소·TCP 확인은 **`cliutil`**.

### `mol --discovery` (설정 파일 없이 UDP Discovery만)

- **`reply_udp_port`**: `DISCOVERY_REQUEST` JSON에 로컬 바인드 포트를 넣고, 원격은 응답을 **그 포트**로 유니캐스트한다. UDP 소스 포트가 잘못 보이는 환경에서도 동작하도록 함.
- **다중 NIC**: 서비스 mol과 동일하게, brd 서브넷에 맞춰 **인터페이스별 `로컬IP:src-port` UDP 소켓**을 열어 브로드캐스트를 보냄 (`discovery.OpenDiscoveryClientUDP`, `SendDiscoveryClientBroadcast`).
- **시작 시 출력**: 사용하는 **브로드캐스트(brd) 주소**를 모두 한 줄씩 출력.
- **결과**: `[...]` 안에는 **응답한 IP**(`responded_from_ip`, UDP 패킷 실제 발신지)만 표시. 줄 끝 **`version=<키> (control|compute)`** — variant는 웹 UI와 같이 버전 뒤 괄호(없으면 `version=<키>`만, 버전 없으면 `version=?`).
- **`[Local]` / `[Remote]`**: (1) 로컬 `hostinfo`의 CPU UUID와 응답 `cpu_uuid` 일치(대소문자 무시) → Local. (2) 아니면 **응답한 IP**가 이 머신의 IPv4와 겹치면 Local(보조).
- **UX**: 같은 줄 `Discovering ... N` 카운트다운 후 `Discovery Done.`(이전 줄 덮어쓰기).

### 서비스 (HTTP + Discovery)

- Discovery UDP listen을 **`udp4`**로 통일(IPv4 sockaddr 일관성).
- **`LocalIPsInSubnet`** export, 브로드캐스트 송신 시 매칭 소켓 없으면 `conns[0]` 폴백.
- UDP **`DISCOVERY_RESPONSE`에는 `host_ips` 배열을 넣지 않음**(HTTP `/self` 등에서만).

### 웹·로그·배포 (최근)

- **원격 카드 「업데이트 적용」**: **`GET /update-status?ip=`** 의 `can_apply` / `apply_version` 사용(서버 `StagingUpdateAvailable` 과 일치). 스테이징 최신 디렉터리명만으로 카드 버전과 문자열 비교하지 않음.
- **SSE** `event: discoveryfail` + `data.message`, 실패 시 **`discovery: ERROR:`** 한 줄 로그(`journalctl` 검색).
- **DISCOVERY_REQUEST** JSON은 마샬 후 **1300바이트 미만** 검증(UDP·MTU).
- **`maintenance/updatescripts/`** 에 `update.sh`·`rollback.sh` 임베드(`Makefile` 동기화), 배포는 `{base}/current/` 스크립트 실행.
- 버전 키: 빌드 시 **`main.VersionKey`**(`Makefile`·`maintenance/scripts/build-version.sh`); 업로드 시 바이너리 **`--version`**; `agent.local.yml`에서는 버전 제거.
- 저장소 정책: Go **`*_test.go`** 는 트리에 두지 않음(상세는 PRD §1).

상세 스펙은 **[PRD.md](PRD.md)** §3, CLI 사용은 **[README.md](README.md)** 를 참고한다.

## 명명·업데이트 유닛 (최근)

- 실행 파일명 **`contrabass-moleU`** (`maintenance/appmeta.BinaryName`), 상시 유닛 **`contrabass-mole.service`**, `systemd-run` 임시 업데이트 유닛 **`contrabass-mole-update.service`** (`appmeta.UpdateTransientUnit*`).
- 업로드 multipart 필드는 **`bundle`**(tar.gz 아카이브). 디스크상 바이너리명은 **`contrabass-moleU`**.
- 설정: **`MOL_CONFIG` 미사용** — 서비스는 **`-cfg`**(첫 인자)로 경로 지정; `config.Load("")` 시 현재 디렉터리 `agent.local.yml`.
- Discovery 기본 서비스명 **`Mole-Discovery`** (`DefaultDiscoveryServiceName`).
- PRD **§12** 표에 요약.
