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

대상 에이전트의 **Gin(`Server.HTTPPort`, 기본 8888)** 에 **`GET {APIPrefix}/self`** 를 호출한다. **대상 HTTP 서비스가 떠 있어야 한다** — `GET …/health` 실패 시 `agent service is not running at …` 오류(영문).

### 사용법

```text
contrabass-moleU agent --host-info [-apiprefix <path>] <self|remote-ip>
contrabass-moleU agent --host-info -h
```

### 플래그

| 플래그 | 기본값 | 설명 |
|--------|--------|------|
| **`-apiprefix`** | `/maintenance/api/v1` | Gin에 노출된 API 경로 prefix (`Maintenance.APIPrefix` 와 동일). |

**`-apiprefix`** 와 **`<self|remote-ip>`** 는 **순서 무관**.

### 인자

| 위치 | 설명 |
|------|------|
| **첫 번째 인자** | **`self`**: `http://127.0.0.1:8888{APIPrefix}/self`. **IPv4/IPv6**: 해당 호스트 Gin에 직접 요청(호스트명 불가). |

표준 출력: 한 줄 요약 라벨 후 `TYPE`, `HOSTNAME`, `VERSION`, **`BUILD_VARIANT`**, `CPU_UUID` 등 라벨·값 테이블(영문 헤더).

구현: `maintenance/hostinfocli/hostinfocli.go` → `maintenance/clirest`.

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
[Local|Remote] <hostname> - <primary> : [<response IPs>] version=<agent version key> (<control|compute>)
```

- **`[response IPs]`**: UDP 패킷 **실제 발신지**만 취합(`responded_from_ip`).
- **`version=`**: `DISCOVERY_RESPONSE` JSON 의 **`version`** 필드(에이전트 버전 키). **`build_variant`** 가 있으면 웹 UI와 같이 **`version=<key> (control|compute)`** 형태. 버전 없으면 `version=?`, variant만 없으면 괄호 생략.
- **`[Local]`** / **`[Remote]`**: 로컬 CPU UUID와 응답 `cpu_uuid` 일치 우선, 아니면 응답 IP가 로컬 IPv4와 겹치는지로 보조 판별.

구현: `maintenance/discoverycli/discovery_cli.go`.

---

## `--apply-update`

대상 에이전트 Gin에 **`POST {APIPrefix}/upload`**(번들 검증·스테이징) 후 **`POST {APIPrefix}/apply-update`**(JSON `version`·`ip:self`·`agent_variant`)를 호출한다. 웹 UI의 업로드+적용과 동등. **대상 HTTP 서비스가 떠 있어야 한다.**

### 사용법

```text
contrabass-moleU agent --apply-update [-apiprefix=<path>] [-agent-variant=compute|control] <self|remote-ip> /path/to/bundle.tar.gz
contrabass-moleU agent --apply-update -h
```

### 인자

| 위치 | 설명 |
|------|------|
| **`-apiprefix`** | **선택.** API path prefix (기본 `/maintenance/api/v1`). |
| **`-agent-variant`** | **선택.** `compute` 또는 `control`. **생략 시** `GET …/self`의 `build_variant`(없으면 CLI ldflags, 그것도 없으면 `compute`). |
| **첫 번째 인자** | **`self`** 또는 원격 **IP**. |
| **두 번째 인자** | 업로드할 **번들 파일 경로** (`.tar.gz`). |

검증·업로드 상한·적용 정책은 **대상 서버** 설정·핸들러가 처리한다. HTTP 클라이언트 타임아웃 **300초**.

구현: `maintenance/applycli/applycli.go` → `maintenance/clirest`.

---

## `--versions-list`

대상 에이전트 Gin에 **`GET {APIPrefix}/versions/list`** 를 호출한다. **대상 HTTP 서비스가 떠 있어야 한다.**

### 사용법

```text
contrabass-moleU agent --versions-list [-apiprefix <path>] <self|remote-ip>
contrabass-moleU agent --versions-list -h
```

### 인자

| 위치 | 설명 |
|------|------|
| **`-apiprefix`** | **선택.** 기본 `/maintenance/api/v1`. |
| **첫 번째 인자** | **`self`** 또는 원격 **IP**. |

표준 출력: `host …` 한 줄 후 `VERSION` / `CURRENT` / `PREVIOUS` 컬럼 테이블.

구현: `maintenance/versionscli/versionscli.go` (`RunList`) → `maintenance/clirest`.

---

## `--versions-switch`

대상 에이전트 Gin에 **`POST {APIPrefix}/versions/switch-current`**(JSON `version`)를 호출한다. **대상 HTTP 서비스가 떠 있어야 한다.**

### 사용법

```text
contrabass-moleU agent --versions-switch [-apiprefix <path>] <self|remote-ip> <version-key>
contrabass-moleU agent --versions-switch -h
```

### 인자

| 위치 | 설명 |
|------|------|
| **`-apiprefix`** | **선택.** 기본 `/maintenance/api/v1`. |
| **첫 번째 인자** | **`self`** 또는 원격 **IP**. |
| **두 번째 인자** | 전환할 **버전 키**. |

구현: `maintenance/versionscli/versionscli.go` (`RunSwitch`) → `maintenance/clirest`.

---

## 관련 문서

| 문서 | 내용 |
|------|------|
| **[PRD.md](../PRD.md)** | 제품 요구사항 전체(§1.1 소스 트리, §4.1 CLI, §9 버전, §3 Discovery, §5.5 업데이트 API). |
| **[REST_API.md](./REST_API.md)** | maintenance HTTP API 경로·쿼리·응답 형식. |
