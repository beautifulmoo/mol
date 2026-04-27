# 변경 이력 (mol)

## 레이아웃

- **`maintenance/`**: `maintenance.go`에 **`Run(binVersion, args []string) int`**(서비스·CLI 진입; `args`는 보통 `os.Args`), `discovery`, `discoverycli`(`--discovery`), `applycli`, `versionscli`(`--versions-list` / `--versions-switch`), **`cliutil`**(CLI 공용: 원격 Gin URL·`APIPrefix`·TCP 확인), `versionsapi`(로컬 `versions/`·로컬 switch/apply 공통), `hostinfoapi`, `hostinfocli`(`--host-info`), `hostinfo`, `server`(HTTP·`applylocal` 로컬 번들 스테이징), `svcstatus`, `web` 패키지가 여기에 있다. **`maintenance/scripts/`**·**`maintenance/packaging/`**(빌드·번들 보조), 루트 **`main.go`** 는 `maintenance.Run(Version, os.Args)` 후 **`os.Exit`** 만 수행한다. Go import는 `contrabass-agent/maintenance/<패키지>` 형태.
- **`maintenance/config/`**: YAML 설정 로드·검증(`Config`, `Load`, `LoadFromBytes` 등). 구현 파일은 `maintenance_config.go`. **`ClampMaxUploadBytes`** 로 업로드/번들 크기 한도를 서버와 apply CLI가 공유. Go import는 `contrabass-agent/maintenance/config`.

## Discovery / CLI (최근)

- **에이전트 CLI**: HTTP·Discovery **서비스**는 **`contrabass-moleU -cfg /path/to/config.yaml`**(첫 인자 `-cfg`; 레거시 `agent -cfg` 허용). 그 외 Discovery·host-info 등은 **`agent`** 다음에 옵션(예: `contrabass-moleU agent --discovery`).

### 유지보수 REST 대응 CLI (`agent` + `-cfg` 등)

- **`--versions-list`**: **`self`** 는 **`versionsapi`** 로 디스크만 읽음; **원격 IP** 는 **`http://<ip>:Server.HTTPPort` + `APIPrefix` + `GET …/versions/list`** (대상 Gin에 직접, 로컬 에이전트·maintenance 불필요). `maintenance/versionscli`, 공용 주소·TCP 확인은 **`cliutil`**.
- **`--versions-switch`**: **`self`** 는 **`versionsapi.RunSwitchCurrentWithRoots`**(로컬 maintenance HTTP 불필요, 보통 sudo); **원격** 은 동일 Gin에 `POST …/versions/switch-current`. `maintenance/versionscli`.
- **`--host-info`**: `GET …/host-info` 와 동일 규칙 — `self`는 로컬 hostinfo, 원격은 UDP 유니캐스트; **로컬 maintenance HTTP 불필요**. 핵심 로직은 **`maintenance/hostinfoapi`** 에서 HTTP 핸들러와 공유. `maintenance/hostinfocli`.
- 위 CLI는 **`APIPrefix`**·**`Server.HTTPPort`**(원격 호출 시) 등을 설정 YAML에서 읽는다. `-h` 옵션 나열 순서에서 **`--host-info`** 는 **`--version`과 `--nic-brd` 사이**.

### Discovery 유니캐스트(멀티홈)

- **`DoDiscoveryUnicast`**: 응답의 `host_ip`가 유니캐스트 목적지 IP와 다를 수 있음(동일 호스트·다중 NIC). **`request_id`로만** 응답을 매칭하고 `host_ip` 문자열 일치를 요구하지 않는다.

### `mol --apply-update` (번들 한 번에 검증·적용)

- **`-cfg`**, **`<self|remote-ip>`**, **`<bundle.tar.gz>`** — **로컬 maintenance(8889) 불필요.**
- 번들은 서버와 동일하게 임시 풀기·검증 후 **`StagingUpdateAvailable`** 으로만 진행. **self** 의 “현재 버전” 비교는 **`DeployBase/current` 심볼릭** 우선(개발용 CLI 빌드 키와 배포 트리 불일치 방지).
- **self**: **`ApplyUpdateSelfFromBundleExtract`** 로 스테이징 후 **`RunSwitchCurrentWithRoots`**(웹 `POST /upload` + 로컬 적용과 동등; 배포 경로 쓰기·`systemd-run` 은 보통 sudo).
- **remote**: **`http://<ip>:Server.HTTPPort` + `APIPrefix` + `POST …/apply-update`** multipart(`ip`, `bundle`) — 요청은 **원격 Gin**에서 처리(§5.5.3).
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
- 버전 키: 빌드 시 **`main.VersionKey`**(`Makefile`·`maintenance/scripts/build-version.sh`); 업로드 시 바이너리 **`--version`**; `config.yaml`에서는 버전 제거.
- 저장소 정책: Go **`*_test.go`** 는 트리에 두지 않음(상세는 PRD §1).

상세 스펙은 **[PRD.md](PRD.md)** §3, CLI 사용은 **[README.md](README.md)** 를 참고한다.

## 명명·업데이트 유닛 (최근)

- 실행 파일명 **`contrabass-moleU`** (`maintenance/appmeta.BinaryName`), 상시 유닛 **`contrabass-mole.service`**, `systemd-run` 임시 업데이트 유닛 **`contrabass-mole-update.service`** (`appmeta.UpdateTransientUnit*`).
- 업로드 multipart 필드 **`agent`** / `config`; 레거시 디스크상 `mol` 바이너리명 제거.
- 설정: **`MOL_CONFIG` 미사용** — 서비스는 **`-cfg`**(첫 인자)로 경로 지정; `config.Load("")` 시 현재 디렉터리 `config.yaml`.
- Discovery 기본 서비스명 **`Mole-Discovery`** (`DefaultDiscoveryServiceName`).
- PRD **§12** 표에 요약.
