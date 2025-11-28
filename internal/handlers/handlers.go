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
	
	// Pre-cache template background and user photos
	var imageURLs []string
	
	// Collect template assets
	for _, url := range req.Template.Assets {
		if url != "" {
			imageURLs = append(imageURLs, url)
		}
	}
	
	// Collect user photos from customFieldValues
	for _, cf := range req.User.User.CustomFieldValues {
		if cf.FieldType == "file" && cf.Value != "" && (strings.HasPrefix(cf.Value, "http://") || strings.HasPrefix(cf.Value, "https://")) {
			imageURLs = append(imageURLs, cf.Value)
		}
	}
	
	// Also check dataBinding fields in template layers to ensure all images are preloaded
	for _, layer := range req.Template.Design.Layers {
		if layer.Type == "image" && layer.DataBinding != "" {
			fieldID := strings.TrimPrefix(layer.DataBinding, "customFields.")
			imageURL := req.User.User.GetFieldValue(fieldID)
			if imageURL != "" && (strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://")) {
				// Check if already in list
				found := false
				for _, url := range imageURLs {
					if url == imageURL {
						found = true
						break
					}
				}
				if !found {
					imageURLs = append(imageURLs, imageURL)
				}
			}
		}
	}
	
	// Pre-fetch all images as base64 (faster, no file I/O during PDF generation)
	var imageBase64Cache map[string]string
	if len(imageURLs) > 0 {
		imageBase64Cache = cache.PreloadImagesAsBase64(imageURLs)
	} else {
		imageBase64Cache = make(map[string]string)
	}
	
	// Generate PDF
	gen := generator.NewPDFGenerator(&req.Template, &req.User.User)
	gen.SetImageBase64Cache(imageBase64Cache)
	
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
	
	// Pre-fetch all images as base64 concurrently (faster)
	imageBase64Cache := cache.PreloadImagesAsBase64(imageURLs)
	
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
			gen.SetImageBase64Cache(imageBase64Cache)
			
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
