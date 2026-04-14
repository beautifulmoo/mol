package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"contrabass-agent/internal/config"
	"contrabass-agent/maintenance"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// VersionKey is the full agent version key "<semver>-<patch>" from git describe at build time (see Makefile, scripts/build-version.sh).
var VersionKey string

func configPathFromArgs(args []string) string {
	if len(args) >= 3 && args[1] == "-cfg" {
		return args[2]
	}
	return ""
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

func normalizeURLPathPrefix(p, fallback string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		p = fallback
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

func newMaintenanceWebProxy(cfg *config.Config) http.Handler {
	port := cfg.MaintenancePort
	if port <= 0 {
		port = 8889
	}
	target, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		panic(err)
	}
	inner := httputil.NewSingleHostReverseProxy(target)
	// httputil.ReverseProxy: if Request.Form is already populated (e.g. Gin parsed the query),
	// after Director it may replace URL.RawQuery via cleanQueryParams, breaking downstream
	// handlers that read r.URL.Query(). Clone without Form and preserve RawQuery from RequestURI.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r2 := r.Clone(r.Context())
		r2.Form = nil
		r2.PostForm = nil
		if r2.URL != nil && r2.URL.RawQuery == "" && r2.RequestURI != "" {
			if i := strings.IndexByte(r2.RequestURI, '?'); i >= 0 {
				r2.URL.RawQuery = r2.RequestURI[i+1:]
			}
		}
		inner.ServeHTTP(w, r2)
	})
}

// registerMaintenanceProxy registers web + API reverse-proxy routes.
//
// Gin/httprouter forbids a catch-all (*filepath) under a prefix if that prefix already has a static
// child (e.g. /maintenance/api/... and /maintenance/*filepath cannot both exist). So when API paths
// sit under WebPrefix, we register only the web catch-all; the backend still receives /maintenance/api/v1/...
// When WebPrefix sits under APIPrefix, we register only the API catch-all.
func registerMaintenanceProxy(engine *gin.Engine, webPrefix, apiPrefix string, proxy http.Handler) {
	h := gin.WrapH(proxy)
	apiExact := apiPrefix
	apiGlob := apiPrefix + "/*path"
	webExact := webPrefix
	webGlob := webPrefix + "/*filepath"

	nestedUnder := func(longer, shorter string) bool {
		if len(longer) <= len(shorter) {
			return false
		}
		if !strings.HasPrefix(longer, shorter) {
			return false
		}
		if longer == shorter {
			return false
		}
		next := longer[len(shorter):]
		return next == "" || next[0] == '/'
	}

	switch {
	case nestedUnder(apiPrefix, webPrefix):
		// e.g. web=/maintenance, api=/maintenance/api/v1 — only one subtree; *filepath cannot sibling "api"
		engine.Any(webExact, h)
		engine.Any(webGlob, h)
	case nestedUnder(webPrefix, apiPrefix):
		// e.g. api=/api/v1, web=/api/v1/ui — entire API tree proxies web + API
		engine.Any(apiExact, h)
		engine.Any(apiGlob, h)
	default:
		// Disjoint prefixes (e.g. /web and /api/v1)
		engine.Any(webExact, h)
		engine.Any(webGlob, h)
		engine.Any(apiExact, h)
		engine.Any(apiGlob, h)
	}
}

func MyGin(cfg *config.Config) *gin.Engine {
	engine := gin.Default()
	engine.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"*"},
		AllowHeaders: []string{"*"},
	}))

	webPrefix := normalizeURLPathPrefix(cfg.WebPrefix, "/web")
	apiPrefix := normalizeURLPathPrefix(cfg.APIPrefix, "/api/v1")

	// WebPrefix, APIPrefix → maintenance (MaintenancePort)로 프록시.
	// 브라우저는 Server.HTTPPort origin 기준으로 APIPrefix를 호출하므로 API도 같이 넘긴다.
	proxy := newMaintenanceWebProxy(cfg)
	registerMaintenanceProxy(engine, webPrefix, apiPrefix, proxy)

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
	// Gin은 `-cfg <파일>` 서비스 모드에서만 띄운다. --nic-brd / --discovery 등은 설정을 읽지 않고 Gin도 바인딩하지 않는다.
	if maintenance.ShouldStartGinReverseProxy(os.Args) {
		gcfg := ginProxyConfig(os.Args)
		httpPort := gcfg.ServerHTTPPort
		if httpPort <= 0 {
			httpPort = 8888
		}
		go func() {
			router := MyGin(gcfg)
			addr := fmt.Sprintf("0.0.0.0:%d", httpPort)
			if err := router.Run(addr); err != nil {
				log.Printf("gin: %v", err)
			}
		}()
	}

	os.Exit(maintenance.Run(VersionKey, os.Args))
}
