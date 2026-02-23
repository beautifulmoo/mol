# mol 빌드
# 소스 수정 후 터미널에서 make (또는 make build) 실행하면 mol 실행 파일이 생성됩니다.
# 자동 빌드(저장 시 빌드)는 없습니다. 수정 후 반드시 make 를 실행하세요.

VERSION ?= 0.0.0

.PHONY: build
build:
	go build -o mol -ldflags "-X main.Version=$(VERSION)" .

# make 만 입력해도 build 가 실행됨
.DEFAULT_GOAL := build
