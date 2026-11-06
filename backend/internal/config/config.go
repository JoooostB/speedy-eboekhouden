package config

import "os"

type Config struct {
	Port           string
	FrontendOrigin string
	CookieDomain   string
	CookieSecure   bool
	SessionMaxAge  int // minutes

	// Database
	PostgresDSN string
	RedisURL    string

	// WebAuthn
	WebAuthnOrigin string
	WebAuthnRPID   string
	WebAuthnRPName string

	// Encryption
	EncryptionKey string // 64 hex chars = 32 bytes AES-256

	// SMTP (optional — passkey recovery emails)
	SMTPHost     string
	SMTPPort     string
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
}

func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	frontendOrigin := os.Getenv("FRONTEND_ORIGIN")
	if frontendOrigin == "" {
		frontendOrigin = "http://localhost:3000"
	}

	postgresDSN := os.Getenv("POSTGRES_DSN")
	if postgresDSN == "" {
		postgresDSN = "postgres://speedy:dev@localhost:5432/speedy?sslmode=disable"
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	webauthnOrigin := os.Getenv("WEBAUTHN_ORIGIN")
	if webauthnOrigin == "" {
		webauthnOrigin = frontendOrigin
	}

	webauthnRPID := os.Getenv("WEBAUTHN_RP_ID")
	if webauthnRPID == "" {
		webauthnRPID = "localhost"
	}

	return &Config{
		Port:           port,
		FrontendOrigin: frontendOrigin,
		CookieDomain:   os.Getenv("COOKIE_DOMAIN"),
		CookieSecure:   os.Getenv("COOKIE_SECURE") != "false",
		SessionMaxAge:  30,
		PostgresDSN:    postgresDSN,
		RedisURL:       redisURL,
		WebAuthnOrigin: webauthnOrigin,
		WebAuthnRPID:   webauthnRPID,
		WebAuthnRPName: "Speedy e-Boekhouden",
		EncryptionKey:  os.Getenv("ENCRYPTION_KEY"),
		SMTPHost:       os.Getenv("SMTP_HOST"),
		SMTPPort:       getEnvDefault("SMTP_PORT", "587"),
		SMTPUsername:   os.Getenv("SMTP_USERNAME"),
		SMTPPassword:   os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:       getEnvDefault("SMTP_FROM", "noreply@speedy-eboekhouden.nl"),
	}
}

func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
