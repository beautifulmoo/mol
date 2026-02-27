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

- **요청**: 한 호스트(A)가 지정된 broadcast 주소(단일 `discovery_broadcast_address` 또는 복수 `discovery_broadcast_addresses`)의 **UDP 9999** 번 포트로 Discovery 요청을 보낸다. 복수 주소가 설정되면 **각 주소마다** 한 번씩 요청을 전송하여 여러 서브넷(예: 172.29.236.x, 172.29.244.x)을 모두 탐색한다.
- **응답**: broadcast를 수신한 각 호스트는 Discovery 응답을 **요청을 보낸 호스트(A)의 IP:9999** 로 **unicast** 로 보낸다.
- 즉, 요청·응답 모두 **UDP 포트 9999**를 사용하며, 응답 수신도 A가 UDP 9999에서 listen하여 처리한다.
- **브로드캐스트 송신**: UDP 소켓에 **SO_BROADCAST** 옵션을 설정하여 broadcast 주소로의 전송을 허용한다.

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
  "service": "programA",
  "request_id": "uuid-1234"
}
```

**응답 예시** (호스트 정보 포함)

```json
{
  "type": "DISCOVERY_RESPONSE",
  "service": "programA",
  "host_ip": "192.168.0.102",
  "hostname": "host-102",
  "service_port": 8888,
  "version": "1.2.3",
  "request_id": "uuid-1234",
  "cpu_info": "Intel Xeon 8 cores",
  "cpu_usage_percent": 23.5,
  "cpu_uuid": "abc-123",
  "memory_total_mb": 16384,
  "memory_used_mb": 8192,
  "memory_usage_percent": 50.0,
  "responded_from_ip": "192.168.0.102"
}
```

- `request_id`: 요청 시 생성한 UUID를 응답에 그대로 넣어 요청·응답 매칭에 사용한다.
- `cpu_uuid`: 호스트 식별용(동일 호스트 병합·self 제거에 사용). 없을 수 있음.
- **응답자는 host_ip 하나만 보낸다.** 같은 호스트가 여러 NIC으로 응답하면 응답마다 다른 host_ip(해당 요청에 대한 outbound IP)가 담긴다. **수신 측**에서 같은 cpu_uuid의 여러 응답을 받아 IP 목록을 취합하여 화면에 표시한다.
- `responded_from_ip`: (수신 측 설정) UDP 패킷의 **발신지 IP**로, 수신 측이 응답을 처리할 때 채운다. 화면에서 "응답한 IP"로 표시하며, 같은 호스트가 여러 IP로 응답한 경우 모두 취합해 보여준다. 전선 상의 메시지에는 없고, API/SSE로 내보낼 때만 포함된다.
- 자기 정보 API(GET /self)에서는 브로드캐스트 대역별 outbound IP를 `host_ips` 배열로 반환할 수 있다. Discovery UDP 응답 메시지 자체에는 host_ips를 넣지 않는다.
- 호스트 정보(CPU, MEMORY)는 위 필드로 확장하며, 단위·필드명은 이 스키마를 기준으로 한다.

### 3.5 중복 제거 및 설정

- **중복 제거**: 스트림/일괄 반환 시 동일한 (host_ip:service_port@responded_from_ip) 조합은 한 번만 전달한다. 즉 같은 호스트가 여러 IP로 응답하면 **응답 건수만큼** 이벤트가 나가며, 각 이벤트의 host_ip·responded_from_ip가 다를 수 있다. 설정 `discovery_deduplicate`로 켜/끌 수 있다.
- **동일 호스트 병합(프론트)**: `cpu_uuid`가 같은 응답은 **한 호스트**로 간주한다. 카드는 하나만 두고, **IP**는 각 응답의 host_ip를 모두 취합해 표시하고, **응답한 IP**는 각 응답의 responded_from_ip를 모두 취합해 표시한다. CPU·메모리는 응답 중 하나만 사용한다.
- **타임아웃**: 응답 수집 대기 시간은 설정 `discovery_timeout_seconds`(기본 10초)로 지정한다.

### 3.6 실시간 Discovery (SSE)

- Discovery 결과를 **타임아웃 만료를 기다리지 않고** 응답이 도착하는 대로 화면에 반영한다.
- **백엔드**: `GET /api/v1/discovery/stream` 엔드포인트를 두고, **Server-Sent Events(SSE)** 로 스트리밍한다. Discovery 요청을 보낸 뒤, 각 DISCOVERY_RESPONSE가 올 때마다 `data: {JSON}\n\n` 형식으로 한 건씩 전송하고 즉시 flush한다. 타임아웃이 되면 `event: done\ndata: {}\n\n` 를 보내고 스트림을 종료한다. 내부적으로는 **DoDiscoveryStream** 과 같이 요청 시 pending 등록 → 브로드캐스트 전송 → 수신 채널에서 응답을 하나씩 읽어 필터(self 제거·중복 제거) 후 SSE로 내보내는 방식을 사용한다.
- **프론트엔드**: Discovery 버튼 클릭 시 **EventSource** 로 `/api/v1/discovery/stream` 에 연결한다. 기본 메시지 이벤트가 올 때마다 수신한 JSON을 파싱해, **같은 CPU UUID**가 이미 있으면 해당 카드에 **IP**(각 응답의 host_ip 취합)·**응답한 IP**(각 응답의 responded_from_ip 취합)를 병합·갱신하고, 없으면 같은 IP 카드 갱신 또는 새 카드 추가한다. `event: done` 수신 시 스트림을 닫고 버튼을 복구한다. 호스트 카드 상세에서는 **CPU UUID**를 맨 위에, **IP**·**응답한 IP** 순으로 표시한다.

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

- Discovery 요청은 **프론트엔드의 Discovery 버튼**에 의해 트리거된다.
- **실시간 스트리밍 (기본 UX)**: `GET {serverUrl}/api/v1/discovery/stream` — **Server-Sent Events(SSE)**. Content-Type `text/event-stream`. 응답이 올 때마다 `data: {JSON}\n\n` 로 호스트 한 건씩 전송, 타임아웃 시 `event: done\ndata: {}\n\n` 후 스트림 종료. 클라이언트는 EventSource로 연결해 응답이 오는 대로 화면에 반영한다.
- **일괄 반환 (선택)**: `GET {serverUrl}/api/v1/discovery` — 타임아웃(설정값)까지 수집한 뒤 `status` + `data`(발견된 호스트 배열)를 한 번에 JSON으로 반환. `data`는 배열이며, 결과가 없어도 `[]` 로 반환한다(null 아님).

### 5.4 서비스 상태·제어 API

- **서비스 상태**: `GET {serverUrl}/api/v1/service-status?ip=`  
  - `ip` 비어 있거나 `"self"`: 로컬에서 `systemctl status <systemctl_service_name>` 실행( **sudo 없음**, mol.service는 root로 실행), 결과를 `{ "status": "success", "data": { "output": "..." } }` 로 반환.
  - `ip` 지정: 요청을 받은 서버가 **원격 mol의 서비스 포트(8888)** 로 `GET .../service-status` 를 호출한다. 원격 mol은 자기 서버에서 `systemctl status` 를 실행한 뒤 그 결과를 응답으로 반환하고, 요청자는 그 응답을 그대로 클라이언트에 전달한다.
  - 실패 시 `{ "status": "fail", "data": "에러 메시지" }`.
- **서비스 제어**: `POST {serverUrl}/api/v1/service-control`  
  - Body: `{ "ip": "" | "self" | "<host_ip>", "action": "start" | "stop" }`.  
  - `ip` 비어 있거나 `"self"`: 로컬 `systemctl start/stop <systemctl_service_name>` (mol.service는 root로 실행).  
  - 그 외: 요청을 받은 서버가 **원격 mol의 서비스 포트(8888)** 로 `POST .../service-control` (Body: `{ "ip": "self", "action": "start"|"stop" }`)를 호출한다. 원격 mol은 자기 서버에서 `systemctl start` 또는 `stop` 을 실행한 뒤 응답을 반환하고, 요청자는 그 응답을 그대로 클라이언트에 전달한다.  
  - 성공 시 `{ "status": "success", "data": null }`, 실패 시 `{ "status": "fail", "data": "에러 메시지" }`.

### 5.5 업데이트 API

- **배포 베이스**: 설정 `deploy_base`(기본값 `/opt/mol`) 아래에 **스테이징** `staging/<버전>/`, **실행 경로** `versions/<버전>/`, **update.sh**, **rollback.sh** 가 있다고 가정한다. 디렉터리 구조 예시는 다음과 같다.

  ```
  deploy_base/                    # 예: /opt/mol
  ├── current -> versions/1.2.6   # 심볼릭 링크, 현재 실행 버전
  ├── previous -> versions/1.2.5  # 심볼릭 링크, 이전 버전(롤백용)
  ├── update.sh
  ├── rollback.sh
  ├── update_last.log             # update.sh 실행 로그
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
- **스크립트 위치**: 소스 저장소 프로젝트 루트에 `update.sh`, `rollback.sh` 가 참고용으로 포함되어 있다. 실제 사용 시에는 이 두 파일을 **배포 베이스 직하**에 복사해 둔다. 즉 `{deploy_base}/update.sh`, `{deploy_base}/rollback.sh`. 스크립트 내부의 `BASE`(기본값 `/opt/mol`)는 `deploy_base` 와 일치해야 하며, 다르면 수정이 필요하다.
- **update.sh**: 인자로 버전 하나를 받는다. `{deploy_base}/versions/{버전}/mol` 이 존재·실행 가능한지 확인한 뒤, 서비스 중지 → `current`/`previous` 심볼릭 링크 갱신 → 서비스 시작을 수행한다. 시작 실패 시 `{deploy_base}/rollback.sh` 를 호출해 이전 버전으로 되돌린다.
- **rollback.sh**: 인자는 없다. `{deploy_base}/previous` 심볼릭 링크가 있어야 하며, 서비스 중지 → `current` 를 `previous` 가 가리키는 버전으로 교체 → 서비스 시작을 수행한다. 웹 API에서는 호출하지 않고, update.sh 의 실패 복구 또는 운영자가 수동 실행할 때 사용한다.
- **업로드**: `POST {serverUrl}/api/v1/upload`  
  - **multipart/form-data**: `mol`(실행 파일), `config`(config.yaml). 버전은 config에서 파싱.  
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
  - **`systemd-run`** 로 실행: `systemd-run --unit=mol-update --property=RemainAfterExit=yes {deploy_base}/update.sh {version}`. 로그는 `{deploy_base}/update_last.log`. mol.service는 root로 실행되므로 sudo를 사용하지 않는다. 응답은 즉시 반환, 실제 업데이트는 백그라운드.
- **적용 (원격)**: 원격 mol로의 배포는 **원격 mol의 업로드 API**를 사용한다. 요청을 받은 서버(로컬 mol)는 대상 원격 mol의 **서비스 포트(8888)** 로 HTTP로 (1) `POST /api/v1/upload` (multipart: `mol`, `config`)를 보내 해당 mol의 **스테이징**에 올린 뒤, (2) `POST /api/v1/apply-update` (Body: `{ "version": "<버전>", "ip": "self" }`)를 보내 그 mol이 자기 스테이징을 적용·재시작하도록 한다.  
  - **JSON** Body: `{ "version": "<버전>", "ip": "<원격 IP>" }`. 로컬의 스테이징 또는 versions에서 해당 버전의 mol·config를 읽어, 위와 같이 원격의 upload API로 전송한 후 원격의 apply-update API를 호출한다.  
  - **multipart** (원격 전용): `ip`, `mol`, `config`. 수신한 파일을 로컬 스테이징에 저장하지 않고, 원격 mol의 upload API로 그대로 전송한 뒤 원격의 apply-update API를 호출한다.
- **업데이트 로그**: `GET {serverUrl}/api/v1/update-log`  
  - `{deploy_base}/update_last.log` 파일 내용을 읽어 `{ "status": "success", "data": { "output": "<로그 텍스트>" } }` 로 반환.  
  - 파일이 없거나 읽기 실패 시 `{ "status": "fail", "data": "에러 메시지" }`.

---

## 6. 프론트엔드

- **구현 방식**: 정적 파일(HTML, CSS, JavaScript)을 **Go embed**로 단일 실행 파일에 포함.
- **JavaScript**: **Vanilla JS**만 사용. API 호출은 `fetch`, UI 업데이트는 DOM 조작으로 처리. SPA 프레임워크(React, Vue 등)는 사용하지 않는다.
- **초기 화면**
  - **내 정보**: 현재 mol 인스턴스의 버전, **IP(또는 응답으로 사용하는 모든 IP `host_ips`)** , 호스트명, CPU UUID, CPU, MEMORY 등을 표시 (자기 정보 API 사용). 자기 정보 API는 각 브로드캐스트 주소별 outbound IP를 `host_ips`로 반환하여 Discovery 응답으로 사용하는 IP들을 모두 보여준다.
- **Discovery 버튼**
  - 클릭 시 **EventSource** 로 `GET /api/v1/discovery/stream` 에 연결하여 **실시간 Discovery**를 수행한다. **기존 발견된 호스트 목록은 비우지 않고** 유지하며, 진행 중에도 해당 카드들의 제어(시작/중지·업데이트 적용·상태 새로고침)가 가능하다. SSE로 호스트가 도착할 때 **같은 CPU UUID**가 있으면 해당 카드에 IP만 병합·갱신하고, 없으면 같은 IP 카드 갱신 또는 새 카드 추가한다. `event: done` 수신 시 스트림을 닫고 버튼을 복구한다.
- **발견된 호스트 표시**
  - 각 호스트를 **서버 모양 아이콘**과 함께 표시한다.
  - 표시 내용: **CPU UUID**(맨 위), mol 버전, **IP**(여러 개면 쉼표 구분, 같은 호스트의 여러 응답에서 host_ip를 취합), **응답한 IP**(실제로 Discovery 응답을 보낸 UDP 발신지 IP, 여러 개면 취합), 호스트명, 서비스 포트, CPU, MEMORY. 동일 CPU UUID의 여러 응답은 **한 카드**로 병합하며, IP와 응답한 IP는 모두 취합해 표시하고 CPU·메모리는 하나만 표시한다.
  - 내 정보와 동일한 형태(카드/테이블 등)로 보여주어 일관된 UX를 유지한다.
- **원격 적용 후**: 원격 mol 업데이트가 성공하면 **Discovery를 다시 수행하지 않고**, 해당 호스트 카드만 갱신한다.  
  - **카드 버전 즉시 갱신(낙관적 갱신)**: apply-update API 성공 시점에 이미 알고 있는 **적용 버전**으로 카드의 버전 표시(`data-host-version` 속성 및 버전 dd 텍스트)를 **즉시** 갱신하고, 적용 버튼 활성/비활성 상태를 다시 계산한다. 이렇게 하면 원격 mol 재시작 중 host-info가 실패하거나 지연되어도 카드에 새 버전이 바로 반영된다.  
  - **지연 후 host-info·service-status**: 일정 시간(예: 5초) 후 `GET /api/v1/host-info?ip=...`를 재시도(최대 4회)하고, 성공 시 카드의 전체 호스트 정보(버전, IP, CPU, 메모리 등)를 덮어쓴 뒤 `GET /api/v1/service-status?ip=...`로 systemctl 상태를 갱신한다. host-info가 실패해도 service-status는 조회하여 업데이트 인디케이터를 숨긴다.

### 6.1 systemctl status 표시 (내 정보·발견된 호스트 공통)

- 각 호스트 카드에 **systemctl status** 결과를 표시한다.
- **접기/펼치기**: 기본은 **접힌 상태**. 헤더(아이콘 ▶/▼ + 요약 문구) 클릭 시 상세 출력(`systemctl status` 전문)을 펼치거나 접는다.
- **접힌 상태에서의 요약**  
  - `Active: active (running)` 이면 **\[정상 서비스 상태]**  
  - 그 외(dead 등)면 **\[서비스 중지 상태]**  
  - 로딩/에러 시 "불러오는 중…", "상태를 불러올 수 없습니다." 등.

### 6.2 서비스 시작/중지 (발견된 호스트만)

- **내 정보(자기 자신) 카드에는 시작/중지 버튼을 두지 않는다.**
- **발견된 호스트** 카드에만 **시작**·**중지** 버튼을 둔다.  
  - **시작**: `POST /api/v1/service-control` with `{ "ip": "<host_ip>", "action": "start" }` 후, 성공 시 해당 카드의 systemctl status를 다시 조회해 표시를 갱신한다. **시작** 버튼은 **파란색** 계열로 표시하여 직관적으로 구분한다.  
  - **중지**: `POST /api/v1/service-control` with `{ "ip": "<host_ip>", "action": "stop" }` 후, 동일하게 status를 갱신한다. **중지** 버튼은 **빨간색** 계열.
- **버튼 비활성화**  
  - **Active (running)** 이면 **시작** 버튼 disabled, **중지** 버튼 enabled.  
  - **dead(중지)** 상태이면 **시작** 버튼 enabled, **중지** 버튼 disabled.

### 6.3 업데이트 (업로드·적용·로그)

- **업로드**: mol 실행 파일 + config.yaml 선택(및 편집) 후 `POST /api/v1/upload` (multipart: `mol`, `config`). 버전은 config에서 파싱. 업로드는 **스테이징**에 저장. 성공/실패 메시지 표시.
- **적용 (로컬)**: 버전이 스테이징 또는 이전 적용으로 존재할 때, 적용 버튼으로 `POST /api/v1/apply-update` (`{ "version": "..." }`). 성공 시 "업데이트를 적용 중입니다. …" 안내, 실패 시 에러 메시지.
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
- **업데이트 인디케이터**: 로컬·리모트 카드 모두, 업데이트 적용이 진행 중일 때(완료로 판단될 때까지) 카드 내 **서버 아이콘 아래**에 회전하는 로딩 인디케이터를 표시한다. 로컬은 요청 실패 시에만 숨기고, 성공 시에는 페이지 자동 새로고침으로 사라진다. 리모트는 성공 시 일정 시간 후 상태·호스트 정보 갱신이 끝나면 숨기고, 실패 시 즉시 숨긴다.
- **파일 선택 초기화**: mol·config 선택 및 편집 내용만 초기화. 스테이징/versions 에 올라간 버전은 유지.
- **업로드된 버전 삭제**: 스테이징에서 해당 버전만 삭제.
- **로그**: `GET /api/v1/update-log` 로 최근 업데이트 로그를 가져와 표시. 새로고침으로 최신 로그 확인 가능.

### 6.4 상태 새로고침 (내 정보·발견된 호스트)

- **내 정보** 카드와 **발견된 호스트** 카드 각각에 **「상태 새로고침」** 버튼을 둔다.
- **내 정보**에서 클릭 시: `GET /api/v1/self`로 호스트 정보를 다시 가져와 카드(버전, IP, 호스트명, CPU, 메모리 등)를 갱신하고, `GET /api/v1/service-status`로 systemctl status를 다시 조회해 표시한다.
- **발견된 호스트**에서 클릭 시: `GET /api/v1/host-info?ip=<해당 호스트 IP>`로 해당 호스트의 최신 정보(버전, CPU, 메모리 등)를 가져와 카드의 호스트 정보를 갱신하고, `GET /api/v1/service-status?ip=...`로 systemctl status를 갱신한다. 적용 버튼의 활성/비활성 및 툴팁도 갱신한다. host-info 요청이 실패해도 service-status는 조회하여 systemctl 상태는 갱신한다.

---

## 7. 설정

- **포맷**: **YAML**
- **위치**: 구현 시 결정 (예: 실행 파일 기준 상대 경로 `config.yaml`, 또는 환경변수 `MOL_CONFIG`로 경로 지정)

### 7.1 설정 항목 (최소)

| 항목 | 설명 | 예시 |
|------|------|------|
| `service_name` | Discovery 메시지의 `service` 값 | `"mol"` |
| `discovery_broadcast_address` | (선택) Discovery broadcast 대상 IP 단일. `discovery_broadcast_addresses` 가 비어 있을 때만 사용 | `"192.168.0.255"` |
| `discovery_broadcast_addresses` | (선택) Discovery broadcast 대상 IP **복수**. 설정 시 각 주소마다 DISCOVERY_REQUEST 전송(여러 서브넷 탐색). 비어 있으면 `discovery_broadcast_address` 또는 기본값 사용 | `["172.29.236.255", "172.29.244.255"]` |
| `discovery_udp_port` | Discovery용 UDP 포트 | `9999` |
| `http_port` | HTTP 서비스 포트 | `8888` |
| `web_prefix` | 프론트엔드 URL prefix | `"/web"` |
| `api_prefix` | 백엔드 API URL prefix | `"/api/v1"` |
| `discovery_timeout_seconds` | Discovery 응답 대기 시간(초) | `10` |
| `discovery_deduplicate` | 동일 호스트 중복 제거 여부 | `true` |
| `version` | (선택) 버전 override. 비어 있으면 빌드 시 ldflags 값 사용 | `"1.2.3"` 또는 빈 문자열 |
| `systemctl_service_name` | (선택) 서비스 상태·제어 대상 유닛 이름 | `"mol.service"` |
| `deploy_base` | (선택) 업데이트 배포 베이스. `staging/`, `versions/`, `update.sh`, `rollback.sh`, `update_last.log` 의 기준 경로 | `"/opt/mol"` |

- IP 대역(예: broadcast 주소)은 실제 환경에 따라 다를 수 있으므로 `discovery_broadcast_address` 또는 `discovery_broadcast_addresses` 로 설정에서 지정한다. 복수 주소를 쓰면 여러 서브넷(예: 172.29.236.x, 172.29.244.x)에 한 번에 Discovery 요청을 보낼 수 있다.
- **mol.service는 root로 실행**되며, 서비스 상태·제어 시 **sudo를 사용하지 않는다** (소스 및 스크립트에서 sudo 제거). 원격 서비스 상태·제어는 요청을 받은 서버가 **원격 mol의 API**(서비스 포트 8888)를 호출하고, 원격 mol이 자체적으로 `systemctl status` / `start` / `stop` 을 실행한 뒤 응답을 반환하는 방식으로 수행한다.

---

## 8. 버전 정보

- **기본**: 빌드 시 **ldflags**로 버전 문자열 주입 (예: `-ldflags "-X main.Version=1.2.3"`).
- **override**: 설정 파일에 `version`이 있으면 해당 값으로 노출 (자기 정보 API 및 DISCOVERY_RESPONSE의 `version` 필드).

---

## 9. 백엔드 역할

- **UDP Discovery**: 포트 9999에서 listen, **SO_BROADCAST** 설정 후 broadcast 주소로 Discovery 요청 송신, 응답은 unicast로 수신.
- **Pending**: 요청 전송 **전에** request_id → 수신 채널을 pending에 등록하여 빠른 응답이 버려지지 않도록 함. 타임아웃 시 반환 전 채널 drain.
- **Self 제거**: 수집 시 **CPU UUID**로 자기 식별(같으면 제외). CPU UUID 없을 때만 IP+ServicePort 폴백. 응답의 `host_ip`는 요청자 기준 outbound IP로 채움.
- Discovery 요청 수신 시 자신의 정보를 담은 DISCOVERY_RESPONSE를 요청자 IP:9999로 unicast 전송.
- **자기 정보 API**: GET /api/v1/self — 브로드캐스트 주소별 outbound IP를 `host_ips`로 반환하고, `host_ip`는 그중 첫 번째. 버전, CPU UUID, CPU, 메모리 등 포함.
- **호스트 정보 API**: GET /api/v1/host-info?ip= — `ip` 없음/self면 /self와 동일. `ip` 지정 시 해당 IP로 Discovery 유니캐스트 요청을 보내 그 호스트의 DISCOVERY_RESPONSE를 반환. 타임아웃 시 fail.
- **Discovery API**: GET /api/v1/discovery/stream (SSE, 실시간), GET /api/v1/discovery (일괄 반환). Discovery 결과는 `data` 배열로 반환하며, 없을 때는 `[]`. **유니캐스트 Discovery**: 특정 IP로 DISCOVERY_REQUEST를 유니캐스트 전송하여 해당 호스트의 DISCOVERY_RESPONSE 한 건만 수신(DoDiscoveryUnicast). 타임아웃은 최대 5초.
- **서비스 상태 API**: GET /api/v1/service-status?ip= — 로컬(`ip` 없음/self)은 `systemctl status` (sudo 없음, root 실행). 원격은 요청자가 원격 mol의 서비스 포트로 GET service-status를 호출하고, 원격 mol이 자체 systemctl status 실행 후 응답을 반환.
- **서비스 제어 API**: POST /api/v1/service-control — body `{ "ip", "action": "start"|"stop" }`. 로컬은 `systemctl start/stop` (sudo 없음, root 실행). 원격은 요청자가 원격 mol의 서비스 포트로 POST service-control(ip: "self", action)을 호출하고, 원격 mol이 자체 systemctl start/stop 실행 후 응답을 반환.
- **업데이트 API**: 업로드는 **API** `POST /api/v1/upload`(multipart: mol, config)를 통해 **스테이징** `deploy_base/staging/{version}/` 에만 저장(text file busy 방지). POST /api/v1/upload/remove → 스테이징에서 해당 버전 삭제(수동 전용, 자동 삭제 없음). 적용 시 버전 소스는 스테이징 우선, 없으면 versions. 로컬 적용: 스테이징에만 있으면 스테이징→versions 복사 후 **systemd-run** 로 update.sh 실행; 스테이징은 남겨 두어 원격 재사용. **원격 적용**: 로컬 서버가 대상 원격 mol의 서비스 포트(8888)로 HTTP로 (1) POST /api/v1/upload 로 해당 mol의 스테이징에 파일 전송, (2) POST /api/v1/apply-update (version, ip: "self")로 그 mol이 자기 스테이징을 적용하도록 호출. JSON(version, ip)이면 로컬 스테이징/versions에서 파일을 읽어 원격 upload·apply-update 호출; multipart(ip, mol, config)이면 원격 upload로 전달 후 apply-update 호출(로컬 스테이징 미사용). GET /api/v1/update-log → 로그 내용 반환. update.sh 실패 시 rollback.sh 자동 호출.
- 정적 파일 서빙 (`/web` prefix).

---

## 10. 요약 체크리스트

- [ ] Go, 소스 경로 `~/work/mol`
- [ ] 단일 실행 파일, net/http 만 사용
- [ ] 포트 8888 (HTTP), 9999 (UDP Discovery), UDP SO_BROADCAST 설정
- [ ] Discovery: UDP broadcast 요청, 응답은 요청자 IP:9999 로 unicast; pending 등록 후 전송, 타임아웃 시 drain
- [ ] Discovery 메시지: DISCOVERY_REQUEST / DISCOVERY_RESPONSE (JSON), 호스트 정보(CPU, MEMORY, cpu_uuid) 포함; 응답에는 host_ip 하나만(요청자 기준 outbound IP); 수신 측이 responded_from_ip(UDP 발신지) 설정; 수신 측에서 같은 호스트의 여러 응답으로 IP·응답한 IP 취합
- [ ] Self 제거: **CPU UUID**로 자기 식별(같으면 제외), CPU UUID 없을 때만 IP+ServicePort 폴백
- [ ] Discovery 복수 브로드캐스트: `discovery_broadcast_addresses` 지원, 각 주소마다 DISCOVERY_REQUEST 전송
- [ ] Discovery 타임아웃(설정), 중복 제거(host_ip:service_port), 설정 파일 반영
- [ ] Discovery 실시간: GET /api/v1/discovery/stream (SSE), EventSource, 응답 오는 대로 화면 갱신; 동일 CPU UUID는 한 카드로 병합(IP 모두 표시), event: done
- [ ] Discovery 일괄: GET /api/v1/discovery, data는 배열(빈 경우 []), null 미사용
- [ ] URL prefix: /web, /api/v1, 설정에서 변경 가능
- [ ] 진입 URL: /web/index.html, Discovery 버튼
- [ ] 초기 화면: 내 정보 (버전, IP 또는 host_ips, CPU UUID, 호스트, CPU, MEMORY)
- [ ] 발견된 호스트: 서버 아이콘 + CPU UUID(맨 위), 버전, IP(복수 시 취합 표시), 응답한 IP(복수 시 취합), 호스트명, CPU, MEMORY; 동일 CPU UUID 한 카드로 병합
- [ ] systemctl status: 접기/펼치기(기본 접힘), 접힌 상태에서 [정상 서비스 상태] / [서비스 중지 상태]
- [ ] 내 정보 카드: 시작/중지 버튼 없음
- [ ] 발견된 호스트 카드: 시작(파란색)·중지(빨간색) 버튼; Active면 시작 disabled, dead면 중지 disabled
- [ ] 서비스 상태/제어 API: 로컬은 systemctl만 (sudo 없음, mol.service root 실행), 원격은 원격 mol API 호출
- [ ] 설정: systemctl_service_name, deploy_base, discovery_broadcast_addresses (선택)
- [ ] 업데이트: deploy_base, **staging/**(upload API로 저장, 수동 삭제만), versions/(실행 경로), update.sh, rollback.sh; upload API → 스테이징만; upload/remove → 스테이징 삭제(수동); 적용 시 버전 소스=스테이징 우선 then versions; 로컬 적용 시 스테이징만 있으면 복사 후 update.sh(스테이징 유지); **원격 적용=원격 mol의 upload API(HTTP)·apply-update API 호출**(JSON(version,ip) 또는 multipart(ip,mol,config)); systemd-run (root 실행으로 sudo 없음); update_last.log, update-log API
- [ ] 프론트: 업데이트 영역 — 업로드(mol+config·편집), 적용(로컬/원격), 파일 선택 초기화, 업로드된 버전 삭제, **스테이징 버전 표시**, 로그 표시/새로고침; **업데이트 인디케이터**(카드 내, 서버 아이콘 아래)
- [ ] Discovery: 진행 중 기존 목록 유지·제어 가능; 동일 호스트 응답 시 카드 갱신; 원격 적용 후 Discovery 재수행 없이 해당 카드만 지연 후 host-info·service-status 갱신
- [ ] 원격 적용: 호스트별 버전 비교(data-host-version), 스테이징 버전과 다를 때만 적용 버튼 활성(초록), 툴팁(스테이징 없음/최신 버전/x.x.x 버전으로 업데이트 가능), 클릭 시 서버가 원격 upload·apply-update API 호출; **적용 성공 시 적용 버전으로 카드 버전 즉시 갱신(낙관적 갱신)**, 지연 후 host-info·service-status로 전체 갱신
- [ ] 호스트 정보 API: GET /api/v1/host-info?ip= (self=로컬, 지정=유니캐스트 Discovery)
- [ ] Discovery 유니캐스트: DoDiscoveryUnicast(ip), 타임아웃 최대 5초
- [ ] 상태 새로고침: 내 정보·발견된 호스트 카드에 「상태 새로고침」; 내 정보=GET /self 후 카드·status 갱신, 원격=GET /host-info?ip= 후 카드·status·적용 버튼 갱신
- [ ] 일반 API 응답: status + data
- [ ] 자기 정보 API: GET /api/v1/self
- [ ] 설정: YAML, 항목 7.1 반영
- [ ] 버전: ldflags 기본, 설정 override 가능
- [ ] 프론트: embed 정적 파일, Vanilla JS, EventSource로 Discovery 스트림 수신

---

*이 문서는 초기 요구 사항과 보완·명확화 사항을 모두 반영한 PRD이며, 구현은 본 PRD 검토 후 진행한다.*
