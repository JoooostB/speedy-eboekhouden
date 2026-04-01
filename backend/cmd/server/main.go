package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/joooostb/speedy-eboekhouden/internal/config"
	"github.com/joooostb/speedy-eboekhouden/internal/handler"
	"github.com/joooostb/speedy-eboekhouden/internal/middleware"
	"github.com/joooostb/speedy-eboekhouden/internal/session"
)

func main() {
	cfg := config.Load()

	gin.SetMode(gin.ReleaseMode)

	store := session.NewStore(cfg.SessionMaxAge)
	authHandler := handler.NewAuthHandler(store, cfg)

	r := gin.New()
	r.Use(gin.Recovery())
	r.SetTrustedProxies([]string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.1/8"})

	r.Use(middleware.CORS(cfg.FrontendOrigin))

	// Health check
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := r.Group("/api/v1")
	{
		// Public routes
		api.POST("/login", authHandler.Login)

		// Authenticated routes
		auth := api.Group("")
		auth.Use(session.Middleware(store))
		{
			auth.GET("/me", authHandler.Me)
			auth.POST("/mfa", authHandler.MFA)
			auth.POST("/logout", authHandler.Logout)
			auth.GET("/employees", handler.GetEmployees)
			auth.GET("/projects", handler.GetProjects)
			auth.GET("/activities", handler.GetActivities)
			auth.POST("/hours", handler.SubmitHours)
		}
	}

	log.Printf("Starting server on :%s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
