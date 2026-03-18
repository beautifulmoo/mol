# mol

단일 실행 파일로 동작하는 Discovery + 웹 UI (Go, net/http, UDP broadcast).

## 요구 사항

- Go 1.21+
- Linux (호스트 정보는 `/proc` 기반)

## 빌드

**웹 페이지(web/)는 빌드 시 바이너리에 포함(embed)됩니다.** 배포 시 실행 파일과 config.yaml만 옮기면 됩니다.

**소스 수정 후**: 저장만으로는 자동 빌드되지 않습니다. 터미널에서 아래 중 하나를 실행하세요.

```bash
cd ~/work/mol
make
```

또는 (Make 없이):

```bash
cd ~/work/mol
go build -o mol -ldflags "-X main.Version=1.0.0" .
```

- 반드시 **web/ 디렉터리가 있는 프로젝트 루트**에서 빌드할 것. 그래야 `web/index.html`, `web/app.js`, `web/style.css` 가 바이너리에 들어갑니다.
- 버전을 넣어 빌드하려면: `make build VERSION=0.0.2` 또는 `go build -o mol -ldflags "-X main.Version=0.0.2" .`

## 배포

- **mol** 실행 파일 + **config.yaml** 만 대상 호스트로 복사하면 됨.
- web/ 디렉터리는 필요 없음 (이미 바이너리 안에 포함됨).

### 업데이트·롤백 스크립트 (update.sh, rollback.sh)

프로젝트 루트에 **update.sh**, **rollback.sh** 가 참고용으로 포함되어 있다. 웹 UI의 “업데이트 적용” 기능을 쓰려면 이 스크립트들을 **배포 베이스 디렉터리**에 두어야 한다.

- **위치**: 설정 `deploy_base`(기본값 `/opt/mol`) 아래에 두 파일을 복사한다.
  - `{deploy_base}/update.sh`
  - `{deploy_base}/rollback.sh`
- 예: `/opt/mol` 를 쓰는 경우
  - `/opt/mol/update.sh`, `/opt/mol/rollback.sh` 로 복사 후 실행 권한 부여 (`chmod +x`).
- 스크립트 안의 `BASE`(또는 경로)가 실제 배포 경로와 같아야 한다. 기본값은 `/opt/mol` 이다. `deploy_base` 를 다르게 쓰면 스크립트 내부 경로를 그에 맞게 수정해야 한다.

**사용 방법**

- **update.sh**: 웹 UI에서 “업데이트 적용” 시 mol 이 `systemd-run ... {deploy_base}/update.sh {버전}` (mol.service는 root 실행, sudo 없음) 형태로 실행한다. 인자로 **버전 하나**를 받으며, 실행 시점에 `{deploy_base}/versions/{버전}/mol` 이 있어야 한다.  
  업로드는 **스테이징** `{deploy_base}/staging/{버전}/` 에만 저장된다(실행 중인 경로를 덮어쓰지 않아 text file busy 를 피함). 로컬 적용 시 스테이징 → versions 복사 후 update.sh 를 실행한다. 스테이징은 자동 삭제하지 않고 남겨 두어 같은 버전으로 원격 업데이트를 할 수 있게 하며, 삭제는 웹의 「업로드된 버전 삭제」로 수동 처리한다. 원격 적용은 스테이징 또는 versions 에 있는 파일을 그대로 사용한다.
- **rollback.sh**: 업데이트 후 서비스가 기동에 실패하면 update.sh 가 자동으로 이 스크립트를 호출해 이전 버전으로 되돌린다. 수동 롤백이 필요할 때는 배포 베이스에서 직접 실행하면 된다.
  - 예: `/opt/mol/rollback.sh` (root 또는 동일 권한으로 실행)
  - `{deploy_base}/previous` 심볼릭 링크가 있어야 하며(최소 한 번 업데이트가 된 뒤에만 유효), 없으면 “no previous version”으로 종료된다.

## 실행

```bash
./mol
# 설정 파일 지정
./mol -config /path/to/config.yaml
# 환경변수
MOL_CONFIG=/path/to/config.yaml ./mol
```

**CLI**

| 옵션 | 설명 |
|------|------|
| (인자 없음) | HTTP 서버 + UDP Discovery 기동 |
| `-h`, `--help` | 사용법 출력 |
| `--version`, `-version` | 빌드 버전 한 줄 출력 후 종료 |
| `--nic-brd` | Discovery에 쓰는 것과 동일 규칙으로 `(인터페이스 : 브로드캐스트 주소)` 출력 후 종료(확인용) |

## 접속

- 웹 UI: http://localhost:8888/web/index.html
- API: http://localhost:8888/api/v1/self, http://localhost:8888/api/v1/discovery

## 설정

`config.yaml` (또는 `MOL_CONFIG`). 상세·전체 항목은 **[PRD.md](PRD.md)** §7.

- **Discovery 브로드캐스트**: 기본은 **NIC에서 brd 자동 수집**(bonding·bridge·vlan 등 포함, `mol --nic-brd`로 확인). 수집이 비어 있을 때만 `discovery_broadcast_address`(단일) 사용, 그다음 `255.255.255.255`. `discovery_broadcast_addresses` 복수 설정은 사용하지 않음.
- `discovery_udp_port`: 9999 · `http_port`: 8888 · `discovery_timeout_seconds` · `discovery_deduplicate`
- `deploy_base` / `install_prefix`(비우면 deploy_base): 스테이징·versions·update.sh 경로
- `version`: 비우면 ldflags 빌드 버전
- `systemctl_service_name`: 기본 `mol.service`
- **SSH** (`ssh_port` 기본 22, `ssh_user` 기본 **root**): 원격 호스트의 **서비스 시작/중지**만 SSH. **상태 조회·재시작**은 원격 mol **HTTP API**(8888)를 통해 처리한다.

### 웹에서 systemctl status

로컬·원격 호스트 카드에 `systemctl status` 결과를 표시한다.

- **로컬**: mol이 직접 `systemctl status`(sudo 없음, 보통 root 서비스).
- **원격 상태**: 중앙 mol이 **원격 mol의 API**로 조회한다. SSH 불필요.
- **원격 시작/중지**: `ssh -p <ssh_port> <ssh_user>@<host_ip> systemctl start|stop …` — 키 기반 인증 필요.

**SSH Permission denied (publickey)**  
mol 프로세스 사용자(예: root)의 `~/.ssh`(또는 해당 사용자로 `ssh`가 쓸 수 있는 키)가 원격 `authorized_keys`와 맞아야 한다. 서비스를 특정 사용자로 돌리면 그 사용자 홈의 SSH 키가 사용된다.

---

자세한 동작(Discovery 메시지, 업데이트 API, 웹 UI 흐름 등)은 **PRD.md** 를 본다.
