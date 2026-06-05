# contrabass-moleU 빌드
# 소스 수정 후 터미널에서 make (또는 make build) 실행하면
# build/image/ 아래에 빌드 variant가 다른 두 바이너리가 생성됩니다:
#   contrabass-moleU-control  (BuildVariant=control)
#   contrabass-moleU-compute  (BuildVariant=compute)
# 자동 빌드(저장 시 빌드)는 없습니다. 수정 후 반드시 make 를 실행하세요.
#
# Version key (full `git describe --tags --long --always`) is injected as main.VersionKey; see maintenance/scripts/build-version.sh.
# Override: make build VERSION_KEY=0.4.4-4-gabc1234

VERSION_KEY ?= $(shell ./maintenance/scripts/build-version.sh)

OUTPUT_DIR ?= build/image
BINARY_CONTROL ?= $(OUTPUT_DIR)/contrabass-moleU-control
BINARY_COMPUTE ?= $(OUTPUT_DIR)/contrabass-moleU-compute
STRIP ?= strip

LDFLAGS_BASE = -X main.VersionKey=$(VERSION_KEY)

.PHONY: build
build: maintenance/updatescripts/update.sh maintenance/updatescripts/rollback.sh
	mkdir -p "$(OUTPUT_DIR)"
	go build -o "$(BINARY_CONTROL)" -ldflags '$(LDFLAGS_BASE) -X main.BuildVariant=control' .
	go build -o "$(BINARY_COMPUTE)" -ldflags '$(LDFLAGS_BASE) -X main.BuildVariant=compute' .
	chmod +x "$(BINARY_CONTROL)" "$(BINARY_COMPUTE)"
	$(STRIP) "$(BINARY_CONTROL)" "$(BINARY_COMPUTE)"
	cp $(BINARY_CONTROL) ./contrabass-moleU

# 바이너리에 내장되는 스크립트 — 루트의 update.sh / rollback.sh 와 동기화됨
maintenance/updatescripts/update.sh: update.sh
	cp -f $< $@

maintenance/updatescripts/rollback.sh: rollback.sh
	cp -f $< $@

# make 만 입력해도 build 가 실행됨
.DEFAULT_GOAL := build
