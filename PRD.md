# Contrabass agent — 제품 요구 사항 문서 (PRD)

## 1. 개요

- **프로젝트명**: Contrabass agent (저장소·작업 트리 디렉터리 예: `~/work/mol`)
- **언어**: Go
- **소스 위치**: `~/work/mol`
- **실행 형태**: 프론트엔드와 백엔드를 포함한 **단일 실행 파일**
- **소스 레이아웃**: Discovery·Discovery CLI(`discoverycli`)·호스트 정보·HTTP API·서비스 상태·웹 정적 파일은 **`maintenance/`** 아래 패키지로 구성된다. 루트 `main.go`는 `contrabass-agent/maintenance` 만 import한다. **설정(YAML)** 은 **`internal/config`** 에서 로드한다. **업데이트/롤백 셸**은 루트 `update.sh`·`rollback.sh` 를 소스로 하여 **`internal/updatescripts/`** 에 복사 후 **`//go:embed`** 로 바이너리에 포함한다(`Makefile`이 빌드 전 동기화). Discovery용 IPv4 brd 자동 수집 규칙과 동일한 의도의 **참고 셸** `brd_for_bm.sh` 를 저장소 루트에 둔다(§3.1.1).
- **진입점·종료 코드**: 루트 `main.go`는 빌드 시 주입되는 **`main.VersionKey`**(ldflags `-X main.VersionKey=…`, `Makefile` → `scripts/build-version.sh`·git describe)과 **`main()`** 만 두고, **`contrabass-moleU -cfg <파일>`**(비어 있지 않은 경로)인 **서비스 모드**에서만 Gin 리버스 프록시(`Server.HTTPPort`)를 `go`로 기동한 뒤 **`maintenance.Run(main.VersionKey, os.Args)`** 를 호출하고, 그 반환값으로 **`os.Exit`** 한다. `--nic-brd`·`--discovery`·`-h` 등 **CLI 전용** 실행 시에는 Gin을 띄우지 않는다. **`maintenance.Run(buildVersionKey, args []string) int`** 는 **명령줄은 `args` 인자로만** 받으며, 성공·오류는 **`0` 또는 `1`** 반환만으로 알린다(`maintenance` 패키지에서 `os.Exit`를 호출하지 않음). HTTP·Discovery 서비스 기동·`-h`·`--version`·`--nic-brd`·`-cfg` 등의 분기와 **`//go:embed web/*`**(웹 정적 파일)은 **`maintenance/maintenance.go`** 에 모은다. **`discoverycli.Run`** 은 **`contrabass-moleU --discovery`** 경로에서 **종료 코드 `int`** 를 반환한다(`os.Exit` 없이).
- **소스 트리와 테스트**: 배포용 저장소에는 Go **`*_test.go`** 단위 테스트 파일을 두지 않는다(단일 바이너리 산출물에는 원래 테스트가 포함되지 않으며, 소스 정책상 별도 테스트 파일 없이 유지한다). 회귀 검증이 필요하면 `go test`용 파일을 로컬·CI에서만 두거나 이력에서 복구한다.
- **웹 서버**: Go 표준 라이브러리 **net/http** 만 사용 (외부 웹 프레임워크 미사용)

---

## 2. 아키텍처 요약

- **서비스 포트(maintenance HTTP)**: 설정 `Maintenance.MaintenancePort` (HTTP — 웹 UI + API). 기본적으로 `Maintenance.MaintenanceListenAddress = "127.0.0.1"` 로 **로컬호스트에만 바인딩**하고, 외부 접근은 **루트 `main.go`의 Gin**이 설정 `Server.HTTPPort`로 리슨하며 **`Maintenance.WebPrefix`·`Maintenance.APIPrefix`**(기본 `/web`, `/api/v1`) 경로를 maintenance로 **리버스 프록시**한다. API가 웹 prefix 아래에 중첩된 경우(예: `WebPrefix=/maintenance`, `APIPrefix=/maintenance/api/v1`) Gin 라우터 제약으로 **와일드카드 한 트리**만 등록하고, 백엔드는 동일 URL 경로로 요청을 받는다. 프록시는 전달 전 **`Form`/`PostForm`을 비우고**, `URL.RawQuery`가 비어 있으면 **`RequestURI`의 쿼리**로 복구하여(표준 `ReverseProxy`+선행 파싱으로 쿼리가 유실되는 경우 방지) API **쿼리 파라미터**가 maintenance 핸들러까지 전달되도록 한다. 필요 시 `Maintenance.MaintenanceListenAddress = "0.0.0.0"` 로 외부 바인딩도 가능하다.
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

- **`contrabass-moleU --nic-brd`** 는 위 규칙과 동일하게 **(인터페이스 이름 : brd)** 를 한 줄씩 출력한다. Gin(`Server.HTTPPort`)은 서비스 모드(`-cfg <파일>`)에서만 기동되므로, **`--nic-brd`·`--discovery`·`-h` 등 CLI 전용 실행에서는 Gin이 바인딩되지 않는다**(루트 `maintenance.ShouldStartGinReverseProxy`: 첫 인자가 `-cfg`일 때만 true).

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
- **프론트엔드**: Discovery 버튼 클릭 시 **EventSource** 로 `{APIPrefix}/discovery/stream` 에 연결한다(설정 기본은 `/api/v1/discovery/stream`). **`discoveryfail` 이벤트**가 오면 `data.message` 를 읽어 상태 영역에 **「Discovery 요청 실패:」+ 서버 메시지**를 표시하고 스트림을 닫는다. 일반 메시지 이벤트가 올 때마다 수신한 JSON을 파싱해, **같은 CPU UUID**가 이미 있으면 해당 카드에 IP·응답한 IP를 병합·갱신하고, 없으면 **같은 IP**가 있는 카드를 찾아 갱신하고, 그 외에는 **새 카드**를 추가한다. 기존 카드 매칭은 cpu_uuid → IP 순서만 사용하며 hostname은 사용하지 않는다. `discoveryfail` 을 처리한 뒤에는 **onerror** 와 중복 문구가 나오지 않도록 구분한다. `event: done` 수신 시 스트림을 닫고 버튼을 복구한다. 연결만 끊기고 사유가 없는 경우에는 **journalctl** 안내 문구를 띄운다. 호스트 카드 상세에서는 **CPU UUID**를 맨 위에, **IP**·**응답한 IP** 순으로 표시한다.

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
- prefix는 설정 파일에서 수정할 수 있어야 한다. 브라우저는 하드코딩된 `/api/v1`가 아니라, 서버가 `{WebPrefix}/client-runtime.js`로 내려주는 **`window.__CONTRABASS_API_PREFIX__`**(실제 `APIPrefix`)를 먼저 로드한 뒤 `app.js`가 API를 호출한다.

### 4.1 CLI (명령줄)

- **인자 없이 실행**: **`contrabass-moleU`** — 버전과 `-cfg <파일>` 필요 안내를 출력하고 종료한다. HTTP 서비스는 **`-cfg`로 설정 파일을 지정했을 때만** 기동한다.
- **`-cfg <파일>`**: 설정 파일 경로(필수 인자). 이 옵션으로만 HTTP·Discovery가 기동한다. systemd 등에서는 `ExecStart=.../contrabass-moleU -cfg /path/to/config.yaml` 형태로 지정한다.
- **`-h`, `--help`**: 도움말(사용법·옵션 설명) 출력 후 종료.
- **`-version`, `--version`**: 버전 문자열 출력 후 종료.
- **`--nic-brd`**: §3.1.1과 동일 규칙으로 IPv4 브로드캐스트(brd)를 `NIC이름 : brd주소` 형식으로 출력(확인용) 후 종료.
- **`--discovery`**: 설정 파일·HTTP 서버 없이 **UDP Discovery만** 수행. `--dest-port`(기본 9999), `--src-port`(기본 9998), `--timeout`(초, 기본 10), `--service`(기본 `Mole-Discovery`). 시작 시 **사용 가능한 brd(브로드캐스트) 주소를 모두 한 줄씩 출력**한다. 에이전트와 같이 **서브넷별로 로컬 IP:src-port 소켓을 열어** 각 brd로 송신한다(다중 NIC·src≠dest 안정화). `reply_udp_port` 포함 `DISCOVERY_REQUEST` 전송 후, 같은 줄에서 `Discovering ... N` 카운트다운 → **`Discovery Done.`** → 수신 유예·드레인. 결과는 호스트별 **`[Local]`** / **`[Remote]`** `hostname - 대표 IP : [응답한 IP만]` 형식으로, **`responded_from_ip`**만 취합한다. Local/Remote는 **CPU UUID 일치(대소문자 무시)** 우선, 아니면 **응답한 IP가 로컬 IPv4와 겹치는지**로 보조 판별한다.

---

## 5. API

**엔드포인트별 메서드(GET/POST)·쿼리/바디·응답 형식 요약**은 [`docs/REST_API.md`](docs/REST_API.md)를 본다.

### 5.1 공통 응답 형식 (일반 API)

- **status**: `"success"` 또는 `"fail"`
- **data**: 숫자, 문자열, 배열 등 JSON으로 표현 가능한 값

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

### 5.5 업데이트 API

#### 5.5.1 배포 디렉터리 구조·버전 키

- **배포 베이스** `DeployBase`(기본 `/var/lib/contrabass/mole`) 아래에는 **스테이징** `staging/`·**버전별 실행 트리** `versions/`·**현재/이전 포인터** `current`·`previous`·**기록** `update_history.log` 가 둔다. **업데이트/롤백 셸 스크립트는 배포 루트에 상주시키지 않는다** — 내용은 **에이전트 단일 바이너리(contrabass-moleU)에 내장**되며, 적용 시점에만 `current`가 가리키는 버전 디렉터리 아래에 풀어 쓴다(아래 5.5.3).
- **버전 디렉터리 이름(버전 키)** 은 빌드·바이너리가 내보내는 문자열 전체(예: git describe **`0.4.4-4-gc44d420`**, 또는 레거시 **`0.4.4-5`** 형태)가 스테이징·`versions/` 아래 디렉터리명이 된다. 비교·정렬 시 describe 접미사 **`-g<해시>`** 는 제거한 뒤 시맨틱·패치만 사용한다. **실행 중인 에이전트**의 키는 빌드 시 **`main.VersionKey`** 로 주입되며, **config.yaml에는 버전을 두지 않는다**. 시맨틱 부분은 점으로 구분된 숫자 세그먼트 개수에 고정 제한이 없다(예: `1.2.3.4-0`).  
  - **비교 규칙**: 동일 **시맨틱**(접두부)인 경우 **패치 숫자**만 정수로 비교한다(구현에서는 마지막 `-`(또는 레거시 `_`) 뒤를 정수로 파싱). 시맨틱이 다르면 기존과 같이 **서로 다른 릴리스**로 보고, 스테이징에 다른 버전 키가 있으면 적용 가능으로 본다(다운그레이드 포함).  
  - **레거시**: 과거에 `versions/0.4.0` 처럼 `-패치` 없이 둔 디렉터리는 **패치 0**으로 해석하여 비교한다. 과거 `_숫자` 형식 디렉터리도 읽을 수 있다.
- **노출 버전 문자열**: 로그·Discovery·`GET /version`·DISCOVERY_RESPONSE의 `version` 등에 쓰이는 문자열은 위 **버전 키**와 동일하다.

  ```
  deploy_base/                    # 예: /var/lib/contrabass/mole (설정 키: DeployBase)
  ├── current -> versions/0.4.0-2 # 심볼릭 링크, 현재 실행 버전(버전 키)
  ├── previous -> versions/0.4.0-1
  ├── update_history.log          # 업데이트·롤백 기록 (맨 앞에 추가, 최근 5건을 웹에 표시)
  ├── staging/                    # 업로드 API로만 채움, 적용 시 versions로 복사 가능
  │   └── <버전 키>/
  │       ├── contrabass-moleU
  │       └── config.yaml
  └── versions/
      └── <버전 키>/
          ├── contrabass-moleU
          └── config.yaml
  ```

- **스테이징**: 업로드는 **실행 중인** `versions/<버전 키>/` 가 아닌 **`{DeployBase}/staging/<버전 키>/`** 에만 저장하여 "text file busy" 를 피한다. 적용 시 소스는 **스테이징 우선**, 없으면 **versions/**.
- **스테이징 정리**: 자동 삭제하지 않는다. 로컬 적용 후에도 스테이징을 남겨 같은 버전 키로 원격 적용을 반복할 수 있다. 삭제는 웹 「업로드된 버전 삭제」로 **스테이징만** 수동 삭제한다.

#### 5.5.2 update.sh·rollback.sh (소스·내장·실행 위치)

- **소스**: 저장소 루트에 `update.sh`, `rollback.sh` 가 있으며, 빌드 시 **`internal/updatescripts/`** 로 복사한 뒤 Go **`//go:embed`** 로 바이너리에 포함한다. **`Makefile`** 의 `build` 타깃이 루트 스크립트를 해당 디렉터리로 동기화한 다음 `go build` 하므로, 릴리스 빌드는 항상 최신 스크립트가 내장된다.
- **배포 베이스에 별도 복사 불필요**: 운영 호스트에 `scp` 로 스크립트만 갱신할 필요가 없다. 에이전트 바이너리를 교체하면 내장 스크립트도 함께 갱신된다.
- **BASE 산정**: 스크립트는 **`{DeployBase}/current/`** 옆이 아니라, **실행 시 `current` 심볼릭 링크가 가리키는 버전 디렉터리**(`versions/<버전 키>/`)에 놓인다.  
  - `SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"` — 스크립트 파일이 있는 디렉터리(현재 버전 트리).  
  - `BASE="$(cd "$SCRIPT_DIR/.." && pwd)"` — 그 **부모**가 배포 루트(`DeployBase`).  
  - 따라서 `VERSIONS="$BASE/versions"`, `HISTORY_LOG="$BASE/update_history.log"` 등이 일관된다.
- **롤백 호출**: `update.sh` 는 실패 시 **`"$SCRIPT_DIR/rollback.sh"`** 를 실행한다(같은 버전 디렉터리에 풀어 둔 rollback).
- **수명**: 적용 API가 시작할 때 `current` 아래에 두 파일을 **쓰기·실행 권한(0755)** 으로 생성한 뒤 `systemd-run` 으로 `update.sh` 를 실행한다. 스크립트는 **정상 종료·롤백 종료·조기 실패** 등 모든 종료 경로에서 **`cleanup_scripts`** 로 **같은 디렉터리의** `update.sh`·`rollback.sh` 를 삭제한다. `systemd-run` 자체가 즉시 실패하면 에이전트가 생성한 두 파일을 제거한다.
- **스크립트 본문 요약**  
  - **PATH**: `export PATH="/usr/bin:/bin:/usr/local/bin:${PATH:-}"` (transient 유닛 대비).  
  - **config 읽기**: 적용 대상 `versions/<인자 버전 키>/config.yaml` 에서 `MaintenancePort`, `SystemctlServiceName` 등(실패 시 기본값, `|| true`).  
  - **update.sh**: 인자 **버전 키** 하나. `{BASE}/versions/{버전 키}/contrabass-moleU`(실행 파일명은 빌드·`appmeta.BinaryName`과 동일) 존재·실행 가능 확인 → 서비스 중지 → `previous` 갱신 → `current` 를 해당 버전으로 교체 → 서비스 시작 → `curl` 로 `http://127.0.0.1:${HTTP_PORT}/version` 헬스. 실패 시 `rollback.sh`.  
  - **rollback.sh**: `previous` 가 있으면 서비스 중지 → `current` 를 `previous` 와 동일 대상으로 교체 → 시작.  
  - **기록**: `update_history.log` 에 prepend 방식으로 한 줄씩 추가.

#### 5.5.3 업로드·삭제·적용

- **업로드** `POST {serverUrl}/api/v1/upload`  
  - **multipart**: 필드명 **`agent`**(실행 파일), **`config`**(config.yaml).  
  - **실행 파일 검증**: ELF 매직 + 스테이징 경로에서 `--version` 실행(5초), 출력이 **`"<BinaryName> "`**(`maintenance/appmeta.BinaryName`, 기본 `contrabass-moleU`)로 시작·종료 0.  
  - **config 검증**: `internal/config` 구조체로 파싱; 실패 시 줄·항목·필요 타입 안내(예: `DiscoveryServiceName`, `DiscoveryUDPPort`, `MaintenancePort` 등).  
  - **버전 키(스테이징 디렉터리명)**: 업로드된 **실행 파일**을 임시 경로에서 **`--version`** 실행하여, 출력 한 줄 `<BinaryName> <버전 키>` 의 뒷부분을 읽는다. config에는 버전을 두지 않는다.  
  - **웹 편집**: 편집 영역 내용이 그대로 전송·스테이징에 반영된다.  
  - **성공**: `{ "status": "success", "data": { "version": "<버전 키>" } }`.
- **업로드 삭제** `POST .../upload/remove` — Body `{ "version": "<버전 키>" }`. **스테이징** 만 삭제; `versions/` 는 유지.
- **적용 (로컬)** `POST .../apply-update`, Body `{ "version": "<버전 키>", "ip": "self" 또는 생략 }`  
  - 소스: 스테이징 우선, 없으면 `versions/`.  
  - 스테이징에만 있으면 `versions/` 로 복사 후 진행.  
  - **`{DeployBase}/current` 존재 필수**(심볼릭 링크 또는 그에 준하는 배포). 없으면 적용 불가.  
  - 내장 `update.sh`·`rollback.sh` 내용을 **`{DeployBase}/current/update.sh`**, `.../rollback.sh` 로 쓴 뒤(실제 파일은 `current` 가 가리키는 `versions/<현재 버전 키>/` 아래),  
    `systemd-run --unit=contrabass-mole-update --property=RemainAfterExit=yes /bin/bash <그 경로>/update.sh <적용할 버전 키>`  
  - 응답은 즉시 성공(백그라운드 적용). 에이전트는 root로 동작·sudo 없음.
- **적용 (원격)**: 대상 호스트 에이전트 HTTP로 (1) upload (2) `apply-update` with `ip: "self"` — 로컬과 동일하게 원격이 `current` 아래에 스크립트를 풀고 실행한다. JSON·multipart(원격) 흐름은 기존과 동일하되, **`version` 은 항상 버전 키 문자열**이다.

#### 5.5.4 업데이트 상태·기록·설정·헬스

- **업데이트 상태** `GET .../update-status`  
  - **Query `ip` (선택)**  
    - 비어 있거나 `"self"`: **이 에이전트** 기준. `current_version`은 `readlink` 등으로 `current` 가 가리키는 디렉터리 이름(버전 키).  
    - **원격 IP** 지정: **이 서버의 로컬 스테이징** 목록은 그대로 사용하고, 비교 대상 “현재 버전”만 **원격 호스트**에서 가져온다 — 요청을 받은 서버가 원격 **`Server.HTTPPort`** 로 `GET .../self` 를 호출해 응답의 `version`(버전 키)을 사용한다. 응답에는 `remote_ip`, `remote_current_version` 을 넣고 `current_version` 은 넣지 않는다. 원격 조회 실패 시 `fail`.  
  - `staging_versions`: 스테이징 아래 디렉터리 목록(버전 키). **비교 가능한 순서**(버전 키 비교, 새 쪽이 앞)로 정렬. (원격 `ip` 여부와 관계없이 **항상 이 서버의 스테이징**이다.)  
  - **`can_apply` / `apply_version`**: 스테이징에 올라온 버전 키 중, **비교 기준 버전**(로컬이면 `current_version`, 원격이면 `remote_current_version`) 대비 **업데이트로 적용할 가치가 있는지** 판단한다 — 규칙은 동일(시맨틱·패치 비교, `StagingUpdateAvailable`). 원격 모드에서는 “**이 서버 스테이징을 그 원격에 적용할 수 있는지**”를 나타낸다.  
  - `remove_version`: 스테이징 정렬 후 **가장 오래된(맨 끝)** 항목 등 UI 삭제용으로 쓸 수 있다.  
  - `update_in_progress`: **요청을 처리하는 이 서버**에서 `systemctl is-active contrabass-mole-update.service` 가 active 이면 true(원격 호스트의 진행 여부는 이 필드로 알 수 없음).
- **업데이트 기록** `GET .../update-log` — `update_history.log` 최근 5줄, `recent_rollback`, 진행 중이면 롤백 플래그 완화 등 기존과 동일.
- **current-cfg** `GET/POST .../current-cfg` — 기존과 동일.
- **헬스** `GET /version` — **`<BinaryName> <버전 키>`** 한 줄(예: `contrabass-moleU 0.4.4-10`), text/plain, 항상 200. update.sh 의 curl 이 사용한다.

### 5.6 설치된 버전(versions) API

- **경로 기준**: `InstallPrefix`(설정, 비면 `DeployBase`) 아래 `versions/` 디렉터리 및 `current`·`previous` 심볼릭 링크를 사용한다. installer 등에서도 동일 경로를 참조할 수 있도록 `InstallPrefix`를 둔다.
- **목록**: `GET {serverUrl}/api/v1/versions/list?ip=`  
  - `ip` 비어 있거나 `"self"`: `{InstallPrefix}/versions/` 디렉터리 내 각 **버전 키** 이름의 하위 디렉터리(그 안에 **`appmeta.BinaryName` 실행 파일**이 있는 것만)를 나열하고, `current`·`previous` 심볼릭 링크가 가리키는 버전을 판별하여 `is_current`·`is_previous` 플래그와 함께 반환한다. 응답: `{ "status": "success", "data": { "versions": [ { "version", "is_current", "is_previous" }, ... ] } }` — 여기서 `version` 문자열은 디렉터리명과 동일한 **버전 키**이다.  
  - **정렬 순서(표시용)**: **current** 대상을 맨 위 → **previous** 대상 → 그 외는 **버전 키 비교 규칙**(시맨틱 부분을 절 단위 정수로 비교한 뒤, 같으면 `-`(또는 레거시 `_`) 뒤 패치를 정수로 비교)에 따른 **내림차순**(더 “새” 버전이 위). 웹 UI에서 현재·이전·나머지 순으로 한눈에 볼 수 있다.  
  - `ip` 지정: 요청을 받은 서버가 **원격 호스트의 `Server.HTTPPort`(Gin)** 로 `GET .../versions/list` 를 호출한 뒤 응답을 그대로 클라이언트에 전달한다.
- **삭제**: `POST {serverUrl}/api/v1/versions/remove`  
  - Body: `{ "versions": [ "<버전>", ... ], "ip": "" | "self" | "<host_ip>" }`. `ip`가 비어 있거나 `"self"`이면 로컬에서 삭제. `ip` 지정 시 요청을 받은 서버가 **원격 `Server.HTTPPort`** 로 `POST .../versions/remove` (Body: `{ "versions": [...] }`)를 호출한 뒤 응답을 그대로 클라이언트에 전달한다. 로컬/원격 공통: `current`·`previous`가 가리키는 버전은 삭제하지 않고 제외 사유와 함께 응답에 포함한다.  
  - **버전 키 검증**: 삭제 대상 문자열은 **`ValidateVersionKeyPath`와 동일한 규칙**(디렉터리명으로 안전한 문자; 패치 구분 `-`(레거시 `_` 허용), 예 `0.4.4-9`)을 따른다. 구현상 업로드·적용 API와 같은 검증을 사용한다.  
  - **원격 `ip` 사용 시 주의**: 실제 삭제·검증은 **`ip`로 지정된 호스트에서 실행되는 에이전트**가 수행한다. 클라이언트가 붙은 머신(또는 Gin 프록시 앞단)만 최신으로 올리고 **원격 호스트는 구버전 바이너리**이면, 응답 메시지·검증 동작은 **원격 프로세스** 기준이 된다(예: 구버전에서 잘못된 문자 제한이 남아 있으면 그쪽 메시지가 그대로 돌아온다). 원격에서도 동일 동작을 기대하려면 **해당 호스트에 동일 빌드를 배포**한다.  
  - **프록시 선검증**: `ip`가 원격일 때 요청을 받은 서버는 원격으로 넘기기 전에 버전 키 형식을 검사하여, 잘못된 항목은 즉시 `fail`(HTTP 400)할 수 있다.

---

## 6. 프론트엔드

- **구현 방식**: 정적 파일(HTML, CSS, JavaScript)을 **Go embed**로 단일 실행 파일에 포함.
- **JavaScript**: **Vanilla JS**만 사용. API 호출은 `fetch`, UI 업데이트는 DOM 조작으로 처리. SPA 프레임워크(React, Vue 등)는 사용하지 않는다.
- **레이아웃**
  - 호스트 정보(내 정보·발견된 호스트) 카드는 **가운데 열**에 배치하고, **업데이트** 영역은 **화면 오른쪽**에 고정(sticky)하여 스크롤 시 카드만 스크롤되고 업데이트 영역은 고정된다. 스크롤바가 생겨도 레이아웃이 밀리지 않도록 `scrollbar-gutter: stable`을 사용한다.
  - 호스트 카드의 가로 최대 너비는 610px로 통일하며, 내 정보와 발견된 호스트 카드는 동일한 카드 스타일 한 겹만 사용한다(내 정보 컨테이너는 카드 클래스를 갖지 않고, 렌더된 카드 한 개만 자식으로 둔다).
  - 카드 내 **시작/중지·업데이트 적용·상태 새로고침** 버튼은 카드 **오른쪽 위**에 절대 위치로 배치한다. 상단의 호스트 정보 항목(CPU UUID, 버전, IP 등)만 버튼과 겹치지 않도록 오른쪽 여백을 두고, **서비스 상태(터미널)** 영역은 카드 오른쪽 끝까지 넓게 표시한다.
- **초기 화면**
  - **내 정보**: 현재 에이전트 인스턴스의 버전, **IP(또는 응답으로 사용하는 모든 IP `host_ips`)** , 호스트명, CPU UUID, CPU, MEMORY 등을 표시 (자기 정보 API 사용). 자기 정보 API는 각 브로드캐스트 주소별 outbound IP를 `host_ips`로 반환하여 Discovery 응답으로 사용하는 IP들을 모두 보여준다.
- **Discovery 버튼**
  - 클릭 시 **EventSource** 로 `GET /api/v1/discovery/stream` 에 연결하여 **실시간 Discovery**를 수행한다. **기존 발견된 호스트 목록은 비우지 않고** 유지하며, 진행 중에도 해당 카드들의 제어(시작/중지·업데이트 적용·상태 새로고침)가 가능하다. SSE로 호스트가 도착할 때 **같은 CPU UUID**가 있으면 해당 카드에 IP만 병합·갱신하고, 없으면 같은 IP 카드 갱신 또는 새 카드 추가한다. `event: done` 수신 시 스트림을 닫고 버튼을 복구한다.
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
  - **카드 버전 즉시 갱신(낙관적 갱신)**: apply-update API 성공 시점에 이미 알고 있는 **적용 버전**으로 카드의 버전 표시(`data-host-version` 속성 및 버전 dd 텍스트)를 **즉시** 갱신하고, 적용 버튼 활성/비활성 상태를 다시 계산한다.  
  - **지연 후 host-info 및 패널 전체 현행화**: 약 5초 후부터 `GET /api/v1/host-info?ip=...`를 **2초 간격으로 최대 8회** 재시도한다. **성공 시** 카드 호스트 정보를 덮어쓴 뒤 **업데이트 기록(update-log)·config.yaml(current)·설치된 버전(versions/list)·서비스 상태(service-status)** 및 로컬 **update-status**(스테이징 표시)를 한꺼번에 다시 불러온다. **재시도를 모두 소진해도** 가능한 API는 동일하게 호출하여 남은 정보를 갱신한다. 그 후 업데이트 인디케이터를 숨긴다.

### 6.1 systemctl status 표시 (내 정보·발견된 호스트 공통)

- 각 호스트 카드에 **systemctl status** 결과를 표시한다.
- **접기/펼치기**: 기본은 **접힌 상태**. 헤더(아이콘 ▶/▼ + 요약 문구) 클릭 시 상세 출력(`systemctl status` 전문)을 펼치거나 접는다.
- **접힌 상태에서의 요약**  
  - `Active: active (running)` 이면 **\[정상 서비스 상태]**  
  - 그 외(dead 등)면 **\[서비스 중지 상태]**  
  - 로딩/에러 시 "불러오는 중…", "상태를 불러올 수 없습니다." 등.

### 6.2 서비스 시작/중지·재시작 및 원격 카드 레이아웃

- **내 정보(자기 자신) 카드**에는 시작/중지 버튼을 두지 않는다. **오른쪽 컬럼**에 업데이트 기록(최근 5건)·config.yaml(current) 편집·설치된 버전(versions) 목록을 두고, **하단**에 서비스 상태(접기/펼치기)·「상태 새로고침」·「서비스 재시작」을 둔다.
- **발견된 호스트(원격) 카드**는 **로컬 카드와 동일한 레이아웃**을 사용한다. 오른쪽 컬럼에 업데이트 기록·config.yaml(current)·설치된 버전을 두고, 하단에 서비스 상태·「상태 새로고침」·「서비스 재시작」·「업데이트 적용」을 둔다. **시작**·**중지** 버튼은 노출하지 않는다(원격 시작/중지는 SSH로만 수행).
- **원격 카드 열릴 때**: 해당 행을 펼치면(아코디언 확장 시) **업데이트 기록**·**config.yaml 불러오기**·**설치된 버전 목록**을 자동으로 해당 원격 호스트 API(`?ip=` 또는 body `ip`)로 요청하여 표시한다. 로컬 카드는 초기 로드 시 동일 데이터를 자동 표시한다.
- **서비스 제어 API 동작**: `POST /api/v1/service-control` with `{ "ip": "<host_ip>", "action": "start"|"stop"|"restart" }`.  
  - **로컬**(ip 없음/self): `systemctl start/stop/restart` (sudo 없음, root 실행).  
  - **원격 start/stop**: 요청을 받은 서버가 해당 원격 호스트로 **SSH** 접속하여 `systemctl start|stop` 실행. 설정 `SSHPort`(기본 22), `SSHUser`(기본 "root") 사용.  
  - **원격 restart**: SSH 없이 요청을 받은 서버가 **원격 에이전트 API**(`Server.HTTPPort`)로 `POST .../service-control` (Body `{ "ip": "self", "action": "restart" }`)를 호출. 원격 에이전트가 자기 서버에서 `systemctl restart` 실행.
- **서비스 재시작 후 UI**: 재시작 요청 성공 시 또는 연결 끊김/terminated 등 재시작 진행 중으로 보이는 오류 시, 요약에 「재시작되었습니다. 잠시 후 상태를 불러옵니다.」 등 친절한 메시지를 표시하고, **몇 초 후 자동으로** (1) `GET /api/v1/self`(로컬) 또는 `GET /api/v1/host-info?ip=...`(원격)로 호스트 정보를 가져와 카드의 **버전·호스트명·IP 등**을 갱신하고, (2) `GET /api/v1/service-status`로 요약을 [정상 서비스 상태] 등으로 갱신한다. config.yaml의 version을 수정한 뒤 재시작한 경우에도 카드에 새 버전이 반영된다. 로컬·원격 동일. 사용자가 「상태 새로고침」을 누르지 않아도 된다.
- (참고) **서비스 상태** 조회(GET /api/v1/service-status?ip=)는 로컬은 직접 systemctl, 원격은 원격 에이전트 API(`Server.HTTPPort`)를 호출하는 방식으로 유지한다.

### 6.3 업데이트 (업로드·적용·로그)

- **업로드**: 실행 파일(`BinaryName`)과 config.yaml을 선택한 뒤 `POST /api/v1/upload` (multipart: **`agent`**, `config`). **버전 키**는 업로드된 바이너리의 **`--version`** 출력에서 읽으며, 스테이징 디렉터리명으로 쓴다. 성공 시 메시지에 그 버전 키가 표시된다. 서버에서 **실행 파일 검증**(ELF 형식 + `--version` 실행, §12)과 **config.yaml 검증**(구조체 파싱, 실패 시 항목/줄/필요 타입 안내)을 수행하며, 잘못된 파일·설정이면 거절하고 에러 메시지를 반환한다.  
  - **config.yaml 수정 후 업로드**: config 파일을 선택하면 내용이 **편집 영역**(textarea)에 표시된다. 사용자가 버전, broadcast 주소 등 설정을 수정한 뒤 업로드하면 **수정된 내용**이 서버로 전송되어 스테이징에 저장된다. 원본 파일을 수정 없이 올릴 수도 있고, 편집만 하고 파일을 다시 선택하지 않아도 업로드 시 편집 영역 내용이 사용된다.
- **적용 (로컬)**: 버전이 스테이징 또는 이전 적용으로 존재할 때, 적용 버튼으로 `POST /api/v1/apply-update` (`{ "version": "..." }`). 성공 시 에이전트(`contrabass-mole.service`) 재시작으로 연결이 끊길 수 있으므로 **전체 페이지 새로고침은 하지 않는다**. 약 4초 후부터 `GET /api/v1/self`를 **2초 간격 최대 15회** 폴링하여 서버가 다시 뜨면 **업데이트 기록·config.yaml·설치된 버전·서비스 상태·update-status**를 모두 다시 불러와 현행화한다. 대기 중 업데이트 로그는 **2초 간격**으로 조용히 갱신한다. 폴링 실패 시 연결 오류 vs 응답 지연 메시지를 구분해 안내한다. 실패 시 에러 메시지.
- **적용 (원격)**  
  - **버튼 활성화**: 각 발견된 호스트 카드의 「업데이트 적용」은 **호스트별**로 활성/비활성을 판단한다. 스테이징(또는 세션 내 업로드된 **버전 키**)이 있고, 그 값이 **해당 호스트에 표시된 버전 키와 문자열 기준으로 다를 때** 적용 가능으로 본다(서버의 `update-status` 판단과 같은 버전 키 체계). 카드에는 `data-host-version`에 버전 키를 저장한다.  
  - **버튼 스타일**: 활성화 시 **초록색** 계열(로컬 적용 버튼과 동일)로 표시하여 적용 가능 상태를 직관적으로 구분한다.  
  - **클릭 동작**: 적용할 버전은 **스테이징에 올라간 버전**(또는 세션 내 업로드 버전)을 사용한다. 파일 선택이 없어도 스테이징에 버전이 있으면 JSON `{ version, ip }` 로 로컬 서버에 보내며, 서버는 원격 에이전트의 upload API·apply-update API를 호출하여 배포한다. 실행 파일·config만 선택된 경우에는 multipart `ip`, **`agent`**, `config` 로 전송하면 서버가 원격 upload API로 전달한 뒤 apply-update를 호출한다.  
  - **적용 성공 후 카드 버전 표시**: JSON 적용 시에는 요청에 넣은 `version`을, multipart 적용 시에는 서버 성공 메시지(예: "원격 ... 에 버전 X 적용 완료")에서 파싱한 버전을 사용하여, **host-info 응답을 기다리지 않고** 해당 호스트 카드의 버전 표시를 즉시 갱신한다. 이후 지연 후 host-info가 성공하면 전체 호스트 정보로 한 번 더 갱신된다.  
  - **툴팁**:  
    - 비활성·스테이징에 파일 없음: "먼저 업데이트 영역에서 버전을 업로드하세요"  
    - 비활성·스테이징 버전과 현재 버전 동일: "최신 버전입니다"  
    - 활성: "x.x.x-y 버전으로 업데이트 가능합니다" (스테이징의 버전 키; 시맨틱·패치 표기)
- **스테이징 버전 표시**: 「업로드된 버전 삭제」 버튼 옆에 현재 스테이징에 올라간 버전(예: "스테이징: 1.2.3")을 표시한다. 스테이징이 비어 있으면 표시하지 않는다.
- **업데이트 인디케이터**: 로컬·원격 카드 모두, 업데이트 적용이 진행 중일 때 카드 내 **서버 아이콘 아래**에 회전하는 로딩 인디케이터를 표시한다. **로컬**은 `/self` 폴링 성공(또는 폴링 종료) 후 숨긴다. **원격**은 host-info 폴링·패널 갱신 완료 후 숨긴다. 요청 실패 시 즉시 숨긴다.
- **파일 선택 초기화**: 실행 파일·config 선택 및 편집 내용만 초기화. 스테이징/versions 에 올라간 버전은 유지.
- **업로드된 버전 삭제**: 스테이징에서 해당 버전만 삭제.
- **업데이트 기록(로그)**: `GET /api/v1/update-log` 로 최근 5건을 표시. **로컬 적용 진행 중**에는 **2초 간격**으로 조용히 폴링한다(완료 후 위 “적용 (로컬)” 흐름에서 전체 패널 갱신과 함께 최종 반영). **업데이트 진행 중**(임시 유닛 `contrabass-mole-update.service` active)에는 서버가 `recent_rollback`을 false로 반환하므로 롤백 경고를 숨긴다.
- **설치된 버전(versions)**: `GET /api/v1/versions/list` 로 목록을 가져오며, **서버 정렬 순서**(5.6)대로 표시한다. **current**·**previous**는 뱃지 및 삭제 비활성화. 목록은 2열·세로 우선으로 표시. 선택 버전만 `POST /api/v1/versions/remove` 로 삭제.
- **프론트엔드 구현 정리**: 동일 로직은 헬퍼로 묶는다(예: 업데이트 로그 응답 반영, 버전 목록 렌더, 적용 후 `/self` 또는 `host-info` 폴링). 사용하지 않는 함수(hostname으로 카드 찾기 등)는 제거한다.

### 6.4 상태 새로고침 (내 정보·발견된 호스트)

- **내 정보** 카드와 **발견된 호스트** 카드 각각에 **「상태 새로고침」** 버튼을 둔다.
- **동작 방식**(로컬·원격 동일): 카드 전체를 다시 그리지 않고, (1) 호스트 정보 API로 카드 내용만 갱신한 뒤 (2) systemctl status를 조회해 표시한다.  
  - **내 정보**: `GET /api/v1/self`로 응답을 받아 기존 카드 DOM의 항목(버전, IP, 호스트명, CPU, 메모리 등)만 갱신하고, 이어서 `GET /api/v1/service-status`로 systemctl status를 갱신한다.  
  - **발견된 호스트**: `GET /api/v1/host-info?ip=<해당 호스트 IP>`로 응답을 받아 기존 카드의 호스트 정보만 갱신하고, 적용 버튼 활성/비활성·툴팁을 갱신한 뒤, `GET /api/v1/service-status?ip=...`로 systemctl status를 갱신한다. host-info가 실패해도 service-status는 조회하여 상태 영역은 갱신한다.

---

## 7. 설정

- **포맷**: **YAML**
- **위치**: 구현 시 결정. 실행 시 **`-cfg <경로>`** 로 지정한다(인자 없이 기본 `config.yaml` 자동 로드는 하지 않음).
- **구조**: 모든 설정은 최상위 `Maintenance:` 아래에 둔다. 예:

```yaml
Maintenance:
  MaintenanceListenAddress: "127.0.0.1"
  MaintenancePort: PORT
  DiscoveryServiceName: "Mole-Discovery"
  DiscoveryUDPPort: 9999
  WebPrefix: "/web"
  APIPrefix: "/api/v1"
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

- **Discovery 브로드캐스트 주소**: **3.1.1**에 따라 sysfs `type`·브리지 `brif/`·`ip` 출력으로 brd를 자동 수집한다(이름 패턴으로 거르지 않음). 수집이 비어 있을 때만 `DiscoveryBroadcastAddress`(단일)를 fallback으로 사용한다.
- **contrabass-mole.service는 root로 실행**되며, 로컬 서비스 상태·제어 시 **sudo를 사용하지 않는다**. 원격 **서비스 상태** 조회는 요청을 받은 서버가 원격 에이전트의 API(**`Server.HTTPPort`**, Gin)를 호출하고, 원격 에이전트가 자체 `systemctl status`를 실행한 뒤 응답을 반환한다. 원격 **서비스 시작/중지**는 요청을 받은 서버가 해당 호스트로 **SSH** 접속하여 `systemctl start/stop`을 실행한다(원격 에이전트가 꺼져 있어도 시작 가능). SSH 포트·사용자는 `SSHPort`, `SSHUser`로 지정하며, 키 기반 인증이 필요하다. 원격 **서비스 재시작**은 SSH를 사용하지 않고, 요청을 받은 서버가 원격 에이전트 API로 `POST service-control` (ip: "self", action: "restart")를 호출하며, 원격 에이전트가 자기 서버에서 `systemctl restart`를 실행한다(SSH 공개키 등록 없이 가능).

---

## 8. 서비스 시작 로그 및 버전 노출

- **systemctl status / journalctl**: 에이전트가 시작할 때 **버전 키**(빌드 시 주입된 `main.VersionKey`, 예: `0.4.0-2`)을 로그에 남긴다. 예: `contrabass-moleU version 0.4.0-2: discovery listening on :9999 (bound IPs: ...)`. `journalctl -u contrabass-mole.service` 로 확인할 수 있다.

---

## 9. 버전 정보

- **CLI `--version`**: **`-version` / `--version`** 은 빌드 **ldflags** `main.VersionKey`(전체 버전 키 문자열)와 `appmeta.BinaryName` 을 한 줄로 출력한다(설정 파일 없음). 미주입 시 `0.0.0-0` 으로 표시된다.
- **HTTP·Discovery 노출 문자열**: 서비스 기동 시(`-cfg`)에는 **`main.VersionKey`** 를 그대로 쓴다. 이 문자열이 **자기 정보 API**, **DISCOVERY_RESPONSE의 `version`**, **`GET /version`**, 시작 로그(§8)에 일관되게 쓰인다.
- **업데이트 판단**: 스테이징·`versions/` 디렉터리명·비교 API는 모두 **버전 키** 문자열을 사용한다(§5.5). 키는 git describe 등 빌드 파이프라인에서 결정된다.
- **실행 파일 검증**: 업로드된 바이너리는 `--version` 으로 실행해 출력이 **`<BinaryName> `** 로 시작하는지 확인한다(§12). 에이전트는 `--version` / `-version` 시 버전 한 줄 출력 후 종료한다.

---

## 10. 백엔드 역할

- **UDP Discovery**: 포트 9999에서 listen, **SO_BROADCAST** 설정 후 broadcast 주소로 Discovery 요청 송신, 응답은 unicast로 수신.
- **Pending**: 요청 전송 **전에** request_id → 수신 채널을 pending에 등록하여 빠른 응답이 버려지지 않도록 함. 타임아웃 시 반환 전 채널 drain.
- **자기(self) 응답 처리**: 일괄·SSE 수집 시 기본은 **자기 응답을 포함**하고 JSON에 `"self": true`를 둔다(CPU UUID 일치 시). **HTTP 쿼리 `exclude_self`**(또는 `exclude_self=true` 등, §5.3)가 켜지면 **CPU UUID**로 자기 식별해 제외하고, CPU UUID가 없을 때만 IP+ServicePort로 폴백 제외. 응답의 `host_ip`는 요청자 기준 outbound IP로 채움.
- Discovery 요청 수신 시 자신의 정보를 담은 DISCOVERY_RESPONSE를 **요청자 IP 및 요청 UDP 패킷의 소스 포트**로 unicast 전송(소스 포트가 0이면 discovery_udp_port로 폴백).
- **자기 정보 API**: GET /api/v1/self — 브로드캐스트 주소별 outbound IP를 `host_ips`로 반환하고, `host_ip`는 그중 첫 번째. 버전, CPU UUID, CPU, 메모리 등 포함.
- **cpu_uuid(호스트 식별자) 확보 순서(Linux)**: `/sys/class/dmi/id/product_uuid`(DMI가 있으면 `dmidecode -s system-uuid`와 동일 값; sysfs만 읽어 **dmidecode 바이너리 불필요**) → `/etc/machine-id` → `/var/lib/dbus/machine-id`(보통 `/etc/machine-id`와 동일). `/proc/cpuinfo`의 `Serial`은 사용하지 않는다(서버에서 비어 있는 경우가 많고, DMI 없는 환경은 machine-id로 식별). VM 템플릿 복제 시 여러 대가 동일 machine-id를 가질 수 있으니 운영 시 주의.
- **호스트 정보 API**: GET /api/v1/host-info?ip= — `ip` 없음/self면 /self와 동일. `ip` 지정 시 해당 IP로 Discovery 유니캐스트 요청을 보내 그 호스트의 DISCOVERY_RESPONSE를 반환. 타임아웃 시 fail.
- **Discovery API**: `GET {APIPrefix}/discovery/stream` (SSE) — 웹 UI에서 사용; 시작 실패 시 `discoveryfail` 이벤트·로그 `discovery: ERROR: DoDiscoveryStream …`. `GET {APIPrefix}/discovery` (일괄) — 웹 UI 미사용; 실패 시 JSON fail·로그 `discovery: ERROR: DoDiscovery …`. 일괄·SSE 공통으로 **쿼리 `exclude_self`·`timeout`(§5.3)**, `DiscoveryRunOptions`, `includeInDiscoveryResults`·`effectiveTimeout` 사용. 일괄 `data`는 배열·없을 때 `[]`. **유니캐스트 Discovery**: `host-info` 등, DoDiscoveryUnicast; 실패 시 로그 `discovery: ERROR: DoDiscoveryUnicast …`. 유니캐스트 타임아웃은 설정을 따르되 **최대 5초**.
- **서비스 상태 API**: GET /api/v1/service-status?ip= — 로컬(`ip` 없음/self)은 `systemctl status` (sudo 없음, root 실행). 원격은 요청자가 원격 **`Server.HTTPPort`** 로 GET service-status를 호출하고, 원격 에이전트가 자체 systemctl status 실행 후 응답을 반환.
- **서비스 제어 API**: POST /api/v1/service-control — body `{ "ip", "action": "start"|"stop"|"restart" }`. 로컬은 `systemctl start/stop/restart` (sudo 없음, root 실행). 원격 start/stop은 **SSH**(`SSHPort`, `SSHUser` 사용)로 `systemctl start|stop` 실행. 원격 **restart**는 SSH 없이 요청자를 받은 서버가 **원격 에이전트 API**로 POST service-control (ip: "self", action: "restart")를 호출하고, 원격 에이전트가 자기 서버에서 `systemctl restart` 실행.
- **업데이트 API**: 업로드는 `POST /api/v1/upload` 로 **스테이징** `DeployBase/staging/{버전 키}/` 에만 저장한다. **버전 키**는 업로드된 바이너리의 `--version` 에서 읽으며, 스테이징·적용 API의 `version` 필드는 항상 이 키 문자열이다. **실행 파일 검증**(ELF + `--version`, §12)·**config 검증**(구조체 파싱 등) 후 400 가능. 적용 시에는 **내장** `update.sh`/`rollback.sh` 를 `{DeployBase}/current/` 경로에 기록해 **`systemd-run`** 으로 `current/update.sh` 실행; 스크립트 종료 시 해당 두 파일은 스크립트가 삭제한다. **원격 적용**은 원격 에이전트의 upload → apply-update(self) 와 동일 모델. `update-log`·`current-cfg` 의 프록시 동작은 기존과 같다. **`GET .../update-status`**: `ip` 없음/`self`는 로컬 `current` vs 로컬 스테이징; `ip=<원격>`은 원격 `GET .../self` 의 버전 vs **로컬 스테이징**(§5.5.4). update 실패 시 rollback 자동.
- **설치된 버전 API**: `install_prefix`(비면 deploy_base) 기준. GET /api/v1/versions/list?ip= — 로컬 목록은 **current → previous → 나머지 버전 키 내림차순**(시맨틱 수치 비교 후 패치 비교) 정렬. POST /api/v1/versions/remove (body에 `ip` 선택) → 원격 프록시 동일. 버전 키 검증·원격 시 대상 호스트 바이너리 일치 요구는 §5.6. current/previous 가리키는 버전 키는 삭제하지 않음.
- 정적 파일 서빙 (`/web` prefix).

---

## 11. 요약 체크리스트

- [ ] Go, 소스 경로 `~/work/mol`
- [ ] 단일 실행 파일, net/http 만 사용; **진입·종료**: 루트 `main.go`는 `maintenance.Run(main.VersionKey, os.Args)` 반환값으로 `os.Exit`만 수행; `maintenance.Run`은 명령줄을 `args`로 받고 **0/1**만 반환(`maintenance`·`discoverycli`에서 `os.Exit` 없음)
- [ ] 포트: MaintenancePort(HTTP), DiscoveryUDPPort(UDP Discovery), UDP SO_BROADCAST 설정
- [ ] Discovery: UDP broadcast 요청(목적지 포트 discovery_udp_port), 응답은 요청자 IP:**요청 소스 포트**로 unicast; pending 등록 후 전송, 타임아웃 시 drain
- [ ] Discovery 메시지: DISCOVERY_REQUEST / DISCOVERY_RESPONSE (JSON), 호스트 정보(CPU, MEMORY, cpu_uuid) 포함; 응답에는 host_ip 하나만(요청자 기준 outbound IP); 수신 측이 responded_from_ip(UDP 발신지) 설정; 수신 측에서 같은 호스트의 여러 응답으로 IP·응답한 IP 취합
- [ ] Discovery 자기 응답: 기본 **포함**(`"self": true`); 쿼리 **`exclude_self`** 시 CPU UUID(또는 IP+ServicePort 폴백)로 제외
- [ ] Discovery 브로드캐스트: **3.1.1** (type=1, 브리지는 brif 슬레이브 존재, IPv4 brd; 이름 필터 없음); 송신 목록은 brd 문자열 중복 제거; fallback은 discovery_broadcast_address 또는 255.255.255.255; **`contrabass-moleU --nic-brd`**로 확인; 참고 셸 **`brd_for_bm.sh`**
- [ ] Discovery 타임아웃(설정), 중복 제거(host_ip:service_port), 설정 파일 반영
- [ ] Discovery 실시간: GET /api/v1/discovery/stream (SSE), **웹 UI는 이 API만 사용**, EventSource, **event: discoveryfail** 시 서버 메시지 표시·**journalctl** 안내; 응답 오는 대로 화면 갱신; 기존 카드 매칭은 **cpu_uuid → IP** 순서만 사용(**hostname 미사용**, 동일 hostname 다른 호스트 병합 방지), event: done 후 스트림 종료(일괄 API 추가 호출 없음)
- [ ] Discovery 일괄: `GET {APIPrefix}/discovery`, data 배열(빈 경우 []); 쿼리 `exclude_self`·`timeout`; **웹 UI 미호출**
- [ ] Discovery SSE: `GET {APIPrefix}/discovery/stream`, 동일 쿼리 지원; 웹 UI는 쿼리 없이 기본만 사용
- [ ] Gin 프록시(루트 main): **`-cfg <파일>` 서비스 모드에서만** 기동(`ShouldStartGinReverseProxy`); `Server.HTTPPort`, `WebPrefix`·`APIPrefix`로 maintenance에 프록시; 쿼리 유실 방지(`Form` 비우기·`RequestURI` 보조)
- [ ] 웹: `client-runtime.js`로 `APIPrefix` 주입 후 `app.js` API 호출
- [ ] URL prefix: `WebPrefix`·`APIPrefix`, 설정에서 변경 가능
- [ ] 진입 URL: /web/index.html, Discovery 버튼
- [ ] 초기 화면: 내 정보 (버전, IP 또는 host_ips, CPU UUID, 호스트, CPU, MEMORY)
- [ ] 호스트 목록: 아코디언(한 줄 요약 + 클릭 시 상세 카드 펼침), 상태 점(파랑=동작 중/빨강=중지/회색=미확인), 로컬은 맨 위·배경/테두리 색으로 구분, 로컬 IP는 Discovery 후 responded_from_ip 반영
- [ ] 발견된 호스트: 서버 아이콘 + CPU UUID(맨 위), 버전, IP(복수 시 취합 표시), 응답한 IP(복수 시 취합), 호스트명, CPU, MEMORY; 병합 시 기존 카드 매칭은 cpu_uuid·IP만(hostname 미사용)
- [ ] systemctl status: 접기/펼치기(기본 접힘), 접힌 상태에서 [정상 서비스 상태] / [서비스 중지 상태]
- [ ] 레이아웃: 호스트 카드 가운데 열(max-width 610px), 업데이트 영역 오른쪽 sticky; scrollbar-gutter: stable; 카드 내 버튼 오른쪽 위 절대 위치, 서비스 상태 영역은 카드 끝까지 넓게; 내 정보는 카드 한 겹만
- [ ] 내 정보 카드: 시작/중지 버튼 없음; 오른쪽 컬럼(업데이트 기록·config.yaml·설치된 버전)·하단(상태 새로고침·서비스 재시작)
- [ ] 발견된 호스트 카드: **로컬과 동일 레이아웃**(오른쪽 컬럼 + 하단 상태 행). 시작·중지 버튼 비노출; 상태 새로고침·서비스 재시작·업데이트 적용. 카드 열릴 때 업데이트 기록·config·버전 목록 자동 로드
- [ ] 서비스 상태 API: 로컬은 systemctl, 원격은 원격 에이전트 API(`Server.HTTPPort`). 서비스 제어: 로컬은 systemctl; 원격 start/stop은 SSH, **원격 restart는 원격 에이전트 API 호출**(SSH 키 불필요)
- [ ] 원격 API 프록시: update-log·current-cfg(GET/POST)·versions/list·versions/remove 에 `ip` 쿼리 또는 body 지원, 중앙 서버가 원격 에이전트 해당 API 호출 후 응답 전달
- [ ] 서비스 재시작 후: 성공 또는 terminated/연결 끊김 시 친절한 메시지 + 잠시 후 자동 호스트 정보(버전 등) 갱신 + 상태 새로고침(로컬·원격 동일)
- [ ] 설정: DiscoveryServiceName, SystemctlServiceName, DeployBase, **InstallPrefix**(비면 DeployBase, versions·installer용), DiscoveryBroadcastAddress(fallback만), SSHPort(기본 22), SSHUser(기본 root) (선택); **버전 키는 빌드(`main.VersionKey`)·업로드 바이너리 `--version`**
- [ ] **CLI**: **`-cfg <파일>`** 로만 HTTP 서버 + Discovery 기동; 인자 없이 실행 시 안내 후 종료; `-h`/`--help`; `--version`/`-version`; `--nic-brd`; **`--discovery`**(UDP만, `--dest-port`/`--src-port`/`--timeout`)
- [ ] 설치된 버전: GET /api/v1/versions/list(정렬: current → previous → 시맨틱 내림차순), POST /api/v1/versions/remove; current/previous 제외 삭제; 웹 UI 2열 세로 우선, 선택 삭제
- [ ] 업데이트: DeployBase, **staging/**, **versions/(버전 키 디렉터리)**, **내장 update.sh/rollback.sh**(`internal/updatescripts` embed, `Makefile` 동기화); 적용 시 **`current/update.sh`**; transient 유닛 **`contrabass-mole-update`**; **스테이징·비교·적용은 버전 키**; 실행 파일·config 검증; 로컬 적용 후 **페이지 전체 새로고침 없이** `/self` 폴링 → 업데이트 기록·config·versions·상태·update-status 현행화; 원격 적용 후 host-info 폴링(최대 8회) → 동일 패널 현행화; 로그 폴링 2초 간격; **GET /version** 헬스; recent_rollback·update_in_progress
- [ ] 프론트: 업데이트 영역 — 업로드(실행 파일+config, **config 편집 영역에서 수정 후 업로드 가능**), 서버에서 실행 파일·config 검증 실패 시 에러 메시지(항목/줄·필요 타입 안내) 표시; 적용(로컬/원격), 파일 선택 초기화, 업로드된 버전 삭제, **스테이징 버전 표시**, 로그 표시/새로고침; **업데이트 인디케이터**(카드 내, 서버 아이콘 아래)
- [ ] Discovery: 진행 중 기존 목록 유지·제어 가능; 원격 적용 후 Discovery 재수행 없이 카드·로그·config·versions·상태까지 현행화; DISCOVERY_REQUEST JSON **1300바이트 미만** 검증; `service` 필드는 **`DiscoveryServiceName`** 과 일치 시에만 응답
- [ ] 원격 적용: 호스트별 버전 비교(data-host-version), 스테이징 버전과 다를 때만 적용 버튼 활성(초록), 툴팁(스테이징 없음/최신 버전/x.x.x 버전으로 업데이트 가능), 클릭 시 서버가 원격 upload·apply-update API 호출; **적용 성공 시 적용 버전으로 카드 버전 즉시 갱신(낙관적 갱신)**, 지연 후 host-info·service-status로 전체 갱신
- [ ] 호스트 정보 API: GET /api/v1/host-info?ip= (self=로컬, 지정=유니캐스트 Discovery)
- [ ] Discovery 유니캐스트: DoDiscoveryUnicast(ip), 타임아웃 최대 5초
- [ ] 상태 새로고침: 내 정보·원격 동일 방식 — 호스트 정보 API(GET /self 또는 GET /host-info?ip=)로 카드 내용만 갱신 후 GET /service-status로 systemctl 상태 갱신(카드 전체 재렌더링 없음)
- [ ] 일반 API 응답: status + data
- [ ] 자기 정보 API: GET /api/v1/self
- [ ] 설정: YAML, 항목 7.1 반영
- [ ] 버전: **`main.VersionKey`**(Makefile/`scripts/build-version.sh`)로 노출·업데이트 경로와 일치; 업로드는 바이너리 `--version`
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
| Discovery `service` 문자열 | 기본 **`Mole-Discovery`** (`Maintenance.DiscoveryServiceName`, `internal/config.DefaultDiscoveryServiceName`) |
| 설정 파일 지정 | **` -cfg <경로>`** 필수로 HTTP+Discovery 기동. **`MOL_CONFIG` 환경 변수는 사용하지 않음** (`config.Load` 빈 경로 시 현재 디렉터리 `config.yaml`) |
| 업로드 multipart | 실행 파일 필드명 **`agent`**, 파일명은 클라이언트가 보낸 이름(서버는 `appmeta.BinaryName` 권장). `config` 필드는 config.yaml |
| 배포 디렉터리 내 실행 파일 | `staging/`·`versions/<버전 키>/` 아래 파일명은 **`BinaryName`** (과거 단일 바이너리 파일명 규칙은 사용하지 않음). `update.sh` 도 동일 파일명을 기대 |
| `GET /version` | 한 줄: **`<BinaryName> <버전 키>`** |
| 업로드 시 `--version` 검증 | 표준 출력 한 줄이 **`<BinaryName> `** 로 시작해야 함 (`maintenance/server.validateAgentBinary`) |

---

*본 PRD는 Contrabass agent 제품 요구 사항을 통합 기술 사양으로 기술하며, 구현·검증의 기준으로 삼는다.*
