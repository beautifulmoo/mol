package maintenance

import (
	"log"

	"contrabass-agent/maintenance/config"
)

func newGinProxyFallbackConfig() *config.Config {
	c := config.Default()
	c.MaintenancePort = 8889
	c.ServerHTTPPort = 8888
	return &c
}

// GinProxyConfig loads Maintenance.WebPrefix, APIPrefix, ports for the outer Gin
// (Server.HTTPPort → maintenance proxy). When -cfg is absent or load fails,
// defaults match the previous hardcoded behavior (8888 / 8889, /web, /api/v1).
func GinProxyConfig(args []string) *config.Config {
	path := ConfigPathForServiceMode(args)
	if path == "" {
		return newGinProxyFallbackConfig()
	}
	cfg, err := config.Load(path)
	if err != nil {
		log.Printf("gin: config %q: %v — using default prefixes and 8888/8889 for proxy", path, err)
		return newGinProxyFallbackConfig()
	}
	return cfg
}
