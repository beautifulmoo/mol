# Contrabass agent — CLI 명세

루트 `main.go`는 **`maintenance.Run(main.VersionKey, os.Args)`** 만 호출하고, 실제 분기는 **`maintenance/maintenance.go`** 에 있다.  
CLI 전용 모드에서는 **Gin(`Server.HTTPPort`) 리버스 프록시를 기동하지 않는다** — 서비스 모드는 **`contrabass-moleU -cfg <비어 있지 않은 경로>`** 일 때만 (`ShouldStartGinReverseProxy`).

실행 파일 표시명은 **`maintenance/appmeta.BinaryName`** (기본 **`contrabass-moleU`**).

---

## 공통

| 항목 | 설명 |
|------|------|
| **종료 코드** | 성공 **`0`**, 실패 **`1`**. `maintenance`·`discoverycli`·`applycli`·`versionscli`·`hostinfocli` 패키지는 **`os.Exit`를 호출하지 않고** 상위 `main`이 `os.Exit` 한다. |
| **도움말 언어** | `-h` / `--help` 본문 및 **`--apply-update`**, **`--versions-list`**, **`--versions-switch`**, **`--host-info`** 관련 진단 메시지는 **영문**으로 출력한다(로캘 미설치 OS 대비). `--discovery` 도움말도 영문. |
| **버전 출력** | **`-version`**, **`--version`**: 빌드 시 주입된 **`main.VersionKey`** 와 `BinaryName` 을 한 줄로 출력한다. 설정 파일 불필요. |

---

## 인자 없음

```text
contrabass-moleU
```

버전 안내와 **`-cfg <파일>`** 이 필요하다는 메시지를 출력하고 종료한다. HTTP·Discovery는 시작하지 않는다.

---

## 서비스 모드 (HTTP + Discovery)

```text
contrabass-moleU -cfg /path/to/config.yaml
```

설정을 로드한 뒤 **maintenance HTTP**(`Maintenance.MaintenanceListenAddress`:`Maintenance.MaintenancePort`)와 **UDP Discovery** 등을 기동한다. 상세는 **[PRD.md](../PRD.md)** §1·§2·§7.

- 인자가 `-cfg`인데 경로가 없으면 stderr에 안내 후 종료 코드 `1`.

---

## `-h` / `--help`

표준 도움말 출력(영문). 서비스 미기동.

---

## `--nic-brd`

Discovery와 **동일 규칙**(PRD §3.1.1)으로 IPv4 브로드캐스트 주소를 **`인터페이스 : brd`** 형식으로 한 줄씩 출력한 뒤 종료한다. 확인용.

---

## `--discovery`

**설정 파일 없이** UDP Discovery만 수행한다. HTTP 서버는 띄우지 않는다.

### 사용법

```text
contrabass-moleU --discovery [flags]
contrabass-moleU --discovery -h
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

번들 **tar.gz** 를 검증한 뒤, **업데이트 정책**(`internal/config.StagingUpdateAvailable`)을 만족할 때만 **업로드·적용**을 한 번에 수행한다. **로컬 maintenance HTTP**가 실행 중이어야 한다(원격 대상이어도 요청은 **이 머신의 maintenance**로 보낸다).

### 사용법

```text
contrabass-moleU --apply-update -cfg /path/to/config.yaml <self|remote-ip> /path/to/bundle.tar.gz
contrabass-moleU --apply-update -h
```

### 인자

| 위치 | 설명 |
|------|------|
| **`-cfg`** | **필수.** 설정 파일 경로 (`config.Load`). |
| **첫 번째 인자** | **`self`**: 이 호스트에 적용. **IPv4/IPv6 주소 문자열**: 해당 원격에 적용(호스트명은 사용하지 않음). |
| **두 번째 인자** | 업로드할 **번들 파일 경로** (`.tar.gz`). |

### 사전 조건·검증

1. 설정 로드 및 **`Maintenance.MaxUploadBytes`** 범위 안에서 번들 크기 확인.
2. 번들을 임시 디렉터리에 풀어 **서버 `POST /upload` 와 동일한 검증**(`server.PrepareAgentBundleFromReader`) — manifest·해시·ELF·바이너리 `--version` 등.
3. **현재 버전**: `GET {APIPrefix}/self`  
   - **self**: `http://<MaintenanceListenAddress 또는 0.0.0.0→127.0.0.1>:MaintenancePort` + `APIPrefix`  
   - **remote**: `http://<remote-ip>:Server.HTTPPort` + `APIPrefix` — 적용 전 **`TCP`로 `<ip>:Server.HTTPPort` 연결** 가능 여부를 확인한다.
4. **`StagingUpdateAvailable(번들 버전 키, 현재 버전 키)`** 가 거짓이면 업로드하지 않고 종료 코드 `1`.

### 적용 경로

| 대상 | 동작 |
|------|------|
| **self** | `POST …/upload` (multipart 필드 **`bundle`**) → `POST …/apply-update` JSON `{"version":"<버전 키>"}` (ip 생략). |
| **remote** | `POST …/apply-update` **multipart**: 필드 **`ip`**(원격 IP), **`bundle`**(번들 파일). 서버가 원격에 **`POST …/upload`** 후 원격 **`apply-update`(self)** 를 호출한다 — **로컬 스테이징 없이 원격만** 갱신(PRD §5.5.3 multipart 원격 적용과 동일). |

HTTP 클라이언트 타임아웃은 **300초** 수준(대용량 번들·느린 링크 대비).

구현: `maintenance/applycli/applycli.go`.

---

## `--versions-list`

**로컬 maintenance HTTP**가 실행 중이어야 한다. `GET …/versions/list`와 동일하게, **`self`**면 이 호스트의 `versions/` 목록을, **원격 IP**면 `?ip=` 프록시로 해당 호스트 목록을 조회한다.

### 사용법

```text
contrabass-moleU --versions-list -cfg /path/to/config.yaml <self|remote-ip>
contrabass-moleU --versions-list -h
```

### 인자

| 위치 | 설명 |
|------|------|
| **`-cfg`** | **필수.** 설정 파일 경로. |
| **첫 번째 인자** | **`self`**: 로컬. **IPv4/IPv6 주소**: 원격(호스트명 불가). |

표준 출력: `host …` 한 줄 후 `VERSION` / `CURRENT` / `PREVIOUS` 컬럼 테이블.

구현: `maintenance/versionscli/versionscli.go` (`RunList`).

---

## `--versions-switch`

스테이징 또는 `versions/`에 있는 **버전 키**를 **current**로 바꾸기 위해 `POST …/versions/switch-current`를 호출한다(서버가 내장 `update.sh`를 `systemd-run`으로 실행). **로컬 maintenance HTTP** 필요. 원격 대상이면 적용 전 **`TCP`로 `<ip>:Server.HTTPPort`** 연결 가능 여부를 확인한다(`--apply-update`와 동일한 패턴).

### 사용법

```text
contrabass-moleU --versions-switch -cfg /path/to/config.yaml <self|remote-ip> <version-key>
contrabass-moleU --versions-switch -h
```

### 인자

| 위치 | 설명 |
|------|------|
| **`-cfg`** | **필수.** 설정 파일 경로. |
| **첫 번째 인자** | **`self`** 또는 원격 **IP**. |
| **두 번째 인자** | 전환할 **버전 키** (`--versions-list` 첫 컬럼과 동일). |

구현: `maintenance/versionscli/versionscli.go` (`RunSwitch`).

---

## `--host-info`

**로컬 maintenance HTTP**가 실행 중이어야 한다. 요청은 항상 설정의 **`MaintenanceListenAddress`**·**`MaintenancePort`** 로 보낸다. **`GET …/host-info`** 와 동일하다.

- **`self`**: `ip` 없이 조회 — 서버가 **`/self`** 와 같은 로컬 호스트 정보(`DISCOVERY_RESPONSE` 형)를 반환한다.
- **원격 IP**: `?ip=` — 이 에이전트가 해당 주소로 **UDP 유니캐스트 Discovery** 를 보내 응답 한 건을 반환한다(HTTP로 원격 에이전트에 프록시하는 방식이 **아님**).

### 사용법

```text
contrabass-moleU --host-info -cfg /path/to/config.yaml <self|remote-ip>
contrabass-moleU --host-info -h
```

### 인자

| 위치 | 설명 |
|------|------|
| **`-cfg`** | **필수.** 설정 파일 경로. |
| **첫 번째 인자** | **`self`**: 로컬. **IPv4/IPv6 주소**: 유니캐스트 대상(호스트명 불가). |

표준 출력: 한 줄 요약 라벨 후 `TYPE`, `HOSTNAME`, `VERSION`, `CPU_UUID` 등 라벨·값 테이블(영문 헤더).

구현: `maintenance/hostinfocli/hostinfocli.go`.

---

## 관련 문서

| 문서 | 내용 |
|------|------|
| **[PRD.md](../PRD.md)** | 제품 요구사항 전체(§4.1 CLI, §3 Discovery, §5.5 업데이트 API). |
| **[REST_API.md](./REST_API.md)** | maintenance HTTP API 경로·쿼리·응답 형식. |
