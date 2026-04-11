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

	// Cloudflare R2 (optional — avatar + PDF storage)
	R2AccountID       string
	R2AccessKeyID     string
	R2SecretAccessKey string
	R2BucketName      string
	R2Jurisdiction    string // "eu" for EU data residency
	R2PublicURL       string // e.g. https://cdn.speedy-eboekhouden.nl
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
		Port:              port,
		FrontendOrigin:    frontendOrigin,
		CookieDomain:      os.Getenv("COOKIE_DOMAIN"),
		CookieSecure:      os.Getenv("COOKIE_SECURE") != "false",
		SessionMaxAge:     30,
		PostgresDSN:       postgresDSN,
		RedisURL:          redisURL,
		WebAuthnOrigin:    webauthnOrigin,
		WebAuthnRPID:      webauthnRPID,
		WebAuthnRPName:    "Speedy e-Boekhouden",
		EncryptionKey:     os.Getenv("ENCRYPTION_KEY"),
		SMTPHost:          os.Getenv("SMTP_HOST"),
		SMTPPort:          getEnvDefault("SMTP_PORT", "587"),
		SMTPUsername:      os.Getenv("SMTP_USERNAME"),
		SMTPPassword:      os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:          getEnvDefault("SMTP_FROM", "noreply@speedy-eboekhouden.nl"),
		R2AccountID:       os.Getenv("R2_ACCOUNT_ID"),
		R2AccessKeyID:     os.Getenv("R2_ACCESS_KEY_ID"),
		R2SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
		R2BucketName:      getEnvDefault("R2_BUCKET", "speedy-eboekhouden"),
		R2Jurisdiction:    os.Getenv("R2_JURISDICTION"),
		R2PublicURL:       os.Getenv("R2_PUBLIC_URL"),
	}
}

func getEnvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
