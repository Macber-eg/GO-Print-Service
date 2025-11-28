package main

import (
	"badge-service/internal/cache"
	"badge-service/internal/handlers"
	"fmt"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

func main() {
	// Get port from environment or default
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	
	// Get cache directory from environment
	cacheDir := os.Getenv("CACHE_DIR")
	if cacheDir == "" {
		cacheDir = "/tmp/badge-cache"
	}
	
	// Initialize cache
	cache.Init(cacheDir)
	
	// Create Fiber app with optimized config
	app := fiber.New(fiber.Config{
		Prefork:       false, // Set to true for multi-process (Railway doesn't need this)
		ServerHeader:  "Badge-Service",
		AppName:       "Badge PDF Generator v1.0.0",
		ReadTimeout:   30 * time.Second,
		WriteTimeout:  60 * time.Second,
		IdleTimeout:   120 * time.Second,
		BodyLimit:     50 * 1024 * 1024, // 50MB max body size for batch requests
		Concurrency:   256 * 1024,       // Max concurrent connections
	})
	
	// Middleware
	app.Use(recover.New())
	app.Use(requestid.New())
	// Minimal logger for production (only errors logged)
	app.Use(logger.New(logger.Config{
		Format:     "${status} ${method} ${path} ${latency}\n",
		TimeFormat: "15:04:05",
		Output:     os.Stderr, // Log to stderr for production
	}))
	
	// CORS
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
	}))
	
	// Routes
	setupRoutes(app)
	
	// Start server
	fmt.Fprintf(os.Stderr, "Badge Service starting on port %s\n", port)
	fmt.Fprintf(os.Stderr, "Cache directory: %s\n", cacheDir)
	
	if err := app.Listen(":" + port); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start server: %v\n", err)
		os.Exit(1)
	}
}

func setupRoutes(app *fiber.App) {
	// Health check
	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"service": "Badge PDF Generator",
			"version": "1.0.0",
			"status":  "running",
		})
	})
	
	app.Get("/health", handlers.HealthCheck)
	
	// API routes
	api := app.Group("/api")
	
	// Badge generation
	api.Post("/badge/generate", handlers.GenerateBadge)
	api.Post("/badge/batch", handlers.GenerateBadgeBatch)
	
	// Template management
	api.Post("/template/preload", handlers.PreloadTemplate)
	
	// Cache management
	api.Get("/cache/stats", handlers.GetCacheStats)
	api.Post("/cache/clear", handlers.ClearCache)
	
	// 404 handler
	app.Use(func(c *fiber.Ctx) error {
		return c.Status(404).JSON(fiber.Map{
			"error": "Not found",
			"path":  c.Path(),
		})
	})
}
