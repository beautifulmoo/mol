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

- **update.sh**: 웹 UI에서 “업데이트 적용” 시 mol 이 `sudo systemd-run ... {deploy_base}/update.sh {버전}` 형태로 실행한다. 인자로 **버전 하나**를 받으며, 실행 시점에 `{deploy_base}/versions/{버전}/mol` 이 있어야 한다.  
  업로드는 먼저 **스테이징** `{deploy_base}/staging/{버전}/` 에 저장된다(실행 중인 경로를 덮어쓰지 않아 text file busy 를 피함). 로컬 적용 시 스테이징 → versions 복사 후 update.sh 를 실행하고, 성공하면 해당 버전 스테이징을 삭제한다. 원격 적용은 스테이징 또는 versions 에 있는 파일을 그대로 사용한다.
- **rollback.sh**: 업데이트 후 서비스가 기동에 실패하면 update.sh 가 자동으로 이 스크립트를 호출해 이전 버전으로 되돌린다. 수동 롤백이 필요할 때는 배포 베이스에서 직접 실행하면 된다.
  - 예: `sudo /opt/mol/rollback.sh`
  - `{deploy_base}/previous` 심볼릭 링크가 있어야 하며(최소 한 번 업데이트가 된 뒤에만 유효), 없으면 “no previous version”으로 종료된다.

## 실행

```bash
./mol
# 또는 설정 파일 지정
./mol -config /path/to/config.yaml
# 또는 환경변수
MOL_CONFIG=/path/to/config.yaml ./mol
```

## 접속

- 웹 UI: http://localhost:8888/web/index.html
- API: http://localhost:8888/api/v1/self, http://localhost:8888/api/v1/discovery

## 설정

`config.yaml` (또는 `MOL_CONFIG`로 지정한 경로). 항목은 PRD §7 참고.

- `discovery_broadcast_address`: broadcast IP (예: 192.168.0.255)
- `discovery_udp_port`: 9999
- `http_port`: 8888
- `discovery_timeout_seconds`: 10
- `discovery_deduplicate`: true
- `version`: 비우면 빌드 시 ldflags 값 사용
- `systemctl_service_name`: (선택) 서비스 상태 조회용, 기본 `mol.service`
- `ssh_user`: (선택) 발견된 호스트 SSH 사용자, 기본 `kt`

### 웹에서 systemctl status 표시내 정보·발견된 호스트 카드에 `systemctl status mol.service` 결과를 표시한다.  
원격 호스트는 `ssh <ssh_user>@<host_ip> "sudo systemctl status <systemctl_service_name>"` 로 조회한다.  
원격에서 비밀번호 없이 동작하려면: SSH는 공개키 인증, sudo는 sudoers에서 NOPASSWD가 필요하다.  
(예: `kt ALL=(ALL) NOPASSWD: ALL` 이 있으면 별도 항목 없이 동작한다.)

**Permission denied (publickey) 가 나올 때**  
mol을 systemd 서비스로 돌리면 **실행 사용자**가 터미널의 kt와 다를 수 있다(예: root).  
그러면 ssh가 `kt` 의 `~/.ssh/` 키를 찾지 못해 공개키 인증에 실패한다.

- **방법 1**: 서비스 유닛에서 **User=kt** 로 실행하도록 설정. (kt 의 홈·키를 그대로 사용.)
- **방법 2**: 서비스를 root 등 다른 사용자로 유지할 경우, `config.yaml` 에 **ssh_identity_file** 을 둔다.  
  예: `ssh_identity_file: "/home/kt/.ssh/id_rsa"` (root가 읽을 수 있게 권한 조정) 또는 전용 키를 `/etc/mol/id_rsa` 등에 두고 그 공개키를 원격의 `authorized_keys` 에 등록한 뒤 `ssh_identity_file: "/etc/mol/id_rsa"` 로 지정.
