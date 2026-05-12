package ginproxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"contrabass-agent/maintenance/molcfg"

	"github.com/gin-gonic/gin"
)

// RegisterMaintenanceProxy registers Gin routes that reverse-proxy WebPrefix and APIPrefix
// to the local maintenance HTTP listener (MaintenancePort). It builds the httputil reverse
// proxy from cfg and applies the same prefix/nesting rules as before (see PRD: Gin → maintenance).
func RegisterMaintenanceProxy(engine *gin.Engine, cfg *molcfg.Config) {
	webPrefix := normalizeURLPathPrefix(cfg.WebPrefix, "/web")
	apiPrefix := normalizeURLPathPrefix(cfg.APIPrefix, "/api/v1")
	proxy := newMaintenanceWebProxy(cfg)
	registerMaintenanceProxyRoutes(engine, webPrefix, apiPrefix, proxy)
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

func newMaintenanceWebProxy(cfg *molcfg.Config) http.Handler {
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

// registerMaintenanceProxyRoutes registers web + API reverse-proxy routes.
//
// Gin/httprouter forbids a catch-all (*filepath) under a prefix if that prefix already has a static
// child (e.g. /maintenance/api/... and /maintenance/*filepath cannot both exist). So when API paths
// sit under WebPrefix, we register only the web catch-all; the backend still receives /maintenance/api/v1/...
// When WebPrefix sits under APIPrefix, we register only the API catch-all.
func registerMaintenanceProxyRoutes(engine *gin.Engine, webPrefix, apiPrefix string, proxy http.Handler) {
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
		engine.Any(webExact, h)
		engine.Any(webGlob, h)
	case nestedUnder(webPrefix, apiPrefix):
		engine.Any(apiExact, h)
		engine.Any(apiGlob, h)
	default:
		engine.Any(webExact, h)
		engine.Any(webGlob, h)
		engine.Any(apiExact, h)
		engine.Any(apiGlob, h)
	}
}
