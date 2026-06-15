# Contrabass agent — 제품 요구 사항 문서 (PRD)

## 1. 개요

- **프로젝트명**: Contrabass agent (저장소·작업 트리 디렉터리 예: `~/work/mol`)
- **언어**: Go
- **소스 위치**: `~/work/mol`
- **실행 형태**: 프론트엔드와 백엔드를 포함한 **단일 실행 파일**
- **소스 레이아웃**: 런타임 Go·웹·내장 스크립트·빌드 보조는 **`maintenance/`** 트리와 루트 **`main.go`** 로 구성한다(§1.1). 저장소 루트에는 **`main.go`**, **`go.mod`**, **`Makefile`**, **`build/build.sh`**(루트에서 `make "$@"` 호출), **`update.sh`·`rollback.sh`**, **`bin/ubuntu/`**(greenfield install/uninstall), 예시 설정 **`cfg/agent.local.yml`**, 참고 **`brd_for_bm.sh`** 등을 둔다. **`make build`** 는 **`build/image/contrabass-moleU-control`**·**`contrabass-moleU-compute`**(각 `-X main.BuildVariant=control|compute`)를 **`strip`** 한 뒤, 편의상 **control** 을 **`./contrabass-moleU`** 로 복사한다. **설정(YAML)** 은 패키지 **`maintenance/agentcfg`**(`maintenance_config.go` 등; Go **`agentcfg`**)에서 로드한다. **업데이트/롤백 셸**은 루트 스크립트를 **`maintenance/updatescripts/`** 로 복사한 뒤 **`//go:embed`** 로 바이너리에 포함한다(`Makefile` `build` 타깃이 동기화 후 `go build`). **버전 키 스크립트**·**배포 번들 패키징**은 각각 **`maintenance/scripts/`**, **`maintenance/packaging/`** 에 둔다.
- **진입점·종료 코드**: 루트 `main.go`는 빌드 시 주입되는 **`main.VersionKey`**·**`main.BuildVariant`**(ldflags `-X main.VersionKey=…`·`-X main.BuildVariant=control|compute`, `Makefile` 기본 `VERSION_KEY`는 **`./maintenance/scripts/build-version.sh`** 의 **`git describe --tags --long --always` 전체**, 예: `0.4.4-4-gc44d420`; **`make build VERSION_KEY=…`** 로 덮어쓸 수 있음)와 **`main()`** 의 argv 분기만 둔다. **`maintenance.Run(buildVersionKey, buildVariantArg, args []string) int`** 는 **명령줄은 `args` 인자로만** 받으며, 성공·오류는 **`0` 또는 `1`** 반환만으로 알린다(`maintenance` 패키지에서 `os.Exit`를 호출하지 않음). **`main()`** 은 그 반환값으로 **`os.Exit`** 한다. 분기 요약:
  - **`IsAgentSubcommand`** (`<bin> agent …` 전체, **`agent -cfg …` 서비스 포함**): Gin 없이 **`os.Exit(maintenance.Run(…))`** 만 수행하고 종료한다.
  - **`!IsServiceModeRootCfg`** (인자 없음, 루트 `--version`, 잘못된 argv 등): Gin 없이 동일하게 **`Run` 후 즉시 종료**한다.
  - **`IsServiceModeRootCfg`** (`<bin> -cfg <파일>`): **`MyGIN()`** + **`RegisterMaintenanceProxy`**, maintenance는 **`go func() { os.Exit(maintenance.Run(…)) }()`** 로 별도 고루틴에서 기동하고, 메인 고루틴은 **`router.Run("0.0.0.0:"+Server.HTTPPort)`** 에 블로킹한다(다른 Go 프로젝트에 병합할 때와 같은 orchestration). maintenance가 끝나면 고루틴의 **`os.Exit`** 로 프로세스 전체가 종료된다.
- **시그널**: **`SIGINT`/`SIGTERM` 처리는 `maintenance.runServiceWithConfigPath` 내부의 `signal.Notify`만** 사용한다. **`main`은 시그널 핸들러를 등록하지 않는다.** 서비스 종료 시 maintenance HTTP `Shutdown`·Discovery 소켓 정리 후 `Run`이 반환한다.
- **Gin·JSON 헤더(병합 시 주의)**: 바깥 Gin에 **전역** `Content-Type: application/json` 미들웨어를 두면 `/maintenance` 프록시의 HTML·CSS·JS가 잘못된 MIME으로 내려가 UI가 깨진다. JSON API는 루트 `main.go`의 **`routerGroupJSON(engine, "/prefix")`** 처럼 **라우트 그룹에만** 적용한다. **`MyGIN()`** 본문은 CORS 등만 설정한다.
- HTTP·Discovery 서비스·CLI 분기와 **`//go:embed web/*`** 는 **`maintenance/maintenance.go`** 에 모으며, 루트 Gin이 쓰는 **`GinProxyConfig`** 는 **`maintenance/ginproxy_config.go`**(`agentcfg.Load`)에 둔다. 바깥 Gin의 **WebPrefix/APIPrefix 리버스 프록시**는 **`maintenance.RegisterMaintenanceProxy`** → **`maintenance/ginproxy`**(**타 Go 모듈은 `contrabass-agent/maintenance` import 하나**). **`discoverycli.Run`** / **`applycli.Run`** / **`versionscli`** / **`hostinfocli.Run`** 은 각 `agent` 하위 명령에서 **종료 코드 `int`** 만 반환한다.
- **소스 트리와 테스트**: 배포용 저장소에는 Go **`*_test.go`** 단위 테스트 파일을 두지 않는다(단일 바이너리 산출물에는 원래 테스트가 포함되지 않으며, 소스 정책상 별도 테스트 파일 없이 유지한다). 회귀 검증이 필요하면 `go test`용 파일을 로컬·CI에서만 두거나 이력에서 복구한다.
- **웹 서버**: Go 표준 라이브러리 **net/http** 만 사용 (외부 웹 프레임워크 미사용)

### 1.1 `maintenance/` 소스 트리 (병합·정리 기준)

| 경로 | 역할 |
|------|------|
| **`maintenance/maintenance.go`** | `Run` — 서비스(`-cfg` / `agent -cfg`)·`agent` CLI 분기, embed `web/*`; argv: **`IsServiceModeRootCfg`**, **`IsServiceModeAgentCfg`**, **`IsAgentSubcommand`**, **`ConfigPathForServiceMode`**; **`RegisterMaintenanceProxy`** 는 `maintenance/ginproxy` 로 브리지 |
| **`maintenance/ginproxy_config.go`** | 루트 Gin용 **`GinProxyConfig(args)`** — `ConfigPathForServiceMode`와 동일 규칙으로 YAML 경로 결정 후 **`agentcfg.Load`** |
| **`maintenance/ginproxy/`** | 바깥 Gin **리버스 프록시 라우트** 등록(`RegisterMaintenanceProxy` 구현). Go import: **`contrabass-agent/maintenance/ginproxy`**(호출자는 보통 **`contrabass-agent/maintenance`** 만 import) |
| **`maintenance/agentcfg/`** | YAML `Config`, `Load`, 버전 키 비교, `MaxUploadBytes` 등. 핵심 파일명 **`maintenance_config.go`**, `maxuploadbytes.go`, `versionkey.go`. Go import: **`contrabass-agent/maintenance/agentcfg`** (패키지명 **`agentcfg`** — 일반 `config` 패키지와 병합 시 충돌 방지). |
| **`maintenance/updatescripts/`** | 루트 `update.sh`·`rollback.sh` 복사본 + `embed.go`(`//go:embed`) — 바이너리 내장 스크립트 |
| **`maintenance/scripts/`** | `build-version.sh`(Makefile `VERSION_KEY`), `pack-agent-tarball.sh`(배포 tar.gz 생성) |
| **`maintenance/packaging/`** | `contrabass.manifest.yaml.template` 등 번들 manifest 참고 |
| **`maintenance/server`**, **`discovery`**, **`web/`** 등 | HTTP·Discovery·정적 UI |

**`internal` 디렉터리 이름을 쓰지 않는 이유**: Go는 **`…/internal/…`** 패키지를 해당 `internal`의 **부모 디렉터리 이하**에서만 import할 수 있다. 루트 **`main.go`** 가 설정 패키지를 import해야 하므로, 저장소 루트에 `internal/config`를 두면 **가시성 규칙 위반**이 된다. 따라서 **`maintenance/agentcfg`**·**`maintenance/updatescripts`** 로 경로를 통일한다.

---

## 2. 아키텍처 요약

- **서비스 포트(maintenance HTTP)**: 설정 `Maintenance.MaintenancePort` (HTTP — 웹 UI + API). 기본적으로 `Maintenance.MaintenanceListenAddress = "127.0.0.1"` 로 **로컬호스트에만 바인딩**한다. **외부 접근**(브라우저·원격이 쓰는 `Server.HTTPPort`)은 **`<bin> -cfg …` 로 기동했을 때만** 루트 `main.go`의 **Gin**이 메인 고루틴에서 **`router.Run`** 으로 리슨하며 **`Maintenance.WebPrefix`·`Maintenance.APIPrefix`**(기본 `/web`, `/api/v1`) 경로를 maintenance로 **리버스 프록시**한다(프록시 구현 **`maintenance/ginproxy`**, 등록 **`maintenance.RegisterMaintenanceProxy`**). **`<bin> agent -cfg …`** 는 `main`이 **`IsAgentSubcommand`** 로 먼저 분기하므로 **Gin 없이** `Run`만 실행한다(HTTP·Discovery는 `Run` 안에서 동일). API가 웹 prefix 아래에 중첩된 경우(예: `WebPrefix=/maintenance`, `APIPrefix=/maintenance/api/v1`) Gin 라우터 제약으로 **와일드카드 한 트리**만 등록하고, 백엔드는 동일 URL 경로로 요청을 받는다. 프록시는 전달 전 **`Form`/`PostForm`을 비우고**, `URL.RawQuery`가 비어 있으면 **`RequestURI`의 쿼리**로 복구하여(표준 `ReverseProxy`+선행 파싱으로 쿼리가 유실되는 경우 방지) API **쿼리 파라미터**가 maintenance 핸들러까지 전달되도록 한다. 필요 시 `Maintenance.MaintenanceListenAddress = "0.0.0.0"` 로 외부 바인딩도 가능하다.
- **원격 호출 포트(Gin)**: 원격 호스트의 업데이트 로그(`update-log`), config(`current-cfg`), versions(list/remove), service-status 등은 **maintenance 포트가 아니라** 설정 `Server.HTTPPort`(외부 노출 포트, Gin)로 호출한다. (maintenance가 loopback-only인 경우 `http://<ip>:<MaintenancePort>`는 연결 거부가 정상이다.)
- **Discovery 포트**: **9999** (UDP — broadcast 수신·송신 및 응답 수신)
- 동일한 **contrabass-moleU** 에이전트 바이너리가 여러 서버 호스트에 분산 배포되며, **Discovery**를 통해 서로를 찾는다.
- Discovery는 **UDP broadcast** 방식으로 동작한다.

---

## 3. Discovery

### 3.1 흐름

- **요청**: 한 호스트(A)가 **Discovery에 사용할 broadcast 주소**의 **UDP 9999** 번 포트로 Discovery 요청을 보낸다. 브로드캐스트 주소는 **인터페이스 자동 수집**(아래 3.1.1)으로 얻은 IPv4 brd를 사용하며, 수집이 비어 있을 때만 설정 `discovery_broadcast_address`(단일)를 fallback, 그것도 없으면 255.255.255.255를 쓴다. **각 brd 주소마다** 한 번씩 요청을 전송하여 여러 서브넷을 탐색한다.
- **응답**: broadcast를 수신한 각 호스트는 Discovery 응답을 **unicast**로 보낸다. **DISCOVERY_REQUEST** JSON에 **`reply_udp_port`**(요청자가 응답을 받을 UDP 포트)가 있으면 **그 포트**를 우선한다(최신 에이전트). 없거나 0이면 **UDP 패킷의 소스 포트**, 그것도 0이면 discovery 포트로 보낸다. 이렇게 해서 CLI가 `--src-port`와 `--dest-port`를 다르게 써도, 커널에서 소스 포트가 잘못 보이는 환경에서도 응답이 맞게 간다.
- **요청**은 브로드캐스트 **목적지 포트** `DiscoveryUDPPort`(기본 9999)로 보낸다. **응답**은 요청자의 **소스 포트**로 온다(수신은 그 포트에서 하면 된다).
- **브로드캐스트 송신**: UDP 소켓에 **SO_BROADCAST** 옵션을 설정하여 broadcast 주소로의 전송을 허용한다.

### 3.1.1 Discovery 브로드캐스트 주소 수집 (상세)

Discovery에 쓸 IPv4 브로드캐스트(brd) 주소는 **설정이 아니라** `/sys/class/net/`·sysfs `type`·(브리지인 경우) `brif/`·`ip -4 -o addr show dev <iface>`로 수집한다. **이름으로 인터페이스를 거르지 않는다.** 목표는 **호스트 내부 전용 가상망이 아니라**, 물리 BM 간 브로드캐스트로 Discovery가 가능한 경로의 brd를 잡는 것이다(물리 NIC, bonding, VLAN, **슬레이브가 붙은** bridge 등). 인터페이스 이름 패턴(`docker*`, `veth*` 등)으로 제외하지 않는다.

**1. 인터페이스 열거**

- `/sys/class/net/`에서 OS가 인식한 **모든** 인터페이스 이름을 얻는다.

**2. 루프백 제외**

- `lo`만 이름으로 제외한다(외부 브로드캐스트 불가).

**3. sysfs `type` (이더넷 계열만)**

- `/sys/class/net/<iface>/type` 값이 **`1`(ARPHRD_ETHER)** 인 경우만 후보로 한다. 이더넷 기반으로 보는 물리 NIC·bond·VLAN·bridge·일부 TAP/TUN 등이 포함된다. `1`이 아니면 제외한다.

**4. 브리지: 슬레이브 유무**

- `/sys/class/net/<iface>/brif/` 디렉터리가 **있으면**(브리지 마스터) 그 안에 **슬레이브 인터페이스가 1개 이상** 있어야 한다. **0개**면(예: 내부망 전용 virbr) 제외한다. `brif/`가 없으면 브리지 마스터가 아니므로 이 검사를 건너뛴다.

**5. IPv4·brd 추출**

- 각 후보 인터페이스에 대해 `ip -4 -o addr show dev <iface>`로 IPv4 라인을 읽는다. IPv4가 없으면 제외한다. 출력 줄에 **`brd <주소>`**가 있으면 그 브로드캐스트 주소를 사용한다.

**6. 한 인터페이스·여러 주소**

- 한 인터페이스에 IPv4가 여러 개면 줄마다 brd를 볼 수 있다. **같은 인터페이스 안에서** 동일 brd는 한 번만 유지한다.

**7. 인터페이스 간 중복**

- **서로 다른 인터페이스**에서 같은 brd가 나오면, **`--nic-brd` 출력**에서는 **각각 한 줄씩** 내보낸다(`iface : brd` 형식). **Discovery UDP 송신 목록**을 만들 때는 **동일 brd 문자열은 한 번만** 써도 된다(같은 서브넷으로의 중복 전송 방지).

**8. fallback**

- 자동 수집 결과가 비어 있으면 설정 `discovery_broadcast_address`(단일)를 쓰고, 그것도 없으면 `255.255.255.255`를 쓴다.

**9. 확인용 CLI**

- **`contrabass-moleU agent --nic-brd`** 는 위 규칙과 동일하게 **(인터페이스 이름 : brd)** 를 한 줄씩 출력한다. 바깥 Gin(`Server.HTTPPort`)은 **`IsServiceModeRootCfg`**(`<bin> -cfg <파일>`)일 때만 `main`에서 기동한다. **`<bin> agent -cfg <파일>`** 로 서비스를 띄운 경우에는 Gin이 없다. **`IsAgentSubcommand`** 만 참인 **`agent` 다음의** `--nic-brd`·`--discovery`·`-h` 등 **CLI 전용 실행에서도 Gin이 바인딩되지 않는다**(`ConfigPathForServiceMode`는 `-cfg`/`agent -cfg` 서비스 형에서 비어 있지 않은 경로를 반환).

**10. 참고 스크립트 `brd_for_bm.sh` (저장소 루트)**

- **BM 간 브로드캐스트에 쓸 수 있는** IPv4 brd를, sysfs `type`·브리지 `brif/`·`ip -4 -o addr show` 로 골라 **`iface : brd`** 형식으로 출력하는 **bash 참고 구현**이다. 에이전트 내부의 `maintenance/hostinfo` 브로드캐스트 수집과 **동일한 설계 의도**를 따르며, 셸·Go 간 줄 단위 출력이 완전히 같을 필요는 없다(파싱 방식 차이 허용). 운영 호스트에서 brd 목록을 빠르게 확인하거나 스펙 검토용으로 쓴다.

### 3.1.2 DISCOVERY_REQUEST 페이로드 크기 (UDP·MTU)

- IPv4 브로드캐스트 UDP 패킷은 일반적으로 **한 MTU**(대략 1500바이트) 단위로 전달된다. IP·UDP 헤더와 여유를 두고, **DISCOVERY_REQUEST** JSON 본문은 마샬한 뒤 길이가 **1300바이트 미만**이어야 한다.
- 서버·CLI는 요청을 보내기 전에 위 한도를 검사하고, **1300바이트 이상**이면 오류로 처리한다(브로드캐스트 단편화·손실 위험 방지).

### 3.2 백엔드 동작 세부 (요청자)

- **Pending 등록 순서**: 요청자 측에서는 **브로드캐스트를 보내기 전에** `request_id` → 수신 채널을 **pending** 맵에 등록한다. 응답이 매우 빨리 도착(자기 자신 응답, 동일 LAN 응답)해도 "no pending waiter"로 버려지지 않도록 하기 위함이다.
- **타임아웃**: 설정된 시간(기본 10초) 동안 응답을 수집한다. **타이머가 만료될 때** 채널과 타이머가 동시에 준비되면 `select`가 타이머만 선택할 수 있으므로, 반환 전에 **채널을 한 번 비우고(drain)** 남아 있는 응답을 모두 처리한 뒤 반환한다.
- **Self 제거**: 수집된 목록에서 **자기 자신**은 제외한다. 자기 식별에는 **CPU UUID**를 사용한다: 응답의 `cpu_uuid`가 로컬 getter의 CPU UUID와 같으면 self로 제외한다. CPU UUID가 없는 환경에서는 **IP + ServicePort**로 폴백(브로드캐스트 outbound IP와 일치하면 제외)한다. 이렇게 하면 로컬이 여러 IP로 응답하는 경우에도 한 번만 제외된다.

### 3.3 백엔드 동작 세부 (응답자)

- **응답의 host_ip**: DISCOVERY_RESPONSE에는 **host_ip 하나만** 넣어 보낸다. 이 값은 **요청자로 나갈 때의 outbound IP**(요청자 쪽에서 보이는 주소)이다. 요청을 보낸 주소(예: 172.29.236.41)에 따라 outbound IP가 달라지므로, 같은 호스트가 여러 인터페이스(예: .236, .237)로 응답하면 응답마다 다른 host_ip가 담긴다. **host_ips 배열은 응답 메시지에 넣지 않고**, 수신 측에서 같은 호스트(cpu_uuid)의 여러 응답을 받아 IP를 취합한다. outbound IP를 구할 수 없을 때만 hostinfo 기본 IP를 사용한다.

### 3.4 메시지 형식

**요청 예시**

```json
{
  "type": "DISCOVERY_REQUEST",
  "service": "Mole-Discovery",
  "request_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "reply_udp_port": 9998
}
```

- `service`: 요청 대상 서비스 식별자. 설정 **`DiscoveryServiceName`** 과 **일치하는** DISCOVERY_REQUEST만 응답자가 처리한다(기본값 `"Mole-Discovery"`).
- `reply_udp_port`(선택, 0이면 생략 가능): 응답을 보낼 **목적지 UDP 포트**. CLI·최신 에이전트는 로컬 바인드 포트를 넣는다. 응답자는 이 값이 0보다 크면 **UDP 패킷의 소스 포트보다 우선**한다.

**응답 예시** (호스트 정보 포함)

```json
{
  "type": "DISCOVERY_RESPONSE",
  "service": "Mole-Discovery",
  "host_ip": "172.29.237.41",
  "hostname": "example-host-41",
  "service_port": 8889,
  "version": "0.2.0-0",
  "build_variant": "compute",
  "request_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "cpu_info": "Intel Xeon 8 cores",
  "cpu_usage_percent": 23.5,
  "cpu_uuid": "550e8400-e29b-41d4-a716-446655440000",
  "memory_total_mb": 16384,
  "memory_used_mb": 8192,
  "memory_usage_percent": 50.0,
  "responded_from_ip": "172.29.236.50"
}
```

- `service_port`: **maintenance HTTP API가 리슨하는 TCP 포트**(`Maintenance.MaintenancePort`, 예: 8889). `Server.HTTPPort`(Gin, 예: 8888)나 UDP Discovery 포트(`DiscoveryUDPPort`, 예: 9999)와는 별개다. 중복 제거 키 `host_ip:service_port` 등에 쓰인다.
- 위 예시는 **다른 호스트(다른 서브넷)에서 온 Discovery 요청**에 대한 응답을 가정한다. 응답자가 그 요청자로 나갈 때의 outbound IP는 `host_ip`(172.29.237.41)이고, 수신 측에서 본 이 UDP 패킷의 발신지 IP는 `responded_from_ip`(172.29.236.50)로 서로 다를 수 있다(같은 호스트가 여러 NIC로 응답한 경우 등).
- `request_id`: 요청 시 생성한 UUID를 응답에 그대로 넣어 요청·응답 매칭에 사용한다.
- `cpu_uuid`: 호스트 식별용(동일 호스트 병합·self 제거에 사용). 없을 수 있음.
- `build_variant`: **`control`** 또는 **`compute`**. 빌드 시 `-X main.BuildVariant=…` 로 주입. 레거시·미주입 바이너리는 생략 가능(HTTP/CLI에서는 `-` 또는 UI 기본 `compute`).
- **응답자는 host_ip 하나만 보낸다.** 같은 호스트가 여러 NIC으로 응답하면 응답마다 다른 host_ip(해당 요청에 대한 outbound IP)가 담긴다. **수신 측**에서 같은 cpu_uuid의 여러 응답을 받아 IP 목록을 취합하여 화면에 표시한다.
- `responded_from_ip`: (수신 측 설정) UDP 패킷의 **발신지 IP**로, 수신 측이 응답을 처리할 때 채운다. 화면에서 "응답한 IP"로 표시하며, 같은 호스트가 여러 IP로 응답한 경우 모두 취합해 보여준다. 전선 상의 메시지에는 없고, API/SSE로 내보낼 때만 포함된다.
- 자기 정보 API(GET /self)에서는 브로드캐스트 대역별 outbound IP를 `host_ips` 배열로 반환할 수 있다. Discovery UDP 응답 메시지 자체에는 host_ips를 넣지 않는다.
- 호스트 정보(CPU, MEMORY)는 위 필드로 확장하며, 단위·필드명은 이 스키마를 기준으로 한다.

### 3.5 중복 제거 및 설정

- **중복 제거**: 스트림/일괄 반환 시 동일한 (host_ip:service_port@responded_from_ip) 조합은 한 번만 전달한다. 즉 같은 호스트가 여러 IP로 응답하면 **응답 건수만큼** 이벤트가 나가며, 각 이벤트의 host_ip·responded_from_ip가 다를 수 있다. 설정 `DiscoveryDeduplicate`로 켜/끌 수 있다.
- **동일 호스트 병합(프론트)**: `cpu_uuid`가 같은 응답은 **한 호스트**로 간주한다. 카드는 하나만 두고, **IP**는 각 응답의 host_ip를 모두 취합해 표시하고, **응답한 IP**는 각 응답의 responded_from_ip를 모두 취합해 표시한다. CPU·메모리는 응답 중 하나만 사용한다. **기존 카드 찾기**는 **cpu_uuid** → **IP**(host_ip / data-host-ips) 순으로만 하며, **hostname으로는 찾지 않는다**. 서로 다른 물리 호스트가 같은 hostname(예: kt-vm)을 쓰면 hostname으로 찾을 경우 한 카드로 잘못 병합되므로 hostname 매칭을 사용하지 않는다.
- **타임아웃**: 응답 수집 대기 시간은 설정 `Maintenance.DiscoveryTimeoutSeconds`로 지정한다. 설정값이 **0 이하**이면 구현상 **10초**를 쓴다. HTTP 일괄·SSE Discovery API에는 쿼리 **`timeout`(초, 1~600)** 로 **한 요청만** 덮어쓸 수 있다(미지정 시 위 설정·기본).

### 3.6 실시간 Discovery (SSE)

- Discovery 결과를 **타임아웃 만료를 기다리지 않고** 응답이 도착하는 대로 화면에 반영한다.
- **백엔드**: `GET {APIPrefix}/discovery/stream` 엔드포인트를 두고, **Server-Sent Events(SSE)** 로 스트리밍한다. Discovery 요청을 보낸 뒤, 각 DISCOVERY_RESPONSE가 올 때마다 `data: {JSON}\n\n` 형식으로 한 건씩 전송하고 즉시 flush한다. 타임아웃이 되면 `event: done\ndata: {}\n\n` 를 보내고 스트림을 종료한다. 내부적으로는 **DoDiscoveryStream** 과 같이 요청 시 pending 등록 → 브로드캐스트 전송 → 수신 채널에서 응답을 하나씩 읽어 **includeInDiscoveryResults**(기본: 자기 응답 포함·`self`: true, **쿼리 `exclude_self`로 자기 제외 가능**)·중복 제거 후 SSE로 내보내는 방식을 사용한다. 쿼리 파라미터는 **§5.3**과 동일.
- **스트림 시작 전 실패**(예: DISCOVERY_REQUEST JSON 크기 제한 위반, 브로드캐스트 주소 없음 등): 브라우저 **EventSource** 는 HTTP 4xx/5xx 응답 본문을 읽지 못하므로, 서버는 **HTTP 200** 으로 SSE 헤더를 연 뒤 **`event: discoveryfail`** 한 번만 보내고 `data` 에 JSON `{"message":"…"}` 형태로 상세 사유를 실은 다음 스트림을 닫는다. 동일 실패는 **표준 로그**에 `discovery: ERROR: DoDiscoveryStream failed: …` 처럼 남겨 **`journalctl -u contrabass-mole.service`** 등으로 확인할 수 있다.
- **프론트엔드**: Discovery 버튼 클릭 시 **EventSource** 로 `{APIPrefix}/discovery/stream` 에 연결한다(설정 기본은 `/api/v1/discovery/stream`). **`discoveryfail` 이벤트**가 오면 `data.message` 를 읽어 상태 영역에 **「Discovery 요청 실패:」+ 서버 메시지**를 표시하고 스트림을 닫는다. 일반 메시지 이벤트가 올 때마다 수신한 JSON을 파싱해, **같은 CPU UUID**가 이미 있으면 해당 카드에 IP·응답한 IP를 병합·갱신하고, 없으면 **같은 IP**가 있는 카드를 찾아 갱신하고, 그 외에는 **새 카드**를 추가한다. 기존 카드 매칭은 cpu_uuid → IP 순서만 사용하며 hostname은 사용하지 않는다. **`event: done`** 수신 시 **이번 run에 UDP 응답이 없었던 기존 카드**에는 **「이번 Discovery 미응답」** 표시(한 줄 요약 배지·펼친 카드 안내 배너)를 남기고, 응답한 카드는 표시를 제거한다. 카드 자체는 삭제하지 않는다. 완료 문구 예: `호스트 3개 표시 · 이번 Discovery 응답 2대 · 미응답 1대.`

### 3.7 유니캐스트 Discovery (단일 호스트 조회)

- **목적**: 특정 IP의 호스트 정보(버전, CPU, 메모리 등)만 갱신할 때 사용한다.
- **동작**: 해당 IP의 Discovery UDP 포트(9999)로 **DISCOVERY_REQUEST를 유니캐스트**로 보낸다. 해당 호스트만 응답하므로 **한 건의 DISCOVERY_RESPONSE**를 수신한다.
- **타임아웃**: 응답 대기 시간은 Discovery 타임아웃 설정을 따르되, **최대 5초**로 제한한다.
- **매칭**: 수신한 응답의 `host_ip`가 요청한 IP와 일치하는지 확인한 뒤 반환한다.

### 3.8 로깅 (구현 참고)

- 디버깅·운영 시 다음을 로그로 남길 수 있다: DISCOVERY_REQUEST 수신(발신지 주소), DISCOVERY_RESPONSE 전송(대상 주소), DISCOVERY_RESPONSE 수신(발신지, request_id, delivered / no pending waiter / channel full).
- **Discovery 오류(요청 측)**: 일괄 API `GET /api/v1/discovery`·유니캐스트 `host-info`·스트림 `DoDiscoveryStream` 이 실패하면 **`discovery: ERROR:`** 접두가 붙은 한 줄을 표준 로그로 남긴다. systemd·`journalctl -u <contrabass-mole.service>` 에서 동일 문구를 검색할 수 있다.

---

## 4. URL 및 라우팅

- **프론트엔드 prefix**: `{serverUrl}{WebPrefix}` (기본 `/web`, 설정 `Maintenance.WebPrefix`)
- **백엔드 API prefix**: `{serverUrl}{APIPrefix}` (기본 `/api/v1`, 설정 `Maintenance.APIPrefix`)
- **프론트엔드 진입 URL**: `{serverUrl}{WebPrefix}/index.html`
- prefix는 설정 파일에서 수정할 수 있어야 한다. 브라우저는 하드코딩된 `/api/v1`가 아니라, 서버가 `{WebPrefix}/client-runtime.js`로 내려주는 **`window.__CONTRABASS_API_PREFIX__`**(실제 `APIPrefix`)와 **`window.__CONTRABASS_REMOTE_HEALTH__`**(원격 HTTP 헬스 폴링 간격·타임아웃·실패 임계·지터, §7.1 `Maintenance.RemoteHealth`)를 먼저 로드한 뒤 `app.js`가 API를 호출한다.

### 4.1 CLI (명령줄)

- **인자 없이 실행**: **`contrabass-moleU`** — 버전과 **`-cfg <파일>`**(HTTP·Discovery 기동) 및 **`agent …`**(기타 CLI) 안내를 출력하고 종료한다. HTTP·Discovery 서비스는 **`<bin> -cfg <파일>`** 또는 **`<bin> agent -cfg <파일>`** 로 설정 파일을 지정했을 때 기동한다(`Run` 동작 동일). **바깥 Gin** 은 **`<bin> -cfg …` 일 때만** `main`이 연다.
- **`-cfg <파일>`**(서비스): 설정 파일 경로(필수 인자). **`contrabass-moleU`의 첫 인자로 `-cfg`** 와 경로를 두면 HTTP·Discovery가 기동한다. systemd 등에서는 `ExecStart=.../contrabass-moleU -cfg /path/to/agent.local.yml` 형태를 권장한다. **`contrabass-moleU agent -cfg <파일>`** 도 동일하게 HTTP·Discovery를 기동한다.
- **접두**: **`-h`·`--host-info`·`--discovery` 등**(서비스용 `-cfg` 제외)은 모두 **`contrabass-moleU agent …`** 형태(첫 인자 **`agent`**)로 호출한다.
- **`-h`, `--help`**: 도움말(사용법·옵션 설명) 출력 후 종료. **`agent` 다음**에만 지원(`contrabass-moleU agent --help`).
- **`-version`, `--version` (두 경로)**  
  - **권장**: **`contrabass-moleU agent --version`** 또는 **`agent -version`** — 다른 CLI와 동일하게 `agent` 접두.  
  - **전환용(루트)**: **`contrabass-moleU --version`** / **`-version`** — 구버전 업데이트·외부 스크립트가 루트 플래그만 호출하는 경우를 위해 **`agent` 없이** 한 줄 출력을 허용한다. 향후 제거·비권장으로 좁힐 수 있다.  
  - 출력 형식은 동일: **`<BinaryName> <main.VersionKey>`** 한 줄.
- **`--host-info`**: **`-apiprefix`**(선택, 기본 `/maintenance/api/v1`)와 **`<self|원격 IP>`** 한 인자. 대상 에이전트 **Gin(`Server.HTTPPort`, 기본 8888)** 에 **`GET {APIPrefix}/self`** — **대상 HTTP 서비스가 떠 있어야 한다**. 표준 출력은 DISCOVERY_RESPONSE 주요 필드를 영문 라벨 표로 출력(`BUILD_VARIANT` 포함). 구현: `maintenance/hostinfocli` → `maintenance/clirest`. **`-h` 도움말 순서**: `-h` 다음 `-version` 다음 **`--host-info`** …
- **`--nic-brd`**: §3.1.1과 동일 규칙으로 IPv4 브로드캐스트(brd)를 `NIC이름 : brd주소` 형식으로 출력(확인용) 후 종료.
- **`--discovery`**: 설정 파일·HTTP 서버 없이 **UDP Discovery만** 수행. `--dest-port`(기본 9999), `--src-port`(기본 9998), `--timeout`(초, 기본 10), `--service`(기본 `Mole-Discovery`). 시작 시 **사용 가능한 brd(브로드캐스트) 주소를 모두 한 줄씩 출력**한다. 에이전트와 같이 **서브넷별로 로컬 IP:src-port 소켓을 열어** 각 brd로 송신한다(다중 NIC·src≠dest 안정화). `reply_udp_port` 포함 `DISCOVERY_REQUEST` 전송 후, 같은 줄에서 `Discovering ... N` 카운트다운 → **`Discovery Done.`** → 수신 유예·드레인. 결과는 호스트별 **`[Local]`** / **`[Remote]`** `hostname - 대표 IP : [응답한 IP만] version=<에이전트 버전 키> (<control|compute>)` 형식으로, **`responded_from_ip`**만 취합하고 **버전·variant**는 DISCOVERY_RESPONSE JSON의 **`version`**·**`build_variant`** 필드(§3.4·§9)를 표시한다(웹 UI와 같이 variant는 버전 뒤 괄호; 버전 없으면 `version=?`, variant 없으면 괄호 생략). Local/Remote는 **CPU UUID 일치(대소문자 무시)** 우선, 아니면 **응답한 IP가 로컬 IPv4와 겹치는지**로 보조 판별한다.
- **`--apply-update`**: **`-apiprefix`**(선택), **`-agent-variant`**(선택), **`<self|원격 IP>`**, **`<bundle.tar.gz>`**. 대상 Gin에 **`POST …/upload`** 후 **`POST …/apply-update`**(JSON). **대상 HTTP 서비스 필수**. 검증·정책·적용은 서버 핸들러가 처리(웹 UI와 동등). `maintenance/applycli` → `maintenance/clirest`. **CLI 도움말·진단 메시지**는 **영문**.
- **`--versions-list`**: **`-apiprefix`**(선택)와 **`<self|원격 IP>`**. **`GET {APIPrefix}/versions/list`** on target Gin. **대상 HTTP 서비스 필수**.
- **`--versions-switch`**: **`-apiprefix`**(선택), **`<self|원격 IP>`**, **`<버전 키>`**. **`POST {APIPrefix}/versions/switch-current`**. **대상 HTTP 서비스 필수**.

---

## 5. API

**엔드포인트별 메서드(GET/POST)·쿼리/바디·응답 형식 요약**은 [`docs/REST_API.md`](docs/REST_API.md)를, **CLI 옵션**(`--discovery`, `--apply-update`, `--versions-list`, `--versions-switch`, `--host-info` 등)은 [`docs/CLI.md`](docs/CLI.md)를 본다.

### 5.1 공통 응답 형식 (일반 API)

- **status**: `"success"` 또는 `"fail"`
- **data**: 숫자, 문자열, 배열 등 JSON으로 표현 가능한 값
- **문자열 `data` 언어**: 적용·전환·업로드 삭제·원격 프록시 오류 등 **사용자에게 보이는 API 메시지는 영문**이다. CLI(원격 호출 시 stdout)와 웹 UI가 동일 JSON을 소비한다. 웹 화면 라벨·버튼 문구는 한국어(§6)를 유지할 수 있다.

### 5.2 자기 정보 API

- **목적**: 초기 화면에 “내 정보”를 표시하기 위함.
- **엔드포인트**: `GET {serverUrl}/api/v1/self`
- **응답**: 위 공통 형식(`status`, `data`)을 따르며, `data`에는 DISCOVERY_RESPONSE와 동일한 구조의 호스트 정보를 넣는다.
  - 버전, IP, 호스트명, service_port, CPU 정보, MEMORY 정보 등.
- **IP 표시**: "내 정보"의 IP는 **브로드캐스트 대역에서 사용하는 로컬 IP**로 노출한다. Discovery에 사용하는 broadcast 주소로 나갈 때의 outbound IP를 사용하며, 구할 수 없을 때만 hostinfo 기본 IP를 사용한다.

### 5.2.1 호스트 정보 API (원격 단일 호스트)

- **목적**: 발견된 호스트 카드에서 "상태 새로고침" 시 해당 호스트의 최신 정보(버전, CPU, 메모리 등)를 가져오기 위함.
- **엔드포인트**: `GET {serverUrl}/api/v1/host-info?ip=`
- **동작**  
  - `ip`가 비어 있거나 `"self"`: `/api/v1/self`와 동일하게 로컬 호스트 정보를 반환한다.  
  - `ip`가 지정됨: 해당 IP로 **Discovery 유니캐스트** 요청을 보내 그 호스트의 DISCOVERY_RESPONSE를 받아 `data`에 넣어 반환한다.
- **응답**: 공통 형식. 성공 시 `data`는 DISCOVERY_RESPONSE와 동일한 구조. 타임아웃 또는 응답 없음 시 `status: "fail"`, `data`에 에러 메시지.

### 5.3 Discovery API

- Discovery 요청은 **프론트엔드의 Discovery 버튼**에 의해 트리거되며, **웹 UI는 스트리밍 API만 사용**한다(쿼리 없음 → 기본 동작).
- **공통 쿼리 (일괄·SSE 모두, `GET …/discovery`, `GET …/discovery/stream`)**  
  - **`exclude_self`**: `true` / `1` / `yes` / `on` 이면 **이 호스트(자신) 응답을 결과에서 제외**. 생략 또는 그 외 값이면 **포함**하며, 포함 시 JSON에 `"self": true`(해당 시). 별칭 **`exclude-self`** 동일.  
  - **`timeout`**: 정수 **초**, **1~600**. 한 요청의 수집 대기 시간만 덮어쓴다. 생략 시 `Maintenance.DiscoveryTimeoutSeconds`(0 이하이면 구현상 10초).  
  - 파싱은 `URL` 쿼리 문자열이 비어 있으면 **`RequestURI`**의 `?` 이후로도 시도한다(프록시·클라이언트 조합 대비).
- **실시간 스트리밍 (웹 UI 사용)**: `GET {serverUrl}{APIPrefix}/discovery/stream` — **Server-Sent Events(SSE)**. Content-Type `text/event-stream`. 응답이 올 때마다 `data: {JSON}\n\n` 로 호스트 한 건씩 전송, 타임아웃(설정 또는 `timeout` 쿼리) 시 `event: done\ndata: {}\n\n` 후 스트림 종료. **스트림을 열기 전 단계에서 실패**하면(페이로드 검증 등) 위 **§3.6** 과 같이 **`event: discoveryfail`** + `data: {"message":"…"}` 를 보내고 종료한다(쿼리 파싱 오류도 동일 형식으로 안내 가능). 웹 UI는 Discovery 버튼 클릭 시 EventSource로 이 엔드포인트만 호출하며(쿼리 없음), 응답이 오는 대로 화면에 반영하고 `event: done` 수신 시 스트림을 닫고 버튼을 복구한다. 타임아웃 이후 별도의 일괄 API 호출은 하지 않는다.
- **일괄 반환 (웹 UI 미사용)**: `GET {serverUrl}{APIPrefix}/discovery` — 타임아웃까지 수집한 뒤 `status` + `data`(발견된 호스트 배열)를 한 번에 JSON으로 반환. `data`는 배열이며, 결과가 없어도 `[]` 로 반환한다(null 아님). **웹 UI에서는 호출하지 않으며**, 스크립트·다른 클라이언트용. 스트림과 동일한 **include** 규칙·쿼리(`exclude_self`, `timeout`)를 지원한다.

### 5.4 서비스 상태·제어 API

- **서비스 상태**: `GET {serverUrl}/api/v1/service-status?ip=`  
  - `ip` 비어 있거나 `"self"`: 로컬에서 `systemctl status <systemctl_service_name>` 실행( **sudo 없음**, contrabass-mole.service는 root로 실행), 결과를 `{ "status": "success", "data": { "output": "..." } }` 로 반환.
  - `ip` 지정: 요청을 받은 서버가 **원격 호스트의 `Server.HTTPPort`(Gin)** 로 `GET .../service-status` 를 호출한다. 원격 에이전트는 자기 서버에서 `systemctl status` 를 실행한 뒤 그 결과를 응답으로 반환하고, 요청자는 그 응답을 그대로 클라이언트에 전달한다.
  - 실패 시 `{ "status": "fail", "data": "에러 메시지" }`.
- **서비스 제어**: `POST {serverUrl}/api/v1/service-control`  
  - Body: `{ "ip": "" | "self" | "<host_ip>", "action": "start" | "stop" | "restart" }`.  
  - `ip` 비어 있거나 `"self"`: 로컬 `systemctl start/stop/restart <systemctl_service_name>` (contrabass-mole.service는 root로 실행).  
  - **원격 start/stop**: 요청을 받은 서버가 대상 호스트로 **SSH** 접속(`SSHPort`·`SSHUser` 설정 사용, 미지정 시 22·root)하여 `systemctl start` 또는 `stop <서비스명>`을 실행한다. 원격 에이전트가 중지된 상태여도 SSH로 시작할 수 있다.  
  - **원격 restart**: SSH를 사용하지 않고, 요청을 받은 서버가 **원격 `Server.HTTPPort`(Gin)** 로 `POST .../service-control` (Body: `{ "ip": "self", "action": "restart" }`)를 호출한다. 원격 에이전트는 자기 서버에서 `systemctl restart` 를 실행한 뒤 응답을 반환한다. SSH 공개키 등록 없이 재시작 가능하다.  
  - 성공 시 `{ "status": "success", "data": null }`, 실패 시 `{ "status": "fail", "data": "에러 메시지" }`.
- **원격 일괄 재시작**: `POST {serverUrl}/api/v1/service-control/restart-all`  
  - **Body**(선택): `{ "hosts": [ { "primary_ip", "hostname", "cpu_uuid", "ips": [] }, … ] }` — **웹 UI 카드 1장 = 호스트 1대**(권장). `ips`는 해당 카드의 접속 후보 IP 목록이며, 호스트마다 순서대로 시도해 **첫 성공 시** 다음 호스트로 넘어간다. 레거시 `{ "ips": [] }` 도 지원.  
  - **동작**: 호스트별로 위 **원격 restart** 프록시(`POST …/service-control`, `ip: self`, `action: restart`)를 호출한 뒤, **2초 대기** 후 **최대 45초·2초 간격**으로 재기동을 확인한다 — `GET …/health`(JSON `success`) 또는 `GET …/service-status` 출력의 `Active: active (running)`. 연결 끊김(connection reset·EOF·broken pipe 등)은 **재시작 진행 중**으로 간주(단일 카드 「서비스 재시작」과 동일).  
  - **응답**: `Content-Type: application/x-ndjson`. `start` → 호스트별 `progress`(`verify_ok`, `verify_detail`, `connect_ip`, `tried_ips`) → `done`. 완료 시 `{DeployBase}/update_history.log`에 **요약 1줄**만 append: `service restart-all finished succeeded=N failed=M`.  
  - **웹 UI**: §6.6.

### 5.4.1 설정(current-config) API·원격 레지스트리

- **조회·저장**: `GET/POST {APIPrefix}/current-config` — Query/Body `ip`(선택). 로컬은 `current` 심볼릭 대상의 config YAML; 원격은 `Server.HTTPPort`로 프록시. POST 시 **`backup_before_write`: true** 이면 덮어쓰기 전 `{DeployBase}/current/` 아래 **`agent.local.yml.backup`**(또는 manifest config basename + `.backup`)으로 백업.  
- **로컬 → 원격 1대 복사**: `POST {APIPrefix}/current-config/push-local` — Body `{ "ip": "<원격 IP>" }`. **이 서버(로컬) `current` config** 내용을 읽어 해당 원격 `POST …/current-config`로 전송(`backup_before_write`: true).  
- **로컬 → 원격 일괄 복사**: `POST {APIPrefix}/current-config/push-local-all` — Body는 **§5.4 restart-all**과 동일한 `hosts`/`ips` 형식. 호스트별 NDJSON `progress` 스트림; 완료 시 `update_history.log`에 **요약 1줄**: `config push-all finished succeeded=N failed=M`. per-host 상세는 스트림·웹 「결과 보기」에만 표시.  
- **발견 원격 스냅샷**: `GET {APIPrefix}/discovered-remotes` — 서버 메모리 **`remoteregistry`**(Discovery 스트림·host-info·update-status 등으로 채움, **프로세스 수명** 동안 유지, 헬스 실패 `health_dead` 포함)의 스냅샷. 일괄 API의 `hosts` body가 없을 때 fallback으로 쓸 수 있으나, **웹 UI는 화면 DOM 카드 목록을 body로 보내는 것을 권장**(에이전트 재시작 후 레지스트리가 비어도 DOM은 유지될 수 있음).  
- **일괄 작업 기록**: config push-all·restart-all·**apply-update-all**·**rollback-all** 요약 줄은 **`appendDeployHistory`**(`update_history.log`, embedded `update.sh`와 동일 **`flock`**)로 append한다. **`update …` / `rollback …` 줄과 구분**되며, `GET …/update-log`의 **`recent_rollback`** 판별(§5.5.4)에서는 **업데이트 실패로 취급하지 않는다**.

### 5.5 업데이트 API

업로드·원격 배포·`agent --apply-update` 가 공통으로 쓰는 **배포 번들** 형식은 **§5.5.0** 에 정리한다. 스테이징·적용·`update.sh` 동작은 **§5.5.1** 이하를 따른다.

#### 5.5.0 배포 번들 (tar.gz)

웹 UI **「업로드」**, REST **`POST …/upload`**, **`agent --apply-update`**, 원격 **`POST …/apply-update`**(multipart `bundle`)가 받는 페이로드는 모두 **하나의 gzip 압축 tar 아카이브(`*.tar.gz`)** 이다. 번들 안에는 **에이전트 실행 파일**(하나 또는 둘), **에이전트 설정 YAML**, 그리고 파일의 경로·무결성을 선언하는 **manifest** 가 반드시 포함된다.

##### 구성 요소

| 구성 | 필수 | 설명 |
|------|------|------|
| **에이전트 바이너리** | 예 | Linux **ELF** 실행 파일. **manifest v2**에서는 두 variant — **`contrabass-moleU-control`**(`agent_control`)·**`contrabass-moleU-compute`**(`agent_compute`) — 를 각각의 `path`·`sha256`으로 선언한다. v1(레거시)은 단일 `agent`. 빌드 시 `-X main.BuildVariant=control\|compute`로 variant를 구분한다. |
| **설정 YAML** | 예 | 에이전트가 기동 시 읽는 설정(예: **`agent.local.yml`**). manifest `config.path` 로 지정. 업로드 시 **`maintenance/agentcfg`** 구조체로 파싱·검증한다(`DiscoveryUDPPort`, `MaintenancePort`, `DeployBase` 등). **버전 문자열은 config에 넣지 않는다** — 버전 키는 바이너리에서만 읽는다(§5.5.1·§12). |
| **`contrabass.manifest.yaml`** | 예 | 아카이브 **루트**에 두는 manifest. **`manifestVersion: 2`** (현재 기본) 및 레거시 **`1`** 을 지원한다. |

manifest에 **선언되지 않은** 추가 파일을 tar에 넣어도, 서버 검증·스테이징은 **manifest가 선언한 멤버**를 기준으로 한다.

##### 아카이브 레이아웃

- **형식**: **tar.gz** (gzip + POSIX tar). multipart 필드명은 **`bundle`**(구현 상수 `uploadBundleField`).
- **권장 레이아웃**(패키징 스크립트 기본, manifest v2): 아카이브 루트에 **평면(flat)** 으로 다음 네 파일.

  ```
  contrabass.manifest.yaml
  contrabass-moleU-control   # BuildVariant=control, 실행 권한 0755
  contrabass-moleU-compute   # BuildVariant=compute, 실행 권한 0755
  agent.local.yml
  ```

- **경로 규칙**: manifest의 `agent_control.path`·`agent_compute.path`·`config.path` 는 **아카이브 루트 기준 상대 경로**(예: `./contrabass-moleU-control`). `..`·절대 경로는 거부한다. 서브디렉터리 경로도 허용되나, 스테이징에는 **basename** 으로 저장한다(§5.5.1).
- **안전 압축 해제**: 서버는 **심볼릭/하드 링크 금지**, 경로 탈출 차단, tar 항목 **최대 512개**, 압축 해제 총량은 요청 상한(`Maintenance.MaxUploadBytes`)의 배수로 제한(`maintenance/server/bundleupload.go`의 `extractTarGzSafe`).

##### manifest 내용

참고 파일:

- **템플릿**: `maintenance/packaging/contrabass.manifest.yaml.template` — `__CONTROL_SHA256__`·`__COMPUTE_SHA256__`·`__CONFIG_SHA256__` 플레이스홀더.
- **예시(값은 설명용)**: `maintenance/packaging/contrabass.manifest.example.yaml`.

**manifest v2 (현재 기본)**:

```yaml
manifestVersion: 2

bundle:
  format: tar.gz

agent_control:
  path: ./contrabass-moleU-control
  sha256: "<64자 hex>"

agent_compute:
  path: ./contrabass-moleU-compute
  sha256: "<64자 hex>"

config:
  path: ./agent.local.yml
  sha256: "<64자 hex>"
```

**manifest v1 (레거시, 단일 에이전트)**:

```yaml
manifestVersion: 1

bundle:
  format: tar.gz

agent:
  path: ./contrabass-moleU
  sha256: "<64자 hex>"

config:
  path: ./agent.local.yml
  sha256: "<64자 hex>"
```

- v2에서는 두 바이너리의 `sha256`을 **각각** 검증한다. 두 바이너리의 `--version` 출력(버전 키)은 **동일**해야 한다.
- **`sha256`**: 업로드 시 디스크에 풀린 파일과 **바이트 단위 일치**를 검증한다. 불일치 시 400.

##### agent_variant (적용 시 바이너리 선택)

manifest v2 번들에는 control·compute 두 바이너리가 모두 포함된다. **적용(`apply-update`) 시점**에 `agent_variant` 파라미터(`"control"` 또는 `"compute"`)로 **어떤 바이너리를 `contrabass-moleU`(BinaryName)로 설치할지** 결정한다(`MaterializeCanonicalAgent`). 스테이징에는 항상 두 바이너리 모두 보관되며, variant 선택 후 canonical 바이너리가 복사된다. **명시적으로 비우거나 생략하지 않고 API가 `compute`만 보낼 때**는 `compute`; **CLI·웹에서 생략**하면 적용 대상의 **설치된 `build_variant`** 를 따른다(§352).

- **웹 UI**: 로컬 패널의 variant 라디오는 **스테이징에 dual agent가 있을 때만** 표시하며, 기본 선택은 **실행 중인 `build_variant`**(self·host-info·카드 `data-build-variant`, 미상이면 `compute`). 리모트 카드의 variant 라디오는 **「업데이트 적용」이 활성**이고 dual-agent 스테이징(또는 multipart로 tar.gz만 전송)일 때만 표시한다. `GET …/update-status?ip=` 결과로 `can_apply`가 false이면(`AllowSameVersionUpdate` false로 원격이 이미 스테이징과 동일 버전 등) **적용 버튼·variant 선택을 함께 비활성·숨김**한다 — 업로드 영역에 파일만 선택해 있어도 서버 판단을 덮어쓰지 않는다.
- **CLI**: `agent --apply-update -agent-variant=compute|control`. **생략 시** 적용 대상의 설치된 `build_variant`를 따른다(self: `DeployBase/current` 바이너리 `--version` 접미사 또는 CLI 바이너리 variant, remote: `GET …/self`의 `build_variant`; 미상이면 `compute`).
- **REST**: `POST …/apply-update` JSON `agent_variant` 필드 또는 multipart `agent_variant` 필드.

##### 패키징(번들 만들기)

**권장 — 저장소 스크립트**

1. 에이전트 바이너리 빌드: 루트에서 **`make`** 또는 **`make build`** → **`build/image/contrabass-moleU-control`**(BuildVariant=control)·**`build/image/contrabass-moleU-compute`**(BuildVariant=compute) 두 파일 생성. 각 바이너리에 `-X main.VersionKey`·`-X main.BuildVariant`가 주입된 뒤 **`strip`** 으로 심볼 제거. **`Makefile`** 은 편의상 **`contrabass-moleU-control`** 을 저장소 루트 **`./contrabass-moleU`** 로도 복사한다(로컬 실행·레거시 스크립트 호환; `STRIP` 변수로 strip 도구 변경 가능).
2. 설정 파일 준비: 예시 **`cfg/agent.local.yml`**(배포 대상 환경에 맞게 수정).
3. 번들 생성:

   ```bash
   ./maintenance/scripts/pack-agent-tarball.sh [control] [compute] [config] [output.tar.gz]
   ```

   - **기본 인자**: control `./build/image/contrabass-moleU-control`, compute `./build/image/contrabass-moleU-compute`, config `./cfg/agent.local.yml`, 출력 `./dist/contrabass-agent-<버전 키>.tar.gz`(두 바이너리 **`agent --version`** 키 일치 필수; `build-version.sh`/`git describe` 미사용).
   - 스크립트는 세 파일의 **SHA-256** 을 각각 계산해 manifest v2 템플릿에 넣고, 임시 디렉터리에서 **`tar -czf`** 로 아카이브를 만든다. 멤버 목록을 stdout에 출력한다.
   - **필요 도구**: `sha256sum`, `tar`.

**수동 패키징**: manifest에 맞는 파일을 동일 레이아웃으로 tar.gz에 넣되, `path`·`sha256` 이 실제 멤버와 일치해야 한다.

##### 서버 검증 파이프라인(업로드 시)

`POST …/upload`·`PrepareAgentBundleFromReader`(CLI `apply-update` 사전 검증) 공통 순서:

1. 요청 본문을 **`Maintenance.MaxUploadBytes`**(기본 `64 << 20`) 이내로 수신.
2. tar.gz **안전 압축 해제** → `contrabass.manifest.yaml` 파싱(`manifestVersion: 1 또는 2`).
3. **v2**: `agent_control.path`·`agent_compute.path`·`config.path` 파일 존재 및 **SHA-256** 각각 일치. **v1**: `agent.path`·`config.path`.
4. config YAML → **`agentcfg.LoadFromBytes`**.
5. agent(v2는 두 바이너리 모두) → **ELF** 확인 후 **`--version` → `agent --version`** 폴백으로 **버전 키** 추출(§12). `(control)` / `(compute)` 등 variant 접미사는 검증 시 제거한다.
6. 성공 시 스테이징 디렉터리명 = **버전 키**; v2는 control·compute 바이너리를 각각 저장하고, **canonical `appmeta.BinaryName` 복사는 적용 시점에 `MaterializeCanonicalAgent`가 수행**. v1은 단일 바이너리를 `BinaryName`으로 복사.

##### 번들 이용 경로

| 경로 | 입력 | 결과 |
|------|------|------|
| **웹 UI** | 파일 선택 → `POST {APIPrefix}/upload` multipart **`bundle`** | `{DeployBase}/staging/<버전 키>/` + 원본 **`upload.bundle.tar.gz`** 보관(§5.5.1) |
| **로컬 적용** | 스테이징 후 `POST …/apply-update` `{ "version", "ip": "self" }` | 스테이징→`versions/` 복사 후 내장 **`update.sh`** `systemd-run` |
| **원격 적용(JSON)** | 스테이징·`versions/` 에 번들 또는 풀린 트리 존재 | 이 서버가 원격 **`POST …/upload`**(`upload.bundle.tar.gz` 우선) 후 원격 **`apply-update`(self)** 호출 |
| **원격 적용(multipart)** | `POST …/apply-update` 필드 **`ip`** + **`bundle`** | 로컬 스테이징 없이 원격만 업로드·적용 |
| **CLI** | `contrabass-moleU agent --apply-update [-apiprefix=…] [-agent-variant=…] <self\|ip> <bundle.tar.gz>` | 대상 **Gin**에 `POST …/upload` + `POST …/apply-update` (`clirest`) |

- **업데이트 가능 여부**: 업로드·적용 전 **`StagingUpdateAvailable`**(현재 `current` 대비 스테이징 버전 키 비교, §5.5.1)을 만족할 때만 진행한다. `AllowSameVersionUpdate` 설정이 `true`이면 동일 버전도 적용 가능.
- **variant 선택**: v2 번들에서 `agent_variant`로 canonical name으로 설치할 바이너리를 고른다. **CLI `--apply-update`에서 `-agent-variant` 생략** 시 설치된 `build_variant`를 따른다(§352). REST JSON에서 필드를 **비우면** 서버는 `compute`로 처리한다(웹 UI는 카드의 설치 variant를 보냄).
- **원본 바이트 재사용**: 스테이징에 **`upload.bundle.tar.gz`** 가 남아 있으면 원격 `POST …/upload` 에 **같은 tar.gz** 를 실어 보낸다. 없을 때만 서버가 바이너리+config로 **최소 tar.gz를 재생성**(`writeBundleTarGz`).

##### 관련 구현·문서

| 위치 | 역할 |
|------|------|
| `maintenance/server/bundleupload.go` | 압축 해제·manifest v1/v2 파싱·해시·ELF·버전 키·`writeBundleTarGz` |
| `maintenance/server/server.go` | `handleUpload`, `handleApplyUpdate`, 원격 multipart apply |
| `maintenance/server/agentvariant.go` | `MaterializeCanonicalAgent` — variant→`BinaryName` 복사 |
| `maintenance/server/applylocal.go` | `ApplyUpdateSelfFromBundleExtract` — CLI/서버 로컬 적용 |
| `maintenance/appmeta/agentvariant.go` | `ParseAgentVariant`, variant 상수·basename 매핑 |
| `maintenance/versionsapi/staging.go` | `StagingHasDualAgents`, `DirHasStagedAgents` |
| `maintenance/applycli/applycli.go` | CLI `--apply-update` (`-agent-variant` 플래그) |
| `maintenance/scripts/pack-agent-tarball.sh` | 릴리스 번들 빌드 (manifest v2) |
| `bin/ubuntu/contrabass-agent-install.sh` | **최초 설치**(greenfield): manifest v2 tar.gz → `versions/`·`current`·systemd |
| `bin/ubuntu/contrabass-agent-uninstall.sh` | **제거**: `contrabass-mole.service` 중지·비활성·유닛 삭제, `{DeployBase}`·로그 디렉터리 삭제 |
| `docs/CLI.md` | `--apply-update` 사용법 |

#### 5.5.0.1 최초 설치 스크립트 (greenfield installer)

- **경로**: `bin/ubuntu/contrabass-agent-install.sh` (POSIX `sh`, **root** 필수).
- **입력**: `contrabass-agent-install.sh <tar.gz> <control|compute>` — 번들은 §5.5.0·`pack-agent-tarball.sh` 산출 **manifestVersion 2** tar.gz.
- **권한·Usage**: `id -u` 가 0 이 아니거나 인자가 부족하면 **Usage** 와 `error:` 한 줄(영문)을 출력하고 종료한다.
- **검증**: `contrabass.manifest.yaml` 존재·v2, `contrabass-moleU-control`·`contrabass-moleU-compute`·`agent.local.yml` 존재. **`sha256sum` 이 있으면** manifest 의 control/compute/config SHA256 을 파일과 대조하고, **없으면** 해시 검증을 건너뛴다(경고 한 줄).
- **버전 키**: tar 파일명이 아니라 번들 내 바이너리 **`agent --version`** 출력에서 읽는다(control·compute 키 일치 필수). 디렉터리는 `{DeployBase}/versions/<버전 키>/`.
- **설치 후 트리**: flat 번들 멤버를 `versions/<키>/` 에 복사 → 인자 variant 로 **`contrabass-moleU` materialize** (`chmod 755`) → `current` → `versions/<키>`(상대 심볼릭, `update.sh` 와 동일) → `{DeployBase}/staging/` 생성 → `/var/log/contrabass/mole` 생성(향후 로그용).
- **systemd**: `/etc/systemd/system/contrabass-mole.service` 에 유닛 작성, `ExecStart={DeployBase}/current/contrabass-moleU -cfg {DeployBase}/current/agent.local.yml`, `systemctl enable`·`start`. 이후 업데이트는 웹/CLI·내장 `update.sh` 로 수행한다(§5.5.3).

#### 5.5.0.2 제거 스크립트 (uninstaller)

- **경로**: `bin/ubuntu/contrabass-agent-uninstall.sh` (POSIX `sh`, **root** 필수, 인자 없음).
- **동작**: `contrabass-mole.service` **stop**·**disable** → `/etc/systemd/system/contrabass-mole.service` 삭제 → `systemctl daemon-reload` → `{DeployBase}`(기본 `/var/lib/contrabass/mole`, `versions/`·`current`·`staging/`·`update_history.log` 등 전체) 및 `/var/log/contrabass/mole` 삭제. 경로가 없으면 `note:` 한 줄 후 건너뜀. 메시지는 **영문**.
- **권한·Usage**: install(§5.5.0.1)과 동일하게 비 root 또는 인자가 있으면 Usage·`error:` 후 종료.

#### 5.5.1 배포 디렉터리 구조·버전 키

- **배포 베이스** `DeployBase`(기본 `/var/lib/contrabass/mole`) 아래에는 **스테이징** `staging/`·**버전별 실행 트리** `versions/`·**현재/이전 포인터** `current`·`previous`·**기록** `update_history.log` 가 둔다. **업데이트/롤백 셸 스크립트는 배포 루트에 상주시키지 않는다** — 내용은 **에이전트 단일 바이너리(contrabass-moleU)에 내장**되며, 적용 시점에만 `current`가 가리키는 버전 디렉터리 아래에 풀어 쓴다(아래 5.5.3).
- **버전 디렉터리 이름(버전 키)** 은 빌드·바이너리가 내보내는 문자열 전체(예: git describe **`0.4.4-4-gc44d420`**, 또는 레거시 **`0.4.4-5`** 형태)가 스테이징·`versions/` 아래 디렉터리명이 된다. 비교·정렬 시 describe 접미사 **`-g<해시>`** 는 제거한 뒤 시맨틱·패치만 사용한다. **실행 중인 에이전트**의 키는 빌드 시 **`main.VersionKey`** 로 주입되며, **agent.local.yml에는 버전을 두지 않는다**. 시맨틱 부분은 점으로 구분된 숫자 세그먼트 개수에 고정 제한이 없다(예: `1.2.3.4-0`).  
  - **비교 규칙**: 동일 **시맨틱**(접두부)인 경우 **패치 숫자**만 정수로 비교한다(구현에서는 마지막 `-`(또는 레거시 `_`) 뒤를 정수로 파싱). 시맨틱이 다르면 **서로 다른 릴리스**로 보고, 스테이징에 다른 버전 키가 있으면 적용 가능으로 본다(다운그레이드 포함).  
  - **레거시**: 과거에 `versions/0.4.0` 처럼 `-패치` 없이 둔 디렉터리는 **패치 0**으로 해석하여 비교한다. 과거 `_숫자` 형식 디렉터리도 읽을 수 있다.
- **노출 버전 문자열**: 로그·Discovery·`GET /version`·DISCOVERY_RESPONSE의 `version` 등에 쓰이는 문자열은 위 **버전 키**와 동일하다.

  ```
  deploy_base/                       # 예: /var/lib/contrabass/mole (설정 키: DeployBase)
  ├── current -> versions/0.4.0-2    # 심볼릭 링크, 현재 실행 버전(버전 키)
  ├── previous -> versions/0.4.0-1
  ├── update_history.log             # 업데이트·롤백 기록 (append, 웹에는 최근 10줄 tail)
  ├── staging/                       # 업로드 API로만 채움; 원본 번들·풀린 트리 보관
  │   └── <버전 키>/
  │       ├── contrabass-moleU-control  # manifest v2: BuildVariant=control
  │       ├── contrabass-moleU-compute  # manifest v2: BuildVariant=compute
  │       ├── agent.local.yml
  │       ├── upload.bundle.tar.gz      # 원본 tar.gz (원격 재전송 시 우선 사용)
  │       └── (manifest에 따라 풀린 기타 파일들…)
  └── versions/
      └── <버전 키>/                    # 적용 후: upload.bundle.tar.gz 제외
          ├── contrabass-moleU           # MaterializeCanonicalAgent가 variant에서 복사
          ├── contrabass-moleU-control   # 원본 보관
          ├── contrabass-moleU-compute   # 원본 보관
          ├── agent.local.yml
          └── (기타 풀린 파일들…; 원본 tar.gz 없음)
  ```

- **스테이징**: 업로드는 **실행 중인** `versions/<버전 키>/` 가 아닌 **`{DeployBase}/staging/<버전 키>/`** 에만 저장하여 "text file busy" 를 피한다. 적용 시 소스는 **스테이징 우선**, 없으면 **versions/**.
- **원본 번들 보관**: 업로드 성공 시 **`upload.bundle.tar.gz`** 라는 이름으로 **클라이언트가 보낸 tar.gz 전체**를 스테이징에 함께 둔다. manifest·파일 개수가 늘어도 **원격에 동일 바이트를 다시 보낼 때** 서버가 번들 형식을 하드코딩하지 않도록 하기 위함이다. **`versions/<버전 키>/`에는 원본 번들을 두지 않는다** — 설치 트리는 실행·롤백·향후 임의 버전을 `current`로 지정하는 용도로 **풀린 파일만**이면 되며, 원본 아카이브는 필수 아님.
- **config 파일명 규칙**: 스테이징/설치 트리에 저장되는 config 파일명은 **바이너리 상수로 고정하지 않고**, 번들 manifest의 **`config.path` basename** 을 따른다(예: `config.path: ./agent.local.yml` → 디스크에도 `agent.local.yml`). 설정 파일명이 다시 바뀌어도 번들 규약만 맞으면 동일하게 동작한다.
- **agent 파일명 규칙**: **manifest v2**: `agent_control.path`·`agent_compute.path` basename으로 스테이징에 저장한다(`contrabass-moleU-control`, `contrabass-moleU-compute`). 스테이징 시점에는 canonical `appmeta.BinaryName`(`contrabass-moleU`)을 생성하지 않으며, **적용 시점**에 선택된 variant를 `MaterializeCanonicalAgent`로 복사한다. **v1(레거시)**: 단일 `agent.path` basename으로 저장하고, basename이 `BinaryName`과 다르면 복사한다.
- **스테이징 → `versions/` (로컬 적용 직전)**: **`staging/<버전 키>/` 디렉터리 전체를 `versions/<버전 키>/`로 복사**한 뒤, **`upload.bundle.tar.gz`만 삭제**한다. 번들에 에이전트·config 외 파일이 추가되어도 동일 규칙으로 설치 트리에 반영된다. **`reuse_previous_config`가 true**(웹 기본)이면 **`MaterializeCanonicalAgent` 직후·`update.sh` 직전**에 **업데이트 직전 `current`가 가리키는 버전 트리**의 config 파일(manifest basename, 예: `agent.local.yml`)을 **`versions/<적용 키>/`로 복사**하여 번들에 실린 config를 덮어쓴다. (`previous` 심볼릭 링크는 `update.sh` 안에서만 갱신되므로, 복사 출처는 **적용 전 running `current`**이다. `DeployBase`와 `InstallPrefix`가 다를 때는 `deployRoot/current`·`versionsBase/current` 순으로 해석한다.)
- **스테이징 정리**: 자동 삭제하지 않는다. 로컬 적용 후에도 스테이징을 남겨 같은 버전 키로 원격 적용을 반복할 수 있다(원본 번들이 스테이징에 남아 있으면 원격 `POST .../upload`에 그대로 실을 수 있음). 삭제는 웹 「업로드된 버전 삭제」로 **스테이징만** 수동 삭제한다.

#### 5.5.2 update.sh·rollback.sh (소스·내장·실행 위치)

- **소스**: 저장소 루트에 `update.sh`, `rollback.sh` 가 있으며, 빌드 시 **`maintenance/updatescripts/`** 로 복사한 뒤 Go **`//go:embed`** 로 바이너리에 포함한다. **`Makefile`** 의 `build` 타깃이 루트 스크립트를 해당 디렉터리로 동기화한 뒤 **control·compute 두 바이너리**를 `go build`·`strip` 하므로, 릴리스 빌드는 항상 최신 스크립트가 내장된다.
- **배포 베이스에 별도 복사 불필요**: 운영 호스트에 `scp` 로 스크립트만 갱신할 필요가 없다. 에이전트 바이너리를 교체하면 내장 스크립트도 함께 갱신된다.
- **BASE 산정**: 스크립트는 **`{DeployBase}/current/`** 옆이 아니라, **실행 시 `current` 심볼릭 링크가 가리키는 버전 디렉터리**(`versions/<버전 키>/`)에 놓인다.  
  - `SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"` — 스크립트 파일이 있는 디렉터리(현재 버전 트리).  
  - `BASE="$(cd "$SCRIPT_DIR/.." && pwd)"` — 그 **부모**가 배포 루트(`DeployBase`).  
  - 따라서 `VERSIONS="$BASE/versions"`, `HISTORY_LOG="$BASE/update_history.log"` 등이 일관된다.
- **롤백 호출**: `update.sh` 는 실패 시 **`"$SCRIPT_DIR/rollback.sh"`** 를 실행한다(같은 버전 디렉터리에 풀어 둔 rollback).
- **수명**: 적용 API가 시작할 때 `current` 아래에 두 파일을 **쓰기·실행 권한(0755)** 으로 생성한 뒤 `systemd-run` 으로 `update.sh` 를 실행한다. 스크립트는 **정상 종료·롤백 종료·조기 실패** 등 모든 종료 경로에서 **`cleanup_scripts`** 로 **같은 디렉터리의** `update.sh`·`rollback.sh` 를 삭제한다. `systemd-run` 자체가 즉시 실패하면 에이전트가 생성한 두 파일을 제거한다.
- **스크립트 본문 요약**  
  - **PATH**: `export PATH="/usr/bin:/bin:/usr/local/bin:${PATH:-}"` (transient 유닛 대비).  
  - **config 읽기**: 적용 대상 `versions/<인자 버전 키>/agent.local.yml` 에서 `MaintenancePort`, `SystemctlServiceName` 등(실패 시 기본값, `|| true`).  
  - **update.sh**: 인자 **버전 키** 하나. `{BASE}/versions/{버전 키}/contrabass-moleU`(실행 파일명은 빌드·`appmeta.BinaryName`과 동일) 존재·실행 가능 확인 → 서비스 중지 → `previous` 갱신 → `current` 를 해당 버전으로 교체 → 서비스 시작 → **헬스**(아래). 실패 시 **`invoke_rollback`** 으로 `rollback.sh` 호출·기록(롤백 성공/실패 exit 코드 구분).  
  - **헬스(재시도)**: 병합·추가 라이브러리 링크 등 **느린 기동**을 고려해 기본 대기를 길게 둔다. `systemctl is-active` 는 **`SERVICE_ACTIVE_MAX_ATTEMPTS`×`SERVICE_ACTIVE_INTERVAL`**(기본 45×2초≈90초)까지 재시도한 뒤, `http://127.0.0.1:${HTTP_PORT}/version` 에 **`HEALTH_INITIAL_SLEEP`**(기본 5초) 후 **`HEALTH_MAX_ATTEMPTS`×`HEALTH_RETRY_INTERVAL`**(기본 72×5초, 약 6분)까지 GET 재시도. 서비스가 중간에 내려가면 즉시 롤백. 본문이 **`<BinaryName> <버전 키>`** 한 줄과 일치해야 성공. 환경 변수로 조정: `HEALTH_INITIAL_SLEEP`, `HEALTH_RETRY_INTERVAL`, `HEALTH_MAX_ATTEMPTS`, `SERVICE_ACTIVE_MAX_ATTEMPTS`, `SERVICE_ACTIVE_INTERVAL`.  
  - **rollback.sh**: `previous` 가 있으면 서비스 중지 → `current` 를 `previous` 와 동일 대상으로 교체 → 시작.  
  - **기록**: `update_history.log` 에 **append** 방식으로 한 줄씩 추가(최신이 파일 맨 아래). 동시 기록은 **`flock`**(`update_history.log.lock`, 최대 30초 대기)으로 직렬화한다. lock 파일은 **0바이트로 `{DeployBase}`에 남을 수 있으며**, 잠금은 기록 subshell 종료 시 해제되므로 **다음 업데이트를 막지 않는다**(동시에 다른 `update.sh`/`rollback.sh`가 돌 때만 대기·실패 가능).

#### 5.5.3 업로드·삭제·적용

- **업로드** `POST {serverUrl}/api/v1/upload`  
  - **multipart**: 필드 **`bundle`** 하나 — **tar.gz** 배포 번들(구성·manifest·패키징·검증은 **§5.5.0**). **브라우저·CLI·다른 에이전트가 원격에 배포할 때도 동일 경로·동일 필드명**으로 호출한다.  
  - **본문 크기**: `http.MaxBytesReader`로 **`Maintenance.MaxUploadBytes`**(기본 `64 << 20` 바이트) 상한. 서버는 번들을 임시 디렉터리에 **안전하게 압축 해제**(경로 탈출·심볼릭 링크 등 차단, GNU tar의 `./` 디렉터리 항목 등은 건너뜀, 항목 수·압축 해제 총량 한도)한 뒤 **`contrabass.manifest.yaml`** 존재·`manifestVersion`·`agent`/`config`의 `path`·`sha256` 대로 파일 존재·해시 일치를 검증한다. 그다음 **manifest의 `config.path`에 해당하는 YAML** 구조체 파싱, **에이전트 ELF**·바이너리 버전 키 검증(§12, `--version`→`agent --version` 폴백)을 수행한다. 검증·`clearStaging` 후 **`staging/<버전 키>/`** 에 표준 이름 **`BinaryName`** 실행 파일과 **manifest의 `config.path` basename**(예: `agent.local.yml`)을 두고, **요청 본문으로 받은 tar.gz 원본 전체**를 **`upload.bundle.tar.gz`** 로 저장한다(원격 재전송·manifest 확장 시 서버가 번들 레이아웃을 재하드코딩하지 않도록).  
  - **실행 파일 검증**: ELF 매직 + 스테이징 경로에서 바이너리 실행으로 버전 키 확인(각 시도 **5초** 타임아웃). **먼저 `<path> --version`**, 실패 시 **`<path> agent --version`** 순으로 시도한다(`maintenance/server.versionKeyFromAgentBinary`). 출력 한 줄이 **`"<BinaryName> "`**(`maintenance/appmeta.BinaryName`)로 시작하고, 뒤의 버전 키가 유효해야 하며 종료 코드 0.  
  - **config 검증**: `maintenance/agentcfg` 구조체로 파싱; 실패 시 줄·항목·필요 타입 안내(예: `DiscoveryServiceName`, `DiscoveryUDPPort`, `MaintenancePort` 등).  
  - **버전 키(스테이징 디렉터리명)**: 추출·검증된 **실행 파일**에 대해 위와 동일하게 **`--version` → `agent --version`** 폴백으로 버전 키를 읽는다. 출력 한 줄 `<BinaryName> <버전 키>` 의 뒷부분을 스테이징 디렉터리명으로 쓴다. config에는 버전을 두지 않는다.  
  - **성공**: `{ "status": "success", "data": { "version": "<버전 키>" } }`.
- **업로드 삭제** `POST .../upload/remove` — Body `{ "version": "<버전 키>" }`. **스테이징** 만 삭제; `versions/` 는 유지.
- **적용 (로컬)** `POST .../apply-update`, Body `{ "version": "<버전 키>", "ip": "self" 또는 생략, "agent_variant": "control"|"compute" (선택), "reuse_previous_config": true|false (선택) }`  
  - 소스: 스테이징 우선, 없으면 `versions/`.  
  - 스테이징에만 있으면 **`staging/<버전 키>/` 전체를 `versions/<버전 키>/`로 복사**한 뒤 **`upload.bundle.tar.gz`만 제거**하고 `update.sh` 경로로 진행한다(§5.5.1). v2 dual-agent 트리는 **`MaterializeCanonicalAgent`** 로 `agent_variant`(비면 설치 `build_variant`, 서버 ldflags 폴백)에 맞게 `contrabass-moleU`를 준비한다. **`reuse_previous_config`가 true**이면 §5.5.1의 **적용 전 `current` config 복사**를 수행한다. **생략·false**이면 번들(스테이징)의 config가 그대로 `versions/<키>/`에 남는다.  
  - **`{DeployBase}/current` 존재 필수**(심볼릭 링크 또는 그에 준하는 배포). 없으면 적용 불가.  
  - 내장 `update.sh`·`rollback.sh` 내용을 **`{DeployBase}/current/update.sh`**, `.../rollback.sh` 로 쓴 뒤(실제 파일은 `current` 가 가리키는 `versions/<현재 버전 키>/` 아래),  
    `systemd-run --unit=contrabass-mole-update --property=RemainAfterExit=yes /bin/bash <그 경로>/update.sh <적용할 버전 키>`  
  - 응답은 즉시 성공(백그라운드 적용). 에이전트는 root로 동작·sudo 없음.
- **적용 (원격)**  
  - **JSON** `{"version":"<키>","ip":"<원격 IP>","agent_variant":"…"(선택),"reuse_previous_config":true|false(선택)}`: 요청을 받은 서버가 **`resolveVersionDir`**로 로컬 **`staging/` 또는 `versions/`** 에서 해당 버전 디렉터리를 고른 뒤, **`reuse_previous_config`가 true**이면 원격 **`GET …/current-config`** 로 **그 호스트의 current config**를 읽어 스테이징 트리의 config 파일을 덮어쓰고 **`upload.bundle.tar.gz`를 제거**한 뒤(재전송 시 트리에서 tar.gz를 다시 빌드), (1) **`POST http://<원격>:<Server.HTTPPort>/api/v1/upload`** — **로컬 업로드와 동일한 API**이며, 해당 디렉터리에 **`upload.bundle.tar.gz`가 있으면 그 파일을 multipart `bundle`로 그대로 보내고**, 없으면(스테이징 삭제 후 `versions/`만 남은 경우·config 주입 후 등) **풀린 트리에서 tar.gz를 생성**해 보낸다. (2) **`POST .../apply-update`** with `{"version":"<키>","ip":"self","agent_variant":"…","reuse_previous_config":…}`. 원격 에이전트는 로컬과 동일하게 준비·§5.5.1 config 복사(백업)·`current` 아래 스크립트 실행을 수행한다. **`version`은 항상 버전 키 문자열**이다.  
  - **multipart 원격 적용**: 필드 **`ip`** + **`bundle`**(tar.gz) + **`agent_variant`**(선택) + **`reuse_previous_config`**(선택, 기본 true) — 로컬 스테이징 없이 원격에만 번들 업로드·적용. **`reuse_previous_config`가 true**이면 업로드 전에 원격 current config로 번들 내 config를 교체한 뒤 전송한다. 동일 **`MaxUploadBytes`** 상한.

#### 5.5.3.1 원격 일괄 적용·롤백

- **일괄 적용** `POST {APIPrefix}/apply-update-all`  
  - **Body**(선택): `{ "hosts": [ … ], "version": "<버전 키>" (생략 시 로컬 스테이징 최신), "agent_variant": "control"|"compute", "reuse_previous_config": true|false }` — `hosts` 형식은 §5.4 **restart-all**과 동일(웹 UI **카드 1장 = 호스트 1대**).  
  - **동작**: 호스트마다 원격 `GET …/self`의 `version`과 **이 서버 로컬 스테이징**을 비교해 **`StagingUpdateAvailable`** 이면 §5.5.3 **원격 적용(JSON)** 과 동일 경로(업로드+apply). 미충족은 NDJSON **`skipped`**(실패 아님).  
  - **응답**: `application/x-ndjson` — `start` → 호스트별 `progress` → `done`. 완료 시 `update_history.log`에 **`apply-update-all finished succeeded=N failed=M skipped=K`** 한 줄 append.  
  - **웹 UI**: §6.6 — 버튼 활성은 **호스트별** `GET …/update-status?ip=` 의 `can_apply`(로컬 `can_apply` 아님). 적용 성공 호스트는 완료 후 카드 자동 갱신(§6.3).
- **일괄 롤백** `POST {APIPrefix}/versions/rollback-all`  
  - **Body**(선택): `{ "hosts": [ … ] }` — `hosts` 형식 동일.  
  - **롤백 가능 판단**: 원격 `GET …/versions/list` 응답에서 **`is_previous` 버전 키가 있고**, **`is_current` 버전 키와 다를 때**만 롤백한다 — 즉 `current`·`previous` 심볼릭 링크가 **같은 버전을 가리키면 이미 롤백된 상태**로 보고 **`skipped`**. `previous` 없음도 **`skipped`**.  
  - **동작**: 롤백 가능 호스트에 대해 `POST …/versions/rollback` 프록시(embedded **`rollback.sh`**: `previous`→`current`·서비스 재시작).  
  - **응답**: NDJSON. 완료 시 **`rollback-all finished succeeded=N failed=M skipped=K`** 요약 append.  
  - **웹 UI**: §6.6 — 호스트별 `GET …/versions/list?ip=` 로 동일 판단을 캐시해 버튼 활성 제어.

#### 5.5.4 업데이트 상태·기록·설정·헬스

- **업데이트 상태** `GET .../update-status`  
  - **Query `ip` (선택)**  
    - 비어 있거나 `"self"`: **이 에이전트** 기준. `current_version`은 `readlink` 등으로 `current` 가 가리키는 디렉터리 이름(버전 키).  
    - **원격 IP** 지정: **이 서버의 로컬 스테이징** 목록은 그대로 사용하고, 비교 대상 “현재 버전”만 **원격 호스트**에서 가져온다 — 요청을 받은 서버가 원격 **`Server.HTTPPort`** 로 `GET .../self` 를 호출해 응답의 `version`(버전 키)을 사용한다. 응답에는 `remote_ip`, `remote_current_version` 을 넣고 `current_version` 은 넣지 않는다. 원격 조회 실패 시 `fail`.  
  - `staging_versions`: 스테이징 아래 디렉터리 목록(버전 키). **비교 가능한 순서**(버전 키 비교, 새 쪽이 앞)로 정렬. (원격 `ip` 여부와 관계없이 **항상 이 서버의 스테이징**이다.)  
  - **`can_apply` / `apply_version`**: 스테이징에 올라온 버전 키 중, **비교 기준 버전**(로컬이면 `current_version`, 원격이면 `remote_current_version`) 대비 **업데이트로 적용할 가치가 있는지** 판단한다 — 규칙은 동일(시맨틱·패치 비교, `StagingUpdateAvailable`). 원격 모드에서는 “**이 서버 스테이징을 그 원격에 적용할 수 있는지**”를 나타낸다.  
  - `remove_version`: 스테이징 정렬 후 **가장 오래된(맨 끝)** 항목 등 UI 삭제용으로 쓸 수 있다.  
  - `update_in_progress`: **요청을 처리하는 이 서버**에서 `systemctl is-active contrabass-mole-update.service` 가 active 이면 true(원격 호스트의 진행 여부는 이 필드로 알 수 없음).
- **업데이트 기록** `GET .../update-log` — `{DeployBase}/update_history.log` **맨 아래 최근 10줄**(tail, 파일 순서는 **오래된 줄→새 줄**). 응답 `data.output`(문자열), `data.recent_rollback`(bool, **파일 맨 아래 줄** 기준). **`recent_rollback`은 실제 업데이트/롤백 실패**(`update … failed`, `rollback failed`, `rollback completed after update failure` 등)일 때만 true — **`config push-all finished … failed=N`**·**`service restart-all finished … failed=N`**·**`apply-update-all finished …`**·**`rollback-all finished …`** 요약의 `failed=N` 카운트는 **무시**한다. **`Cache-Control: no-store`**. `contrabass-mole-update.service`가 active이면 `recent_rollback`을 false로 내려 새 적용과 이전 실패 기록이 섞이지 않게 한다. 원격 `ip`는 `Server.HTTPPort`로 프록시하며, 프록시 응답도 **동일 tail 10·`no-store`** 로 정규화한다. 웹 UI(로컬·원격 카드 공통)는 `output` 줄을 **최신이 위**가 되도록 **역순 표시**한다. 롤백 경고(「⚠ 최근 업데이트 실패·롤백」)는 `recent_rollback`이 true일 때만 표시한다.
- **current-cfg** `GET/POST .../current-cfg` — `current` 심볼릭 대상의 config YAML 조회·저장. (REST 경로명은 구현·문서에서 `current-config`와 동일 계열.)
- **헬스** `GET /version` — **`<BinaryName> <버전 키>`** 한 줄(버전 키는 describe 전체일 수 있음, 예: `contrabass-moleU 0.4.4-4-gc44d420`), text/plain, 항상 200. update.sh 의 curl 이 사용한다.
- **에이전트 HTTP 헬스(JSON)** `GET {APIPrefix}/health` — JSON `success`, `data`에 `{ "ok": true }` 수준의 최소 응답. **원격 가용성 모니터링** 시 로컬 에이전트가 같은 경로로 노출하며(Gin이 `Server.HTTPPort`로 프록시), 웹 UI의 원격 헬스 확인은 **이 경로**를 대상으로 한다(UDP 미사용).
- **원격 헬스 프록시** `GET {APIPrefix}/remote-health-check?ip=<원격 IP>` — 요청을 받은 에이전트가 `http://<ip>:Server.HTTPPort` + `{APIPrefix}/health` 로 HTTP GET(타임아웃은 `Maintenance.RemoteHealth.TimeoutSeconds`, §7.1)을 수행하고 성공·실패를 JSON으로 반환한다.

### 5.6 설치된 버전(versions) API

- **경로 기준**: `InstallPrefix`(설정, 비면 `DeployBase`) 아래 `versions/` 디렉터리 및 `current`·`previous` 심볼릭 링크를 사용한다. **최초 설치**는 §5.5.0.1 `contrabass-agent-install.sh`, **완전 제거**는 §5.5.0.2 `contrabass-agent-uninstall.sh`, 이후 목록·전환·업데이트는 동일 경로를 사용한다. `InstallPrefix`를 둔다.
- **목록**: `GET {serverUrl}/api/v1/versions/list?ip=`  
  - `ip` 비어 있거나 `"self"`: `{InstallPrefix}/versions/` 디렉터리 내 각 **버전 키** 이름의 하위 디렉터리(그 안에 **`appmeta.BinaryName` 실행 파일**이 있는 것만)를 나열하고, `current`·`previous` 심볼릭 링크가 가리키는 버전을 판별하여 `is_current`·`is_previous` 플래그와 함께 반환한다. 응답: `{ "status": "success", "data": { "versions": [ { "version", "is_current", "is_previous" }, ... ] } }` — 여기서 `version` 문자열은 디렉터리명과 동일한 **버전 키**이다.  
  - **정렬 순서(표시용)**: **current** 대상을 맨 위 → **previous** 대상 → 그 외는 **버전 키 비교 규칙**(시맨틱 부분을 절 단위 정수로 비교한 뒤, 같으면 `-`(또는 레거시 `_`) 뒤 패치를 정수로 비교)에 따른 **내림차순**(더 “새” 버전이 위). 웹 UI에서 현재·이전·나머지 순으로 한눈에 볼 수 있다.  
  - `ip` 지정: 요청을 받은 서버가 **원격 호스트의 `Server.HTTPPort`(Gin)** 로 `GET .../versions/list` 를 호출한 뒤 응답을 그대로 클라이언트에 전달한다.
- **삭제**: `POST {serverUrl}/api/v1/versions/remove`  
  - Body: `{ "versions": [ "<버전>", ... ], "ip": "" | "self" | "<host_ip>" }`. `ip`가 비어 있거나 `"self"`이면 로컬에서 삭제. `ip` 지정 시 요청을 받은 서버가 **원격 `Server.HTTPPort`** 로 `POST .../versions/remove` (Body: `{ "versions": [...] }`)를 호출한 뒤 응답을 그대로 클라이언트에 전달한다. 로컬/원격 공통: `current`·`previous`가 가리키는 버전은 삭제하지 않고 제외 사유와 함께 응답에 포함한다.  
  - **버전 키 검증**: 삭제 대상 문자열은 **`ValidateVersionKeyPath`와 동일한 규칙**(디렉터리명으로 안전한 문자; 패치 구분 `-`(레거시 `_` 허용), 예 `0.4.4-9`)을 따른다. 구현상 업로드·적용 API와 같은 검증을 사용한다.  
  - **원격 `ip` 사용 시 주의**: 실제 삭제·검증은 **`ip`로 지정된 호스트에서 실행되는 에이전트**가 수행한다. 클라이언트가 붙은 머신(또는 Gin 프록시 앞단)만 최신으로 올리고 **원격 호스트는 구버전 바이너리**이면, 응답 메시지·검증 동작은 **원격 프로세스** 기준이 된다(예: 구버전에서 잘못된 문자 제한이 남아 있으면 그쪽 메시지가 그대로 돌아온다). 원격에서도 동일 동작을 기대하려면 **해당 호스트에 동일 빌드를 배포**한다.  
  - **프록시 선검증**: `ip`가 원격일 때 요청을 받은 서버는 원격으로 넘기기 전에 버전 키 형식을 검사하여, 잘못된 항목은 즉시 `fail`(HTTP 400)할 수 있다.
- **이 버전으로 서비스(switch-current)**: `POST {serverUrl}/api/v1/versions/switch-current` — Body `{ "version": "<버전 키>", "ip": "" | "self" | "<host_ip>" }`. **스테이징 또는 `versions/`** 에 해당 키가 있으면 로컬에서는 **`apply-update`와 동일한 준비**(스테이징→`versions/` 반영, v2 시 `MaterializeCanonicalAgent`로 `contrabass-moleU` 준비) 뒤 내장 `update.sh`를 `systemd-run`으로 실행하여 그 버전을 **current**로 둔다. `ip`가 원격이면 요청 서버가 원격 **`Server.HTTPPort`** 로 동일 경로를 프록시한다. 웹 UI에서는 설치된 버전 블록에 **라벨「이 버전으로 서비스」**·**단일 선택(select)**·**「이 버전으로 적용」**을 두며, **select 옵션에는 이미 current인 버전(디렉터리)은 넣지 않는다**(불필요한 재적용 방지). **성공 응답 후**에는 로컬·원격 모두 **업데이트 적용과 동일하게** `/self` 또는 `host-info` 폴링 뒤 **업데이트 기록·config·설치된 버전·서비스 상태·update-status** 등 패널을 자동 갱신한다(롤백으로 버전이 되돌아간 경우에도 반영). **로컬 전환** 시 업데이트 기록은 §6.3과 같이 **호스트 폴링과 별도로** 2초 간격 자동 갱신한다. 선택 변경 시 **「버전 … 을(를) 선택했습니다.」** 형태의 짧은 안내 문구를 표시한다.
- **롤백 (단일 호스트)** `POST {serverUrl}/api/v1/versions/rollback` — Body `{ "ip": "" | "self" | "<host_ip>" }`. 로컬(또는 원격 프록시)에서 embedded **`rollback.sh`** 실행: `previous` 심볼릭 링크가 가리키는 버전으로 `current` 교체·서비스 재시작. `previous` 없으면 `fail`. 롤백 후에는 `current`·`previous`가 **동일 버전**을 가리킬 수 있다(§5.5.3.1 일괄 롤백 `skipped` 판단 기준).
- **일괄 롤백**: §5.5.3.1 `POST …/versions/rollback-all`.

---

## 6. 프론트엔드

- **구현 방식**: 정적 파일(HTML, CSS, JavaScript)을 **Go embed**로 단일 실행 파일에 포함.
- **JavaScript**: **Vanilla JS**만 사용. API 호출은 `fetch`, UI 업데이트는 DOM 조작으로 처리. SPA 프레임워크(React, Vue 등)는 사용하지 않는다.
- **레이아웃**
  - 호스트 정보(내 정보·발견된 호스트) 카드는 **가운데 열**에 배치하고, **업데이트** 영역과 **「모든 리모트 호스트 일괄 작업」**(§6.6)은 **화면 오른쪽 sticky 사이드바**에 고정하여 스크롤 시 카드만 스크롤되고 사이드바는 고정된다. 스크롤바가 생겨도 레이아웃이 밀리지 않도록 `scrollbar-gutter: stable`을 사용한다.
  - 호스트 카드의 가로 최대 너비는 610px로 통일하며, 내 정보와 발견된 호스트 카드는 동일한 카드 스타일 한 겹만 사용한다(내 정보 컨테이너는 카드 클래스를 갖지 않고, 렌더된 카드 한 개만 자식으로 둔다).
  - 카드 내 **시작/중지·업데이트 적용·상태 새로고침** 버튼은 카드 **오른쪽 위**에 절대 위치로 배치한다. 상단의 호스트 정보 항목(CPU UUID, 버전, IP 등)만 버튼과 겹치지 않도록 오른쪽 여백을 두고, **서비스 상태(터미널)** 영역은 카드 오른쪽 끝까지 넓게 표시한다.
- **초기 화면**
  - **내 정보**: 현재 에이전트 인스턴스의 버전, **IP(또는 응답으로 사용하는 모든 IP `host_ips`)** , 호스트명, CPU UUID, CPU, MEMORY 등을 표시 (자기 정보 API 사용). 자기 정보 API는 각 브로드캐스트 주소별 outbound IP를 `host_ips`로 반환하여 Discovery 응답으로 사용하는 IP들을 모두 보여준다.
- **Discovery 버튼**
  - 클릭 시 **EventSource** 로 `GET /api/v1/discovery/stream` 에 연결하여 **실시간 Discovery**를 수행한다. **기존 발견된 호스트 목록은 비우지 않고** 유지하며, 진행 중에도 해당 카드들의 제어(시작/중지·업데이트 적용·상태 새로고침)가 가능하다. SSE로 호스트가 도착할 때 **같은 CPU UUID**가 있으면 해당 카드에 IP만 병합·갱신하고, 없으면 같은 IP 카드 갱신 또는 새 카드 추가한다. `event: done` 수신 시 스트림을 닫고 버튼을 복구한다. **진행 중 상태 줄**에 남은 초·호스트 수를 표시한다(타임아웃은 `client-runtime.js`의 `discovery.timeoutSec`). **run 완료 후** 이번 UDP run에 응답하지 않은 **기존 원격 카드**에는 「**이번 Discovery 미응답**」 배지·펼친 카드 안내 배너를 표시하되 **카드는 제거하지 않는다**(`discoveryfail`·새 run 시작 직후에는 이전 run의 미응답 표시를 지우지 않음).
- **원격 일괄 작업**: 오른쪽 사이드바 **§6.6**.
- **호스트 목록 구조 (아코디언·상태 점)**
  - 호스트(로컬·발견된 원격)는 **세로 목록**으로 표시한다. 기본은 **한 줄 요약**만 보이고, 해당 행을 클릭하면 그 호스트의 **상세 카드**가 펼쳐진다(아코디언). 여러 호스트를 동시에 펼쳐 둘 수 있다.
  - **한 줄 요약**: **상태 점**(동작 중 = 파란색, 중지 = 빨간색, 미확인 = 회색) + **구분자**. 로컬 구분자: hostname 또는 "로컬" + " · " + IP. 원격 구분자: hostname + " · " + IP(또는 CPU UUID 앞 8자).
  - **로컬 호스트**: **맨 위**(내 정보 섹션)에 한 줄로 표시하며, 배경·테두리 색을 달리(예: 파란 톤)하여 원격과 구분한다.
  - **로컬의 IP 표시**: 초기에는 자기 정보 API의 IP(또는 host_ips)를 사용하고, **Discovery 수행 후**에는 응답으로 받은 **responded_from_ip**를 반영하여 한 줄 요약의 IP를 갱신한다.
- **발견된 호스트 표시**
  - 각 호스트를 **서버 모양 아이콘**과 함께 **상세 카드**로 표시한다(아코디언에서 해당 행을 펼쳤을 때).
  - 표시 내용: **CPU UUID**(맨 위), 에이전트 버전, **IP**(여러 개면 쉼표 구분, 같은 호스트의 여러 응답에서 host_ip를 취합), **응답한 IP**(실제로 Discovery 응답을 보낸 UDP 발신지 IP, 여러 개면 취합), 호스트명, 서비스 포트, CPU, MEMORY. 동일 CPU UUID의 여러 응답은 **한 카드**로 병합하며, IP와 응답한 IP는 모두 취합해 표시하고 CPU·메모리는 하나만 표시한다.
  - 내 정보와 동일한 형태(카드/테이블 등)로 보여주어 일관된 UX를 유지한다.
- **원격 적용 후**: 원격 에이전트 업데이트가 성공하면 **Discovery를 다시 수행하지 않고**, 해당 호스트 카드만 갱신한다.  
  - **카드 버전 즉시 갱신(낙관적 갱신)**: apply-update API 성공 시점에 이미 알고 있는 **적용 버전**으로 카드의 버전 표시(`data-host-version` 속성 및 버전 dd 텍스트)를 **즉시** 갱신한다. 이때 **오래된 `can_apply`로 버튼을 다시 켜지 않는다**.  
  - **지연 후 host-info 및 패널 전체 현행화**: 약 5초 후부터 `GET /api/v1/host-info?ip=...`를 **2초 간격으로 최대 8회** 재시도한다. **성공 시** 카드 호스트 정보를 덮어쓴 뒤 **업데이트 기록(update-log)·agent.local.yml(current)·설치된 버전(versions/list)·서비스 상태(service-status)**·해당 IP **`GET …/update-status?ip=`** 및 로컬 **update-status**를 한꺼번에 다시 불러 **`can_apply`·variant 표시**를 서버와 일치시킨다. **재시도를 모두 소진해도** 가능한 API는 동일하게 호출한다. 적용 실패·완료 후에도 동일 IP에 대해 **`update-status?ip=`** 를 다시 조회한다. 그 후 업데이트 인디케이터를 숨긴다.

### 6.1 systemctl status 표시 (내 정보·발견된 호스트 공통)

- 각 호스트 카드에 **systemctl status** 결과를 표시한다.
- **접기/펼치기**: 기본은 **접힌 상태**. 헤더(아이콘 ▶/▼ + 요약 문구) 클릭 시 상세 출력(`systemctl status` 전문)을 펼치거나 접는다.
- **접힌 상태에서의 요약**  
  - `Active: active (running)` 이면 **\[정상 서비스 상태]**  
  - 그 외(dead 등)면 **\[서비스 중지 상태]**  
  - 로딩/에러 시 "불러오는 중…", "상태를 불러올 수 없습니다." 등.

### 6.2 서비스 시작/중지·재시작 및 원격 카드 레이아웃

- **내 정보(자기 자신) 카드**에는 시작/중지 버튼을 두지 않는다. **오른쪽 컬럼**에 업데이트 기록(최근 10건)·agent.local.yml(current) 편집·설치된 버전(versions) 목록을 두고, **하단**에 서비스 상태(접기/펼치기)·「상태 새로고침」·「서비스 재시작」을 둔다.
- **발견된 호스트(원격) 카드**는 **로컬 카드와 동일한 레이아웃**을 사용한다. 오른쪽 컬럼에 업데이트 기록·agent.local.yml(current)·설치된 버전을 두고, 하단에 서비스 상태·「상태 새로고침」·「서비스 재시작」·「업데이트 적용」을 둔다. **시작**·**중지** 버튼은 노출하지 않는다(원격 시작/중지는 SSH로만 수행).
- **원격 조작 가드**: **HTTP 헬스 연속 실패**(§6.5) 또는 **이번 Discovery 미응답** 표시가 있는 카드에서는 **「업데이트 적용」·「서비스 재시작」** 을 비활성화한다(스테이징·`can_apply`와 무관). 일괄 작업 버튼의 **도달 가능(reachable)** 판단에도 동일 규칙을 쓴다.
- **원격 카드 열릴 때**: 해당 행을 펼치면(아코디언 확장 시) **업데이트 기록**·**agent.local.yml 불러오기**·**설치된 버전 목록**을 자동으로 해당 원격 호스트 API(`?ip=` 또는 body `ip`)로 요청하여 표시한다. 로컬 카드는 초기 로드 시 동일 데이터를 자동 표시한다.
- **서비스 제어 API 동작**: `POST /api/v1/service-control` with `{ "ip": "<host_ip>", "action": "start"|"stop"|"restart" }`.  
  - **로컬**(ip 없음/self): `systemctl start/stop/restart` (sudo 없음, root 실행).  
  - **원격 start/stop**: 요청을 받은 서버가 해당 원격 호스트로 **SSH** 접속하여 `systemctl start|stop` 실행. 설정 `SSHPort`(기본 22), `SSHUser`(기본 "root") 사용.  
  - **원격 restart**: SSH 없이 요청을 받은 서버가 **원격 에이전트 API**(`Server.HTTPPort`)로 `POST .../service-control` (Body `{ "ip": "self", "action": "restart" }`)를 호출. 원격 에이전트가 자기 서버에서 `systemctl restart` 실행.
- **서비스 재시작 후 UI**: 재시작 요청 성공 시 또는 연결 끊김/terminated 등 재시작 진행 중으로 보이는 오류 시, 요약에 「재시작되었습니다. 잠시 후 상태를 불러옵니다.」 등 친절한 메시지를 표시하고, **몇 초 후 자동으로** (1) `GET /api/v1/self`(로컬) 또는 `GET /api/v1/host-info?ip=...`(원격)로 호스트 정보를 가져와 카드의 **버전·호스트명·IP 등**을 갱신하고, (2) `GET /api/v1/service-status`로 요약을 [정상 서비스 상태] 등으로 갱신한다. agent.local.yml의 version을 수정한 뒤 재시작한 경우에도 카드에 새 버전이 반영된다. 로컬·원격 동일. 사용자가 「상태 새로고침」을 누르지 않아도 된다.
- (참고) **서비스 상태** 조회(GET /api/v1/service-status?ip=)는 로컬은 직접 systemctl, 원격은 원격 에이전트 API(`Server.HTTPPort`)를 호출하는 방식으로 유지한다.

### 6.3 업데이트 (업로드·적용·로그)

  - **업로드**: `maintenance/scripts/pack-agent-tarball.sh` 등으로 만든 **tar.gz 번들** 하나를 선택해 `POST /api/v1/upload` (multipart: **`bundle`**). **버전 키**는 서버가 번들 내 바이너리에 대해 **`versionKeyFromAgentBinary`**(§5.5.3·§12)로 읽으며, 스테이징 디렉터리명으로 쓴다. 성공 시 메시지에 그 버전 키가 표시된다. 서버는 manifest·해시·**실행 파일 검증**(ELF + 버전 한 줄, §12)·**agent.local.yml 검증**을 수행하며, 실패 시 에러 메시지를 반환한다. 스테이징에는 **원본 번들 파일(`upload.bundle.tar.gz`)** 도 함께 저장되어(§5.5) 원격 적용 시 동일 바이트 재전송에 쓰인다.  
  - **config 변경**: 번들을 만들기 전에 로컬에서 `agent.local.yml`을 수정한 뒤 패킹 스크립트로 번들을 다시 생성한다(웹에서 개별 config 편집·업로드 흐름은 사용하지 않음).
- **적용 (로컬)**: 버전이 스테이징 또는 이전 적용으로 존재할 때, **「업데이트 적용」**(`POST /api/v1/apply-update`, Body `{ "version": "<버전 키>", "agent_variant": "compute"|"control", "reuse_previous_config": true|false }`). **로컬 스테이징에 번들이 있을 때만** 「업데이트 적용」 버튼 아래 **「이전버전의 환경설정 파일 재사용」** 체크박스를 표시하며 **기본값은 체크(재사용)** 이다. 스테이징이 비면 체크박스를 숨긴다. 체크 해제 후 적용 시 **확인 대화상자**로 번들 config 사용 여부를 묻고, **취소** 시 `alert`로 재사용 체크 후 다시 적용하라고 안내한다(적용은 진행하지 않음). 활성 시 **초록색** 버튼 스타일. 성공 시 에이전트 재시작으로 HTTP가 끊길 수 있으므로 **전체 페이지 새로고침은 하지 않는다**. 이후 **두 갱신 루프를 병렬**로 돌린다(§6.3.1). (1) **호스트**: 약 **4초 후**부터 `GET /api/v1/self`를 **2초 간격·최대 15회** — 성공 시 카드 호스트 정보·config·versions·서비스 상태·update-status 현행화. (2) **업데이트 기록**: §6.3.1. `/self`가 먼저 돌아와도 **기록 폴링은 success/failed까지 유지**한다. 폴링 실패 시 연결 오류 vs 응답 지연 메시지를 구분해 안내한다.
- **적용 (원격)**  
  - **버튼 활성화**: 각 발견된 호스트 카드의 「업데이트 적용」은 **호스트별**로 활성/비활성을 판단한다. 브라우저는 **`GET …/update-status?ip=<해당 호스트 IP>`** 를 호출해 받은 **`can_apply`**·**`apply_version`**·**`remote_current_version`**(및 스테이징 목록)을 사용한다 — **로컬 스테이징**과 **그 호스트의 현재 버전**(원격 `GET …/self`)에 대해 서버가 **`StagingUpdateAvailable`**·**`AllowSameVersionUpdate`**(§5.5.4·§7.1)로 계산한 결과와 일치시킨다. **`can_apply`가 확정된 응답(`ok`)이 있으면**, 업로드 영역의 **파일 선택만으로는** 버튼을 켜지 않는다. `can_apply`가 false이고 원격이 스테이징과 동일 버전이면 툴팁에 동일 버전 재적용 안내를 표시한다. 단순히 **스테이징 최상위 버전 문자열과 카드 `data-host-version`만 비교**하지 않는다. 원격 비교 조회가 진행 중이면 버튼·variant를 비활성·숨김·짧은 안내로 둔다. 카드에는 `data-host-version`·`data-build-variant`에 버전 키·variant를 저장한다.  
  - **버튼 스타일**: 활성화 시 **초록색** 계열(로컬 적용 버튼과 동일)로 표시하여 적용 가능 상태를 직관적으로 구분한다.  
  - **환경설정 재사용(원격)**: 로컬 스테이징에 번들이 있을 때만, **각 원격 카드**의 「업데이트 적용」 근처에 **독립된** 「이전버전의 환경설정 파일 재사용」 체크박스를 표시한다(기본 체크). 로컬 업데이트 패널의 체크박스와 **연동되지 않으며**, 해당 카드 적용 시 그 카드의 값만 `reuse_previous_config`로 전달한다. 스테이징 삭제 시 로컬·원격 카드의 체크박스를 모두 숨긴다. 체크 해제 후 적용 시 로컬과 동일한 확인·`alert` 규칙을 따른다.  
  - **클릭 동작**: 적용할 버전은 **`update-status` 응답의 `apply_version`**(또는 동등한 서버 판단)을 우선한다. 파일 선택이 없고 스테이징에 버전이 있으면 JSON `{ version, ip, agent_variant, reuse_previous_config }`로 로컬 서버에 보내며, 서버는 원격 에이전트의 upload API·apply-update API를 호출하여 배포한다(§5.5.3). **번들 파일을 함께 선택한 경우**에는 multipart `ip`, **`bundle`**, **`reuse_previous_config`** 로 전송한다(스테이징 없이 원격만 갱신할 때는 체크박스가 숨겨지므로 재사용은 off).  
  - **적용 성공 후**: JSON 적용 시에는 요청에 넣은 `version`을, multipart 적용 시에는 서버 성공 메시지에서 파싱한 버전을 사용하여, **host-info 응답을 기다리지 않고** 해당 호스트 카드의 버전 표시를 즉시 갱신한다. 이후 로컬 적용과 동일하게 **host-info 폴링**과 **§6.3.1 업데이트 기록 폴링**(`GET …/update-log?ip=`)을 병렬로 수행한다.  
  - **툴팁**:  
    - 비활성·스테이징에 파일 없음: "먼저 업데이트 영역에서 버전을 업로드하세요"  
    - 비활성·적용 불가(서버 `can_apply` false, 동일 버전·`AllowSameVersionUpdate` false 등): "최신 버전입니다" 또는 동일 버전·설정 안내  
    - 활성: 적용 가능한 **버전 키**를 표시(서버 `apply_version` 기준)
- **스테이징 버전 표시**: 「업로드된 버전 삭제」 버튼 옆에 현재 스테이징에 올라간 버전(예: "스테이징: 1.2.3")을 표시한다. 스테이징이 비어 있으면 표시하지 않는다.
- **업데이트 인디케이터**: 로컬·원격 카드 모두, 업데이트 적용이 진행 중일 때 카드 내 **서버 아이콘 아래**에 회전하는 로딩 인디케이터를 표시한다. **로컬**은 `/self` 폴링 성공(또는 폴링 종료) 후 숨긴다. **원격**은 host-info 폴링·패널 갱신 완료 후 숨긴다. 요청 실패 시 즉시 숨긴다.
- **파일 선택 초기화**: 번들 파일 선택만 초기화. 스테이징/versions 에 올라간 버전은 유지.
- **업로드된 버전 삭제**: 스테이징에서 해당 버전만 삭제. 삭제 성공 시 스테이징 표시·적용 버튼·**환경설정 재사용 체크박스**를 즉시 갱신한다.
- **업데이트 기록(로그)**: 호스트 카드 오른쪽 컬럼 **「업데이트 기록 (최근 10건)」** 블록(로컬·원격 동일). 수동 갱신 버튼 문구는 **「로그 새로고침」**(설치된 버전 목록 블록의 **「목록 새로고침」** 과 구분). `GET /api/v1/update-log`(`?ip=` 원격)로 표시하며, 요청마다 쿼리 `&_=<타임스탬프>`·`fetch` `cache: 'no-store'` 로 캐시를 쓰지 않는다. API tail 10줄을 **역순 표시**(최신이 위). **업데이트 진행 중**(임시 유닛 `contrabass-mole-update.service` active)에는 서버가 `recent_rollback`을 false로 반환하므로 롤백 경고를 숨긴다. **일괄 작업** 요약 줄(`config push-all`·`service restart-all`·`apply-update-all`·`rollback-all finished …`)만 맨 아래에 있을 때는 **`recent_rollback` false** — 롤백 경고를 띄우지 않는다.

#### 6.3.1 업데이트 기록 자동 갱신 (로컬·원격 적용·switch-current)

- **대상**: **「업데이트 적용」**·**「이 버전으로 적용」**(switch-current) 성공 직후 — **로컬 카드와 원격 카드 모두**. 원격은 `GET …/update-log?ip=` 를 사용한다.
- **주기**: 즉시 1회 + **2초 간격** `GET …/update-log`(최대 약 15분).
- **완료 판정**: `update_history.log`는 **맨 아래가 최신**이다. 해당 버전 키에 대해 로그에 **`update <버전> started`** 가 나타난 뒤, **표시 구간의 마지막 줄**(또는 전체 파일 기준 마지막 줄)이 **`update <버전> success`** 또는 **`update <버전> failed`**(부분 일치)이면 이번 실행이 끝난 것으로 보고 폴링을 멈춘다.
- **호스트 폴링과 분리**: `/self`·host-info 폴링이 먼저 성공해도 기록 폴링은 위 완료 조건까지 계속한다.
- **패널 일괄 갱신과의 조합**: 기록 폴링이 돌아가는 동안 `refreshAllPanelsAfterUpdate`는 **update-log 요청을 생략**하여, 호스트 복구 직후의 일괄 갱신이 **오래된 캐시 응답**으로 기록 영역을 덮어쓰지 않게 한다. 기록 폴링이 끝난 뒤에는 수동 「로그 새로고침」·다음 일괄 갱신으로 최종 내용을 맞출 수 있다.
- **재시작 구간**: maintenance HTTP가 잠시 끊기면 해당 tick은 실패할 수 있으나, 에이전트 기동 후 다음 tick에서 `started`→`success` 순으로 반영된다.
- **설치된 버전(versions)**: `GET /api/v1/versions/list` 로 목록을 가져오며, **서버 정렬 순서**(5.6)대로 표시한다. **current**·**previous**는 뱃지 및 삭제 비활성화. 목록은 2열·세로 우선으로 표시. 선택 버전만 `POST /api/v1/versions/remove` 로 삭제. **「이 버전으로 서비스」** 행의 select에는 **current에 해당하는 버전 키는 옵션에서 제외**한다(§5.6 switch-current).
- **프론트엔드 구현 정리**: 동일 로직은 헬퍼로 묶는다(예: 업데이트 로그 응답 반영, 버전 목록 렌더, 적용 후 `/self` 또는 `host-info` 폴링). 사용하지 않는 함수(hostname으로 카드 찾기 등)는 제거한다.

### 6.4 상태 새로고침 (내 정보·발견된 호스트)

- **내 정보** 카드와 **발견된 호스트** 카드 각각에 **「상태 새로고침」** 버튼을 둔다.
- **동작 방식**(로컬·원격 동일): 카드 전체를 다시 그리지 않고, (1) 호스트 정보 API로 카드 내용만 갱신한 뒤 (2) systemctl status를 조회해 표시한다.  
  - **내 정보**: `GET /api/v1/self`로 응답을 받아 기존 카드 DOM의 항목(버전, IP, 호스트명, CPU, 메모리 등)만 갱신하고, 이어서 `GET /api/v1/service-status`로 systemctl status를 갱신한다.  
  - **발견된 호스트**: `GET /api/v1/host-info?ip=<해당 호스트 IP>`로 응답을 받아 기존 카드의 호스트 정보만 갱신하고, 적용 버튼 활성/비활성·툴팁을 갱신한 뒤, `GET /api/v1/service-status?ip=...`로 systemctl status를 갱신한다. host-info가 실패해도 service-status는 조회하여 상태 영역은 갱신한다.

### 6.5 원격 HTTP 헬스 모니터링 (Discovery로 발견된 호스트)

- **목적**: 브로드캐스트 Discovery로만 알려진 원격 에이전트가 **Gin(`Server.HTTPPort`)** 경로에서 여전히 응답하는지 **HTTP**로 주기적으로 확인한다(UDP Discovery와 별개).
- **실행 조건**: **브라우저 탭이 열려 있고** `document.visibilityState`가 visible인 동안만 타이머로 폴링한다. 백그라운드 탭·에이전트 프로세스 단독에서는 수행하지 않는다.
- **클라이언트 동작**: `GET {APIPrefix}/remote-health-check?ip=` 를 호출한다(동일 출처·프록시). 간격·지터·실패 임계는 `Maintenance.RemoteHealth`(§7.1)와 `client-runtime.js`에 실린 값을 따른다. 연속 실패가 임계 이상이면 원격 카드에 경고·**「헬스 수동 확인」** 버튼을 표시하고, 한 줄 요약 행의 상태 점 스타일을 실패에 맞게 조정할 수 있다. 수동 확인 성공 시 `GET .../host-info?ip=`(UDP 유니캐스트 Discovery)로 호스트 정보를 다시 받아 카드·관련 패널을 갱신한다.
- **신규 Discovery**: 스트림으로 새 원격 카드가 추가되면 **동일 규칙**으로 해당 IP에 대한 헬스 모니터링을 시작한다.

### 6.6 모든 리모트 호스트 일괄 작업

- **위치**: 오른쪽 **sticky 사이드바** — 「업데이트」 패널 **아래** **「모든 리모트 호스트 일괄 작업」** 섹션. Discovery 섹션(가운데 열)과 분리한다.
- **버튼(4개)**  
  1. **「로컬 설정을 리모트 호스트에 일괄 복사」** — `POST …/current-config/push-local-all`  
  2. **「리모트 호스트 일괄 재시작」** — `POST …/service-control/restart-all`  
  3. **「리모트 호스트에 일괄 업데이트 적용」** — `POST …/apply-update-all`  
  4. **「리모트 호스트 일괄 롤백」** — `POST …/versions/rollback-all`  
- **대상 호스트 목록**: 화면 **원격 호스트 카드**(`.host-card`, self 제외)에서 **카드 1장 = 물리 호스트 1대**로 수집한다(`primary_ip`, `hostname`, `cpu_uuid`, `ips[]`). 요청 body `{ hosts: [...] }` 로 전송(레지스트리만 의존하지 않음).
- **버튼 활성 조건**  
  - **설정 복사·재시작**: 원격 카드 **≥1** (진행 중 해당 버튼은 `data-busy`로 비활성).  
  - **일괄 업데이트 적용**: 로컬 스테이징에 버전이 있고, **호스트별** `GET …/update-status?ip=` 로 확인한 **`can_apply`** 중 **도달 가능(§6.2 가드 통과) 호스트 ≥1**. 버튼 라벨에 **`(적용가능/전체)`** 표시. 로컬 패널 `can_apply`만으로 켜지 않는다.  
  - **일괄 롤백**: **호스트별** `GET …/versions/list?ip=` 로 **`is_current`·`is_previous` 버전 키**를 비교 — **`previous` 있고 `current`≠`previous`** 일 때만 롤백 가능. **도달 가능 호스트 중 롤백 가능 ≥1** 일 때만 활성. 롤백 성공 후 `current`·`previous`가 같아지면 비활성 유지(새 업데이트로 다시 갈라지면 활성).
- **일괄 업데이트 확인 모달**: 실행 전 확인 대화상자. **환경설정**은 오른쪽 「업데이트」 패널의 **「이전버전의 환경설정 파일 재사용」** 체크 상태를 **모든 원격에 동일**하게 `reuse_previous_config`로 전달한다(원격 카드별 체크박스는 따르지 않음). 적용 대상 대수·스테이징 버전을 요약한다.
- **NDJSON 진행**: 공통 `runBulkHostsNDJSON` — 진행 중 버튼 **`N/M`**·비활성. 완료 요약 예: `N대 모두 …` / `완료: 성공 N대, 실패 M대, 건너뜀 K대`.
- **결과·상태 UX**  
  - 작업마다 **클릭 순서대로** `#bulk-remote-status-list`에 **새 줄 추가**(고정 슬롯 없음, 반복 클릭 시 줄 누적).  
  - 상태 메시지 접두: 짧은 라벨 — **설정 복사**·**서비스 재시작**·**업데이트 적용**·**롤백**.  
  - 같은 줄 오른쪽: **「결과 보기」** + **×** — × 클릭 시 **해당 줄 전체** 제거.  
  - **「결과 보기」**: 공용 모달에 호스트별 `성공` / `실패` / `건너뜀` 및 `(IP 로 연결)` 등 표시.
- **완료 후 갱신**  
  - **설정 복사**: 원격 카드 config·service-status·업데이트 기록.  
  - **재시작**: service-status·원격 HTTP 헬스 모니터링(§6.5) 재등록.  
  - **일괄 업데이트**: **성공 호스트**에 대해 §6.3과 동일한 카드 갱신(host-info 폴링·패널 refresh·`update-status` 재조회).  
  - **일괄 롤백**: 성공 호스트의 service-status·업데이트 기록·versions/list·**롤백 가능 캐시** 갱신.  
  - 공통: `update_history.log` 요약 줄 자동 fetch.
- **API 상세**: §5.4·§5.4.1·§5.5.3.1.

---

## 7. 설정

- **포맷**: **YAML**
- **위치**: 구현 시 결정. 실행 시 **`-cfg <경로>`**(서비스 첫 인자)로 지정한다(인자 없이 기본 `agent.local.yml` 자동 로드는 하지 않음).
- **구조**: 모든 설정은 최상위 `Maintenance:` 아래에 둔다. 예:

```yaml
Maintenance:
  MaintenanceListenAddress: "127.0.0.1"
  MaintenancePort: PORT
  DiscoveryServiceName: "Mole-Discovery"
  DiscoveryUDPPort: 9999
  WebPrefix: "/web"
  APIPrefix: "/api/v1"
  RemoteHealth:
    IntervalSeconds: 10
    TimeoutSeconds: 2
    FailureThreshold: 3
    JitterSeconds: 2
```

### 7.1 설정 항목 (최소)

| 항목 | 설명 | 예시 |
|------|------|------|
| `Maintenance.DiscoveryServiceName` | Discovery 메시지의 `service` 값 | `"Mole-Discovery"` |
| `Maintenance.DiscoveryBroadcastAddress` | (선택) **Fallback**: 3.1.1 자동 수집이 비어 있을 때만 사용하는 단일 broadcast IP | `"192.168.0.255"` |
| ~~`Maintenance.DiscoveryBroadcastAddresses`~~ | **사용 안 함**. Discovery brd는 3.1.1 자동 수집(bonding·bridge·vlan 포함). |
| `Maintenance.DiscoveryUDPPort` | Discovery용 UDP 포트 | `9999` |
| `Maintenance.MaintenanceListenAddress` | (선택) maintenance HTTP 바인딩 주소. 기본 `"127.0.0.1"`(외부 비노출). 필요 시 `"0.0.0.0"` | `"127.0.0.1"`, `"0.0.0.0"` |
| `Maintenance.MaintenancePort` | HTTP 서비스 포트 | (예: `PORT`) |
| `Server.HTTPPort` | (필수) 원격 호스트에 대해 API를 호출할 때 사용하는 **외부 노출 포트(Gin)**. maintenance가 loopback-only(`127.0.0.1`)인 경우 원격 호출은 반드시 이 포트로 간다. | `8888` |
| `Maintenance.WebPrefix` | 프론트엔드 URL prefix | `"/web"` |
| `Maintenance.APIPrefix` | 백엔드 API URL prefix | `"/api/v1"` |
| `Maintenance.DiscoveryTimeoutSeconds` | Discovery 응답 대기 시간(초) | `10` |
| `Maintenance.DiscoveryDeduplicate` | 동일 호스트 중복 제거 여부 | `true` |
| `Maintenance.SystemctlServiceName` | (선택) 서비스 상태·제어 대상 유닛 이름 | `"contrabass-mole.service"` |
| `Maintenance.DeployBase` | (선택) 업데이트 배포 베이스. `staging/`·`versions/`·`current`·`previous`·`update_history.log` 의 기준 경로. **update/rollback 셸은 바이너리에 내장**되어 적용 시 `current` 아래에만 기록된다 | `"/var/lib/contrabass/mole"` |
| `Maintenance.InstallPrefix` | (선택) 에이전트(`BinaryName`) 설치 경로 prefix. `versions/` 목록·삭제 API 및 installer에서 사용. 비면 `DeployBase` 사용 | `"/var/lib/contrabass/mole"` |
| `Maintenance.SSHPort` | (선택) 원격 서비스 시작/중지 시 SSH 포트. 미지정 또는 0이면 22 사용 | `22` |
| `Maintenance.SSHUser` | (선택) 원격 서비스 시작/중지 시 SSH 사용자. 미지정이면 `"root"` | `"root"` |
| `Maintenance.MaxUploadBytes` | (선택) `POST /upload` 및 multipart `apply-update`의 **최대 요청 본문 크기**(바이트). 생략 시 `maintenance/agentcfg.DefaultMaxUploadBytes`(코드상 `64 << 20`). YAML에서는 **정수** 또는 문자열 **`"M << N"`** / 십진 문자열(예: `"67108864"`) — `maintenance/agentcfg`의 `uploadBytesExpr`로 파싱. 구현상 **1 MiB–10 GiB**로 클램프 | `67108864`, `"64 << 20"` |
| `Maintenance.RemoteHealth` | (선택) **원격 HTTP 헬스** 폴링(웹 UI, §6.5). 하위 키는 모두 정수. 생략 시 코드 기본값 적용 | 아래 표 참고 |
| `Maintenance.RemoteHealth.IntervalSeconds` | 기본 간격(초); 매 주기마다 `JitterSeconds` 이내 균등 랜덤 지연을 더해 다음 체크 시각을 잡는다 | `10` |
| `Maintenance.RemoteHealth.TimeoutSeconds` | `remote-health-check`가 원격 `GET …/health`를 기다리는 **HTTP 타임아웃**(초) | `2` |
| `Maintenance.RemoteHealth.FailureThreshold` | 연속 실패 횟수가 이 값 이상이면 카드에 실패 UI·수동 확인 버튼 | `3` |
| `Maintenance.RemoteHealth.JitterSeconds` | 매 간격에 `[0, JitterSeconds]` 초 범위의 추가 지연(초) | `2` |

- **Discovery 브로드캐스트 주소**: **3.1.1**에 따라 sysfs `type`·브리지 `brif/`·`ip` 출력으로 brd를 자동 수집한다(이름 패턴으로 거르지 않음). 수집이 비어 있을 때만 `DiscoveryBroadcastAddress`(단일)를 fallback으로 사용한다.
- **contrabass-mole.service는 root로 실행**되며, 로컬 서비스 상태·제어 시 **sudo를 사용하지 않는다**. 원격 **서비스 상태** 조회는 요청을 받은 서버가 원격 에이전트의 API(**`Server.HTTPPort`**, Gin)를 호출하고, 원격 에이전트가 자체 `systemctl status`를 실행한 뒤 응답을 반환한다. 원격 **서비스 시작/중지**는 요청을 받은 서버가 해당 호스트로 **SSH** 접속하여 `systemctl start/stop`을 실행한다(원격 에이전트가 꺼져 있어도 시작 가능). SSH 포트·사용자는 `SSHPort`, `SSHUser`로 지정하며, 키 기반 인증이 필요하다. 원격 **서비스 재시작**은 SSH를 사용하지 않고, 요청을 받은 서버가 원격 에이전트 API로 `POST service-control` (ip: "self", action: "restart")를 호출하며, 원격 에이전트가 자기 서버에서 `systemctl restart`를 실행한다(SSH 공개키 등록 없이 가능).

---

## 8. 서비스 시작 로그 및 버전 노출

- **systemctl status / journalctl**: 에이전트가 시작할 때 **버전 키**(빌드 시 주입된 `main.VersionKey`, 예: `0.4.0-2` 또는 describe 전체 `0.4.4-4-gc44d420`)을 로그에 남긴다. 예: `contrabass-moleU version 0.4.4-4-gc44d420: discovery listening on :9999 (bound IPs: ...)`. `journalctl -u contrabass-mole.service` 로 확인할 수 있다.

---

## 9. 버전 정보

- **CLI 버전 출력**: **`-version` / `--version`** 은 빌드 **ldflags** `main.VersionKey`(전체 버전 키 문자열)와 `appmeta.BinaryName` 을 **한 줄**로 출력한다(설정 파일 없음). 미주입 시 `0.0.0-0` 으로 표시된다. **호출 형태**: 권장은 **`contrabass-moleU agent --version`**; 구 스크립트 호환을 위해 **루트** `contrabass-moleU --version`(및 `-version`)도 허용한다(§4.1).
- **HTTP·Discovery 노출 문자열**: 서비스 기동 시(`-cfg`)에는 **`main.VersionKey`** 를 그대로 쓴다. 이 문자열이 **자기 정보 API**, **DISCOVERY_RESPONSE의 `version`**, **`GET /version`**, 시작 로그(§8)에 일관되게 쓰인다.
- **빌드 시 버전 키 주입**: 기본은 **`maintenance/scripts/build-version.sh`** 가 **`git describe --tags --long --always` 전체**를 표준 출력한다(`Makefile` 의 `VERSION_KEY ?= $(shell ./maintenance/scripts/build-version.sh)` → `go build -ldflags "-X main.VersionKey=…"`). 태그 없음·빈 저장소 등 예외는 스크립트 주석·구현과 동일하다. **수동 문자열**을 넣으려면 **`make build VERSION_KEY=<원하는 문자열>`** 이거나, 동일한 `-ldflags "-X main.VersionKey=…"` 를 직접 `go build` 에 넘긴다.
- **업데이트 판단**: 스테이징·`versions/` 디렉터리명·비교 API는 모두 **버전 키** 문자열을 사용한다(§5.5). 키는 위 파이프라인 또는 수동 주입으로 결정된다. **문자열 비교가 아니라** `maintenance/agentcfg` 의 비교 로직에서 describe 접미사 **`-g<해시>`** 를 제거한 뒤 시맨틱·패치로 순서를 정한다(§5.5.1).
- **실행 파일 검증**: 업로드·번들 검증 시 바이너리에 대해 **`--version` 실패 후 `agent --version`** 순으로 시도해 출력이 **`<BinaryName> `** 로 시작하는지 확인한다(`versionKeyFromAgentBinary`, §5.5.3·§12). 에이전트 자체는 루트 및 `agent` 경로 모두에서 버전 한 줄 출력 후 종료한다(§4.1).

---

## 10. 백엔드 역할

- **UDP Discovery**: 포트 9999에서 listen, **SO_BROADCAST** 설정 후 broadcast 주소로 Discovery 요청 송신, 응답은 unicast로 수신.
- **Pending**: 요청 전송 **전에** request_id → 수신 채널을 pending에 등록하여 빠른 응답이 버려지지 않도록 함. 타임아웃 시 반환 전 채널 drain.
- **자기(self) 응답 처리**: 일괄·SSE 수집 시 기본은 **자기 응답을 포함**하고 JSON에 `"self": true`를 둔다(CPU UUID 일치 시). **HTTP 쿼리 `exclude_self`**(또는 `exclude_self=true` 등, §5.3)가 켜지면 **CPU UUID**로 자기 식별해 제외하고, CPU UUID가 없을 때만 IP+ServicePort로 폴백 제외. 응답의 `host_ip`는 요청자 기준 outbound IP로 채움.
- Discovery 요청 수신 시 자신의 정보를 담은 DISCOVERY_RESPONSE를 **요청자 IP 및 요청 UDP 패킷의 소스 포트**로 unicast 전송(소스 포트가 0이면 discovery_udp_port로 폴백).
- **자기 정보 API**: GET /api/v1/self — 브로드캐스트 주소별 outbound IP를 `host_ips`로 반환하고, `host_ip`는 그중 첫 번째. 버전, CPU UUID, CPU, 메모리 등 포함.
- **cpu_uuid(호스트 식별자) 확보 순서(Linux)**: `/sys/class/dmi/id/product_uuid`(DMI가 있으면 `dmidecode -s system-uuid`와 동일 값; sysfs만 읽어 **dmidecode 바이너리 불필요**) → `/etc/machine-id` → `/var/lib/dbus/machine-id`(보통 `/etc/machine-id`와 동일). `/proc/cpuinfo`의 `Serial`은 사용하지 않는다(서버에서 비어 있는 경우가 많고, DMI 없는 환경은 machine-id로 식별). VM 템플릿 복제 시 여러 대가 동일 machine-id를 가질 수 있으니 운영 시 주의.
- **호스트 정보 API**: GET /api/v1/host-info?ip= — `ip` 없음/self면 /self와 동일. `ip` 지정 시 해당 IP로 Discovery 유니캐스트 요청을 보내 그 호스트의 DISCOVERY_RESPONSE를 반환. 타임아웃 시 fail.
- **HTTP 헬스(JSON)**: GET {APIPrefix}/health — 최소 JSON 성공 응답(원격 모니터링·`remote-health-check` 프록시 대상).
- **원격 헬스 프록시**: GET {APIPrefix}/remote-health-check?ip= — 로컬 에이전트가 원격 `Server.HTTPPort` + `{APIPrefix}/health` 로 HTTP GET(타임아웃 `Maintenance.RemoteHealth.TimeoutSeconds`).
- **Discovery API**: `GET {APIPrefix}/discovery/stream` (SSE) — 웹 UI에서 사용; 시작 실패 시 `discoveryfail` 이벤트·로그 `discovery: ERROR: DoDiscoveryStream …`. `GET {APIPrefix}/discovery` (일괄) — 웹 UI 미사용; 실패 시 JSON fail·로그 `discovery: ERROR: DoDiscovery …`. 일괄·SSE 공통으로 **쿼리 `exclude_self`·`timeout`(§5.3)**, `DiscoveryRunOptions`, `includeInDiscoveryResults`·`effectiveTimeout` 사용. 일괄 `data`는 배열·없을 때 `[]`. **유니캐스트 Discovery**: `host-info` 등, `DoDiscoveryUnicast`; 응답은 **`request_id`로 요청과만 매칭**한다. **멀티홈 호스트**에서는 유니캐스트 목적지 IP와 DISCOVERY_RESPONSE의 `host_ip`(또는 UDP 출발지)가 다를 수 있으므로, **`host_ip` 문자열이 목적지와 일치하지 않아도** 동일 응답으로 처리한다. 실패 시 로그 `discovery: ERROR: DoDiscoveryUnicast …`. 유니캐스트 타임아웃은 설정을 따르되 **최대 5초**.
- **서비스 상태 API**: GET /api/v1/service-status?ip= — 로컬(`ip` 없음/self)은 `systemctl status` (sudo 없음, root 실행). 원격은 요청자가 원격 **`Server.HTTPPort`** 로 GET service-status를 호출하고, 원격 에이전트가 자체 systemctl status 실행 후 응답을 반환.
- **서비스 제어 API**: POST /api/v1/service-control — body `{ "ip", "action": "start"|"stop"|"restart" }`. 로컬은 `systemctl start/stop/restart` (sudo 없음, root 실행). 원격 start/stop은 **SSH**(`SSHPort`, `SSHUser` 사용)로 `systemctl start|stop` 실행. 원격 **restart**는 SSH 없이 요청자를 받은 서버가 **원격 에이전트 API**로 POST service-control (ip: "self", action: "restart")를 호출하고, 원격 에이전트가 자기 서버에서 `systemctl restart` 실행. **일괄 재시작**은 POST `/service-control/restart-all`(§5.4·§6.6). **일괄 적용·롤백**은 §5.5.3.1.
- **업데이트 API**: 업로드는 `POST /api/v1/upload` 로 **스테이징** `DeployBase/staging/{버전 키}/` 에 **풀린 바이너리·config와 함께 원본 번들 `upload.bundle.tar.gz`** 를 저장한다(§5.5.1·5.5.3). **버전 키**는 업로드된 바이너리에 대해 §5.5.3과 동일한 **`--version`→`agent --version`** 폴백으로 읽으며, 스테이징·적용 API의 `version` 필드는 항상 이 키 문자열이다. **실행 파일 검증**(ELF + 버전 한 줄, §12)·**config 검증**(구조체 파싱 등) 후 400 가능. 로컬 적용 시 스테이징 전체를 `versions/`로 복사한 뒤 `upload.bundle.tar.gz`만 제거한다. 적용 시에는 **내장** `update.sh`/`rollback.sh` 를 `{DeployBase}/current/` 경로에 기록해 **`systemd-run`** 으로 `current/update.sh` 실행; 스크립트 종료 시 해당 두 파일은 스크립트가 삭제한다. **원격 적용(JSON)** 은 동일 **`POST .../upload`** 로 원격에 번들을 올린 뒤 apply-update(self); 스테이징에 원본 번들이 남아 있으면 그 바이트를 그대로 전송한다. `update-log`·`current-cfg` 는 원격 IP 지정 시 해당 호스트로 프록시한다. **`GET .../update-status`**: `ip` 없음/`self`는 로컬 `current` vs 로컬 스테이징; `ip=<원격>`은 원격 `GET .../self` 의 버전 vs **로컬 스테이징**(§5.5.4). update 실패 시 rollback 자동.
- **설치된 버전 API**: `install_prefix`(비면 deploy_base) 기준. GET /api/v1/versions/list?ip= — 로컬 목록은 **current → previous → 나머지 버전 키 내림차순**(시맨틱 수치 비교 후 패치 비교) 정렬. POST /api/v1/versions/remove (body에 `ip` 선택) → 원격 프록시 동일. 버전 키 검증·원격 시 대상 호스트 바이너리 일치 요구는 §5.6. current/previous 가리키는 버전 키는 삭제하지 않음.
- 정적 파일 서빙 (`/web` prefix).

---

## 11. 요약 체크리스트

- [ ] Go, 소스 경로 `~/work/mol`
- [ ] 단일 실행 파일, net/http 만 사용; **진입·종료**: `main`은 argv로 Gin 여부 분기(`IsAgentSubcommand`→`Run`만, `IsServiceModeRootCfg`→Gin+`go os.Exit(Run)`); `maintenance.Run`은 **0/1**만 반환·패키지 내 `os.Exit` 없음; **시그널은 `runServiceWithConfigPath`만** (`main`은 `signal.Notify` 없음)
- [ ] 포트: MaintenancePort(HTTP), DiscoveryUDPPort(UDP Discovery), UDP SO_BROADCAST 설정
- [ ] Discovery: UDP broadcast 요청(목적지 포트 discovery_udp_port), 응답은 요청자 IP:**요청 소스 포트**로 unicast; pending 등록 후 전송, 타임아웃 시 drain
- [ ] Discovery 메시지: DISCOVERY_REQUEST / DISCOVERY_RESPONSE (JSON), 호스트 정보(CPU, MEMORY, cpu_uuid) 포함; 응답에는 host_ip 하나만(요청자 기준 outbound IP); 수신 측이 responded_from_ip(UDP 발신지) 설정; 수신 측에서 같은 호스트의 여러 응답으로 IP·응답한 IP 취합
- [ ] Discovery 자기 응답: 기본 **포함**(`"self": true`); 쿼리 **`exclude_self`** 시 CPU UUID(또는 IP+ServicePort 폴백)로 제외
- [ ] Discovery 브로드캐스트: **3.1.1** (type=1, 브리지는 brif 슬레이브 존재, IPv4 brd; 이름 필터 없음); 송신 목록은 brd 문자열 중복 제거; fallback은 discovery_broadcast_address 또는 255.255.255.255; **`contrabass-moleU agent --nic-brd`**로 확인; 참고 셸 **`brd_for_bm.sh`**
- [ ] Discovery 타임아웃(설정), 중복 제거(host_ip:service_port), 설정 파일 반영
- [ ] Discovery 실시간: GET /api/v1/discovery/stream (SSE), **웹 UI는 이 API만 사용**, EventSource, **event: discoveryfail** 시 서버 메시지 표시·**journalctl** 안내; 응답 오는 대로 화면 갱신; 기존 카드 매칭은 **cpu_uuid → IP** 순서만 사용(**hostname 미사용**, 동일 hostname 다른 호스트 병합 방지), event: done 후 스트림 종료(일괄 API 추가 호출 없음)
- [ ] Discovery 일괄: `GET {APIPrefix}/discovery`, data 배열(빈 경우 []); 쿼리 `exclude_self`·`timeout`; **웹 UI 미호출**
- [ ] Discovery SSE: `GET {APIPrefix}/discovery/stream`, 동일 쿼리 지원; 웹 UI는 쿼리 없이 기본만 사용
- [ ] Gin 프록시(루트 main): **`<bin> -cfg <파일>`**(`IsServiceModeRootCfg`)일 때만 `router.Run`; maintenance는 고루틴 `os.Exit(Run)`; **`agent …` 전체**는 Gin 없음; JSON `Content-Type`은 **전역 미들웨어 금지**·`routerGroupJSON` 그룹만; 쿼리 유실 방지(`Form` 비우기·`RequestURI` 보조)
- [ ] 웹: `client-runtime.js`로 `APIPrefix`·`RemoteHealth` 설정 주입 후 `app.js` API 호출
- [ ] URL prefix: `WebPrefix`·`APIPrefix`, 설정에서 변경 가능
- [ ] 진입 URL: /web/index.html, Discovery 버튼
- [ ] 초기 화면: 내 정보 (버전, IP 또는 host_ips, CPU UUID, 호스트, CPU, MEMORY)
- [ ] 호스트 목록: 아코디언(한 줄 요약 + 클릭 시 상세 카드 펼침), 상태 점(파랑=동작 중/빨강=중지/회색=미확인), 로컬은 맨 위·배경/테두리 색으로 구분, 로컬 IP는 Discovery 후 responded_from_ip 반영
- [ ] 발견된 호스트: 서버 아이콘 + CPU UUID(맨 위), 버전, IP(복수 시 취합 표시), 응답한 IP(복수 시 취합), 호스트명, CPU, MEMORY; 병합 시 기존 카드 매칭은 cpu_uuid·IP만(hostname 미사용)
- [ ] systemctl status: 접기/펼치기(기본 접힘), 접힌 상태에서 [정상 서비스 상태] / [서비스 중지 상태]
- [ ] 레이아웃: 호스트 카드 가운데 열(max-width 610px), **업데이트·일괄 작업** 오른쪽 sticky 사이드바; scrollbar-gutter: stable; 카드 내 버튼 오른쪽 위 절대 위치, 서비스 상태 영역은 카드 끝까지 넓게; 내 정보는 카드 한 겹만
- [ ] 내 정보 카드: 시작/중지 버튼 없음; 오른쪽 컬럼(업데이트 기록·agent.local.yml·설치된 버전)·하단(상태 새로고침·서비스 재시작)
- [ ] 발견된 호스트 카드: **로컬과 동일 레이아웃**(오른쪽 컬럼 + 하단 상태 행). 시작·중지 버튼 비노출; 상태 새로고침·서비스 재시작·업데이트 적용. 카드 열릴 때 업데이트 기록·config·버전 목록 자동 로드
- [ ] 서비스 상태 API: 로컬은 systemctl, 원격은 원격 에이전트 API(`Server.HTTPPort`). 서비스 제어: 로컬은 systemctl; 원격 start/stop은 SSH, **원격 restart는 원격 에이전트 API 호출**(SSH 키 불필요)
- [ ] 원격 API 프록시: update-log·current-cfg(GET/POST)·**current-config/push-local**·versions/list·versions/remove 에 `ip` 쿼리 또는 body 지원, 중앙 서버가 원격 에이전트 해당 API 호출 후 응답 전달
- [ ] **일괄 원격 작업**: **push-local-all**·**restart-all**·**apply-update-all**·**rollback-all** NDJSON(`hosts` body = UI 카드 1장=1대); **remoteregistry**·**discovered-remotes**; `update_history.log` 요약 1줄; **`recent_rollback`** 이 bulk `failed=N`에 반응하지 않음; 롤백-all은 **current=previous** 시 skipped
- [ ] **일괄 UI(§6.6)**: 오른쪽 사이드바 4버튼; 상태 줄 **클릭 순 누적**·짧은 접두·「결과 보기」+**×**; apply-all **호스트별 can_apply**·`(N/M)`·확인 모달(로컬 재사용 체크); rollback-all **versions/list** 기반 버튼 비활성; Discovery **미응답**·헬스 dead 가드
- [ ] 서비스 재시작 후: 성공 또는 terminated/연결 끊김 시 친절한 메시지 + 잠시 후 자동 호스트 정보(버전 등) 갱신 + 상태 새로고침(로컬·원격 동일)
- [ ] 설정: DiscoveryServiceName, SystemctlServiceName, DeployBase, **InstallPrefix**(비면 DeployBase, versions·installer용), DiscoveryBroadcastAddress(fallback만), SSHPort(기본 22), SSHUser(기본 root), **MaxUploadBytes**(선택, 기본 `64<<20`, YAML 정수·`"M << N"` 문자열), **`Maintenance.RemoteHealth`**(선택, 원격 HTTP 헬스 폴링 간격·타임아웃·임계·지터); **버전 키는 빌드(`main.VersionKey`)·업로드 바이너리**(§12, `--version`→`agent --version` 폴백)
- [ ] **CLI**: **`-cfg <파일>`** 또는 **`agent -cfg <파일>`** 로 HTTP 서버 + Discovery 기동(동일 서비스); 그 외 서브커맨드는 첫 인자 **`agent`** 필수; **`--host-info` / `--apply-update` / `--versions-list` / `--versions-switch`** 는 **`-apiprefix`**(기본 `/maintenance/api/v1`)·대상 Gin REST·**서비스 필수**(`clirest`); `agent --nic-brd`; **`agent --discovery`**(UDP만); 번들·ELF 검증은 서버 `POST /upload` 시 **`--version` → `agent --version`** 폴백
- [ ] 설치된 버전: GET /api/v1/versions/list(정렬: current → previous → 시맨틱 내림차순), POST /api/v1/versions/remove; current/previous 제외 삭제; 웹 UI 2열 세로 우선, 선택 삭제
- [ ] **최초 설치**: `bin/ubuntu/contrabass-agent-install.sh` — root·manifest v2 tar.gz·`control|compute`·`agent --version` 버전 키·optional `sha256sum`·`versions/`+`current`+`staging/`+systemd
- [ ] **완전 제거**: `bin/ubuntu/contrabass-agent-uninstall.sh` — root·인자 없음·service stop/disable·유닛 삭제·`DeployBase`·`/var/log/contrabass/mole` 삭제
- [ ] 업데이트: DeployBase, **staging/**, **versions/(버전 키 디렉터리)**, **내장 update.sh/rollback.sh**(`maintenance/updatescripts` embed, `Makefile` 동기화·**strip**); 적용 시 **`current/update.sh`**; transient 유닛 **`contrabass-mole-update`**; **스테이징·비교·적용은 버전 키**; 실행 파일·config 검증; **`reuse_previous_config`**(적용 전 **`current` config** → `versions/<키>/`, 원격은 orchestrator가 current-config 주입+원격 apply); 웹 **환경설정 재사용** 체크박스(스테이징 있을 때만, 로컬 패널·원격 카드 각각); `update_history.log` **append**·**flock**(`update_history.log.lock` 잔존은 정상); **일괄** push·restart·apply·rollback 요약 append; 로컬·원격 적용·switch-current 후 **페이지 전체 새로고침 없이** host-info/`/self` 폴링과 **별도** update-log 폴링(2초·started→**마지막 줄** success/failed, tail 10·캐시 무효화·**역순 표시**); 원격 `update-log` 프록시 tail·`no-store` 정규화; 웹 **「로그 새로고침」** / **「목록 새로고침」** 라벨 구분; **GET /version** 헬스; **`recent_rollback`**(실제 update/rollback 실패만, bulk `failed=N` 제외)·update_in_progress
- [ ] 프론트: 업데이트 영역 — 업로드(실행 파일+config, **config 편집 영역에서 수정 후 업로드 가능**), 서버에서 실행 파일·config 검증 실패 시 에러 메시지(항목/줄·필요 타입 안내) 표시; 적용(로컬/원격), 파일 선택 초기화, 업로드된 버전 삭제, **스테이징 버전 표시**, 로그 표시/새로고침; **업데이트 인디케이터**(카드 내, 서버 아이콘 아래)
- [ ] Discovery: 진행 중 기존 목록 유지·제어 가능; **일괄 작업(§6.6)**·**미응답 배지**·진행 카운트다운; 원격 적용 후 Discovery 재수행 없이 카드·로그·config·versions·상태까지 현행화; DISCOVERY_REQUEST JSON **1300바이트 미만** 검증; `service` 필드는 **`DiscoveryServiceName`** 과 일치 시에만 응답
- [ ] 원격 적용: 호스트별 **`GET …/update-status?ip=`** 의 **`can_apply`·`apply_version`** 으로 버튼·툴팁(스테이징 최신 문자열만과 카드 버전 문자열 비교에만 의존하지 않음), 클릭 시 서버가 원격 upload·apply-update API 호출; **적용 성공 시 적용 버전으로 카드 버전 즉시 갱신(낙관적 갱신)**, 지연 후 host-info·service-status로 전체 갱신
- [ ] 호스트 정보 API: GET /api/v1/host-info?ip= (self=로컬, 지정=유니캐스트 Discovery)
- [ ] Discovery 유니캐스트: DoDiscoveryUnicast(ip), 타임아웃 최대 5초; 멀티홈 시 `host_ip`≠목적지 IP여도 수락(request_id로 상관)
- [ ] 상태 새로고침: 내 정보·원격 동일 방식 — 호스트 정보 API(GET /self 또는 GET /host-info?ip=)로 카드 내용만 갱신 후 GET /service-status로 systemctl 상태 갱신(카드 전체 재렌더링 없음)
- [ ] 일반 API 응답: status + data
- [ ] 자기 정보 API: GET /api/v1/self
- [ ] 설정: YAML, 항목 7.1 반영
- [ ] 버전: **`main.VersionKey`**(`Makefile`·`maintenance/scripts/build-version.sh`의 전체 describe, 또는 `VERSION_KEY=` 수동 주입)로 노출·업데이트 경로와 일치; 업로드·번들 검증은 바이너리 **`--version`→`agent --version`** 폴백(§12); 비교 시 `-g<해시>` 제거(§5.5.1)
- [ ] 프론트: embed 정적 파일, Vanilla JS, EventSource로 Discovery 스트림 수신

---

## 12. 명명·운영 기준 (최근 정리)

다음은 코드·문서·운영에서 혼동을 줄이기 위해 맞춘 기준이다. 세부 동작은 상위 절을 따른다.

| 구분 | 값 / 설명 |
|------|-----------|
| Go 모듈 | `contrabass-agent` (`go.mod`) |
| 실행 파일(바이너리) 이름 | `maintenance/appmeta.BinaryName` — 기본 **`contrabass-moleU`** (Makefile·배포 스크립트와 동일) |
| 상시 systemd 유닛 (에이전트) | 기본 **`contrabass-mole.service`** (`Maintenance.SystemctlServiceName`) — `contrabass-moleU` 프로세스를 띄우는 서비스 |
| 임시 업데이트 유닛 | **`contrabass-mole-update.service`** — `systemd-run --unit=contrabass-mole-update` 로 `current/update.sh` 만 실행하는 **transient** 작업용. 메인 유닛과 별개이며 외부 연동용 이름이 아님. 코드 상수: `appmeta.UpdateTransientUnitStem` / `appmeta.UpdateTransientUnit` |
| Discovery `service` 문자열 | 기본 **`Mole-Discovery`** (`Maintenance.DiscoveryServiceName`, `maintenance/agentcfg.DefaultDiscoveryServiceName`) |
| 설정 파일 지정 | **`-cfg <경로>`** 또는 **`agent -cfg <경로>`** 로 HTTP+Discovery 기동(동일). **바깥 Gin(`Server.HTTPPort`)** 은 **`<bin> -cfg …` 진입에서만** `main`이 연다. **`MOL_CONFIG` 환경 변수는 사용하지 않음** (`config.Load` 빈 경로 시 현재 디렉터리 `agent.local.yml`) |
| 배포 번들 | **§5.5.0** — tar.gz + `contrabass.manifest.yaml` + agent + config; `pack-agent-tarball.sh` |
| 업로드 multipart | 필드 **`bundle`** — 위 번들. 스테이징: **`BinaryName`**·config basename·**`upload.bundle.tar.gz`** |
| 원격 배포 upload | 로컬 에이전트가 호출하는 **`POST .../upload`는 업로드 API와 동일**; 소스 바이트는 스테이징의 `upload.bundle.tar.gz` 우선, 없으면 바이너리+config로 재패킹 |
| 배포 디렉터리 내 실행 파일 | `staging/`·`versions/<버전 키>/` 아래 파일명은 **`BinaryName`** (과거 단일 바이너리 파일명 규칙은 사용하지 않음). `update.sh` 도 동일 파일명을 기대 |
| `GET /version` | 한 줄: **`<BinaryName> <버전 키>`** (버전 키는 `git describe` 전체 문자열일 수 있음) |
| 업로드 시 바이너리 버전 검증 | `<path> --version` 후 실패 시 `<path> agent --version` — 표준 출력 한 줄이 **`<BinaryName> `** 로 시작 (`validateAgentBinary` / `versionKeyFromAgentBinary`) |

---

*본 PRD는 Contrabass agent 제품 요구 사항을 통합 기술 사양으로 기술하며, 구현·검증의 기준으로 삼는다.*
