package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/auth"
	"github.com/joooostb/speedy-eboekhouden/internal/claude"
	"github.com/joooostb/speedy-eboekhouden/internal/config"
	"github.com/joooostb/speedy-eboekhouden/internal/crypto"
	"github.com/joooostb/speedy-eboekhouden/internal/database"
	"github.com/joooostb/speedy-eboekhouden/internal/handler"
	"github.com/joooostb/speedy-eboekhouden/internal/mail"
	"github.com/joooostb/speedy-eboekhouden/internal/middleware"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
	"github.com/joooostb/speedy-eboekhouden/internal/storage"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	// Database
	db, err := database.New(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Redis
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Fatalf("Invalid Redis URL: %v", err)
	}
	redisClient := redis.NewClient(redisOpts)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer redisClient.Close()

	// Encryption key (required)
	if cfg.EncryptionKey == "" {
		log.Fatal("ENCRYPTION_KEY environment variable is required")
	}
	encKey, err := crypto.ParseKey(cfg.EncryptionKey)
	if err != nil {
		log.Fatalf("Invalid encryption key: %v", err)
	}

	// Services
	store := session.NewStore(redisClient, cfg.SessionMaxAge, encKey)
	claudeSvc := claude.NewService()

	webauthnSvc, err := auth.NewWebAuthnService(db, cfg.WebAuthnRPID, cfg.WebAuthnRPName, cfg.WebAuthnOrigin)
	if err != nil {
		log.Fatalf("Failed to create WebAuthn service: %v", err)
	}

	// Services
	mailer := mail.New(mail.Config{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword,
		From:     cfg.SMTPFrom,
	})
	r2Client := storage.New(storage.Config{
		AccountID:       cfg.R2AccountID,
		AccessKeyID:     cfg.R2AccessKeyID,
		SecretAccessKey: cfg.R2SecretAccessKey,
		BucketName:      cfg.R2BucketName,
		Jurisdiction:    cfg.R2Jurisdiction,
		PublicURL:       cfg.R2PublicURL,
	})

	// Handlers
	passkeyHandler := handler.NewPasskeyHandler(webauthnSvc, store, cfg, db, encKey, mailer, r2Client)
	ebAuthHandler := handler.NewEBoekhoudenAuthHandler(store, db, encKey)
	settingsHandler := handler.NewSettingsHandler(db, encKey)
	passkeySettingsHandler := handler.NewPasskeySettingsHandler(db)
	learnedSettingsHandler := handler.NewLearnedSettingsHandler(db)
	invoiceHandler := handler.NewInvoiceHandler(claudeSvc, db, encKey, r2Client, redisClient)
	avatarHandler := handler.NewAvatarHandler(r2Client, db)
	classifyHandler := handler.NewClassifyHandler(claudeSvc, db, encKey)
	inboxHandler := handler.NewInboxHandler(claudeSvc, db, encKey, redisClient, r2Client, store)
	bankStatementsHandler := handler.NewBankStatementsHandler(store, db)
	soapHandler := handler.NewSoapAPIHandler(db, encKey)
	restHandler := handler.NewRestAPIHandler(db, encKey)

	// Router
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.SetTrustedProxies([]string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.1/8"})
	r.MaxMultipartMemory = 10 << 20 // 10 MB
	r.Use(middleware.CORS(cfg.FrontendOrigin))

	// Health check
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Rate limiters — disabled in local dev (COOKIE_SECURE=false), separate counters per action
	rlRegister, rlLogin, rlEB := 5, 20, 10
	if !cfg.CookieSecure {
		rlRegister, rlLogin, rlEB = 0, 0, 0 // 0 = disabled
	}
	registerRateLimit := middleware.RateLimit(redisClient, "register", rlRegister, 15*time.Minute)
	loginRateLimit := middleware.RateLimit(redisClient, "login", rlLogin, 5*time.Minute)
	ebRateLimit := middleware.RateLimit(redisClient, "ebauth", rlEB, 5*time.Minute)

	api := r.Group("/api/v1")
	{
		// Public routes — passkey auth (rate limited per action)
		api.POST("/auth/register/begin", registerRateLimit, passkeyHandler.RegisterBegin)
		api.POST("/auth/register/finish", registerRateLimit, passkeyHandler.RegisterFinish)
		api.POST("/auth/login/begin", loginRateLimit, passkeyHandler.LoginBegin)
		api.POST("/auth/login/finish", loginRateLimit, passkeyHandler.LoginFinish)
		api.POST("/auth/recover", registerRateLimit, passkeyHandler.RecoverRequest)
		api.POST("/auth/recover/begin", registerRateLimit, passkeyHandler.RecoverFinishBegin)
		api.POST("/auth/recover/finish", registerRateLimit, passkeyHandler.RecoverFinishComplete)

		// Authenticated routes (Speedy account required)
		authed := api.Group("")
		authed.Use(session.Middleware(store))
		{
			// Session management
			authed.GET("/auth/me", passkeyHandler.Me)
			authed.POST("/auth/logout", passkeyHandler.Logout)

			// e-Boekhouden secondary auth (rate limited)
			authed.POST("/eboekhouden/login", ebRateLimit, ebAuthHandler.Login)
			authed.POST("/eboekhouden/mfa", ebRateLimit, ebAuthHandler.MFA)
			authed.GET("/eboekhouden/status", ebAuthHandler.Status)
			authed.GET("/eboekhouden/keepalive", ebAuthHandler.Keepalive)
			authed.POST("/eboekhouden/disconnect", ebAuthHandler.Disconnect)

			// Settings
			authed.GET("/settings", settingsHandler.GetSettings)
			authed.GET("/settings/api-key/status", settingsHandler.CheckAPIKey)
			authed.PUT("/settings/api-key", settingsHandler.SetAPIKey)
			authed.DELETE("/settings/api-key", settingsHandler.DeleteAPIKey)
			authed.PUT("/settings/soap-credentials", settingsHandler.SetSoapCredentials)
			authed.DELETE("/settings/soap-credentials", settingsHandler.DeleteSoapCredentials)
			authed.PUT("/settings/rest-token", settingsHandler.SetRestAccessToken)
			authed.DELETE("/settings/rest-token", settingsHandler.DeleteRestAccessToken)
			authed.PUT("/settings/entity-type", settingsHandler.SetEntityType)
			authed.GET("/settings/passkeys", passkeySettingsHandler.List)
			authed.PATCH("/settings/passkeys/:id", passkeySettingsHandler.Rename)
			authed.DELETE("/settings/passkeys/:id", passkeySettingsHandler.Delete)
			authed.GET("/settings/learned", learnedSettingsHandler.List)
			authed.DELETE("/settings/learned", learnedSettingsHandler.DeleteAll)
			authed.DELETE("/settings/learned/item", learnedSettingsHandler.Delete)

			// SOAP API (requires SOAP credentials)
			authed.GET("/soap/relaties", soapHandler.GetRelaties)
			authed.GET("/soap/grootboekrekeningen", soapHandler.GetGrootboekrekeningen)
			authed.GET("/soap/saldi", soapHandler.GetSaldi)
			authed.GET("/soap/openposten", soapHandler.GetOpenPosten)
			authed.GET("/soap/mutaties", soapHandler.GetMutaties)
			authed.GET("/soap/artikelen", soapHandler.GetArtikelen)
			authed.GET("/soap/kostenplaatsen", soapHandler.GetKostenplaatsen)

			// REST API (requires REST access token)
			authed.GET("/rest/invoices", restHandler.GetInvoices)
			authed.POST("/rest/invoices", restHandler.CreateInvoice)
			authed.GET("/rest/costcenters", restHandler.GetCostCenters)
			authed.GET("/rest/emailtemplates", restHandler.GetEmailTemplates)

			// Claude classification (requires API key, not e-boekhouden)
			authed.POST("/classify", classifyHandler.Classify)

			// Bookkeeping Inbox
			authed.GET("/inbox/summary", inboxHandler.DashboardSummary)
			authed.POST("/inbox/classify", inboxHandler.ClassifyBatch)
			authed.POST("/inbox/process-batch", inboxHandler.BatchProcess)
			authed.POST("/inbox/:id/match-invoice", inboxHandler.MatchInvoice)

			// Avatar
			authed.POST("/avatar", avatarHandler.Upload)
			authed.DELETE("/avatar", avatarHandler.Delete)

			// e-Boekhouden data routes (require e-boekhouden connection)
			eb := authed.Group("")
			eb.Use(session.RequireEBoekhouden())
			{
				// Existing — hours
				eb.GET("/employees", handler.GetEmployees)
				eb.GET("/projects", handler.GetProjects)
				eb.GET("/activities", handler.GetActivities)
				eb.POST("/hours", handler.SubmitHours)

				// Bank statements
				eb.GET("/bankstatements", bankStatementsHandler.GetBankStatements)
				eb.GET("/bankstatements/count", bankStatementsHandler.GetBankStatementCount)
				eb.GET("/bankstatements/lastdata", bankStatementsHandler.GetLastMutatieData)
				eb.GET("/bankstatements/:id/suggestion", bankStatementsHandler.GetBankStatementSuggestion)
				eb.POST("/bankstatements/:id/process", bankStatementsHandler.ProcessBankStatement)

				// Mutations
				eb.POST("/mutations", handler.CreateMutation)

				// Reference data
				eb.GET("/ledger-accounts", handler.GetLedgerAccounts)
				eb.GET("/relations", handler.SearchRelations)
				eb.GET("/vat-codes", handler.GetVATCodes)

				// Digital archive
				eb.GET("/archive/folders", handler.GetArchiveFolders)
				eb.POST("/archive/folders", handler.CreateArchiveFolder)
				eb.GET("/archive/files/:folderId", handler.GetArchiveFiles)
				eb.POST("/archive/upload", handler.UploadArchiveFile)
				eb.POST("/archive/link", handler.LinkFileToMutation)

				// Relations + KvK
				eb.GET("/kvk/search", handler.SearchKvK)
				eb.GET("/kvk/address/:vestigingsnummer", handler.GetKvKAddress)
				eb.POST("/relations", handler.CreateRelation)

				// Invoice processing
				eb.POST("/invoices/analyze", invoiceHandler.Analyze)
				eb.POST("/invoices/submit", invoiceHandler.Submit)
				eb.POST("/invoices/submit-full", invoiceHandler.SubmitFull)
				eb.POST("/invoices/submit-receipt", invoiceHandler.SubmitReceipt)
			}
		}
	}

	log.Printf("Starting server on :%s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
