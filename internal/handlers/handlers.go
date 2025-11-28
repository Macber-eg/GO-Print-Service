package handlers

import (
	"badge-service/internal/cache"
	"badge-service/internal/generator"
	"badge-service/internal/models"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

var startTime = time.Now()

// HealthCheck handles health check requests
func HealthCheck(c *fiber.Ctx) error {
	return c.JSON(models.HealthResponse{
		Status:  "healthy",
		Version: "1.0.0",
		Uptime:  time.Since(startTime).String(),
	})
}

// GetCacheStats returns cache statistics
func GetCacheStats(c *fiber.Ctx) error {
	return c.JSON(cache.GetCacheStats())
}

// GenerateBadge generates a single badge PDF
func GenerateBadge(c *fiber.Ctx) error {
	var req models.GenerateBadgeRequest
	
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
	}
	
	// Validate request
	if req.Template.ID == 0 && req.Template.Design.Layers == nil {
		return c.Status(400).JSON(fiber.Map{
			"error": "Template is required",
		})
	}
	
	if req.User.User.ID == "" && req.User.User.Identifier == "" {
		return c.Status(400).JSON(fiber.Map{
			"error": "User data is required",
		})
	}
	
	// Collect image requests with dimensions for direct loading
	var imageRequests []cache.ImageRequest
	
	// Get DPI from template settings
	dpi := req.Template.Design.Settings.DPI
	if dpi == 0 {
		dpi = 300 // Default DPI
	}
	
	// Helper function to recursively collect image layers
	var collectImageLayers func(layers []models.Layer)
	collectImageLayers = func(layers []models.Layer) {
		for _, layer := range layers {
			if !layer.Visible {
				continue
			}
			
			var imageURL string
			
			// Check if this is an asset reference
			if strings.HasPrefix(layer.Content, "asset_") {
				// Try exact match first
				if url, ok := req.Template.Assets[layer.Content]; ok {
					imageURL = url
				} else {
					// Fallback: find asset URL with contains match
					for key, url := range req.Template.Assets {
						if strings.Contains(key, layer.Content) {
							imageURL = url
							break
						}
					}
				}
			} else if layer.DataBinding != "" {
				// Get image URL from user data binding
				fieldID := strings.TrimPrefix(layer.DataBinding, "customFields.")
				imageURL = req.User.User.GetFieldValue(fieldID)
			} else if layer.Content != "" && (strings.HasPrefix(layer.Content, "http://") || strings.HasPrefix(layer.Content, "https://")) {
				imageURL = layer.Content
			}
			
			// If we found an image URL and it's an image layer, add to requests
			if imageURL != "" && layer.Type == "image" {
				// Check if already in requests (deduplication)
				found := false
				for _, req := range imageRequests {
					if req.URL == imageURL && req.Width == layer.Size.Width && req.Height == layer.Size.Height {
						found = true
						break
					}
				}
				if !found {
					imageRequests = append(imageRequests, cache.ImageRequest{
						URL:    imageURL,
						Width:  layer.Size.Width,
						Height: layer.Size.Height,
						DPI:    dpi,
					})
				}
			}
			
			// Recursively check container children
			if layer.Type == "container" && len(layer.Children) > 0 {
				collectImageLayers(layer.Children)
			}
		}
	}
	
	// Collect all image layers recursively
	collectImageLayers(req.Template.Design.Layers)
	
	// Pre-fetch all images with dimensions (direct loading, in-memory processing)
	var imageDataCache map[string][]byte
	if len(imageRequests) > 0 {
		imageDataCache = cache.PreloadImagesDirect(imageRequests)
	} else {
		imageDataCache = make(map[string][]byte)
	}
	
	// Generate PDF
	gen := generator.NewPDFGenerator(&req.Template, &req.User.User)
	gen.SetImageDataCache(imageDataCache)
	
	pdfBytes, err := gen.Generate()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error":   "Failed to generate PDF",
			"details": err.Error(),
		})
	}
	
	// Check if client wants base64 or binary
	acceptHeader := c.Get("Accept")
	if acceptHeader == "application/json" {
		// Return as base64
		return c.JSON(fiber.Map{
			"success":    true,
			"pdf_base64": base64.StdEncoding.EncodeToString(pdfBytes),
			"filename":   fmt.Sprintf("badge_%s.pdf", req.User.User.Identifier),
		})
	}
	
	// Return as binary PDF
	c.Set("Content-Type", "application/pdf")
	c.Set("Content-Disposition", fmt.Sprintf("inline; filename=badge_%s.pdf", req.User.User.Identifier))
	return c.Send(pdfBytes)
}

// GenerateBadgeBatch generates multiple badges
func GenerateBadgeBatch(c *fiber.Ctx) error {
	var req models.BatchGenerateRequest
	
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
	}
	
	if len(req.Users) == 0 {
		return c.Status(400).JSON(fiber.Map{
			"error": "No users provided",
		})
	}
	
	if len(req.Users) > 500 {
		return c.Status(400).JSON(fiber.Map{
			"error": "Maximum 500 users per batch",
		})
	}
	
	// Collect all image URLs to pre-fetch
	var imageURLs []string
	urlSet := make(map[string]bool) // Deduplicate URLs
	
	// Template assets
	for _, url := range req.Template.Assets {
		if url != "" && !urlSet[url] {
			imageURLs = append(imageURLs, url)
			urlSet[url] = true
		}
	}
	
	// User photos from customFieldValues
	for _, userData := range req.Users {
		for _, cf := range userData.User.CustomFieldValues {
			if cf.FieldType == "file" && cf.Value != "" && (strings.HasPrefix(cf.Value, "http://") || strings.HasPrefix(cf.Value, "https://")) {
				if !urlSet[cf.Value] {
					imageURLs = append(imageURLs, cf.Value)
					urlSet[cf.Value] = true
				}
			}
		}
	}
	
	// Also check dataBinding fields in template layers to ensure all images are preloaded
	for _, layer := range req.Template.Design.Layers {
		if layer.Type == "image" && layer.DataBinding != "" {
			fieldID := strings.TrimPrefix(layer.DataBinding, "customFields.")
			for _, userData := range req.Users {
				imageURL := userData.User.GetFieldValue(fieldID)
				if imageURL != "" && (strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://")) {
					if !urlSet[imageURL] {
						imageURLs = append(imageURLs, imageURL)
						urlSet[imageURL] = true
					}
				}
			}
		}
	}
	
	// Collect image requests with dimensions for all users
	var imageRequests []cache.ImageRequest
	
	// Get DPI from template settings
	dpi := req.Template.Design.Settings.DPI
	if dpi == 0 {
		dpi = 300 // Default DPI
	}
	
	// Helper function to recursively collect image layers
	var collectImageLayers func(layers []models.Layer, user *models.User)
	collectImageLayers = func(layers []models.Layer, user *models.User) {
		for _, layer := range layers {
			if !layer.Visible {
				continue
			}
			
			var imageURL string
			
			// Check if this is an asset reference
			if strings.HasPrefix(layer.Content, "asset_") {
				if url, ok := req.Template.Assets[layer.Content]; ok {
					imageURL = url
				} else {
					for key, url := range req.Template.Assets {
						if strings.Contains(key, layer.Content) {
							imageURL = url
							break
						}
					}
				}
			} else if layer.DataBinding != "" {
				fieldID := strings.TrimPrefix(layer.DataBinding, "customFields.")
				imageURL = user.GetFieldValue(fieldID)
			} else if layer.Content != "" && (strings.HasPrefix(layer.Content, "http://") || strings.HasPrefix(layer.Content, "https://")) {
				imageURL = layer.Content
			}
			
			if imageURL != "" && layer.Type == "image" {
				// Deduplicate by URL+dimensions
				found := false
				for _, req := range imageRequests {
					if req.URL == imageURL && req.Width == layer.Size.Width && req.Height == layer.Size.Height {
						found = true
						break
					}
				}
				if !found {
					imageRequests = append(imageRequests, cache.ImageRequest{
						URL:    imageURL,
						Width:  layer.Size.Width,
						Height: layer.Size.Height,
						DPI:    dpi,
					})
				}
			}
			
			if layer.Type == "container" && len(layer.Children) > 0 {
				collectImageLayers(layer.Children, user)
			}
		}
	}
	
	// Collect image layers for first user (template structure is same for all)
	if len(req.Users) > 0 {
		collectImageLayers(req.Template.Design.Layers, &req.Users[0].User)
	}
	
	// Pre-fetch all images with dimensions (direct loading)
	imageDataCache := cache.PreloadImagesDirect(imageRequests)
	
	// Generate PDFs concurrently
	results := make([]models.BadgeResult, len(req.Users))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 50) // Limit concurrency to 50
	
	for i, userData := range req.Users {
		wg.Add(1)
		go func(idx int, user models.UserData) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			
			result := models.BadgeResult{
				UserID:     user.User.ID,
				Identifier: user.User.Identifier,
			}
			
			// Generate PDF
			gen := generator.NewPDFGenerator(&req.Template, &user.User)
			gen.SetImageDataCache(imageDataCache)
			
			pdfBytes, err := gen.Generate()
			if err != nil {
				result.Success = false
				result.Error = err.Error()
			} else {
				result.Success = true
				result.PDFBase64 = base64.StdEncoding.EncodeToString(pdfBytes)
			}
			
			results[idx] = result
		}(i, userData)
	}
	
	wg.Wait()
	
	// Count successes
	successCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		}
	}
	
	return c.JSON(models.BatchGenerateResponse{
		Success: successCount == len(results),
		Total:   len(results),
		Results: results,
	})
}

// PreloadTemplate pre-caches template assets
func PreloadTemplate(c *fiber.Ctx) error {
	var req struct {
		Template models.Template `json:"template"`
	}
	
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}
	
	// Pre-cache all template assets
	var urls []string
	for _, url := range req.Template.Assets {
		urls = append(urls, url)
	}
	
	cached := cache.PreloadImages(urls)
	
	return c.JSON(fiber.Map{
		"success":       true,
		"cached_assets": len(cached),
	})
}

// ClearCache clears all cached data
func ClearCache(c *fiber.Ctx) error {
	if err := cache.ClearCache(); err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	
	// Re-initialize cache
	cache.Init("")
	
	return c.JSON(fiber.Map{
		"success": true,
		"message": "Cache cleared",
	})
}
