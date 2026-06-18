# Mole Agent — 대화형 REPL

**`contrabass-moleU agent`**(인자 없음)로 진입하는 대화형 셸이다. **`-cfg` 서비스 기동·웹 UI는 대상이 아니다.** 일회성 `agent --…` CLI와 같은 작업을 프롬프트에서 반복 실행한다.

일회성 CLI 옵션(`--discovery`, `--host-info`, 리모트 일괄 4종 등)은 **[CLI.md](./CLI.md)** 를 본다. HTTP API는 **[REST_API.md](./REST_API.md)**.

구현: `maintenance/replcli`.

---

## 진입·종료

```text
contrabass-moleU agent
contrabass-moleU agent repl          # 동일 (별칭)
contrabass-moleU agent -h            # REPL이 아니라 agent CLI 도움말(일회성 옵션 목록)
```

| 항목 | 설명 |
|------|------|
| **프롬프트** | `Mole-Agent>` |
| **종료** | `exit` 또는 `quit` |
| **도움말** | `help` — 전체 목록. `help <명령>` — 해당 명령만(예: `help discover`) |
| **버전** | `version` — 빌드 버전 한 줄 |

---

## REPL vs 일회성 CLI

| 항목 | REPL | `agent --…` (일회성) |
|------|------|----------------------|
| **Discovery** | `discover` 후 **캐시**; bulk는 **재-discovery 없음**; `host-info`는 캐시로 IP 보강(없으면 UDP) | bulk마다 내부 discovery |
| **기본값** | `set apiprefix` 등 **세션** 유지 | 매 명령 플래그 |
| **종료** | `exit` / `quit` | 프로세스 종료 |
| **입력** | TTY: **↑/↓** 명령 히스토리, **Tab** 완성 | — |

---

## 입력 (TTY)

### 명령 히스토리

- **↑/↓** 로 이전·다음 명령 탐색.
- 히스토리 파일: **`$XDG_CACHE_HOME/<BinaryName>/repl_history`** (기본 `BinaryName` = `contrabass-moleU`).
- **파이프·리다이렉트** 입력 시 readline 미사용(히스토리·Tab 없음).

### Tab 완성 (bash 유사)

readline 기반. **Tab 한 번** = 공통 접두어까지 확장(후보 1개면 끝까지). **Tab 두 번** = 후보 목록.

| 위치 | 완성 대상 |
|------|-----------|
| 명령어 | `help`, `discover`, `host-info`, `push-config-all` 등 |
| `help` | help topic·명령 이름 |
| `set` | `apiprefix`, `maintenance-port`, `agent-variant`, `use-bundle-config` 및 값 |
| `discover` | `--dest-port=`, `--src-port=`, `--timeout=`, `--service=` |
| `host-info` 등 | `self`, `local`, **`discover` 캐시 IP** |
| `apply-update` / `apply-update-all` | **로컬 파일 경로** — `./dist/…`, `~/…`, 절대 경로. 디렉터리는 `/` 접미 |

**작업 디렉터리**: REPL은 셸과 같이 **프로세스 시작 시점의 CWD**를 쓴다. `~/work/mol$ ./contrabass-moleU agent` 이면 `./dist/…` 는 `~/work/mol/dist/…` 이다. 실행 시 **`~`는 자동 확장**된다.

파이프 입력 시 Tab 미지원.

---

## 세션

`set` / `show` / `hosts` / `clear-hosts` 로 세션·Discovery 캐시를 관리한다.

### `set <key> <value>`

| 키 | 기본·설명 |
|----|-----------|
| **`apiprefix`** | 기본 `/maintenance/api/v1` — 단일 호스트 명령의 Gin API prefix |
| **`maintenance-port`** | 기본 **8889** — bulk 명령의 로컬 maintenance HTTP |
| **`agent-variant`** | `control` \| `compute` — `apply-update` / `apply-update-all` (미설정 시 CLI 빌드 variant 또는 `compute`) |
| **`use-bundle-config`** | `on` \| `off` — 기본 **off**(각 원격 **current config** 재사용). `on`이면 번들 config |

### `show`

현재 세션 설정과 **캐시된 호스트 수**, 마지막 `discover` 응답 건수를 출력한다.

### `hosts` / `clear-hosts`

- **`hosts`**: 마지막 `discover`의 `primary_ip`, `hostname`, `cpu_uuid`, `ips[]` 표.
- **`clear-hosts`**: Discovery 캐시 비우기.

---

## Discovery

### `discover [flags]`

UDP Discovery만 수행(오케스트레이터 `-cfg` **불필요**). 출력 형식·IP 규칙은 **`agent --discovery`** 와 동일 — **[CLI.md](./CLI.md)** 「`--discovery`」.

| 플래그 | 기본 |
|--------|------|
| `--dest-port` | `9999` |
| `--src-port` | `9998` |
| `--timeout` | `10` (초) |
| `--service` | `Mole-Discovery` |

성공 시 원격 호스트를 **메모리에 캐시**하고 `Cached N remote host(s) for bulk commands.` 를 출력한다.

### `nic-brd`

CLI **`agent --nic-brd`** 와 동일 — Discovery용 `(인터페이스 : brd)` 한 줄씩.

---

## 단일 호스트 명령

대상 **Gin HTTP**(기본 `127.0.0.1:8888` 또는 원격 IP). **`self`** / **`local`** 동의어.

| 명령 | 설명 |
|------|------|
| **`host-info <self\|local\|ip>`** | `GET …/self` 표 출력. **`HOST_IP`** / **`HOST_IPS`** 는 `discover` 캐시 우선, 없으면 UDP ~3s 보강 |
| **`versions-list <self\|local\|ip>`** | `GET …/versions/list` |
| **`versions-switch <self\|local\|ip> <version-key>`** | `POST …/versions/switch-current` |
| **`apply-update <self\|local\|ip> <bundle.tar.gz>`** | 업로드 + 적용. 번들 경로는 **`~/…`·`./`·상대/절대** (CWD 기준, 실행 시 `~` 확장). 세션 `agent-variant`·`use-bundle-config` |

대상 에이전트 HTTP 서비스가 떠 있어야 한다. 상세는 **[CLI.md](./CLI.md)** 해당 절.

---

## 리모트 일괄 명령 (bulk)

로컬 **maintenance HTTP**(`maintenance-port`, 기본 **8889**)가 떠 있어야 한다 — orchestrator **`contrabass-moleU -cfg …`** 서비스.

| REPL 명령 | 대응 일회성 CLI | API |
|-----------|-----------------|-----|
| **`push-config-all`** | `--push-config-all-remotes` | `POST …/current-config/push-local-all` |
| **`restart-all`** | `--restart-all-remotes` | `POST …/service-control/restart-all` |
| **`apply-update-all <bundle>`** | `--apply-update-all-remotes` | 업로드 → `POST …/apply-update-all`. 번들 경로 규칙은 `apply-update` 와 동일 |
| **`rollback-all`** | `--rollback-all-remotes` | `POST …/versions/rollback-all` |

### bulk 동작 요약

1. **`discover`를 먼저 실행**해 호스트를 캐시한다.
2. bulk는 **캐시된 `hosts[]`만** 사용 — **재-discovery 없음**.
3. `hosts[]`의 `ips[]`·`primary_ip` 규칙은 일회성 CLI·웹 UI와 동일 — **[CLI.md](./CLI.md)** 「리모트 일괄 CLI (공통)」·**[REST_API.md](./REST_API.md)**.

일회성 CLI는 명령마다 내부 Discovery 1회를 수행한다. REPL은 이 단계를 `discover`에 맡긴다.

---

## 명령 목록 (요약)

```text
Session     set, show, hosts, clear-hosts
Meta        help, version, exit, quit
Discovery   discover, nic-brd
Single-host host-info, versions-list, versions-switch, apply-update
Bulk        push-config-all, restart-all, apply-update-all, rollback-all
```

REPL 안에서 `help` / `help <명령>` 으로 영문 상세 도움말을 볼 수 있다.

---

## 관련 문서

| 문서 | 내용 |
|------|------|
| **[CLI.md](./CLI.md)** | 일회성 `agent --…` 옵션, 리모트 일괄 CLI 공통 규칙 |
| **[REST_API.md](./REST_API.md)** | maintenance HTTP API·`hosts[]` body |
| **[PRD.md](../PRD.md)** | §4.1.2 REPL 요구사항 요약 |
