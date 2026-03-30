# 변경 이력 (mol)

## 레이아웃

- **`maintenance/`**: `maintenance.go`에 **`Run(binVersion, args []string) int`**(서비스·CLI 진입; `args`는 보통 `os.Args`), `discovery`, `discoverycli`(`Run`: `--discovery` CLI, 반환 코드), `hostinfo`, `server`, `svcstatus`, `web` 패키지가 여기에 있다. 루트 **`main.go`** 는 `maintenance.Run(Version, os.Args)` 후 **`os.Exit`** 만 수행한다. Go import는 `mol/maintenance/<패키지>` 형태.
- **`internal/config/`**: YAML 설정 로드·검증(`Config`, `Load`, `LoadFromBytes` 등). 구현 파일은 `configFile2.go`. Go import는 `mol/internal/config`.

## Discovery / CLI (최근)

### `mol --discovery` (설정 파일 없이 UDP Discovery만)

- **`reply_udp_port`**: `DISCOVERY_REQUEST` JSON에 로컬 바인드 포트를 넣고, 원격은 응답을 **그 포트**로 유니캐스트한다. UDP 소스 포트가 잘못 보이는 환경에서도 동작하도록 함.
- **다중 NIC**: 서비스 mol과 동일하게, brd 서브넷에 맞춰 **인터페이스별 `로컬IP:src-port` UDP 소켓**을 열어 브로드캐스트를 보냄 (`discovery.OpenDiscoveryClientUDP`, `SendDiscoveryClientBroadcast`).
- **시작 시 출력**: 사용하는 **브로드캐스트(brd) 주소**를 모두 한 줄씩 출력.
- **결과**: `[...]` 안에는 **응답한 IP**(`responded_from_ip`, UDP 패킷 실제 발신지)만 표시. 접속 가능 주소와 맞춤.
- **`[Local]` / `[Remote]`**: (1) 로컬 `hostinfo`의 CPU UUID와 응답 `cpu_uuid` 일치(대소문자 무시) → Local. (2) 아니면 **응답한 IP**가 이 머신의 IPv4와 겹치면 Local(보조).
- **UX**: 같은 줄 `Discovering ... N` 카운트다운 후 `Discovery Done.`(이전 줄 덮어쓰기).

### 서비스 (HTTP + Discovery)

- Discovery UDP listen을 **`udp4`**로 통일(IPv4 sockaddr 일관성).
- **`LocalIPsInSubnet`** export, 브로드캐스트 송신 시 매칭 소켓 없으면 `conns[0]` 폴백.
- UDP **`DISCOVERY_RESPONSE`에는 `host_ips` 배열을 넣지 않음**(HTTP `/self` 등에서만).

### 웹·로그·배포 (최근)

- **SSE** `event: discoveryfail` + `data.message`, 실패 시 **`discovery: ERROR:`** 한 줄 로그(`journalctl` 검색).
- **DISCOVERY_REQUEST** JSON은 마샬 후 **1300바이트 미만** 검증(UDP·MTU).
- **`internal/updatescripts/`** 에 `update.sh`·`rollback.sh` 임베드(`Makefile` 동기화), 배포는 `{base}/current/` 스크립트 실행.
- 설정 **`discovery_service_name`**, 스테이징/업데이트용 **`patch_version`**·버전키(`version_patch` 등) 비교.
- 저장소 정책: Go **`*_test.go`** 는 트리에 두지 않음(상세는 PRD §1).

상세 스펙은 **[PRD.md](PRD.md)** §3, CLI 사용은 **[README.md](README.md)** 를 참고한다.
