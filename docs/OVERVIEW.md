# Mole Agent — 시스템 개요

다른 사람에게 이 시스템을 소개할 때 쓰는 **개괄 설명**이다. 상세 명세·API·CLI 옵션은 아래 「관련 문서」를 본다.

---

## 이 시스템은 무엇인가

**Mole Agent**는 Linux 서버 여러 대에 깔리는 **단일 실행 파일(`contrabass-moleU`)** 기반의 **호스트 관리·배포 에이전트**이다.

한 머신에서 다음을 할 수 있다.

- 네트워크에 흩어진 **에이전트들을 UDP Discovery로 찾기**
- **웹 UI** 또는 **CLI/REPL**로 각 호스트의 상태·버전·서비스 확인
- **에이전트 바이너리·설정 업데이트**, **롤백**, **설정 복사**, **일괄 재시작** 등 운영 작업

웹 화면·HTTP API·명령줄·대화형 REPL이 **같은 바이너리 안**에 들어 있으며, 설정 파일(`agent.local.yml`)과 함께 배포한다.

---

## 어떤 문제를 푸는가

베어메탈·클러스터처럼 **여러 물리/가상 호스트**에 같은 에이전트를 올려 두고, 운영자가 **중앙(또는 한 대의 orchestrator)** 에서:

1. **누가 있는지** 찾고  
2. **어떤 버전**이 돌아가는지 보고  
3. **새 버전을 배포**하거나 **문제 시 이전 버전으로 되돌리고**  
4. **설정을 맞추거나 서비스를 재시작**하는  

일련의 작업을 **브라우저나 터미널**로 처리하려는 목적에 맞춰져 있다.

---

## 전체 그림 (아키텍처)

### 배포 단위

| 구성 | 설명 |
|------|------|
| **에이전트 바이너리** | `contrabass-moleU` (빌드 variant: `control` / `compute`) |
| **설정 파일** | `agent.local.yml` — 포트, Discovery, 배포 경로 등 |
| **내장 웹 UI** | 빌드 시 바이너리에 포함; 별도 정적 파일 배포 불필요 |
| **내장 스크립트** | `update.sh`, `rollback.sh` — 버전 전환·실패 시 복구 |

각 **관리 대상 호스트**에 에이전트가 **systemd 서비스** 등으로 떠 있다.

### 역할 구분

```
  [운영자 PC / orchestrator 호스트]
        │
        │  브라우저 ──► Gin(8888) ──► maintenance(8889)  ← 웹 UI·일괄 API
        │  CLI/REPL ──► Discovery(UDP) + HTTP
        │
        ▼  UDP broadcast / HTTP
  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐
  │  Host A     │  │  Host B     │  │  Host C     │
  │  agent      │  │  agent      │  │  agent      │
  └─────────────┘  └─────────────┘  └─────────────┘
```

- **각 호스트의 에이전트**: 자기 정보 제공, Discovery 응답, 업데이트 수신·적용, `systemctl` 연동.
- **orchestrator로 쓰는 한 대**: `-cfg`로 서비스 기동. 웹에서 Discovery·일괄 작업을 **이 호스트의 maintenance API**로 실행. (브라우저 접속용 **Gin**은 `-cfg` 진입 시에만 외부 포트를 연다.)

### 통신 방식 (개요)

| 채널 | 용도 |
|------|------|
| **UDP 9999 (Discovery)** | 서브넷 브로드캐스트로 “누가 있는지” 탐색 |
| **HTTP maintenance (기본 8889)** | 웹 UI·일괄 작업 API (주로 loopback; Gin이 프록시) |
| **HTTP Gin (기본 8888)** | 원격 호스트별 API — 업데이트 적용, 버전 목록, 서비스 상태 등 |
| **SSH (선택)** | 원격 **서비스 start/stop** 만; 상태 조회·재시작은 HTTP |

Discovery는 **JSON over UDP**로 hostname, IP, CPU UUID, 버전, control/compute variant 등을 주고받는다.

---

## 주요 기능

### 1. Discovery (호스트 탐색)

- 버튼 한 번(웹) 또는 `agent --discovery` / REPL `discover`로 **같은 LAN의 에이전트**를 찾는다.
- 같은 호스트가 NIC 여러 개로 응답하면 **IP 목록을 합쳐** 카드 한 장으로 보여 준다.
- 이번 탐색에 **응답하지 않은 기존 카드**는 “미응답” 표시를 남긴다(카드는 유지).

### 2. 호스트 정보·모니터링

- **내 정보 / 원격 카드**: CPU·메모리·버전, IP, HTTP 헬스, `systemctl status` 요약.
- 원격은 **HTTP 헬스 폴링**으로 도달 가능 여부를 표시; 불안정·미응답 호스트는 위험한 조작(업데이트·재시작)을 막는다.

### 3. 버전·업데이트

- **번들(tar.gz)** 업로드 → **스테이징** → **적용** → `versions/`·`current`/`previous` 포인터 갱신.
- **control / compute** 두 종류 바이너리가 번들에 있을 수 있음.
- 기본은 **기존 config 재사용**; 필요 시 번들 안 config 사용.
- 적용 실패 시 내장 **rollback**으로 이전 버전 복구 시도.
- **설치된 버전 목록**, **특정 버전으로 전환**, **수동 롤백** 지원.

### 4. 설정(current config)

- 호스트별 **현재 config** 조회·편집·저장.
- orchestrator의 로컬 config를 **원격 전체에 일괄 복사** 가능.

### 5. 서비스 제어

- 로컬·원격 **서비스 상태** 조회, **재시작**, (원격) **시작/중지**(SSH).
- **원격 일괄 재시작** — Discovery로 찾은 호스트에 한 번에 적용.

### 6. 일괄 원격 작업 (웹 사이드바·CLI 4종)

한 orchestrator에서 Discovery 후 아래를 **모든(또는 화면에 있는) 원격**에 실행한다.

| 작업 | 요약 |
|------|------|
| 설정 일괄 복사 | 로컬 current config → 원격 |
| 일괄 재시작 | 원격 `contrabass-mole.service` 재시작 |
| 일괄 업데이트 | 스테이징 번들을 원격에 적용 |
| 일괄 롤백 | 원격을 `previous` 버전으로 |

결과는 **호스트별 진행·성공/실패**를 NDJSON·모달로 확인한다.

### 7. CLI·REPL (터미널)

서비스 없이도 쓸 수 있는 **일회성 명령**과, **`contrabass-moleU agent`만** 실행하면 들어가는 **대화형 REPL**이 있다.

- Discovery, host-info, 단일 호스트 업데이트·버전 전환  
- REPL: `discover` 후 캐시된 목록으로 bulk 명령 (재-discovery 없음)  
- 일괄 4종 CLI: orchestrator `-cfg` 서비스가 떠 있어야 함  

자동화·스크립트·SSH 세션 안에서 웹과 **동일 API**를 호출하는 용도에 맞다.

---

## 배포·디렉터리 (개념만)

호스트마다 대략 다음 구조를 쓴다 (기본 `/var/lib/contrabass/mole`).

- **`current`** — 지금 돌아가는 버전  
- **`previous`** — 직전 버전 (롤백용)  
- **`staging/`** — 업로드만 된 버전 (적용 전)  
- **`versions/<버전키>/`** — 설치된 각 버전의 파일  
- **`update_history.log`** — 적용·롤백·일괄 작업 요약 기록  

**최초 설치**는 Ubuntu용 install 스크립트 + manifest 번들로 한 번에 깔고, 이후는 **웹/CLI 업데이트**로 버전을 올린다.

---

## 운영자 관점 사용 흐름

**일상 점검**

1. orchestrator에서 `contrabass-moleU -cfg …` 로 서비스 기동  
2. 브라우저로 웹 UI 접속 → **Discovery** → 카드에서 버전·헬스 확인  

**한 호스트 업데이트**

1. 번들 tar.gz 업로드 (웹 또는 CLI)  
2. 대상 호스트 카드에서 **업데이트 적용** (config 재사용 여부 선택)  
3. 업데이트 기록·서비스 상태 확인  

**여러 호스트 한꺼번에**

1. Discovery로 목록 확보  
2. 사이드바 **일괄 작업** (설정 복사 / 재시작 / 업데이트 / 롤백)  
3. 「결과 보기」로 호스트별 성공·실패 확인  

**터미널 선호**

- `agent --discovery`, `agent --apply-update`, 일괄 `*all-remotes`  
- 또는 `agent` → REPL에서 `discover` → `apply-update-all ./dist/….tar.gz`  

---

## 기술 스택 (한 줄)

- **Go** 단일 바이너리, 표준 **net/http**, **UDP Discovery**, embed **웹(HTML/JS/CSS)**  
- 선택적으로 호스트 앱에 **Gin**을 붙여 maintenance를 **리버스 프록시**  
- **Linux** (`/proc` 등), **systemd** 연동  

---

## 관련 문서 (상세는 여기서)

| 문서 | 내용 |
|------|------|
| **[README.md](../README.md)** | 빌드·실행·설치·CLI 요약 |
| **[PRD.md](../PRD.md)** | 제품 요구사항·동작 규칙 전체 |
| **[docs/CLI.md](./CLI.md)** | 일회성 `agent --…` 명령 |
| **[docs/REPL.md](./REPL.md)** | 대화형 REPL |
| **[docs/REST_API.md](./REST_API.md)** | HTTP API 경로·요청·응답 |
| **[CHANGELOG.md](../CHANGELOG.md)** | 기능별 변경 이력 |

---

## 한 문장 요약

**Mole Agent**는 여러 Linux 호스트에 깔린 **단일 Go 에이전트**를 **UDP Discovery로 찾고**, **웹·CLI·REPL**로 **버전 배포·롤백·설정·서비스**를 **로컬·원격·일괄**로 운영하기 위한 시스템이다.
