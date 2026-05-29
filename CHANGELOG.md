# 변경 이력 (mol)

## 유지보수 웹 UI · 업데이트 기록

- **로그/목록 버튼**: 업데이트 기록 블록 **「로그 새로고침」**, 설치된 버전 블록 **「목록 새로고침」**.
- **로컬 적용·로컬 switch-current**: `update-log` **2초 자동 갱신** — 이번 run의 `started` 확인 후 맨 위 줄 `success`/`failed`까지. `/self` 폴링과 **분리**; 진행 중 패널 일괄 갱신은 기록 fetch 생략. 요청은 캐시 무효화(`no-store`, `&_=`).
- **적용 버튼**: `can_apply` 시 **초록색** 스타일.
- **switch-current (로컬)**: `apply-update`와 동일하게 버전 트리 준비·`MaterializeCanonicalAgent` 후 `update.sh`.
- **`update_history.log`**: `prepend_history`에 **`flock`**(`.lock` 파일).

## update.sh · rollback 경로·헬스 대기

- **헬스 기본값 확대**: 느린 병합 바이너리 기동 오탐 롤백 방지(기본 HTTP 재시도 약 6분, service active 약 90초). `HEALTH_*`·`SERVICE_ACTIVE_*` 환경 변수로 조정.

## Web UI · update.sh 헬스체크

- **CLI `--apply-update` `-agent-variant`**: 생략 시 웹 UI와 같이 적용 대상의 설치된 `build_variant`를 따름(self: `current` 바이너리 `--version`, remote: `GET …/self`; 미상이면 `compute`).

- **원격 「업데이트 적용」**: `GET …/update-status?ip=`의 `can_apply`가 확정되면 업로드 파일 선택만으로 버튼을 켜지 않음. 적용 성공·실패·host-info 폴링 후 해당 IP에 대해 `update-status`를 재조회해 `AllowSameVersionUpdate: false`일 때 원격이 스테이징과 같으면 적용 버튼·variant 라디오를 비활성·숨김.
- **Agent variant (웹)**: 로컬·리모트 라디오 기본값 = 설치된 `build_variant`(`data-build-variant`). 리모트 variant는 적용 버튼이 활성일 때만 표시.
- **update.sh 헬스**: `systemctl is-active`·`GET /version` 재시도(환경 변수로 조정 가능). `invoke_rollback`으로 롤백 성공/실패 로그 구분. 루트·`maintenance/updatescripts/` 동기화.
- **Makefile**: `make build` 후 `contrabass-moleU-compute` → `./contrabass-moleU` 복사.

## Manifest v2 · Agent Variant (dual-binary)

- **Manifest v2**: 배포 번들이 `manifestVersion: 2`를 지원한다. `agent_control`·`agent_compute` 두 바이너리를 각각의 `path`·`sha256`으로 선언하여 번들에 포함. 레거시 `manifestVersion: 1`(단일 `agent`)도 계속 지원.
- **BuildVariant 주입**: `Makefile`이 `go build`를 두 번 수행하여 `-X main.BuildVariant=control`·`compute`를 각각 주입. `contrabass-moleU --version` 출력에 `(control)` / `(compute)` 표시. variant 미지정 빌드는 빈 문자열(정상 동작).
- **Agent Variant 선택**: 적용(`apply-update`) 시 `agent_variant` 파라미터로 어떤 바이너리를 `contrabass-moleU`(BinaryName)로 설치할지 결정. 기본값 `compute`.
  - **웹 UI**: 로컬 패널·각 리모트 카드에 라디오 버튼. 스테이징에 dual agent가 있을 때만 표시.
  - **CLI**: `--apply-update -agent-variant=compute|control` (생략 시 설치된 `build_variant` 따름).
  - **REST**: `POST …/apply-update` JSON/multipart `agent_variant` 필드.
- **MaterializeCanonicalAgent** (`server/agentvariant.go`): 선택된 variant를 canonical `contrabass-moleU`로 복사. 스테이징 시점에는 canonical 파일 미생성, 적용 시점에만 수행.
- **AllowSameVersionUpdate**: `agent.local.yml`의 `AllowSameVersionUpdate: true`로 동일 버전 재적용 허용 (기본 false).
- **pack-agent-tarball.sh**: manifest v2 기반 번들 생성. control·compute·config 세 파일의 SHA-256을 각각 계산.
- **버전 키 검증**: `--version` 출력의 `(control)` / `(compute)` 접미사를 검증 시 제거.
- **UI Build Variant 표시**: 로컬·리모트 호스트 카드에 현재 실행 중인 variant를 badge로 표시.
- **관련 파일**: `maintenance/appmeta/agentvariant.go`, `maintenance/versionsapi/staging.go`, `maintenance/server/agentvariant.go`.

## 레이아웃

- **`build/`**: **`build/build.sh`** 가 루트에서 `make "$@"` 를 호출한다. **`make`** 기본 산출 바이너리는 **`build/image/contrabass-moleU`** (`Makefile` 의 `OUTPUT_DIR`·`BINARY`).
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
- **결과**: `[...]` 안에는 **응답한 IP**(`responded_from_ip`, UDP 패킷 실제 발신지)만 표시. 줄 끝에 **`version=<DISCOVERY_RESPONSE.version>`**(에이전트 버전 키).
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
