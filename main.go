package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"contrabass-agent/maintenance"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// VersionKey is the full agent version key "<semver>-<patch>" from git describe at build time (see Makefile, maintenance/scripts/build-version.sh).
var VersionKey string

func MyGIN() *gin.Engine {
	engine := gin.Default()
	engine.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"*"},
		AllowHeaders: []string{"*"},
	}))

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
	// 바깥 Gin(Server.HTTPPort)은 `<bin> -cfg <파일>`(IsServiceModeRootCfg)일 때만 띄운다.
	// `<bin> agent -cfg <파일>` 로도 Run() 안에서 HTTP+Discovery는 기동하지만, 여기서는 Gin을 바인딩하지 않는다.
	// `<bin> agent --nic-brd` 등은 IsAgentSubcommand 만 참이고 rootCfg 는 거짓이므로 Gin 없음.
	rootCfg := maintenance.IsServiceModeRootCfg(os.Args)
	if rootCfg {
		gcfg := maintenance.GinProxyConfig(os.Args)
		httpPort := gcfg.ServerHTTPPort
		if httpPort <= 0 {
			httpPort = 8888
		}
		go func() {
			router := MyGIN()
			// WebPrefix, APIPrefix → maintenance (MaintenancePort)로 프록시.
			// 브라우저는 Server.HTTPPort origin 기준으로 APIPrefix를 호출하므로 API도 같이 넘긴다.
			maintenance.RegisterMaintenanceProxy(router, gcfg)
			addr := fmt.Sprintf("0.0.0.0:%d", httpPort)
			if err := router.Run(addr); err != nil {
				log.Printf("gin: %v", err)
			}
		}()
	}

	os.Exit(maintenance.Run(VersionKey, os.Args))
}
