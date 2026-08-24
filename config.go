package main

import (
	"flag"
	"os"
	"strings"
)

// config is the whole configuration surface: four environment variables, no
// file, no flags beyond -addr.
//
// Two base URLs is not a mistake. The gateway proxies /api/v1/* and nothing
// else, and its endpoint list contains neither the consent screen nor the
// revoke endpoint, so those two calls have to go straight to the application
// host. Everything else goes through the gateway, which is also what validates
// the token signature.
type config struct {
	Gateway  string
	Provider string
	Redirect string
	Addr     string
}

func loadConfig() config {
	c := config{
		Gateway:  env("VR_GATEWAY_URL", "https://api-vr2.verticalresponse.com"),
		Provider: env("VR_OCEANS_URL", "https://vr2api.verticalresponse.com"),
		Redirect: env("VR_REDIRECT_URL", "http://localhost:9190/callback"),
		Addr:     env("VR_ADDR", ":9190"),
	}

	flag.StringVar(&c.Addr, "addr", c.Addr, "address to listen on")
	flag.Parse()

	c.Gateway = strings.TrimSuffix(c.Gateway, "/")
	c.Provider = strings.TrimSuffix(c.Provider, "/")
	return c
}

// tokenURL is the one OAuth endpoint that does go through the gateway.
func (c config) tokenURL() string { return c.Gateway + "/api/v1/authorize" }

// consentURL is a server-rendered page on the application host, not an API
// route, so it is not behind the gateway and cannot be fetched by this app.
func (c config) consentURL() string { return c.Provider + "/oauth/authorize" }

// revokeURL is an /api/v1 path that the gateway does not list, so it too has to
// be called directly.
func (c config) revokeURL() string { return c.Provider + "/api/v1/oauth/revoke" }

func (c config) apiURL(path string) string { return c.Gateway + "/api/v1" + path }

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
