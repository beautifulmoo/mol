# Contrabass agent — CLI 명세

루트 `main.go`는 argv를 나눈 뒤 **`maintenance.Run(main.VersionKey, os.Args)`** 의 반환값으로 **`os.Exit`** 한다. 실제 명령 분기는 **`maintenance/maintenance.go`** 의 `Run`에 있다.

| argv | `main` 동작 | Gin (`Server.HTTPPort`) |
|------|-------------|-------------------------|
| **`<bin> agent …`** (`IsAgentSubcommand`, `agent -cfg` 포함) | `os.Exit(Run(…))` | 없음 |
| **인자 없음**, 루트 **`--version`**, 잘못된 argv (`!IsServiceModeRootCfg`) | `os.Exit(Run(…))` | 없음 |
| **`<bin> -cfg <파일>`** (`IsServiceModeRootCfg`) | `go func() { os.Exit(Run(…)) }()` + 메인에서 **`router.Run`** | 있음 (`RegisterMaintenanceProxy`) |

**HTTP·Discovery 서비스**는 **`<bin> -cfg …`** 또는 **`<bin> agent -cfg …`** 로 기동한다(`Run` 동작 동일). **Discovery·host-info·apply-update 등**은 **`agent` 다음** 옵션만(실행 후 셸 복귀). 예: `contrabass-moleU agent --discovery -h`.

**시그널**: `SIGINT`/`SIGTERM`은 **`maintenance.runServiceWithConfigPath`** 가 `signal.Notify`로 처리한다. **`main`은 시그널 핸들러를 등록하지 않는다.** `<bin> -cfg …` 에서 Ctrl+C 시 maintenance가 내려가고 `Run`이 끝나면, 고루틴의 **`os.Exit(Run(…))`** 로 프로세스(Gin 포함)가 종료된다.

**병합 호스트**: 바깥 Gin에 **전역** `Content-Type: application/json` 미들웨어를 두면 `/maintenance` UI CSS가 깨진다. JSON API만 **`routerGroupJSON`**(루트 `main.go`)처럼 **라우트 그룹**에 적용한다.

`ConfigPathForServiceMode`는 **`<bin> -cfg …`** 와 **`<bin> agent -cfg …`** 에서 설정 경로를 돌려준다.

저장소에서는 예시 설정 파일을 **`cfg/agent.local.yml`** 에 둔다(`maintenance/scripts/pack-agent-tarball.sh` 기본 config 소스).

실행 파일 표시명은 **`maintenance/appmeta.BinaryName`** (기본 **`contrabass-moleU`**).

---

## 공통

| 항목 | 설명 |
|------|------|
| **종료 코드** | 성공 **`0`**, 실패 **`1`**. `maintenance`·`discoverycli`·`applycli`·`versionscli`·`hostinfocli` 패키지는 **`os.Exit`를 호출하지 않고** 상위 `main`이 `os.Exit` 한다. |
| **도움말·API 언어** | `-h` / `--help` 본문 및 **`--apply-update`**, **`--versions-list`**, **`--versions-switch`**, **`--host-info`** 관련 **CLI 진단 메시지**는 **영문**이다. 원격 호출 시 stdout에 찍히는 성공/실패 문구는 **원격 에이전트 REST API**의 `data` 문자열(영문)을 그대로 출력한다. `--discovery` 도움말도 영문. |
| **버전 출력** | **권장**: **`contrabass-moleU agent --version`** 또는 **`agent -version`** / **`agent --version`** — 빌드 시 주입된 **`main.VersionKey`** 와 `BinaryName` 한 줄. `BuildVariant`가 주입된 경우 **`contrabass-moleU 0.4.4-test (compute)`** 형태로 variant 접미사가 붙는다. **전환용**: 루트 **`contrabass-moleU --version`** / **`-version`** 도 동일 한 줄을 출력한다(구 업데이트 스크립트 호환; PRD §4.1·§9). 설정 파일 불필요. |

---

## 인자 없음

```text
contrabass-moleU
```

버전 안내와 **`-cfg <파일>`**(서비스)·**`agent --help`**(기타 CLI) 안내를 출력하고 종료한다. HTTP·Discovery는 시작하지 않는다.

---

## 서비스 모드 (HTTP + Discovery)

```text
contrabass-moleU -cfg /path/to/agent.local.yml
```

설정을 로드한 뒤 **maintenance HTTP**(`Maintenance.MaintenanceListenAddress`:`Maintenance.MaintenancePort`)와 **UDP Discovery** 등을 기동한다. 상세는 **[PRD.md](../PRD.md)** §1·§2·§7.

**`<bin> -cfg …` 만** 루트 Gin이 함께 뜬다: 브라우저는 `http://<host>:Server.HTTPPort` + `WebPrefix`(예: `/maintenance/`)로 접속하고, API는 같은 origin의 `APIPrefix`로 프록시된다. **`<bin> agent -cfg …`** 는 maintenance·Discovery만(Gin 없음).

종료: 서비스 프로세스는 **Ctrl+C / SIGTERM** 시 maintenance 쪽에서 HTTP `Shutdown` 후 `Run`이 반환한다. `<bin> -cfg …` 진입에서는 위 고루틴 **`os.Exit`** 로 Gin 리스너까지 프로세스가 끝난다.

참고: **업로드 번들**(`POST /upload`)로 스테이징/설치 트리에 저장되는 파일명은 고정값이 아니라, 번들 manifest의 basename을 따른다.
- config: `config.path` basename (예: `config.path: ./agent.local.yml` → 디렉터리에도 `agent.local.yml`)
- agent: `agent.path` basename (단, deploy 트리 내부 로직은 `appmeta.BinaryName` 실행 파일을 기준으로 동작하므로 basename이 다르면 같은 바이너리를 `appmeta.BinaryName`로 한 번 더 저장한다)

- **`-cfg`** 인데 경로가 없으면 stderr에 안내 후 종료 코드 `1`.
- **`contrabass-moleU agent -cfg /path/to/agent.local.yml`** 는 위 **`<bin> -cfg …`** 와 동일하게 서비스(HTTP·Discovery) 기동으로 처리한다.

---

## 버전 출력 (`--version` / `-version`)

| 호출 | 설명 |
|------|------|
| **`contrabass-moleU agent --version`** | 권장. 다른 `agent` 하위 명령과 동일한 접두. |
| **`contrabass-moleU --version`** / **`-version`** | 루트 플래그만으로 한 줄 출력(전환용). `agent` 없이 실행 가능. |

**업로드·번들 검증**(서버·`apply-cli` 등): 스테이징·번들 내 바이너리에 대해 **`--version`** 실행을 먼저 시도하고, 실패하면 **`agent --version`** 을 시도한다(`maintenance/server.VersionKeyFromAgentBinary`). 출력은 **`<BinaryName> <버전 키>`** 한 줄이어야 한다. `BuildVariant` 접미사(예: `(control)`, `(compute)`)가 있으면 검증 시 자동으로 제거한다.

---

## `-h` / `--help`

표준 도움말 출력(영문). 서비스 미기동. 아래 **개별 명령 절 순서**는 **`contrabass-moleU agent --help`** 에 나오는 옵션 순서와 같다(`--version` 다음 **`--host-info`**, 그다음 **`--nic-brd`** …).

---

## `--host-info`

**로컬 maintenance HTTP는 필요 없다.** 동작은 **`GET …/host-info`** 와 같은 규칙으로, 공유 패키지 **`maintenance/hostinfoapi`** 에서 서버 핸들러와 같은 핵심 로직을 쓴다.

- **`self`**: 로컬 `hostinfo`와 설정(버전 문자열·`MaintenancePort`·`DiscoveryServiceName` 등)으로 **`GET /self`** 와 동일한 `DISCOVERY_RESPONSE` 형을 만든다. `VERSION` 은 빌드 시 주입된 **`main.VersionKey`**(인자 없을 때 `0.0.0-0`)를 쓴다.
- **원격 IP**: **`OpenDiscoveryClientUDP`** 로 **`-src-port`**(기본 `9998`)에 바인드하고, 요청은 **`DiscoveryUDPPort`**(기본 9999)로 보낸다. 로컬 에이전트가 9999를 쓰는 경우와 충돌하지 않도록 기본 소스 포트를 9998로 둔다(`--discovery`와 동일한 발상). **`--src-port`를 `DiscoveryUDPPort`와 같게 주면** 에이전트가 떠 있을 때 바인드에 실패할 수 있다.

### 사용법

```text
contrabass-moleU agent --host-info -cfg /path/to/agent.local.yml [flags] <self|remote-ip>
contrabass-moleU agent --host-info -h
```

### 플래그 (원격 IP일 때만 의미 있음)

| 플래그 | 기본값 | 설명 |
|--------|--------|------|
| **`-src-port`** | `9998` | 유니캐스트용 **로컬 UDP 바인드 포트**. 생략 시 항상 **9998**. 에이전트가 이미 `DiscoveryUDPPort`(보통 9999)를 쓰는 경우와 충돌하지 않게 하려는 값이다. **`--discovery`** 와 같은 패턴. |

**`-cfg`**, **`-src-port`**, **`<self|remote-ip>`** 는 **순서와 무관**하게 줄 수 있다(예: `<ip> -cfg path` 도 유효).

목적지 UDP 포트는 설정의 **`DiscoveryUDPPort`**(원격 에이전트 listen 포트)를 쓴다.

### 인자

| 위치 | 설명 |
|------|------|
| **`-cfg`** | **필수.** 설정 파일 경로(Discovery·표시용 메타·버전 키 외 필드 로드). |
| **첫 번째 인자** | **`self`**: 로컬. **IPv4/IPv6 주소**: 유니캐스트 대상(호스트명 불가). |

표준 출력: 한 줄 요약 라벨 후 `TYPE`, `HOSTNAME`, `VERSION`, `CPU_UUID` 등 라벨·값 테이블(영문 헤더).

구현: `maintenance/hostinfocli/hostinfocli.go` → `maintenance/hostinfoapi`.

---

## `--nic-brd`

Discovery와 **동일 규칙**(PRD §3.1.1)으로 IPv4 브로드캐스트 주소를 **`인터페이스 : brd`** 형식으로 한 줄씩 출력한 뒤 종료한다. 확인용.

---

## `--discovery`

**설정 파일 없이** UDP Discovery만 수행한다. HTTP 서버는 띄우지 않는다.

### 사용법

```text
contrabass-moleU agent --discovery [flags]
contrabass-moleU agent --discovery -h
```

### 플래그

| 플래그 | 기본값 | 설명 |
|--------|--------|------|
| `--dest-port` | `9999` | 브로드캐스트 목적지 UDP 포트(원격 에이전트가 listen 하는 포트). |
| `--src-port` | `9998` | 로컬에서 바인드하는 UDP 포트(응답 수신). |
| `--timeout` | `10` | Discovery 수집 시간(초). |
| `--service` | `Mole-Discovery` | `DISCOVERY_REQUEST` 의 `service` 필드 (`DiscoveryServiceName` 과 일치해야 응답). |

### 동작 요약

- 사용 가능한 **brd(브로드캐스트) 주소**를 시작 시 한 줄씩 출력한다.
- 에이전트와 같이 **NIC별 UDP 소켓**을 열어 각 brd로 `DISCOVERY_REQUEST`를 보낸다. JSON에 **`reply_udp_port`**(로컬 바인드 포트)를 넣어, 응답이 **올바른 포트**로 오도록 한다.
- 같은 줄에서 `Discovering ... N` 카운트다운 후 **`Discovery Done.`**, 짧은 유예·드레인 후 결과를 출력한다.

### 결과 한 줄 형식

```text
[Local|Remote] <hostname> - <primary> : [<response IPs>] version=<agent version key>
```

- **`[response IPs]`**: UDP 패킷 **실제 발신지**만 취합(`responded_from_ip`).
- **`version=`**: `DISCOVERY_RESPONSE` JSON 의 **`version`** 필드(에이전트 버전 키). 없으면 `version=?`.
- **`[Local]`** / **`[Remote]`**: 로컬 CPU UUID와 응답 `cpu_uuid` 일치 우선, 아니면 응답 IP가 로컬 IPv4와 겹치는지로 보조 판별.

구현: `maintenance/discoverycli/discovery_cli.go`.

---

## `--apply-update`

번들 **tar.gz** 를 검증한 뒤, **업데이트 정책**(`maintenance/agentcfg.StagingUpdateAvailable`)을 만족할 때만 **스테이징·적용**을 한 번에 수행한다. 번들 구성(manifest·패키징·검증)은 **[PRD.md §5.5.0](../PRD.md)**. **로컬 maintenance(8889)는 필요 없다** — **`self`** 는 디스크에 스테이징 후 `systemd-run` 적용(`server.ApplyUpdateSelfFromBundleExtract`), **원격 IP** 는 해당 호스트 **Gin**에 multipart `POST …/apply-update`만 보낸다.

### 사용법

```text
contrabass-moleU agent --apply-update -cfg=/path/to/agent.local.yml [-agent-variant=compute|control] <self|remote-ip> /path/to/bundle.tar.gz
contrabass-moleU agent --apply-update -h
```

`-cfg`·`-agent-variant` 는 **`-name=value`** 형태를 권장한다(`-cfg /path`·`-agent-variant compute` 처럼 **공백으로 값을 주는 형태**도 Go `flag` 패키지에서 동일하게 동작).

### 인자

| 위치 | 설명 |
|------|------|
| **`-cfg`** | **필수.** 설정 파일 경로 (`config.Load`). |
| **`-agent-variant`** | **선택.** manifest v2 번들 적용 시 `contrabass-moleU`로 설치할 바이너리 variant. `compute` 또는 `control`. **생략 시** 적용 대상에 이미 설치된 variant를 따른다(self: `DeployBase/current` 바이너리, remote: `GET …/self`의 `build_variant`; 확인 불가 시 `compute`). |
| **첫 번째 인자** | **`self`**: 이 호스트에 적용. **IPv4/IPv6 주소 문자열**: 해당 원격에 적용(호스트명은 사용하지 않음). |
| **두 번째 인자** | 업로드할 **번들 파일 경로** (`.tar.gz`). |

### 사전 조건·검증

1. 설정 로드 및 **`Maintenance.MaxUploadBytes`** 범위 안에서 번들 크기 확인.
2. 번들을 임시 디렉터리에 풀어 **서버 `POST /upload` 와 동일한 검증**(`server.PrepareAgentBundleFromReader`) — manifest·해시·ELF·바이너리 버전 키(`--version`→`agent --version` 폴백) 등.
3. **현재 버전**  
   - **self**: **`DeployBase/current` → `versions/…` 의 버전 키**(디스크 기준 설치 current). CLI 바이너리의 `main.VersionKey` 는 **`current` 심볼릭을 읽을 수 없을 때만** 보조로 사용한다. **`GET /self` HTTP 없음**. **`-agent-variant` 생략 시** `current/contrabass-moleU`의 `--version` 접미사 `(control)`/`(compute)`로 variant를 정한다(실패 시 CLI 바이너리 variant, 그것도 없으면 `compute`).  
   - **remote**: `http://<remote-ip>:Server.HTTPPort` + `APIPrefix` + `/self` — 적용 전 **`TCP`로 `<ip>:Server.HTTPPort` 연결** 가능 여부를 확인한다. **`build_variant`** 로 생략 시 variant를 정한다.
4. **`StagingUpdateAvailable(번들 버전 키, 현재 버전 키)`** 가 거짓이면 업로드하지 않고 종료 코드 `1`.

### 적용 경로

| 대상 | 동작 |
|------|------|
| **self** | 검증된 번들을 `DeployBase` 아래 스테이징한 뒤 `versionsapi.RunSwitchCurrentWithRoots` 와 동일한 로컬 적용(웹 `POST /upload` + 로컬 `apply-update` 와 동등). **`DeployBase/current` 등에 쓰기·`systemd-run` 은 보통 root 권한(예: `sudo`) 필요** — **`agent --apply-update -h`** 에도 동일 안내. |
| **remote** | `http://<ip>:Server.HTTPPort` + `{APIPrefix}` + **`POST …/apply-update`** multipart: 필드 **`ip`**, **`bundle`**. 요청은 **원격 Gin**에서 처리되며, 원격이 **`POST …/upload`** 후 로컬 **`apply-update`(self)** 를 이어서 호출한다(PRD §5.5.3 multipart 원격 적용과 동일). **로컬 에이전트·maintenance 불필요.** |

HTTP 클라이언트 타임아웃은 **300초** 수준(대용량 번들·느린 링크 대비).

구현: `maintenance/applycli/applycli.go`, 로컬 적용 공유: `maintenance/server/applylocal.go` · `maintenance/versionsapi/switchlocal.go`.

---

## `--versions-list`

- **`self`**: **로컬 maintenance HTTP 없이** 동작한다. 설정의 **`InstallPrefix`**(비어 있으면 **`DeployBase`**, 둘 다 없으면 기본 `/var/lib/contrabass/mole`) 아래 `versions/` 를 읽어, HTTP `GET …/versions/list`(로컬)과 동일한 규칙으로 목록을 만든다(`maintenance/versionsapi`).
- **원격 IP**: 해당 호스트의 **Gin**(`Server.HTTPPort`, 기본 8888)으로 `GET http://<ip>:<port>{APIPrefix}/versions/list` 를 직접 호출한다. **로컬 에이전트·maintenance(8889)는 필요 없다** — 원격만 리슨 중이면 된다(적용 전 `--versions-switch`와 같이 TCP 연결 가능 여부를 확인).

### 사용법

```text
contrabass-moleU agent --versions-list -cfg /path/to/agent.local.yml <self|remote-ip>
contrabass-moleU agent --versions-list -h
```

`-cfg` 와 `<self|remote-ip>` 는 **순서 무관**(위치 인자 한 개).

### 인자

| 위치 | 설명 |
|------|------|
| **`-cfg`** | **필수.** 설정 파일 경로. |
| **첫 번째 인자** | **`self`**: 로컬 디스크. **IPv4/IPv6 주소**: 원격(호스트명 불가). |

표준 출력: `host …` 한 줄 후 `VERSION` / `CURRENT` / `PREVIOUS` 컬럼 테이블.

구현: `maintenance/versionscli/versionscli.go` (`RunList`) → `maintenance/versionsapi`.

---

## `--versions-switch`

스테이징 또는 `versions/`에 있는 **버전 키**를 **current**로 바꾸기 위해 `POST …/versions/switch-current`를 호출한다(서버가 내장 `update.sh`를 `systemd-run`으로 실행).

- **`self`**: **로컬 HTTP 없이** 동작한다(스테이징/versions 해석·필요 시 복사·`current/`에 embedded 스크립트 기록 후 `systemd-run` — 서버 `POST …/versions/switch-current` 로컬 처리와 동일). **`DeployBase`/`current` 쓰기·`systemd-run`은 보통 root 권한(예: `sudo`)이 필요**하며, **`agent --versions-switch -h`** 에도 안내한다. **로컬 에이전트·maintenance(8889) 불필요.**
- **원격 IP**: 해당 호스트 **Gin**으로 `POST http://<ip>:<port>{APIPrefix}/versions/switch-current` 를 **직접** 호출한다. 바디는 `version`만. **로컬 에이전트는 필요 없다.** 적용 전 **`TCP`로 `<ip>:Server.HTTPPort`** 연결 가능 여부를 확인한다.

### 사용법

```text
contrabass-moleU agent --versions-switch -cfg /path/to/agent.local.yml <self|remote-ip> <version-key>
contrabass-moleU agent --versions-switch -h
```

### 인자

| 위치 | 설명 |
|------|------|
| **`-cfg`** | **필수.** 설정 파일 경로. |
| **첫 번째 인자** | **`self`** 또는 원격 **IP**. |
| **두 번째 인자** | 전환할 **버전 키** (`--versions-list` 첫 컬럼과 동일). |

구현: `maintenance/versionscli/versionscli.go` (`RunSwitch`).

---

## 관련 문서

| 문서 | 내용 |
|------|------|
| **[PRD.md](../PRD.md)** | 제품 요구사항 전체(§1.1 소스 트리, §4.1 CLI, §9 버전, §3 Discovery, §5.5 업데이트 API). |
| **[REST_API.md](./REST_API.md)** | maintenance HTTP API 경로·쿼리·응답 형식. |
