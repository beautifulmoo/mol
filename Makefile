# contrabass-moleU 빌드
# 소스 수정 후 터미널에서 make (또는 make build) 실행하면 contrabass-moleU 실행 파일이 생성됩니다.
# 자동 빌드(저장 시 빌드)는 없습니다. 수정 후 반드시 make 를 실행하세요.
#
# Version key (full `git describe --tags --long --always`) is injected as main.VersionKey; see scripts/build-version.sh.
# Override: make build VERSION_KEY=0.4.4-4-gabc1234

VERSION_KEY ?= $(shell ./scripts/build-version.sh)

.PHONY: build
build: internal/updatescripts/update.sh internal/updatescripts/rollback.sh
	go build -o contrabass-moleU -ldflags "-X main.VersionKey=$(VERSION_KEY)" .

# 바이너리에 내장되는 스크립트 — 루트의 update.sh / rollback.sh 와 동기화됨
internal/updatescripts/update.sh: update.sh
	cp -f $< $@

internal/updatescripts/rollback.sh: rollback.sh
	cp -f $< $@

# make 만 입력해도 build 가 실행됨
.DEFAULT_GOAL := build
