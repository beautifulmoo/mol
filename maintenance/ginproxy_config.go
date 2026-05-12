package maintenance

import (
	"log"

	"contrabass-agent/maintenance/molcfg"
)

func newGinProxyFallbackConfig() *molcfg.Config {
	c := molcfg.Default()
	c.MaintenancePort = 8889
	c.ServerHTTPPort = 8888
	return &c
}

// GinProxyConfig loads Maintenance.WebPrefix, APIPrefix, ports for the outer Gin
// (Server.HTTPPort → maintenance proxy). Callers typically pass argv only when
// starting that Gin (e.g. main uses it only for `<bin> -cfg <path>`).
// When -cfg is absent or load fails, defaults match the previous hardcoded behavior (8888 / 8889, /web, /api/v1).
func GinProxyConfig(args []string) *molcfg.Config {
	path := ConfigPathForServiceMode(args)
	if path == "" {
		return newGinProxyFallbackConfig()
	}
	cfg, err := molcfg.Load(path)
	if err != nil {
		log.Printf("gin: config %q: %v — using default prefixes and 8888/8889 for proxy", path, err)
		return newGinProxyFallbackConfig()
	}
	return cfg
}
