# mol — 제품 요구 사항 문서 (PRD)

## 1. 개요

- **프로젝트명**: mol
- **언어**: Go
- **소스 위치**: `~/work/mol`
- **실행 형태**: 프론트엔드와 백엔드를 포함한 **단일 실행 파일**
- **웹 서버**: Go 표준 라이브러리 **net/http** 만 사용 (외부 웹 프레임워크 미사용)

---

## 2. 아키텍처 요약

- **서비스 포트**: **8888** (HTTP — 웹 UI + API)
- **Discovery 포트**: **9999** (UDP — broadcast 수신·송신 및 응답 수신)
- 동일한 mol 실행 파일이 여러 서버 호스트에 분산 배포되며, **Discovery**를 통해 서로를 찾는다.
- Discovery는 **UDP broadcast** 방식으로 동작한다.

---

## 3. Discovery

### 3.1 흐름

- **요청**: 한 호스트(A)가 **Discovery에 사용할 broadcast 주소**의 **UDP 9999** 번 포트로 Discovery 요청을 보낸다. 브로드캐스트 주소는 **인터페이스 자동 수집**(아래 3.1.1)으로 얻은 IPv4 brd를 사용하며, 수집이 비어 있을 때만 설정 `discovery_broadcast_address`(단일)를 fallback, 그것도 없으면 255.255.255.255를 쓴다. **각 brd 주소마다** 한 번씩 요청을 전송하여 여러 서브넷을 탐색한다.
- **응답**: broadcast를 수신한 각 호스트는 Discovery 응답을 **unicast**로 보낸다. **DISCOVERY_REQUEST** JSON에 **`reply_udp_port`**(요청자가 응답을 받을 UDP 포트)가 있으면 **그 포트**를 우선한다(최신 mol). 없거나 0이면 **UDP 패킷의 소스 포트**, 그것도 0이면 discovery 포트로 보낸다. 이렇게 해서 CLI가 `--src-port`와 `--dest-port`를 다르게 써도, 커널에서 소스 포트가 잘못 보이는 환경에서도 응답이 맞게 간다.
- **요청**은 브로드캐스트 **목적지 포트** `discovery_udp_port`(기본 9999)로 보낸다. **응답**은 요청자의 **소스 포트**로 온다(수신은 그 포트에서 하면 된다).
- **브로드캐스트 송신**: UDP 소켓에 **SO_BROADCAST** 옵션을 설정하여 broadcast 주소로의 전송을 허용한다.

### 3.1.1 Discovery 브로드캐스트 주소 수집 (상세)

Discovery에 쓸 IPv4 브로드캐스트(brd) 주소는 **설정이 아니라** `/sys/class/net/`과 `ip -o -4 addr show`로 수집한다. **물리 NIC**뿐 아니라 **bonding(bond\*), bridge(br\*), vlan(vlan\*)** 등도 포함하여, 해당 인터페이스의 brd로 브로드캐스트가 나가도록 한다.

**1. 대상 인터페이스**

- `/sys/class/net/` 아래 각 인터페이스 이름(디렉터리/심볼릭 링크)을 열거한다.

**2. 제외(이름)**

- 다음에 해당하면 **제외**한다.
  - `lo` (루프백)
  - `docker*`, `veth*`, `virbr*`
  - `br-int`, `br-tun` (및 해당 접두사)
  - `cni*`, `flannel*`, `vxlan_sys*`, `genev_sys*`

**3. operstate(UP만)**

- `/sys/class/net/<iface>/operstate`를 읽어 **값이 `up`인 인터페이스만** 사용한다. `down` 등은 제외한다.

**4. IPv4 + brd 존재**

- 남은 인터페이스마다 `ip -o -4 addr show <iface>`를 실행한다. 출력에 **`brd`**가 포함된 줄만 사용하여 brd 주소를 추출한다. 한 인터페이스에 IPv4가 여러 개면 brd도 여러 개 나올 수 있다.

**5. /virtual/ 인데 허용하는 경우**

- `readlink /sys/class/net/<iface>` 결과가 **`/virtual/`**를 포함하면, 그 인터페이스는 기본적으로 “가상”으로 보아 **제외**한다.
- **단, 이름이** `bond*`, `br*`, `vlan*`, `eth*`, `en*` **중 하나로 시작하면 제외하지 않고 포함**한다.  
  → bonding(bond0 등), bridge(br0 등), vlan(vlan10 등), 물리 NIC에 가까운 이름(eth*, en*)은 `/virtual/` 아래에 있어도 Discovery brd 수집 대상이 된다.

**6. 중복 제거 및 fallback**

- 위 조건을 만족하는 인터페이스에서 추출한 **brd 주소**를 모은 뒤 **중복을 제거**하여 Discovery에 사용한다.
- **수집 결과가 비어 있으면** 설정 `discovery_broadcast_address`(단일)를 사용하고, 그것도 없으면 `255.255.255.255`를 사용한다.

**7. 확인용 CLI**

- `mol --nic-brd` 실행 시 위와 동일한 규칙으로 수집한 **(인터페이스 이름 : brd 주소)** 쌍을 한 줄씩 출력한다. Discovery에 실제로 쓰이는 brd 목록을 확인할 때 사용한다.

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
  "service": "mol",
  "request_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "reply_udp_port": 9998
}
```

- `reply_udp_port`(선택, 0이면 생략 가능): 응답을 보낼 **목적지 UDP 포트**. CLI·최신 mol은 로컬 바인드 포트를 넣는다. 응답자는 이 값이 0보다 크면 **UDP 패킷의 소스 포트보다 우선**한다.

**응답 예시** (호스트 정보 포함)

```json
{
  "type": "DISCOVERY_RESPONSE",
  "service": "mol",
  "host_ip": "172.29.237.41",
  "hostname": "mol-host-41",
  "service_port": 8888,
  "version": "0.2.0",
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

- 위 예시는 **다른 호스트(다른 서브넷)에서 온 Discovery 요청**에 대한 응답을 가정한다. 응답자가 그 요청자로 나갈 때의 outbound IP는 `host_ip`(172.29.237.41)이고, 수신 측에서 본 이 UDP 패킷의 발신지 IP는 `responded_from_ip`(172.29.236.50)로 서로 다를 수 있다(같은 호스트가 여러 NIC로 응답한 경우 등).
- `request_id`: 요청 시 생성한 UUID를 응답에 그대로 넣어 요청·응답 매칭에 사용한다.
- `cpu_uuid`: 호스트 식별용(동일 호스트 병합·self 제거에 사용). 없을 수 있음.
- **응답자는 host_ip 하나만 보낸다.** 같은 호스트가 여러 NIC으로 응답하면 응답마다 다른 host_ip(해당 요청에 대한 outbound IP)가 담긴다. **수신 측**에서 같은 cpu_uuid의 여러 응답을 받아 IP 목록을 취합하여 화면에 표시한다.
- `responded_from_ip`: (수신 측 설정) UDP 패킷의 **발신지 IP**로, 수신 측이 응답을 처리할 때 채운다. 화면에서 "응답한 IP"로 표시하며, 같은 호스트가 여러 IP로 응답한 경우 모두 취합해 보여준다. 전선 상의 메시지에는 없고, API/SSE로 내보낼 때만 포함된다.
- 자기 정보 API(GET /self)에서는 브로드캐스트 대역별 outbound IP를 `host_ips` 배열로 반환할 수 있다. Discovery UDP 응답 메시지 자체에는 host_ips를 넣지 않는다.
- 호스트 정보(CPU, MEMORY)는 위 필드로 확장하며, 단위·필드명은 이 스키마를 기준으로 한다.

### 3.5 중복 제거 및 설정

- **중복 제거**: 스트림/일괄 반환 시 동일한 (host_ip:service_port@responded_from_ip) 조합은 한 번만 전달한다. 즉 같은 호스트가 여러 IP로 응답하면 **응답 건수만큼** 이벤트가 나가며, 각 이벤트의 host_ip·responded_from_ip가 다를 수 있다. 설정 `discovery_deduplicate`로 켜/끌 수 있다.
- **동일 호스트 병합(프론트)**: `cpu_uuid`가 같은 응답은 **한 호스트**로 간주한다. 카드는 하나만 두고, **IP**는 각 응답의 host_ip를 모두 취합해 표시하고, **응답한 IP**는 각 응답의 responded_from_ip를 모두 취합해 표시한다. CPU·메모리는 응답 중 하나만 사용한다. **기존 카드 찾기**는 **cpu_uuid** → **IP**(host_ip / data-host-ips) 순으로만 하며, **hostname으로는 찾지 않는다**. 서로 다른 물리 호스트가 같은 hostname(예: kt-vm)을 쓰면 hostname으로 찾을 경우 한 카드로 잘못 병합되므로 hostname 매칭을 사용하지 않는다.
- **타임아웃**: 응답 수집 대기 시간은 설정 `discovery_timeout_seconds`(기본 10초)로 지정한다.

### 3.6 실시간 Discovery (SSE)

- Discovery 결과를 **타임아웃 만료를 기다리지 않고** 응답이 도착하는 대로 화면에 반영한다.
- **백엔드**: `GET /api/v1/discovery/stream` 엔드포인트를 두고, **Server-Sent Events(SSE)** 로 스트리밍한다. Discovery 요청을 보낸 뒤, 각 DISCOVERY_RESPONSE가 올 때마다 `data: {JSON}\n\n` 형식으로 한 건씩 전송하고 즉시 flush한다. 타임아웃이 되면 `event: done\ndata: {}\n\n` 를 보내고 스트림을 종료한다. 내부적으로는 **DoDiscoveryStream** 과 같이 요청 시 pending 등록 → 브로드캐스트 전송 → 수신 채널에서 응답을 하나씩 읽어 필터(self 제거·중복 제거) 후 SSE로 내보내는 방식을 사용한다.
- **프론트엔드**: Discovery 버튼 클릭 시 **EventSource** 로 `/api/v1/discovery/stream` 에 연결한다. 기본 메시지 이벤트가 올 때마다 수신한 JSON을 파싱해, **같은 CPU UUID**가 이미 있으면 해당 카드에 IP·응답한 IP를 병합·갱신하고, 없으면 **같은 IP**가 있는 카드를 찾아 갱신하고, 그 외에는 **새 카드**를 추가한다. 기존 카드 매칭은 cpu_uuid → IP 순서만 사용하며 hostname은 사용하지 않는다. `event: done` 수신 시 스트림을 닫고 버튼을 복구한다. 호스트 카드 상세에서는 **CPU UUID**를 맨 위에, **IP**·**응답한 IP** 순으로 표시한다.

### 3.7 유니캐스트 Discovery (단일 호스트 조회)

- **목적**: 특정 IP의 호스트 정보(버전, CPU, 메모리 등)만 갱신할 때 사용한다.
- **동작**: 해당 IP의 Discovery UDP 포트(9999)로 **DISCOVERY_REQUEST를 유니캐스트**로 보낸다. 해당 호스트만 응답하므로 **한 건의 DISCOVERY_RESPONSE**를 수신한다.
- **타임아웃**: 응답 대기 시간은 Discovery 타임아웃 설정을 따르되, **최대 5초**로 제한한다.
- **매칭**: 수신한 응답의 `host_ip`가 요청한 IP와 일치하는지 확인한 뒤 반환한다.

### 3.8 로깅 (구현 참고)

- 디버깅·운영 시 다음을 로그로 남길 수 있다: DISCOVERY_REQUEST 수신(발신지 주소), DISCOVERY_RESPONSE 전송(대상 주소), DISCOVERY_RESPONSE 수신(발신지, request_id, delivered / no pending waiter / channel full).

---

## 4. URL 및 라우팅

- **프론트엔드 prefix**: `{serverUrl}/web` (기본값, 설정에서 변경 가능)
- **백엔드 API prefix**: `{serverUrl}/api/v1` (기본값, 설정에서 변경 가능)
- **프론트엔드 진입 URL**: `{serverUrl}/web/index.html`
- prefix는 설정 파일에서 수정할 수 있어야 한다.

### 4.1 CLI (명령줄)

- **인자 없이 실행**: `mol` — 버전과 `-config <파일>` 필요 안내를 출력하고 종료한다. HTTP 서비스는 **`-config`로 설정 파일을 지정했을 때만** 기동한다.
- **`-config <파일>`**: 설정 파일 경로(필수 인자). 이 옵션으로만 HTTP·Discovery가 기동한다. systemd 등에서는 `ExecStart=.../mol -config /path/to/config.yaml` 형태로 지정한다.
- **`-h`, `--help`**: 도움말(사용법·옵션 설명) 출력 후 종료.
- **`-version`, `--version`**: 버전 문자열 출력 후 종료.
- **`--nic-brd`**: 물리 NIC별 IPv4 브로드캐스트(brd) 주소를 `NIC이름 : brd주소` 형식으로 출력(Discovery에 사용되는 주소 확인용) 후 종료.
- **`--discovery`**: 설정 파일·HTTP 서버 없이 **UDP Discovery만** 수행. `--dest-port`(기본 9999), `--src-port`(기본 9998), `--timeout`(초, 기본 10), `--service`(기본 `mol`). 시작 시 **사용 가능한 brd(브로드캐스트) 주소를 모두 한 줄씩 출력**한다. 서비스 mol과 같이 **서브넷별로 로컬 IP:src-port 소켓을 열어** 각 brd로 송신한다(다중 NIC·src≠dest 안정화). `reply_udp_port` 포함 `DISCOVERY_REQUEST` 전송 후, 같은 줄에서 `Discovering ... N` 카운트다운 → **`Discovery Done.`** → 수신 유예·드레인. 결과는 호스트별 **`[Local]`** / **`[Remote]`** `hostname - 대표 IP : [응답한 IP만]` 형식으로, **`responded_from_ip`**만 취합한다. Local/Remote는 **CPU UUID 일치(대소문자 무시)** 우선, 아니면 **응답한 IP가 로컬 IPv4와 겹치는지**로 보조 판별한다.

---

## 5. API

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

- Discovery 요청은 **프론트엔드의 Discovery 버튼**에 의해 트리거되며, **웹 UI는 스트리밍 API만 사용**한다.
- **실시간 스트리밍 (웹 UI 사용)**: `GET {serverUrl}/api/v1/discovery/stream` — **Server-Sent Events(SSE)**. Content-Type `text/event-stream`. 응답이 올 때마다 `data: {JSON}\n\n` 로 호스트 한 건씩 전송, 타임아웃(설정값) 시 `event: done\ndata: {}\n\n` 후 스트림 종료. mol 웹 UI는 Discovery 버튼 클릭 시 EventSource로 이 엔드포인트만 호출하며, 응답이 오는 대로 화면에 반영하고 `event: done` 수신 시 스트림을 닫고 버튼을 복구한다. 타임아웃 이후 별도의 일괄 API 호출은 하지 않는다.
- **일괄 반환 (웹 UI 미사용)**: `GET {serverUrl}/api/v1/discovery` — 타임아웃(설정값)까지 수집한 뒤 `status` + `data`(발견된 호스트 배열)를 한 번에 JSON으로 반환. `data`는 배열이며, 결과가 없어도 `[]` 로 반환한다(null 아님). 서버에는 구현되어 있으나 **mol 웹 UI에서는 호출하지 않으며**, 스크립트·다른 클라이언트용으로만 사용할 수 있다.

### 5.4 서비스 상태·제어 API

- **서비스 상태**: `GET {serverUrl}/api/v1/service-status?ip=`  
  - `ip` 비어 있거나 `"self"`: 로컬에서 `systemctl status <systemctl_service_name>` 실행( **sudo 없음**, mol.service는 root로 실행), 결과를 `{ "status": "success", "data": { "output": "..." } }` 로 반환.
  - `ip` 지정: 요청을 받은 서버가 **원격 mol의 서비스 포트(8888)** 로 `GET .../service-status` 를 호출한다. 원격 mol은 자기 서버에서 `systemctl status` 를 실행한 뒤 그 결과를 응답으로 반환하고, 요청자는 그 응답을 그대로 클라이언트에 전달한다.
  - 실패 시 `{ "status": "fail", "data": "에러 메시지" }`.
- **서비스 제어**: `POST {serverUrl}/api/v1/service-control`  
  - Body: `{ "ip": "" | "self" | "<host_ip>", "action": "start" | "stop" | "restart" }`.  
  - `ip` 비어 있거나 `"self"`: 로컬 `systemctl start/stop/restart <systemctl_service_name>` (mol.service는 root로 실행).  
  - **원격 start/stop**: 요청을 받은 서버가 대상 호스트로 **SSH** 접속(`ssh_port`·`ssh_user` 설정 사용, 미지정 시 22·root)하여 `systemctl start` 또는 `stop <서비스명>`을 실행한다. 원격 mol이 중지된 상태여도 SSH로 시작할 수 있다.  
  - **원격 restart**: SSH를 사용하지 않고, 요청을 받은 서버가 **원격 mol의 서비스 포트(8888)** 로 `POST .../service-control` (Body: `{ "ip": "self", "action": "restart" }`)를 호출한다. 원격 mol은 자기 서버에서 `systemctl restart` 를 실행한 뒤 응답을 반환한다. SSH 공개키 등록 없이 재시작 가능하다.  
  - 성공 시 `{ "status": "success", "data": null }`, 실패 시 `{ "status": "fail", "data": "에러 메시지" }`.

### 5.5 업데이트 API

- **배포 베이스**: 설정 `deploy_base`(기본값 `/opt/mol`) 아래에 **스테이징** `staging/<버전>/`, **실행 경로** `versions/<버전>/`, **update.sh**, **rollback.sh** 가 있다고 가정한다. 디렉터리 구조 예시는 다음과 같다.

  ```
  deploy_base/                    # 예: /opt/mol
  ├── current -> versions/1.2.6   # 심볼릭 링크, 현재 실행 버전
  ├── previous -> versions/1.2.5  # 심볼릭 링크, 이전 버전(롤백용)
  ├── update.sh
  ├── rollback.sh
  ├── update_history.log          # 업데이트·롤백 기록 (맨 앞에 추가, 최근 5건을 웹에 표시)
  ├── staging/                    # 업로드 API로 저장, 적용 시 versions로 복사
  │   └── <버전>/
  │       ├── mol
  │       └── config.yaml
  └── versions/                   # 실제 실행 경로, current/previous가 가리킴
      └── <버전>/
          ├── mol
          └── config.yaml
  ```

- **스테이징**: 업로드는 **실행 경로(versions/)가 아닌** `{deploy_base}/staging/<버전>/` 에만 저장된다. 이렇게 해서 실행 중인 바이너리 경로를 덮어쓰지 않아 "text file busy" 를 피한다. 적용 시에는 스테이징을 우선 사용하고, 없으면 versions/ 를 사용한다.
- **스테이징 정리**: 스테이징은 자동 삭제하지 않는다. 로컬 적용 후에도 스테이징을 남겨 두어 같은 버전으로 원격 업데이트를 할 수 있게 한다. 삭제는 사용자가 웹의 「업로드된 버전 삭제」를 눌러 수동으로만 수행하며, 이때 스테이징에서만 해당 버전을 삭제하고 versions/ 는 건드리지 않는다.
- **스크립트 위치**: 소스 저장소 프로젝트 루트에 `update.sh`, `rollback.sh` 가 참고용으로 포함되어 있다. 실제 사용 시에는 이 두 파일을 **배포 베이스 직하**에 복사해 둔다. 즉 `{deploy_base}/update.sh`, `{deploy_base}/rollback.sh`.
- **update.sh / rollback.sh 구현 상세**
  - **BASE**: 스크립트가 있는 디렉터리를 배포 루트로 사용한다. `BASE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"` 로 설정하여, 서버가 `{deploy_base}/update.sh`를 실행할 때 로그 경로가 서버의 deploy_base와 자동으로 일치한다.
  - **update_history.log**: `HISTORY_LOG="$BASE/update_history.log"`. prepend_history()로 새 줄을 **맨 앞**에 추가하여 최근 기록이 최상단에 오도록 한다.
  - **PATH**: systemd transient 유닛은 PATH가 비어 있을 수 있어, `export PATH="/usr/bin:/bin:/usr/local/bin:${PATH:-}"` 를 스크립트 상단에 두었다. 환경에 따라 필수는 아니나, 미니멀 환경에서 grep/sed를 찾지 못해 스크립트가 죽는 것을 방지하는 방어용이다.
  - **config 읽기**: 적용할 버전의 `config.yaml`에서 `http_port`, `systemctl_service_name`을 읽는다. 실패해도 기본값을 유지하도록 `|| true`로 감싼다.
  - **헬스 체크**: 서비스 시작 후 `curl` 로 `http://127.0.0.1:${HTTP_PORT}/version` 에 요청하여 200이면 성공, 그렇지 않으면 롤백한다. 루트 `/`는 브라우저일 때만 /web 리다이렉트하고 그 외에는 404이므로 `/version` 전용 엔드포인트를 사용한다.
- **update.sh**: 인자로 버전 하나를 받는다. `{BASE}/versions/{버전}/mol` 이 존재·실행 가능한지 확인한 뒤, 서비스 중지 → `current`/`previous` 심볼릭 링크 갱신 → 서비스 시작 → **HTTP 헬스 체크(/version)** 를 수행한다. 시작 실패 또는 헬스 체크 실패 시 `{BASE}/rollback.sh` 를 호출해 이전 버전으로 되돌린다.
- **rollback.sh**: 인자는 없다. `{BASE}/previous` 심볼릭 링크가 있어야 하며, 서비스 중지 → `current` 를 `previous` 가 가리키는 버전으로 교체 → 서비스 시작을 수행한다. 웹 API에서는 호출하지 않고, update.sh 의 실패 복구 또는 운영자가 수동 실행할 때 사용한다.
- **업로드**: `POST {serverUrl}/api/v1/upload`  
  - **multipart/form-data**: `mol`(실행 파일), `config`(config.yaml). 버전은 config에서 파싱.  
  - **mol 실행 파일 검증**: 업로드된 mol 파트에 대해 (1) **ELF 매직**: 앞 4바이트가 `\x7fELF`인지 확인(텍스트·설정·압축 파일 등 잘못된 파일 차단). (2) **실행 검증**: 스테이징에 저장한 뒤 해당 경로의 바이너리를 `--version` 인자로 실행(타임아웃 5초)하여, 출력이 `"mol "`로 시작하고 종료 코드 0인지 확인. 검증 실패 시 스테이징에 넣지 않거나 저장 후 삭제하고 `status: "fail"`, `data`에 에러 메시지를 반환(400).  
  - **config.yaml 검증**: 업로드된 config 본문을 mol **config 구조체**로 파싱한다. 파싱 실패 시(구문 오류·타입 오류) **어떤 항목/몇 번째 줄**에서 오류가 났는지, **어떤 항목이 있어야 하고 타입은 무엇인지**를 안내하는 에러 메시지를 반환한다(예: "N번째 줄: 숫자 항목에 문자열이 들어갔습니다", "필요한 항목 및 타입: service_name(문자열), discovery_udp_port(숫자), http_port(숫자), version(문자열) 등").  
  - **config 수정 업로드**: 웹 UI에서는 config.yaml 파일을 선택하면 내용이 편집 영역에 표시되고, 사용자가 버전 등 내용을 **수정한 뒤** 업로드할 수 있다. 이때 전송·스테이징에 저장되는 것은 **수정된 내용**이며, 원본 파일을 그대로 보내지 않아도 된다. API는 클라이언트가 보낸 config 본문을 그대로 받아 저장한다.  
  - 버전은 영문·숫자·`.`·`-`만 허용.  
  - 요청을 받은 mol 인스턴스는 **자기 자신의 스테이징** `{deploy_base}/staging/{version}/` 에만 저장한다(mol, config.yaml). 로컬 웹에서 업로드하면 로컬 mol의 스테이징에 저장되고, 원격 배포 시에는 **원격 mol**에 같은 upload API를 호출하여 그 mol의 스테이징에 저장한다.  
  - 성공 시 `{ "status": "success", "data": { "version": "..." } }`, 실패 시 `{ "status": "fail", "data": "에러 메시지" }`.
- **업로드 삭제**: `POST {serverUrl}/api/v1/upload/remove`  
  - Body: `{ "version": "<버전>" }`.  
  - **스테이징** `{deploy_base}/staging/{version}/` 만 삭제한다. versions/ 에 있는 동일 버전은 삭제하지 않는다.
- **적용 (로컬)**: `POST {serverUrl}/api/v1/apply-update` Body: `{ "version": "<버전>" }` (ip 없음 또는 `"self"`).  
  - 버전 소스: **스테이징** 우선, 없으면 **versions/** 에서 확인. 둘 다 없으면 실패.  
  - **스테이징에만 있는 경우**: 스테이징 → versions 복사 후 `update.sh` 실행. 스테이징은 삭제하지 않고 남겨 두어 원격 업데이트에 재사용 가능.  
  - **versions에 이미 있는 경우**: 그대로 `update.sh` 실행.  
  - 실행 전 **mol-update 유닛 정리**: `systemctl reset-failed mol-update.service`, `systemctl stop mol-update.service` (실패해도 무시).  
  - **`systemd-run`** 로 실행: `systemd-run --unit=mol-update --property=RemainAfterExit=yes /bin/bash {deploy_base}/update.sh {version}`. 스크립트는 **bash로 명시 실행**하여 transient 유닛 환경에서도 동작하도록 한다. 실행 출력은 남기지 않으며, 상세 기록은 update.sh·rollback.sh가 `{deploy_base}/update_history.log`에 맨 앞줄로 추가한다. mol.service는 root로 실행되므로 sudo를 사용하지 않는다. 응답은 즉시 반환, 실제 업데이트는 백그라운드.
- **적용 (원격)**: 원격 mol로의 배포는 **원격 mol의 업로드 API**를 사용한다. 요청을 받은 서버(로컬 mol)는 대상 원격 mol의 **서비스 포트(8888)** 로 HTTP로 (1) `POST /api/v1/upload` (multipart: `mol`, `config`)를 보내 해당 mol의 **스테이징**에 올린 뒤, (2) `POST /api/v1/apply-update` (Body: `{ "version": "<버전>", "ip": "self" }`)를 보내 그 mol이 자기 스테이징을 적용·재시작하도록 한다.  
  - **JSON** Body: `{ "version": "<버전>", "ip": "<원격 IP>" }`. 로컬의 스테이징 또는 versions에서 해당 버전의 mol·config를 읽어, 위와 같이 원격의 upload API로 전송한 후 원격의 apply-update API를 호출한다.  
  - **multipart** (원격 전용): `ip`, `mol`, `config`. 수신한 mol에 대해 로컬에서 동일한 검증(ELF + `--version` 실행)을 수행한 뒤, 통과 시에만 원격 mol의 upload API로 전송하고 apply-update API를 호출한다. 검증 실패 시 400 및 에러 메시지.
- **업데이트 기록**: `GET {serverUrl}/api/v1/update-log?ip=`  
  - `ip` 비어 있거나 `"self"`: `{deploy_base}/update_history.log` 파일의 **최근 5줄**을 읽어 `{ "status": "success", "data": { "output": "<5줄 텍스트>", "recent_rollback": true|false } }` 로 반환. `recent_rollback`은 최상단 줄에 "rollback" 또는 "failed"가 있으면 true. **단, mol-update.service가 active(업데이트 진행 중)이면** `recent_rollback`을 false로 반환한다.  
  - `ip` 지정: 요청을 받은 서버가 **원격 mol의 서비스 포트** 로 `GET .../update-log` 를 호출한 뒤 응답을 그대로 클라이언트에 전달한다.  
  - 파일이 없거나 읽기 실패 시 `{ "status": "fail", "data": "에러 메시지" }`.
- **config.yaml (current)**: `GET {serverUrl}/api/v1/current-config?ip=` / `POST {serverUrl}/api/v1/current-config` (Body: `{ "content": "<yaml 본문>", "ip": "" | "self" | "<host_ip>" }`).  
  - `ip` 비어 있거나 `"self"`: 로컬 current 버전의 config.yaml 읽기·저장. GET은 `{ "status": "success", "data": { "content": "..." } }`, POST는 저장 후 success/fail.  
  - `ip` 지정: 요청을 받은 서버가 **원격 mol의 서비스 포트** 로 GET 또는 POST current-config를 호출한 뒤 응답을 그대로 클라이언트에 전달한다.
- **업데이트 상태**: `GET {serverUrl}/api/v1/update-status` 응답에 `update_in_progress`(boolean)를 포함. `systemctl is-active mol-update.service`가 active이면 true.
- **헬스 체크용 버전 엔드포인트**: `GET {serverUrl}/version` — **text/plain**으로 `mol <version>` 한 줄 반환(예: `mol 0.3.4`). 브라우저/curl 구분 없이 항상 200. update.sh의 HTTP 헬스 체크는 이 URL로 요청한다(루트 `/`는 브라우저일 때만 /web 리다이렉트, 그 외 404이므로 헬스 체크에 사용하지 않음).

### 5.6 설치된 버전(versions) API

- **경로 기준**: `install_prefix`(설정, 비면 `deploy_base`) 아래 `versions/` 디렉터리 및 `current`·`previous` 심볼릭 링크를 사용한다. installer 등에서도 동일 경로를 참조할 수 있도록 `install_prefix`를 둔다.
- **목록**: `GET {serverUrl}/api/v1/versions/list?ip=`  
  - `ip` 비어 있거나 `"self"`: `{install_prefix}/versions/` 디렉터리 내 각 버전 디렉터리(그 안에 `mol` 실행 파일이 있는 것만)를 나열하고, `current`·`previous` 심볼릭 링크가 가리키는 버전을 판별하여 `is_current`·`is_previous` 플래그와 함께 반환한다. 응답: `{ "status": "success", "data": { "versions": [ { "version", "is_current", "is_previous" }, ... ] } }`.  
  - **정렬 순서(표시용)**: **current** 대상 버전을 맨 위 → **previous** 대상 → 그 외는 시맨틱 버전 숫자 **내림차순**(높은 버전이 위). 웹 UI에서 현재·이전 버전을 스크롤 없이 상단에서 확인할 수 있다.  
  - `ip` 지정: 요청을 받은 서버가 **원격 mol의 서비스 포트** 로 `GET .../versions/list` 를 호출한 뒤 응답을 그대로 클라이언트에 전달한다.
- **삭제**: `POST {serverUrl}/api/v1/versions/remove`  
  - Body: `{ "versions": [ "<버전>", ... ], "ip": "" | "self" | "<host_ip>" }`. `ip`가 비어 있거나 `"self"`이면 로컬에서 삭제. `ip` 지정 시 요청을 받은 서버가 **원격 mol의 서비스 포트** 로 `POST .../versions/remove` (Body: `{ "versions": [...] }`)를 호출한 뒤 응답을 그대로 클라이언트에 전달한다. 로컬/원격 공통: `current`·`previous`가 가리키는 버전은 삭제하지 않고 제외 사유와 함께 응답에 포함한다.

---

## 6. 프론트엔드

- **구현 방식**: 정적 파일(HTML, CSS, JavaScript)을 **Go embed**로 단일 실행 파일에 포함.
- **JavaScript**: **Vanilla JS**만 사용. API 호출은 `fetch`, UI 업데이트는 DOM 조작으로 처리. SPA 프레임워크(React, Vue 등)는 사용하지 않는다.
- **레이아웃**
  - 호스트 정보(내 정보·발견된 호스트) 카드는 **가운데 열**에 배치하고, **업데이트** 영역은 **화면 오른쪽**에 고정(sticky)하여 스크롤 시 카드만 스크롤되고 업데이트 영역은 고정된다. 스크롤바가 생겨도 레이아웃이 밀리지 않도록 `scrollbar-gutter: stable`을 사용한다.
  - 호스트 카드의 가로 최대 너비는 610px로 통일하며, 내 정보와 발견된 호스트 카드는 동일한 카드 스타일 한 겹만 사용한다(내 정보 컨테이너는 카드 클래스를 갖지 않고, 렌더된 카드 한 개만 자식으로 둔다).
  - 카드 내 **시작/중지·업데이트 적용·상태 새로고침** 버튼은 카드 **오른쪽 위**에 절대 위치로 배치한다. 상단의 호스트 정보 항목(CPU UUID, 버전, IP 등)만 버튼과 겹치지 않도록 오른쪽 여백을 두고, **서비스 상태(터미널)** 영역은 카드 오른쪽 끝까지 넓게 표시한다.
- **초기 화면**
  - **내 정보**: 현재 mol 인스턴스의 버전, **IP(또는 응답으로 사용하는 모든 IP `host_ips`)** , 호스트명, CPU UUID, CPU, MEMORY 등을 표시 (자기 정보 API 사용). 자기 정보 API는 각 브로드캐스트 주소별 outbound IP를 `host_ips`로 반환하여 Discovery 응답으로 사용하는 IP들을 모두 보여준다.
- **Discovery 버튼**
  - 클릭 시 **EventSource** 로 `GET /api/v1/discovery/stream` 에 연결하여 **실시간 Discovery**를 수행한다. **기존 발견된 호스트 목록은 비우지 않고** 유지하며, 진행 중에도 해당 카드들의 제어(시작/중지·업데이트 적용·상태 새로고침)가 가능하다. SSE로 호스트가 도착할 때 **같은 CPU UUID**가 있으면 해당 카드에 IP만 병합·갱신하고, 없으면 같은 IP 카드 갱신 또는 새 카드 추가한다. `event: done` 수신 시 스트림을 닫고 버튼을 복구한다.
- **호스트 목록 구조 (아코디언·상태 점)**
  - 호스트(로컬·발견된 원격)는 **세로 목록**으로 표시한다. 기본은 **한 줄 요약**만 보이고, 해당 행을 클릭하면 그 호스트의 **상세 카드**가 펼쳐진다(아코디언). 여러 호스트를 동시에 펼쳐 둘 수 있다.
  - **한 줄 요약**: **상태 점**(동작 중 = 파란색, 중지 = 빨간색, 미확인 = 회색) + **구분자**. 로컬 구분자: hostname 또는 "로컬" + " · " + IP. 원격 구분자: hostname + " · " + IP(또는 CPU UUID 앞 8자).
  - **로컬 호스트**: **맨 위**(내 정보 섹션)에 한 줄로 표시하며, 배경·테두리 색을 달리(예: 파란 톤)하여 원격과 구분한다.
  - **로컬의 IP 표시**: 초기에는 자기 정보 API의 IP(또는 host_ips)를 사용하고, **Discovery 수행 후**에는 응답으로 받은 **responded_from_ip**를 반영하여 한 줄 요약의 IP를 갱신한다.
- **발견된 호스트 표시**
  - 각 호스트를 **서버 모양 아이콘**과 함께 **상세 카드**로 표시한다(아코디언에서 해당 행을 펼쳤을 때).
  - 표시 내용: **CPU UUID**(맨 위), mol 버전, **IP**(여러 개면 쉼표 구분, 같은 호스트의 여러 응답에서 host_ip를 취합), **응답한 IP**(실제로 Discovery 응답을 보낸 UDP 발신지 IP, 여러 개면 취합), 호스트명, 서비스 포트, CPU, MEMORY. 동일 CPU UUID의 여러 응답은 **한 카드**로 병합하며, IP와 응답한 IP는 모두 취합해 표시하고 CPU·메모리는 하나만 표시한다.
  - 내 정보와 동일한 형태(카드/테이블 등)로 보여주어 일관된 UX를 유지한다.
- **원격 적용 후**: 원격 mol 업데이트가 성공하면 **Discovery를 다시 수행하지 않고**, 해당 호스트 카드만 갱신한다.  
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
  - **원격 start/stop**: 요청을 받은 서버가 해당 원격 호스트로 **SSH** 접속하여 `systemctl start|stop` 실행. 설정 `ssh_port`(기본 22), `ssh_user`(기본 "root") 사용.  
  - **원격 restart**: SSH 없이 요청을 받은 서버가 **원격 mol API**로 `POST .../service-control` (Body `{ "ip": "self", "action": "restart" }`)를 호출. 원격 mol이 자기 서버에서 `systemctl restart` 실행.
- **서비스 재시작 후 UI**: 재시작 요청 성공 시 또는 연결 끊김/terminated 등 재시작 진행 중으로 보이는 오류 시, 요약에 「재시작되었습니다. 잠시 후 상태를 불러옵니다.」 등 친절한 메시지를 표시하고, **몇 초 후 자동으로** (1) `GET /api/v1/self`(로컬) 또는 `GET /api/v1/host-info?ip=...`(원격)로 호스트 정보를 가져와 카드의 **버전·호스트명·IP 등**을 갱신하고, (2) `GET /api/v1/service-status`로 요약을 [정상 서비스 상태] 등으로 갱신한다. config.yaml의 version을 수정한 뒤 재시작한 경우에도 카드에 새 버전이 반영된다. 로컬·원격 동일. 사용자가 「상태 새로고침」을 누르지 않아도 된다.
- (참고) **서비스 상태** 조회(GET /api/v1/service-status?ip=)는 로컬은 직접 systemctl, 원격은 원격 mol API를 호출하는 방식으로 유지한다.

### 6.3 업데이트 (업로드·적용·로그)

- **업로드**: mol 실행 파일과 config.yaml을 선택한 뒤 `POST /api/v1/upload` (multipart: `mol`, `config`). 버전은 config에서 파싱. 업로드는 **스테이징**에 저장. 성공/실패 메시지 표시. 서버에서 **mol 실행 파일 검증**(ELF 형식 + `--version` 실행)과 **config.yaml 검증**(mol config 구조체로 파싱, 실패 시 항목/줄/필요 타입 안내)을 수행하며, 잘못된 파일·설정이면 거절하고 에러 메시지를 반환한다.  
  - **config.yaml 수정 후 업로드**: config 파일을 선택하면 내용이 **편집 영역**(textarea)에 표시된다. 사용자가 버전, broadcast 주소 등 설정을 수정한 뒤 업로드하면 **수정된 내용**이 서버로 전송되어 스테이징에 저장된다. 원본 파일을 수정 없이 올릴 수도 있고, 편집만 하고 파일을 다시 선택하지 않아도 업로드 시 편집 영역 내용이 사용된다.
- **적용 (로컬)**: 버전이 스테이징 또는 이전 적용으로 존재할 때, 적용 버튼으로 `POST /api/v1/apply-update` (`{ "version": "..." }`). 성공 시 mol 재시작으로 연결이 끊길 수 있으므로 **전체 페이지 새로고침은 하지 않는다**. 약 4초 후부터 `GET /api/v1/self`를 **2초 간격 최대 15회** 폴링하여 서버가 다시 뜨면 **업데이트 기록·config.yaml·설치된 버전·서비스 상태·update-status**를 모두 다시 불러와 현행화한다. 대기 중 업데이트 로그는 **2초 간격**으로 조용히 갱신한다. 폴링 실패 시 연결 오류 vs 응답 지연 메시지를 구분해 안내한다. 실패 시 에러 메시지.
- **적용 (원격)**  
  - **버튼 활성화**: 각 발견된 호스트 카드의 「업데이트 적용」은 **호스트별**로 활성/비활성을 판단한다. 스테이징(또는 세션 내 업로드된 버전)에 버전이 있고, 그 버전이 **해당 호스트의 현재 버전과 다를 때**만 해당 호스트의 「업데이트 적용」이 활성화된다. 카드에는 해당 호스트의 버전을 `data-host-version`으로 저장하여 비교에 사용한다.  
  - **버튼 스타일**: 활성화 시 **초록색** 계열(로컬 적용 버튼과 동일)로 표시하여 적용 가능 상태를 직관적으로 구분한다.  
  - **클릭 동작**: 적용할 버전은 **스테이징에 올라간 버전**(또는 세션 내 업로드 버전)을 사용한다. 파일 선택이 없어도 스테이징에 버전이 있으면 JSON `{ version, ip }` 로 로컬 서버에 보내며, 서버는 원격 mol의 upload API·apply-update API를 호출하여 배포한다. mol·config만 선택된 경우에는 multipart `ip`, `mol`, `config` 로 전송하면 서버가 원격 mol의 upload API로 전달한 뒤 apply-update를 호출한다.  
  - **적용 성공 후 카드 버전 표시**: JSON 적용 시에는 요청에 넣은 `version`을, multipart 적용 시에는 서버 성공 메시지(예: "원격 ... 에 버전 X 적용 완료")에서 파싱한 버전을 사용하여, **host-info 응답을 기다리지 않고** 해당 호스트 카드의 버전 표시를 즉시 갱신한다. 이후 지연 후 host-info가 성공하면 전체 호스트 정보로 한 번 더 갱신된다.  
  - **툴팁**:  
    - 비활성·스테이징에 파일 없음: "먼저 업데이트 영역에서 버전을 업로드하세요"  
    - 비활성·스테이징 버전과 현재 버전 동일: "최신 버전입니다"  
    - 활성: "x.x.x 버전으로 업데이트 가능합니다" (x.x.x는 스테이징 버전)
- **스테이징 버전 표시**: 「업로드된 버전 삭제」 버튼 옆에 현재 스테이징에 올라간 버전(예: "스테이징: 1.2.3")을 표시한다. 스테이징이 비어 있으면 표시하지 않는다.
- **업데이트 인디케이터**: 로컬·원격 카드 모두, 업데이트 적용이 진행 중일 때 카드 내 **서버 아이콘 아래**에 회전하는 로딩 인디케이터를 표시한다. **로컬**은 `/self` 폴링 성공(또는 폴링 종료) 후 숨긴다. **원격**은 host-info 폴링·패널 갱신 완료 후 숨긴다. 요청 실패 시 즉시 숨긴다.
- **파일 선택 초기화**: mol·config 선택 및 편집 내용만 초기화. 스테이징/versions 에 올라간 버전은 유지.
- **업로드된 버전 삭제**: 스테이징에서 해당 버전만 삭제.
- **업데이트 기록(로그)**: `GET /api/v1/update-log` 로 최근 5건을 표시. **로컬 적용 진행 중**에는 **2초 간격**으로 조용히 폴링한다(완료 후 위 “적용 (로컬)” 흐름에서 전체 패널 갱신과 함께 최종 반영). **업데이트 진행 중**(mol-update.service active)에는 서버가 `recent_rollback`을 false로 반환하므로 롤백 경고를 숨긴다.
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
- **위치**: 구현 시 결정. 실행 시 **`mol -config <경로>`** 로 지정한다(인자 없이 기본 `config.yaml` 자동 로드는 하지 않음). 로컬 개발에서 `MOL_CONFIG` 환경변수를 쓰려면 `mol -config "$MOL_CONFIG"` 처럼 명시적으로 넘긴다.

### 7.1 설정 항목 (최소)

| 항목 | 설명 | 예시 |
|------|------|------|
| `service_name` | Discovery 메시지의 `service` 값 | `"mol"` |
| `discovery_broadcast_address` | (선택) **Fallback**: 3.1.1 자동 수집이 비어 있을 때만 사용하는 단일 broadcast IP | `"192.168.0.255"` |
| ~~`discovery_broadcast_addresses`~~ | **사용 안 함**. Discovery brd는 3.1.1 자동 수집(bonding·bridge·vlan 포함). |
| `discovery_udp_port` | Discovery용 UDP 포트 | `9999` |
| `http_port` | HTTP 서비스 포트 | `8888` |
| `web_prefix` | 프론트엔드 URL prefix | `"/web"` |
| `api_prefix` | 백엔드 API URL prefix | `"/api/v1"` |
| `discovery_timeout_seconds` | Discovery 응답 대기 시간(초) | `10` |
| `discovery_deduplicate` | 동일 호스트 중복 제거 여부 | `true` |
| `version` | (선택) 버전 override. 비어 있으면 빌드 시 ldflags 값 사용 | `"1.2.3"` 또는 빈 문자열 |
| `systemctl_service_name` | (선택) 서비스 상태·제어 대상 유닛 이름 | `"mol.service"` |
| `deploy_base` | (선택) 업데이트 배포 베이스. `staging/`, `update.sh`, `rollback.sh`, `update_history.log` 의 기준 경로 | `"/opt/mol"` |
| `install_prefix` | (선택) mol 설치 경로 prefix. `versions/` 목록·삭제 API 및 installer에서 사용. 비면 `deploy_base` 사용 | `"/opt/mol"` |
| `ssh_port` | (선택) 원격 서비스 시작/중지 시 SSH 포트. 미지정 또는 0이면 22 사용 | `22` |
| `ssh_user` | (선택) 원격 서비스 시작/중지 시 SSH 사용자. 미지정이면 `"root"` | `"root"` |

- **Discovery 브로드캐스트 주소**: **3.1.1**에 따라 인터페이스(brd 보유, operstate=up, 이름·/virtual/ 필터)를 자동 수집하여 사용한다. bonding(bond\*), bridge(br\*), vlan(vlan\*), eth\*, en\* 등이 포함되며, 설정에 주소를 넣지 않아도 된다. 수집이 비어 있을 때만 `discovery_broadcast_address`(단일)를 fallback으로 사용한다.
- **mol.service는 root로 실행**되며, 로컬 서비스 상태·제어 시 **sudo를 사용하지 않는다**. 원격 **서비스 상태** 조회는 요청을 받은 서버가 원격 mol의 API(서비스 포트 8888)를 호출하고, 원격 mol이 자체 `systemctl status`를 실행한 뒤 응답을 반환한다. 원격 **서비스 시작/중지**는 요청을 받은 서버가 해당 호스트로 **SSH** 접속하여 `systemctl start/stop`을 실행한다(원격 mol이 꺼져 있어도 시작 가능). SSH 포트·사용자는 `ssh_port`, `ssh_user`로 지정하며, 키 기반 인증이 필요하다. 원격 **서비스 재시작**은 SSH를 사용하지 않고, 요청을 받은 서버가 원격 mol의 API로 `POST service-control` (ip: "self", action: "restart")를 호출하며, 원격 mol이 자기 서버에서 `systemctl restart`를 실행한다(SSH 공개키 등록 없이 가능).

---

## 8. 서비스 시작 로그 및 버전 노출

- **systemctl status / journalctl**: mol 서비스가 시작할 때 **버전**을 로그에 남긴다. 예: `mol version 0.3.4: discovery listening on :9999 (bound IPs: ...)`. `systemctl status mol` 또는 `journalctl -u mol.service` 로 확인할 수 있다.

---

## 9. 버전 정보

- **기본**: 빌드 시 **ldflags**로 버전 문자열 주입 (예: `-ldflags "-X main.Version=1.2.3"` 또는 Makefile `VERSION=`). 미지정 시 `--version` 출력은 `"mol devel"`로 한다.
- **override**: 설정 파일에 `version`이 있으면 해당 값으로 노출 (자기 정보 API 및 DISCOVERY_RESPONSE의 `version` 필드).
- **실행 파일 검증**: 업로드된 mol 바이너리는 `--version` 옵션으로 실행해 출력이 `"mol "`로 시작하는지 확인한다. mol 실행 파일은 `--version` / `-version` 인자 시 버전 한 줄 출력 후 종료한다.

---

## 10. 백엔드 역할

- **UDP Discovery**: 포트 9999에서 listen, **SO_BROADCAST** 설정 후 broadcast 주소로 Discovery 요청 송신, 응답은 unicast로 수신.
- **Pending**: 요청 전송 **전에** request_id → 수신 채널을 pending에 등록하여 빠른 응답이 버려지지 않도록 함. 타임아웃 시 반환 전 채널 drain.
- **Self 제거**: 수집 시 **CPU UUID**로 자기 식별(같으면 제외). CPU UUID 없을 때만 IP+ServicePort 폴백. 응답의 `host_ip`는 요청자 기준 outbound IP로 채움.
- Discovery 요청 수신 시 자신의 정보를 담은 DISCOVERY_RESPONSE를 **요청자 IP 및 요청 UDP 패킷의 소스 포트**로 unicast 전송(소스 포트가 0이면 discovery_udp_port로 폴백).
- **자기 정보 API**: GET /api/v1/self — 브로드캐스트 주소별 outbound IP를 `host_ips`로 반환하고, `host_ip`는 그중 첫 번째. 버전, CPU UUID, CPU, 메모리 등 포함.
- **cpu_uuid(호스트 식별자) 확보 순서(Linux)**: `/proc/cpuinfo`의 `Serial`(전부 0·Not Set 등 무의미하면 스킵) → `dmidecode -s system-uuid` → `/sys/class/dmi/id/product_uuid` → `/etc/machine-id` → `/var/lib/dbus/machine-id`. 최소 설치 등 **dmidecode 미설치**여도 **machine-id**로 대부분 채워진다. VM 템플릿 복제 시 여러 대가 동일 machine-id를 가질 수 있으니 운영 시 주의.
- **호스트 정보 API**: GET /api/v1/host-info?ip= — `ip` 없음/self면 /self와 동일. `ip` 지정 시 해당 IP로 Discovery 유니캐스트 요청을 보내 그 호스트의 DISCOVERY_RESPONSE를 반환. 타임아웃 시 fail.
- **Discovery API**: GET /api/v1/discovery/stream (SSE, 실시간) — 웹 UI에서 사용. GET /api/v1/discovery (일괄 반환) — 웹 UI 미사용, 다른 클라이언트용. 일괄 API의 `data`는 배열이며 없을 때 `[]`. **유니캐스트 Discovery**: 특정 IP로 DISCOVERY_REQUEST를 유니캐스트 전송하여 해당 호스트의 DISCOVERY_RESPONSE 한 건만 수신(DoDiscoveryUnicast). 타임아웃은 최대 5초.
- **서비스 상태 API**: GET /api/v1/service-status?ip= — 로컬(`ip` 없음/self)은 `systemctl status` (sudo 없음, root 실행). 원격은 요청자가 원격 mol의 서비스 포트로 GET service-status를 호출하고, 원격 mol이 자체 systemctl status 실행 후 응답을 반환.
- **서비스 제어 API**: POST /api/v1/service-control — body `{ "ip", "action": "start"|"stop"|"restart" }`. 로컬은 `systemctl start/stop/restart` (sudo 없음, root 실행). 원격 start/stop은 **SSH**(`ssh_port`, `ssh_user` 사용)로 `systemctl start|stop` 실행. 원격 **restart**는 SSH 없이 요청자를 받은 서버가 **원격 mol API**로 POST service-control (ip: "self", action: "restart")를 호출하고, 원격 mol이 자기 서버에서 `systemctl restart` 실행.
- **업데이트 API**: 업로드는 **API** `POST /api/v1/upload`(multipart: mol, config)를 통해 **스테이징** `deploy_base/staging/{version}/` 에만 저장(text file busy 방지). 업로드 시 **mol 검증**(ELF 매직 + `--version` 실행, 타임아웃 5초)과 **config 검증**(mol config 구조체 파싱, 실패 시 항목/줄·필요 타입 안내)을 수행하여 잘못된 파일·설정은 400으로 거절. POST /api/v1/upload/remove → 스테이징에서 해당 버전 삭제(수동 전용, 자동 삭제 없음). 적용 시 버전 소스는 스테이징 우선, 없으면 versions. 로컬 적용: 스테이징에만 있으면 스테이징→versions 복사 후 **systemd-run** 로 update.sh 실행; 스테이징은 남겨 두어 원격 재사용. **원격 적용**: 로컬 서버가 대상 원격 mol의 서비스 포트(8888)로 HTTP로 (1) POST /api/v1/upload 로 해당 mol의 스테이징에 파일 전송, (2) POST /api/v1/apply-update (version, ip: "self")로 그 mol이 자기 스테이징을 적용하도록 호출. JSON(version, ip)이면 로컬 스테이징/versions에서 파일을 읽어 원격 upload·apply-update 호출; multipart(ip, mol, config)이면 원격 upload로 전달 후 apply-update 호출(로컬 스테이징 미사용). GET /api/v1/update-log?ip=, GET/POST /api/v1/current-config?ip= 또는 body ip → `ip` 지정 시 요청을 받은 서버가 원격 mol 해당 API 호출 후 응답 전달. update.sh 실패 시 rollback.sh 자동 호출.
- **설치된 버전 API**: `install_prefix`(비면 deploy_base) 기준. GET /api/v1/versions/list?ip= — 로컬 목록은 **current → previous → 나머지 시맨틱 버전 내림차순** 정렬 후 반환. POST /api/v1/versions/remove (body에 `ip` 선택) → 원격 프록시 동일. current/previous 가리키는 버전은 삭제하지 않음.
- 정적 파일 서빙 (`/web` prefix).

---

## 11. 요약 체크리스트

- [ ] Go, 소스 경로 `~/work/mol`
- [ ] 단일 실행 파일, net/http 만 사용
- [ ] 포트 8888 (HTTP), 9999 (UDP Discovery), UDP SO_BROADCAST 설정
- [ ] Discovery: UDP broadcast 요청(목적지 포트 discovery_udp_port), 응답은 요청자 IP:**요청 소스 포트**로 unicast; pending 등록 후 전송, 타임아웃 시 drain
- [ ] Discovery 메시지: DISCOVERY_REQUEST / DISCOVERY_RESPONSE (JSON), 호스트 정보(CPU, MEMORY, cpu_uuid) 포함; 응답에는 host_ip 하나만(요청자 기준 outbound IP); 수신 측이 responded_from_ip(UDP 발신지) 설정; 수신 측에서 같은 호스트의 여러 응답으로 IP·응답한 IP 취합
- [ ] Self 제거: **CPU UUID**로 자기 식별(같으면 제외), CPU UUID 없을 때만 IP+ServicePort 폴백
- [ ] Discovery 브로드캐스트: **3.1.1** 인터페이스 brd 자동 수집(operstate=up, 이름·/virtual/ 필터, bonding·bridge·vlan 포함); 중복 제거; fallback은 discovery_broadcast_address 또는 255.255.255.255; `mol --nic-brd`로 확인
- [ ] Discovery 타임아웃(설정), 중복 제거(host_ip:service_port), 설정 파일 반영
- [ ] Discovery 실시간: GET /api/v1/discovery/stream (SSE), **웹 UI는 이 API만 사용**, EventSource, 응답 오는 대로 화면 갱신; 기존 카드 매칭은 **cpu_uuid → IP** 순서만 사용(**hostname 미사용**, 동일 hostname 다른 호스트 병합 방지), event: done 후 스트림 종료(일괄 API 추가 호출 없음)
- [ ] Discovery 일괄: GET /api/v1/discovery 구현됨, data는 배열(빈 경우 []), null 미사용; **웹 UI에서는 호출하지 않음**(다른 클라이언트용)
- [ ] URL prefix: /web, /api/v1, 설정에서 변경 가능
- [ ] 진입 URL: /web/index.html, Discovery 버튼
- [ ] 초기 화면: 내 정보 (버전, IP 또는 host_ips, CPU UUID, 호스트, CPU, MEMORY)
- [ ] 호스트 목록: 아코디언(한 줄 요약 + 클릭 시 상세 카드 펼침), 상태 점(파랑=동작 중/빨강=중지/회색=미확인), 로컬은 맨 위·배경/테두리 색으로 구분, 로컬 IP는 Discovery 후 responded_from_ip 반영
- [ ] 발견된 호스트: 서버 아이콘 + CPU UUID(맨 위), 버전, IP(복수 시 취합 표시), 응답한 IP(복수 시 취합), 호스트명, CPU, MEMORY; 병합 시 기존 카드 매칭은 cpu_uuid·IP만(hostname 미사용)
- [ ] systemctl status: 접기/펼치기(기본 접힘), 접힌 상태에서 [정상 서비스 상태] / [서비스 중지 상태]
- [ ] 레이아웃: 호스트 카드 가운데 열(max-width 610px), 업데이트 영역 오른쪽 sticky; scrollbar-gutter: stable; 카드 내 버튼 오른쪽 위 절대 위치, 서비스 상태 영역은 카드 끝까지 넓게; 내 정보는 카드 한 겹만
- [ ] 내 정보 카드: 시작/중지 버튼 없음; 오른쪽 컬럼(업데이트 기록·config.yaml·설치된 버전)·하단(상태 새로고침·서비스 재시작)
- [ ] 발견된 호스트 카드: **로컬과 동일 레이아웃**(오른쪽 컬럼 + 하단 상태 행). 시작·중지 버튼 비노출; 상태 새로고침·서비스 재시작·업데이트 적용. 카드 열릴 때 업데이트 기록·config·버전 목록 자동 로드
- [ ] 서비스 상태 API: 로컬은 systemctl, 원격은 원격 mol API. 서비스 제어: 로컬은 systemctl; 원격 start/stop은 SSH, **원격 restart는 원격 mol API 호출**(SSH 키 불필요)
- [ ] 원격 API 프록시: update-log·current-config(GET/POST)·versions/list·versions/remove 에 `ip` 쿼리 또는 body 지원, 중앙 서버가 원격 mol 해당 API 호출 후 응답 전달
- [ ] 서비스 재시작 후: 성공 또는 terminated/연결 끊김 시 친절한 메시지 + 잠시 후 자동 호스트 정보(버전 등) 갱신 + 상태 새로고침(로컬·원격 동일)
- [ ] 설정: systemctl_service_name, deploy_base, **install_prefix**(비면 deploy_base, versions·installer용), discovery_broadcast_address(fallback만), ssh_port(기본 22), ssh_user(기본 root) (선택)
- [ ] **CLI**: **`-config <파일>`** 로만 HTTP 서버 + Discovery 기동; 인자 없이 실행 시 안내 후 종료; `-h`/`--help`; `--version`/`-version`; `--nic-brd`; **`--discovery`**(UDP만, `--dest-port`/`--src-port`/`--timeout`)
- [ ] 설치된 버전: GET /api/v1/versions/list(정렬: current → previous → 시맨틱 내림차순), POST /api/v1/versions/remove; current/previous 제외 삭제; 웹 UI 2열 세로 우선, 선택 삭제
- [ ] 업데이트: deploy_base, **staging/**, versions/, update.sh, rollback.sh; mol·config 검증; 로컬 적용 후 **페이지 전체 새로고침 없이** `/self` 폴링 → 업데이트 기록·config·versions·상태·update-status 현행화; 원격 적용 후 host-info 폴링(최대 8회) → 동일 패널 현행화; 로그 폴링 2초 간격; **GET /version** 헬스; recent_rollback·update_in_progress
- [ ] 프론트: 업데이트 영역 — 업로드(mol+config, **config 편집 영역에서 수정 후 업로드 가능**), 서버에서 mol·config 검증 실패 시 에러 메시지(항목/줄·필요 타입 안내) 표시; 적용(로컬/원격), 파일 선택 초기화, 업로드된 버전 삭제, **스테이징 버전 표시**, 로그 표시/새로고침; **업데이트 인디케이터**(카드 내, 서버 아이콘 아래)
- [ ] Discovery: 진행 중 기존 목록 유지·제어 가능; 원격 적용 후 Discovery 재수행 없이 카드·로그·config·versions·상태까지 현행화
- [ ] 원격 적용: 호스트별 버전 비교(data-host-version), 스테이징 버전과 다를 때만 적용 버튼 활성(초록), 툴팁(스테이징 없음/최신 버전/x.x.x 버전으로 업데이트 가능), 클릭 시 서버가 원격 upload·apply-update API 호출; **적용 성공 시 적용 버전으로 카드 버전 즉시 갱신(낙관적 갱신)**, 지연 후 host-info·service-status로 전체 갱신
- [ ] 호스트 정보 API: GET /api/v1/host-info?ip= (self=로컬, 지정=유니캐스트 Discovery)
- [ ] Discovery 유니캐스트: DoDiscoveryUnicast(ip), 타임아웃 최대 5초
- [ ] 상태 새로고침: 내 정보·원격 동일 방식 — 호스트 정보 API(GET /self 또는 GET /host-info?ip=)로 카드 내용만 갱신 후 GET /service-status로 systemctl 상태 갱신(카드 전체 재렌더링 없음)
- [ ] 일반 API 응답: status + data
- [ ] 자기 정보 API: GET /api/v1/self
- [ ] 설정: YAML, 항목 7.1 반영
- [ ] 버전: ldflags 기본, 설정 override 가능
- [ ] 프론트: embed 정적 파일, Vanilla JS, EventSource로 Discovery 스트림 수신

---

*이 문서는 초기 요구 사항과 보완·명확화 사항을 모두 반영한 PRD이며, 구현은 본 PRD 검토 후 진행한다.*
