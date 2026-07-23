package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"feature-flag/internal/config"
	"feature-flag/internal/database"
	"feature-flag/internal/flag"
	"feature-flag/internal/health"
	"feature-flag/internal/middleware"

	"github.com/gin-gonic/gin"
)

func main() {
	log.Println("Starting Feature Flag Service...")

	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.NewPostgresDB(ctx, cfg.Postgres)
	if err != nil {
		log.Printf("[FATAL] Failed to connect to PostgreSQL: %v", err)
		return
	}
	defer db.Close()
	log.Println("PostgreSQL connection pool established successfully")

	cache, err := database.NewRedisCache(cfg.Redis)
	if err != nil {
		log.Printf("[FATAL] Failed to connect to Redis: %v", err)
		return
	}
	defer func() {
		if err := cache.Close(); err != nil {
			log.Printf("Error closing Redis connection: %v", err)
		}
	}()
	log.Println("Redis client established successfully")

	flagUsecase := flag.NewUsecase(db, cache)
	flagHandler := flag.NewHandler(flagUsecase)

	gin.SetMode(gin.ReleaseMode)

	router := gin.New()

	router.Use(middleware.LoggerMiddleware())
	router.Use(gin.Recovery())

	authMiddleware := middleware.AuthMiddleware(cfg.Clients)
	rateLimitMiddleware := middleware.RateLimitMiddleware(cache)

	healthHandler := health.NewHandler(db, cache)

	flagHandler.RegisterRoutes(router, authMiddleware, rateLimitMiddleware)
	router.GET("/health", healthHandler.HealthCheck)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	serverAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:    serverAddr,
		Handler: router,
	}

	go func() {
		log.Printf("Server listening on http://%s", serverAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[ERROR] Listen and serve failed: %v", err)
			quit <- syscall.SIGTERM
		}
	}()

	<-quit
	log.Println("Shutting down HTTP server gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[ERROR] Server forced to shutdown: %v", err)
	}

	log.Println("Server exiting gracefully")
}
