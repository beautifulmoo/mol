package main

import (
	"fmt"
	"log"
	"os"

	"contrabass-agent/maintenance"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// VersionKey is the full agent version key "<semver>-<patch>" from git describe at build time (see Makefile, maintenance/scripts/build-version.sh).
var VersionKey string

// BuildVariant distinguishes control vs compute binaries. Injected at build time via -ldflags "-X main.BuildVariant=control|compute".
var BuildVariant string

func MyGIN() *gin.Engine {
	engine := gin.Default()
	// JSON Content-Type 은 전역(engine.Use)에 두지 않는다. HTML/CSS/JS(예: /maintenance 프록시)까지
	// application/json 으로 덮어쓰면 브라우저가 스타일시트·문서로 처리하지 못한다.
	// API 전용 그룹에는 routerGroupJSON(engine, "/prefix") 처럼 그룹에만 미들웨어를 건다.
	engine.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"*"},
		AllowHeaders: []string{"*"},
	}))

	return engine
}

func main() {
	// 바깥 Gin(Server.HTTPPort)은 `<bin> -cfg <파일>`(IsServiceModeRootCfg)일 때만 띄운다.
	// `<bin> agent -cfg <파일>` 로도 Run() 안에서 HTTP+Discovery는 기동하지만, 여기서는 Gin을 바인딩하지 않는다.
	// `<bin> agent --nic-brd` 등은 IsAgentSubcommand 만 참이고 rootCfg 는 거짓이므로 Gin 없음.
	rootCfg := maintenance.IsServiceModeRootCfg(os.Args)
	agentCfg := maintenance.IsAgentSubcommand(os.Args)
	if agentCfg {
		os.Exit(maintenance.Run(VersionKey, BuildVariant, os.Args))
	}
	if !rootCfg {
		os.Exit(maintenance.Run(VersionKey, BuildVariant, os.Args))
	}

	router := MyGIN()
	gcfg := maintenance.GinProxyConfig(os.Args)
	httpPort := gcfg.ServerHTTPPort
	if httpPort <= 0 {
		httpPort = 8888
	}
	// WebPrefix, APIPrefix → maintenance (MaintenancePort)로 프록시.
	// 브라우저는 Server.HTTPPort origin 기준으로 APIPrefix를 호출하므로 API도 같이 넘긴다.
	maintenance.RegisterMaintenanceProxy(router, gcfg)

	// 병합 호스트와 동일한 패턴: Gin은 메인 고루틴에서 블로킹, maintenance는 별도 고루틴.
	go func() {
		os.Exit(maintenance.Run(VersionKey, BuildVariant, os.Args))
	}()

	addr := fmt.Sprintf("0.0.0.0:%d", httpPort)
	err := router.Run(addr)
	if err != nil {
		log.Printf("gin: %v", err)
	}
	os.Exit(1)
}
