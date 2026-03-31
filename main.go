package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"mol/maintenance"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// maintenanceProxyTarget is the base URL of the maintenance HTTP server (MaintenancePort in config).
// Server-side Gin listen address is configured separately (추후 Server 설정에서 읽도록 변경 예정).
const maintenanceProxyTarget = "http://127.0.0.1:8889"

// Version is set at build time: -ldflags "-X main.Version=1.2.3"
var Version string

func newMaintenanceWebProxy() http.Handler {
	target, err := url.Parse(maintenanceProxyTarget)
	if err != nil {
		panic(err)
	}
	return httputil.NewSingleHostReverseProxy(target)
}

func MyGin() *gin.Engine {
	engine := gin.Default()
	engine.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"*"},
		AllowHeaders: []string{"*"},
	}))

	// /web, /api/v1 → maintenance (MaintenancePort)로 프록시.
	// 브라우저는 8888 origin 기준으로 /api/v1 을 호출하므로 API도 같이 넘겨야 한다.
	proxy := newMaintenanceWebProxy()
	engine.Any("/web", gin.WrapH(proxy))
	engine.Any("/web/*filepath", gin.WrapH(proxy))
	engine.Any("/api/v1", gin.WrapH(proxy))
	engine.Any("/api/v1/*path", gin.WrapH(proxy))

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
	// Gin(8888 등): /web 은 maintenance(8889)로 프록시. maintenance 서버는 -config 로 기동.
	go func() {
		router := MyGin()
		// Server listen 주소는 추후 config Server 항목에서 읽도록 할 예정 — 현재 하드코딩.
		if err := router.Run("0.0.0.0:8888"); err != nil {
			log.Printf("gin: %v", err)
		}
	}()

	os.Exit(maintenance.Run(Version, os.Args))
}
