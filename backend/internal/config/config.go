package config

import "os"

type Config struct {
	Port            string
	FrontendOrigin  string
	CookieDomain    string
	CookieSecure    bool
	SessionMaxAge   int // minutes
}

func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "http://localhost:5173"
	}

	cookieDomain := os.Getenv("COOKIE_DOMAIN")

	cookieSecure := os.Getenv("COOKIE_SECURE") != "false"

	return &Config{
		Port:           port,
		FrontendOrigin: frontendOrigin,
		CookieDomain:   cookieDomain,
		CookieSecure:   cookieSecure,
		SessionMaxAge:  30,
	}
}
