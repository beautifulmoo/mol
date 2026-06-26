# Mole Agent — CLI 명세

루트 `main.go`는 argv를 나눈 뒤 **`maintenance.Run(main.VersionKey, os.Args)`** 의 반환값으로 **`os.Exit`** 한다. 실제 명령 분기는 **`maintenance/maintenance.go`** 의 `Run`에 있다.

| argv | `main` 동작 | Gin (`Server.HTTPPort`) |
|------|-------------|-------------------------|
| **`<bin> agent …`** (`IsAgentSubcommand`, `agent -cfg` 포함) | `os.Exit(Run(…))` | 없음 |
| **인자 없음**, 루트 **`--version`**, 잘못된 argv (`!IsServiceModeRootCfg`) | `os.Exit(Run(…))` | 없음 |
| **`<bin> -cfg <파일>`** (`IsServiceModeRootCfg`) | `go func() { os.Exit(Run(…)) }()` + 메인에서 **`router.Run`** | 있음 (`RegisterMaintenanceProxy`) |

**HTTP·Discovery 서비스**는 **`<bin> -cfg …`** 또는 **`<bin> agent -cfg …`** 로 기동한다(`Run` 동작 동일). **Discovery·host-info·apply-update 등**은 **`agent` 다음** 옵션만(실행 후 셸 복귀). **`agent`만** 주면 **대화형 REPL** — **[REPL.md](./REPL.md)**. 예: `contrabass-moleU agent`, `contrabass-moleU agent --discovery -h`.

**시그널**: `SIGINT`/`SIGTERM`은 **`maintenance.runServiceWithConfigPath`** 가 `signal.Notify`로 처리한다. **`main`은 시그널 핸들러를 등록하지 않는다.** `<bin> -cfg …` 에서 Ctrl+C 시 maintenance가 내려가고 `Run`이 끝나면, 고루틴의 **`os.Exit(Run(…))`** 로 프로세스(Gin 포함)가 종료된다.

**병합 호스트**: 바깥 Gin에 **전역** `Content-Type: application/json` 미들웨어를 두면 `/maintenance` UI CSS가 깨진다. JSON API만 **`routerGroupJSON`**(루트 `main.go`)처럼 **라우트 그룹**에 적용한다.

`ConfigPathForServiceMode`는 **`<bin> -cfg …`** 와 **`<bin> agent -cfg …`** 에서 설정 경로를 돌려준다.

저장소에서는 예시 설정 파일을 **`cfg/agent.local.yml`** 에 둔다(`maintenance/scripts/pack-agent-tarball.sh` 기본 config 소스).

실행 파일 표시명은 **`maintenance/appmeta.BinaryName`** (기본 **`contrabass-moleU`**).

---

## 공통

| 항목 | 설명 |
|------|------|
| **종료 코드** | 성공 **`0`**, 실패 **`1`**. `maintenance`·`discoverycli`·`applycli`·`versionscli`·`hostinfocli`·`*clicli`(일괄 원격) 패키지는 **`os.Exit`를 호출하지 않고** 상위 `main`이 `os.Exit` 한다. |
| **도움말·API 언어** | `-h` / `--help` 본문 및 **`--apply-update`**, **`--versions-list`**, **`--versions-switch`**, **`--host-info`**, **리모트 일괄 CLI 4종** 관련 **CLI 진단 메시지**는 **영문**이다. 원격 호출 시 stdout에 찍히는 성공/실패 문구는 **원격 에이전트 REST API**의 `data` 문자열(영문)을 그대로 출력한다. `--discovery` 도움말도 영문. |
| **버전 출력** | **권장**: **`contrabass-moleU agent --version`** 또는 **`agent -version`** / **`agent --version`** — 빌드 시 주입된 **`main.VersionKey`** 와 `BinaryName` 한 줄. `BuildVariant`가 주입된 경우 **`contrabass-moleU 0.4.4-test (compute)`** 형태로 variant 접미사가 붙는다. **전환용**: 루트 **`contrabass-moleU --version`** / **`-version`** 도 동일 한 줄을 출력한다(구 업데이트 스크립트 호환; PRD §4.1·§9). 설정 파일 불필요. |
| **로컬 대상** | **`host-info`**·**`apply-update`**·**`versions-*`** 등의 대상 인자에서 **`self`** 와 **`local`** 은 동의어(이 머신 Gin `127.0.0.1:8888`). REST JSON의 `ip` 필드는 계속 **`self`** 만 사용. Discovery 결과의 **`[Local]`** 태그와는 별개. |
| **번들 파일 경로** | **`--apply-update`**·**`--apply-update-all-remotes`** 및 REPL `apply-update` / `apply-update-all`의 번들 인자: **`~/…` 확장**, `./`·상대 경로는 **프로세스 CWD** 기준(`clirest.ExpandLocalPath`). |

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

표준 도움말 출력(영문). 서비스 미기동. **`contrabass-moleU agent --help`** 옵션 순서:

1. **`<bin> agent`**(인자 없음) — 대화형 **REPL** — **[REPL.md](./REPL.md)**. `-h`는 아래 목록 출력.
2. `-h` / `--help`, `-version` / `--version`
3. `--host-info`, `--nic-brd`, `--discovery`, `--apply-update`
4. `--versions-list`, `--versions-switch`
5. **리모트 일괄 4종**(맨 아래): `--push-config-all-remotes`, `--restart-all-remotes`, `--apply-update-all-remotes`, `--rollback-all-remotes`

아래 **개별 명령 절**은 2→3→4 순으로 정리한다(일괄 4종은 §「리모트 일괄 CLI」).

---

## `--host-info`

대상 에이전트의 **Gin(`Server.HTTPPort`, 기본 8888)** 에 **`GET {APIPrefix}/self`** 를 호출한다. **대상 HTTP 서비스가 떠 있어야 한다** — `GET …/health` 실패 시 `agent service is not running at …` 오류(영문).

### 사용법

```text
contrabass-moleU agent --host-info [-apiprefix <path>] <self|local|remote-ip>
contrabass-moleU agent --host-info -h
```

### 플래그

| 플래그 | 기본값 | 설명 |
|--------|--------|------|
| **`-apiprefix`** | `/maintenance/api/v1` | Gin에 노출된 API 경로 prefix (`Maintenance.APIPrefix` 와 동일). |

**`-apiprefix`** 와 **`<self|local|remote-ip>`** 는 **순서 무관**.

### 인자

| 위치 | 설명 |
|------|------|
| **첫 번째 인자** | **`self`** 또는 **`local`**: `http://127.0.0.1:8888{APIPrefix}/self`. **IPv4/IPv6**: 해당 호스트 Gin에 직접 요청(호스트명 불가). |

표준 출력: 한 줄 요약 라벨 후 `TYPE`, `HOSTNAME`, `VERSION`, **`BUILD_VARIANT`**, `CPU_UUID` 등 라벨·값 테이블(영문 헤더).

**`HOST_IP`**: Discovery와 같이 **응답한 IP**(`responded_from_ip` 대표값). **`HOST_IPS`**: 같은 호스트(CPU UUID)에 대해 UDP Discovery로 모은 **발견 IP**(`host_ip`·`responded_from_ip` 합집합). `GET …/self`로 CPU·메모리 등을 가져온 뒤 **약 3초** 브로드캐스트 Discovery로 IP 열을 보강한다(웹 UI **IP**·**응답한 IP** 규칙과 동일). **REPL** `host-info`는 **`discovery` 캐시**가 있으면 UDP 없이 캐시로 보강.

구현: `maintenance/hostinfocli` → `maintenance/clirest` + `maintenance/discoverycli`.

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
[Local|Remote] <hostname> - <primary> : [<discovered IPs>] version=<agent version key> (<control|compute>)
```

- **`<primary>`**: 마지막으로 수신한 **`responded_from_ip`**(UDP 실제 발신지). 웹 UI의 **「응답한 IP」** 대표값과 같다.
- **`[discovered IPs]`**: CPU UUID로 묶인 응답마다 **`host_ip`**·**`responded_from_ip`** 를 합친 목록(중복 제거·정렬). 웹 UI **「IP」** 컬럼과 같다.
- **`version=`**: `DISCOVERY_RESPONSE` JSON 의 **`version`** 필드(에이전트 버전 키). **`build_variant`** 가 있으면 웹 UI와 같이 **`version=<key> (control|compute)`** 형태. 버전 없으면 `version=?`, variant만 없으면 괄호 생략.
- **`[Local]`** / **`[Remote]`**: 로컬 CPU UUID와 응답 `cpu_uuid` 일치 우선, 아니면 응답 IP가 로컬 IPv4와 겹치는지로 보조 판별.

구현: `maintenance/discoverycli` (`DiscoverToStdout`, `discovery_cli.go`).

---

## `--apply-update`

대상 에이전트 Gin에 **`POST {APIPrefix}/upload`**(번들 검증·스테이징) 후 **`POST {APIPrefix}/apply-update`**(JSON `version`·`ip:self`·`agent_variant`·`reuse_previous_config`)를 호출한다. 웹 UI의 업로드+적용과 동등. **대상 HTTP 서비스가 떠 있어야 한다.**

### 사용법

```text
contrabass-moleU agent --apply-update [-apiprefix=<path>] [-agent-variant=compute|control] [-use-bundle-config] <self|local|remote-ip> /path/to/bundle.tar.gz
contrabass-moleU agent --apply-update -h
```

### 인자

| 위치 | 설명 |
|------|------|
| **`-apiprefix`** | **선택.** API path prefix (기본 `/maintenance/api/v1`). |
| **`-agent-variant`** | **선택.** `compute` 또는 `control`. **생략 시** `GET …/self`의 `build_variant`(없으면 CLI ldflags, 그것도 없으면 `compute`). |
| **`-use-bundle-config`** | **선택.** 지정 시 번들에 포함된 config를 적용한다. **생략 시(기본)** 대상 호스트 **current** config를 재사용한다(`reuse_previous_config: true`, 웹 UI 체크박스 기본과 동일). |
| **첫 번째 인자** | **`self`** / **`local`** 또는 원격 **IP**. |
| **두 번째 인자** | 업로드할 **번들 파일 경로** (`.tar.gz`). |

검증·업로드 상한·적용 정책은 **대상 서버** 설정·핸들러가 처리한다. HTTP 클라이언트 타임아웃 **300초**.

### 환경설정 재사용

| 클라이언트 | 기본 동작 | 번들 config 사용 |
|------------|-----------|------------------|
| **웹 UI** | 「이전버전의 환경설정 파일 재사용」 체크 **on** | 체크 해제 후 적용(확인 대화상자) |
| **CLI** | `reuse_previous_config: true` 전송 | **`-use-bundle-config`** |
| **REST JSON** | 필드 생략 시 서버 **false** | `"reuse_previous_config": false` 명시 |
| **REST multipart** | 필드 생략 시 **true** | `reuse_previous_config=false` |

적용 전 **current** config를 `versions/<키>/`에 복사하는 동작은 PRD §5.5.1·§5.5.3을 따른다.

구현: `maintenance/applycli/applycli.go` → `maintenance/clirest`.

---

## `--versions-list`

대상 에이전트 Gin에 **`GET {APIPrefix}/versions/list`** 를 호출한다. **대상 HTTP 서비스가 떠 있어야 한다.**

### 사용법

```text
contrabass-moleU agent --versions-list [-apiprefix <path>] <self|local|remote-ip>
contrabass-moleU agent --versions-list -h
```

### 인자

| 위치 | 설명 |
|------|------|
| **`-apiprefix`** | **선택.** 기본 `/maintenance/api/v1`. |
| **첫 번째 인자** | **`self`** / **`local`** 또는 원격 **IP**. |

표준 출력: `host …` 한 줄 후 `VERSION` / `CURRENT` / `PREVIOUS` 컬럼 테이블.

구현: `maintenance/versionscli/versionscli.go` (`RunList`) → `maintenance/clirest`.

---

## `--versions-switch`

대상 에이전트 Gin에 **`POST {APIPrefix}/versions/switch-current`**(JSON `version`)를 호출한다. **대상 HTTP 서비스가 떠 있어야 한다.**

### 사용법

```text
contrabass-moleU agent --versions-switch [-apiprefix <path>] <self|local|remote-ip> <version-key>
contrabass-moleU agent --versions-switch -h
```

### 인자

| 위치 | 설명 |
|------|------|
| **`-apiprefix`** | **선택.** 기본 `/maintenance/api/v1`. |
| **첫 번째 인자** | **`self`** / **`local`** 또는 원격 **IP**. |
| **두 번째 인자** | 전환할 **버전 키**. |

구현: `maintenance/versionscli/versionscli.go` (`RunSwitch`) → `maintenance/clirest`.

---

## 리모트 일괄 CLI (공통)

오케스트레이터(이 머신)에서 **`<bin> -cfg <file>`** 로 **로컬 maintenance HTTP**가 떠 있어야 한다(`Maintenance.MaintenancePort`, 기본 **8889**). 단일 호스트 CLI(`--host-info`, `--apply-update` 등)는 대상 **Gin(`Server.HTTPPort`, 기본 8888)** 에 직접 붙지만, 일괄 4종은 **maintenance 리스너**의 NDJSON API를 호출한다.

| 항목 | 설명 |
|------|------|
| **웹 UI** | PRD §6.6 사이드바 4버튼과 **동일 API** |
| **Discovery** | 각 명령 **내부**에서 UDP Discovery **1회**(dest `9999`, src `9998`, timeout `10`s, service `Mole-Discovery`). **`agent --discovery` 선행 불필요**; 이전 discovery·`remoteregistry`·DOM 결과도 **사용하지 않음** |
| **Discovery 옵션** | 일괄 명령에는 `--dest-port` 등 **없음**. 커스텀 설정으로 원격만 확인할 때만 standalone `agent --discovery` |
| **`hosts[]`** | 이번 Discovery 응답을 CPU UUID로 병합(`discoverycli.BulkPushHostsFromDiscovery`). **`host_ip`**·**`responded_from_ip`** 모두 포함, primary는 **`responded_from_ip`**. 로컬·self 제외 |
| **응답** | `application/x-ndjson` — `start` → 호스트별 `progress` → `done`. stdout에 `[N/M] hostname (ip): …` |
| **공통 플래그** | **`-apiprefix`**(기본 `/maintenance/api/v1`), **`-maintenance-port`**(기본 `8889`) |
| **구현** | **`maintenance/bulkcli`**(`flags.go` 공통 플래그·help) + `discoverycli` + `clirest` + 각 `*clicli` 패키지. REPL bulk는 `bulkcli.Run`(캐시된 hosts) |

### 명령·API·웹 UI 대응

| CLI | maintenance API | 웹 UI (§6.6) |
|-----|-----------------|--------------|
| `--push-config-all-remotes` | `POST …/current-config/push-local-all` | 로컬 설정을 리모트 호스트에 일괄 복사 |
| `--restart-all-remotes` | `POST …/service-control/restart-all` | 리모트 호스트 일괄 재시작 |
| `--apply-update-all-remotes <bundle>` | `POST …/upload` 후 `POST …/apply-update-all` | 리모트 호스트에 일괄 업데이트 적용 |
| `--rollback-all-remotes` | `POST …/versions/rollback-all` | 리모트 호스트 일괄 롤백 |

### 종료 코드 (일괄 공통)

| 명령 | 종료 `1` 조건 |
|------|----------------|
| push / restart | `failed > 0`, 처리 호스트 없음, discovery·maintenance 사전 실패 |
| apply-update-all / rollback-all | 위와 같고, 추가로 **`succeeded == 0`**(전원 `skipped` 포함) |

`skipped`는 실패가 아니다(apply·rollback). `update_history.log`에는 작업별 요약 1줄만 append된다.

---

## `--push-config-all-remotes`

오케스트레이터(이 머신)에서 **UDP Discovery**로 원격을 찾은 뒤, 로컬 maintenance HTTP의 **`POST {APIPrefix}/current-config/push-local-all`** 로 **로컬 `current` config**를 모든 원격에 복사한다. 웹 UI **「로컬 설정을 리모트 호스트에 일괄 복사」** 와 동등한 API이며, 호스트 목록은 **이번 Discovery 결과**에서 만든다(메모리 내 병합, 레지스트리/DOM 불필요).

**전제**: `<bin> -cfg <file>` 로 **로컬 maintenance 서비스**가 떠 있어야 한다(로컬 `current` config 읽기·원격 프록시).

### 사용법

```text
contrabass-moleU agent --push-config-all-remotes [-apiprefix=<path>] [-maintenance-port=N]
contrabass-moleU agent --push-config-all-remotes -h
```

### 플래그

| 플래그 | 기본값 | 설명 |
|--------|--------|------|
| **`-apiprefix`** | `/maintenance/api/v1` | 로컬 maintenance API path prefix (`Maintenance.APIPrefix` 와 동일). |
| **`-maintenance-port`** | `8889` | 로컬 maintenance HTTP 포트 (`Maintenance.MaintenancePort`). |

### Discovery

§「리모트 일괄 CLI (공통)」참고.

로컬·자기 응답은 제외하고, CPU UUID·IP로 호스트를 병합한 뒤 `hosts[]` body로 push-local-all에 전달한다. 원격이 없으면 종료 코드 `1`.

### 출력

Discovery brd·카운트다운·`Discovery Done.` 후 호스트 수, 호스트별 `[N/M] hostname (ip): success|fail — …`, 마지막 요약 줄.

구현: `maintenance/configpushclicli` → `maintenance/discoverycli` + `maintenance/clirest`.

---

## `--restart-all-remotes`

오케스트레이터에서 **UDP Discovery**로 원격을 찾은 뒤, 로컬 maintenance HTTP의 **`POST {APIPrefix}/service-control/restart-all`** 로 모든 원격 에이전트 서비스를 재시작한다. 웹 UI **「리모트 호스트 일괄 재시작」** 과 동등하며, 호스트 목록은 **이번 Discovery 결과**에서 만든다.

**전제**: `<bin> -cfg <file>` 로 **로컬 maintenance 서비스**가 떠 있어야 한다(원격 restart 프록시·재기동 확인).

### 사용법

```text
contrabass-moleU agent --restart-all-remotes [-apiprefix=<path>] [-maintenance-port=N]
contrabass-moleU agent --restart-all-remotes -h
```

### 플래그

| 플래그 | 기본값 | 설명 |
|--------|--------|------|
| **`-apiprefix`** | `/maintenance/api/v1` | 로컬 maintenance API path prefix. |
| **`-maintenance-port`** | `8889` | 로컬 maintenance HTTP 포트. |

### Discovery

§「리모트 일괄 CLI (공통)」참고.

### 출력

Discovery brd·카운트다운 후 호스트 수, 호스트별 `[N/M] hostname (ip): restart verified via … — …` 또는 `fail — …`, 마지막 요약 줄.

구현: `maintenance/restartallclicli` → `maintenance/discoverycli` + `maintenance/clirest`.

---

## `--apply-update-all-remotes`

오케스트레이터에서 **번들을 로컬 스테이징에 업로드**한 뒤 **UDP Discovery**로 원격을 찾고, 로컬 maintenance HTTP의 **`POST {APIPrefix}/apply-update-all`** 로 모든 원격에 업데이트를 적용한다. 웹 UI **「리모트 호스트에 일괄 업데이트 적용」** 과 동등하다.

**전제**: `<bin> -cfg <file>` 로 **로컬 maintenance 서비스**가 떠 있어야 한다.

### 사용법

```text
contrabass-moleU agent --apply-update-all-remotes [-apiprefix=<path>] [-maintenance-port=N] [-agent-variant=control|compute] [-use-bundle-config] <bundle.tar.gz>
contrabass-moleU agent --apply-update-all-remotes -h
```

### 플래그

| 플래그 | 기본값 | 설명 |
|--------|--------|------|
| **`-apiprefix`** | `/maintenance/api/v1` | 로컬 maintenance API path prefix. |
| **`-maintenance-port`** | `8889` | 로컬 maintenance HTTP 포트. |
| **`-agent-variant`** | (CLI `build_variant`, 없으면 `compute`) | 모든 원격에 적용할 variant. `--apply-update` 와 달리 **호스트별**이 아니라 **일괄 body 하나**. |
| **`-use-bundle-config`** | off | 지정 시 번들 config 적용. **미지정 시** 각 원격 **current** config 재사용(`reuse_previous_config: true`, `--apply-update`·웹 체크박스 기본과 동일). |

### 인자

| 인자 | 설명 |
|------|------|
| **`<bundle.tar.gz>`** | 로컬 maintenance `POST …/upload` 로 스테이징한 뒤 그 **version 키**로 apply-update-all 호출. |

### Discovery

§「리모트 일괄 CLI (공통)」참고. 실행 순서: **번들 업로드 → Discovery → apply-update-all**.

### 출력

업로드·스테이징 버전, Discovery, 호스트별 `[N/M] …: update apply requested (version) via …` / `skipped — …` / `fail — …`, 마지막 `succeeded`/`failed`/`skipped` 요약.

종료 코드: **`failed > 0`** 또는 **`succeeded == 0`**(전원 skipped 포함)이면 `1`.

구현: `maintenance/applyupdateallclicli` → `maintenance/discoverycli` + `maintenance/clirest`.

---

## `--rollback-all-remotes`

오케스트레이터에서 **UDP Discovery**로 원격을 찾은 뒤, 로컬 maintenance HTTP의 **`POST {APIPrefix}/versions/rollback-all`** 로 모든 원격을 `previous` 버전으로 롤백한다. 웹 UI **「리모트 호스트 일괄 롤백」** 과 동등하다.

**전제**: `<bin> -cfg <file>` 로 **로컬 maintenance 서비스**가 떠 있어야 한다.

### 사용법

```text
contrabass-moleU agent --rollback-all-remotes [-apiprefix=<path>] [-maintenance-port=N]
contrabass-moleU agent --rollback-all-remotes -h
```

### 플래그

| 플래그 | 기본값 | 설명 |
|--------|--------|------|
| **`-apiprefix`** | `/maintenance/api/v1` | 로컬 maintenance API path prefix. |
| **`-maintenance-port`** | `8889` | 로컬 maintenance HTTP 포트. |

### Discovery

§「리모트 일괄 CLI (공통)」참고.

### 출력

Discovery 후 호스트 수, 호스트별 `[N/M] …: rollback requested via …` / `skipped — …` / `fail — …`, 마지막 `succeeded`/`failed`/`skipped` 요약.

`previous` 없음·`current`=`previous`(이미 롤백됨) 호스트는 **`skipped`**(실패 아님). 종료 코드: **`failed > 0`** 또는 **`succeeded == 0`** 이면 `1`.

구현: `maintenance/rollbackallclicli` → `maintenance/discoverycli` + `maintenance/clirest`.

---

## 대화형 REPL

**`contrabass-moleU agent`**(인자 없음) → 프롬프트 `Mole-Agent>`. **`discovery`** 캐시(`discover` 별칭), 세션 `set`, bulk·단일 호스트 명령, readline 히스토리·Tab 완성.

상세(명령 목록, 세션 키, bulk vs 일회성 CLI, Tab 완성): **[REPL.md](./REPL.md)**.

---

## 관련 문서

| 문서 | 내용 |
|------|------|
| **[REPL.md](./REPL.md)** | 대화형 REPL 전체 명세 |
| **[PRD.md](../PRD.md)** | 제품 요구사항 전체(§1.1 소스 트리, §4.1·§4.1.1·§4.1.2 CLI·REPL, §9 버전, §3 Discovery, §5.5 업데이트 API, §6.6 일괄 UI). |
| **[REST_API.md](./REST_API.md)** | maintenance HTTP API 경로·쿼리·응답 형식(§「일괄 원격 API·CLI」). |
