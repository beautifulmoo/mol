package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"contrabass-agent/maintenance"
	"contrabass-agent/maintenance/config"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// VersionKey is the full agent version key "<semver>-<patch>" from git describe at build time (see Makefile, maintenance/scripts/build-version.sh).
var VersionKey string

func configPathFromArgs(args []string) string {
	return maintenance.ConfigPathForServiceMode(args)
}

// ginProxyConfig loads Maintenance.WebPrefix, APIPrefix, ports for the outer Gin (Server.HTTPPort → maintenance proxy).
// When -cfg is absent or load fails, defaults match the previous hardcoded behavior (8888 / 8889, /web, /api/v1).
func ginProxyConfig(args []string) *config.Config {
	path := configPathFromArgs(args)
	if path == "" {
		c := config.Default()
		c.MaintenancePort = 8889
		c.ServerHTTPPort = 8888
		return &c
	}
	cfg, err := config.Load(path)
	if err != nil {
		log.Printf("gin: config %q: %v — using default prefixes and 8888/8889 for proxy", path, err)
		c := config.Default()
		c.MaintenancePort = 8889
		c.ServerHTTPPort = 8888
		return &c
	}
	return cfg
}

func MyGIN(cfg *config.Config) *gin.Engine {
	engine := gin.Default()
	engine.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"*"},
		AllowHeaders: []string{"*"},
	}))

	// WebPrefix, APIPrefix → maintenance (MaintenancePort)로 프록시.
	// 브라우저는 Server.HTTPPort origin 기준으로 APIPrefix를 호출하므로 API도 같이 넘긴다.
	maintenance.RegisterMaintenanceProxy(engine, cfg)

	serviceGroup := routerGroupJSON(engine, "/c-agent/service")
	apiGroupV1 := serviceGroup.Group("/api/v1")
	apiGroupV1.GET("/test", TestGETWeb)

	return engine
}

func routerGroupJSON(r *gin.Engine, prefix string) *gin.RouterGroup {
	g := r.Group(prefix)
	g.Use(func(c *gin.Context) {
		c.Header("Content-Type", "application/json")
		c.Next()
	})
	return g
}

func TestGETWeb(c *gin.Context) {
	responseString := `{"message" : "This is JSON string for GET request"}`

	c.Header("Content-Type", "application/json")
	c.String(http.StatusOK, responseString)
}

func main() {
	// Gin은 `-cfg <파일>`(또는 레거시 `agent -cfg <파일>`) 서비스 모드에서만 띄운다. agent --nic-brd 등은 Gin을 바인딩하지 않는다.
	if maintenance.ShouldStartGinReverseProxy(os.Args) {
		gcfg := ginProxyConfig(os.Args)
		httpPort := gcfg.ServerHTTPPort
		if httpPort <= 0 {
			httpPort = 8888
		}
		go func() {
			router := MyGIN(gcfg)
			addr := fmt.Sprintf("0.0.0.0:%d", httpPort)
			if err := router.Run(addr); err != nil {
				log.Printf("gin: %v", err)
			}
		}()
	}

	os.Exit(maintenance.Run(VersionKey, os.Args))
}
