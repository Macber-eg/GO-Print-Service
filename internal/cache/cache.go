package cache

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chai2010/webp"
	"github.com/disintegration/imaging"
	gocache "github.com/patrickmn/go-cache"
	_ "golang.org/x/image/webp"
)

// Buffer pool for reusing bytes.Buffer to reduce allocations
var bufferPool = sync.Pool{
	New: func() interface{} {
		return &bytes.Buffer{}
	},
}

var (
	// In-memory cache for small data
	memCache *gocache.Cache
	
	// In-memory cache for processed image data (raw bytes)
	imageDataCache *gocache.Cache
	
	// File cache directory
	fileCacheDir string
	
	// HTTP client with timeout
	httpClient *http.Client
	
	// Mutex for file operations
	fileMu sync.RWMutex
	
	once sync.Once
)

func Init(cacheDir string) {
	once.Do(func() {
		// Initialize memory cache (30 min default, 60 min cleanup) - optimized for production
		memCache = gocache.New(30*time.Minute, 60*time.Minute)
		
		// Initialize image data cache (60 min TTL, 120 min cleanup) for processed images - optimized for production
		imageDataCache = gocache.New(60*time.Minute, 120*time.Minute)
		
		// Set file cache directory
		fileCacheDir = cacheDir
		if fileCacheDir == "" {
			fileCacheDir = "/tmp/badge-cache"
		}
		
		// Create cache directory
		os.MkdirAll(fileCacheDir, 0755)
		os.MkdirAll(filepath.Join(fileCacheDir, "images"), 0755)
		os.MkdirAll(filepath.Join(fileCacheDir, "templates"), 0755)
		os.MkdirAll(filepath.Join(fileCacheDir, "qrcodes"), 0755)
		
		// HTTP client with optimized timeout, connection pooling, compression, and HTTP/2
		transport := &http.Transport{
			MaxIdleConns:        500,        // Increased for better connection reuse
			MaxIdleConnsPerHost: 100,       // Increased per-host connections
			IdleConnTimeout:     120 * time.Second, // Longer timeout for connection reuse
			DisableCompression:  false,     // Enable compression (gzip/deflate)
			ForceAttemptHTTP2:   true,      // Enable HTTP/2 for better multiplexing
		}
		httpClient = &http.Client{
			Timeout:   5 * time.Second, // Reduced to 5s for faster failure detection
			Transport: transport,
		}
	})
}

// GetCacheDir returns the cache directory path
func GetCacheDir() string {
	return fileCacheDir
}

// ============ IMAGE CACHING ============

// GetImagePath returns cached image path, downloads if not cached
func GetImagePath(url string) (string, error) {
	if url == "" {
		return "", fmt.Errorf("empty URL")
	}
	
	// Generate cache key from URL
	hash := md5.Sum([]byte(url))
	cacheKey := hex.EncodeToString(hash[:])
	
	// Check memory cache for path (optimized: single stat call)
	if cached, found := memCache.Get("img:" + cacheKey); found {
		path := cached.(string)
		if stat, err := os.Stat(path); err == nil && stat != nil && stat.Size() > 0 {
			return path, nil
		}
	}
	
	// Determine file extension
	ext := filepath.Ext(url)
	if ext == "" || len(ext) > 5 {
		ext = ".png"
	}
	
	// File cache path
	cachePath := filepath.Join(fileCacheDir, "images", cacheKey+ext)
	
	// Check if file exists on disk (optimized: single check with stat)
	fileMu.RLock()
	stat, err := os.Stat(cachePath)
	fileExists := err == nil && stat != nil && stat.Size() > 0
	fileMu.RUnlock()
	
	if fileExists {
		memCache.Set("img:"+cacheKey, cachePath, gocache.DefaultExpiration)
		return cachePath, nil
	}
	
	// Download image
	fileMu.Lock()
	defer fileMu.Unlock()
	
	// Double-check after acquiring lock (optimized: single stat call)
	if stat, err := os.Stat(cachePath); err == nil && stat != nil && stat.Size() > 0 {
		memCache.Set("img:"+cacheKey, cachePath, gocache.DefaultExpiration)
		return cachePath, nil
	}
	
	if err := downloadFile(url, cachePath); err != nil {
		return "", fmt.Errorf("failed to download image from %s: %w", url, err)
	}
	
	// Validate downloaded file exists and has content
	if stat, err := os.Stat(cachePath); err != nil || stat == nil || stat.Size() == 0 {
		return "", fmt.Errorf("downloaded image file is invalid or empty: %s (from %s)", cachePath, url)
	}
	
	memCache.Set("img:"+cacheKey, cachePath, gocache.DefaultExpiration)
	return cachePath, nil
}

// PreloadImage downloads and caches an image in advance
func PreloadImage(url string) error {
	_, err := GetImagePath(url)
	return err
}

// PreloadImages downloads multiple images concurrently
func PreloadImages(urls []string) map[string]string {
	results := make(map[string]string)
	var mu sync.Mutex
	var wg sync.WaitGroup
	
	// Limit concurrent downloads
	sem := make(chan struct{}, 20)
	
	for _, url := range urls {
		if url == "" {
			continue
		}
		
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			
			path, err := GetImagePath(u)
			if err == nil {
				mu.Lock()
				results[u] = path
				mu.Unlock()
			}
		}(url)
	}
	
	wg.Wait()
	return results
}

// ============ DIRECT IMAGE DATA CACHING (RAW BYTES) ============

// ImageRequest represents an image to be loaded with specific dimensions
type ImageRequest struct {
	URL    string
	Width  float64 // in mm
	Height float64 // in mm
	DPI    int     // DPI for size calculation
}

// GetImageDataDirect downloads an image, resizes to exact size, and returns raw PNG bytes
// All processing is done in memory - zero file I/O
func GetImageDataDirect(url string, widthMM, heightMM float64, dpi int) ([]byte, error) {
	if url == "" {
		return nil, fmt.Errorf("empty URL")
	}
	
	// Generate cache key with dimensions for size-specific caching
	hash := md5.Sum([]byte(url))
	urlHash := hex.EncodeToString(hash[:])
	cacheKey := fmt.Sprintf("img_data:%s_%.1f_%.1f_%d", urlHash, widthMM, heightMM, dpi)
	
	// Check cache first (fast path)
	if cached, found := imageDataCache.Get(cacheKey); found {
		return cached.([]byte), nil
	}
	
	// Calculate exact pixel dimensions
	pixelWidth := int(widthMM * float64(dpi) / 25.4)
	pixelHeight := int(heightMM * float64(dpi) / 25.4)
	
	// Download image
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}
	
	// Read image data into memory
	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read image data: %w", err)
	}
	
	// Detect WebP format and use appropriate decoder
	var img image.Image
	if isWebP(imageData) {
		// Use chai2010/webp for better VP8X support
		img, err = webp.Decode(bytes.NewReader(imageData))
		if err != nil {
			return nil, fmt.Errorf("failed to decode WebP: %w", err)
		}
	} else {
		// Use imaging for PNG, JPG, GIF
		img, err = imaging.Decode(bytes.NewReader(imageData))
		if err != nil {
			return nil, fmt.Errorf("failed to decode image: %w", err)
		}
	}
	
	// Get original dimensions
	bounds := img.Bounds()
	origWidth := bounds.Dx()
	origHeight := bounds.Dy()
	
	// Helper function to calculate absolute difference percentage
	absDiff := func(a, b int) int {
		if a > b {
			return a - b
		}
		return b - a
	}
	
	// Only resize if dimensions are significantly different (>10% difference)
	// Skip resize if dimensions match or are very close
	widthDiff := absDiff(origWidth, pixelWidth)
	heightDiff := absDiff(origHeight, pixelHeight)
	
	if widthDiff > pixelWidth/10 || heightDiff > pixelHeight/10 {
		// Use NearestNeighbor for speed (faster than Lanczos)
		// For better quality, use imaging.Lanczos, but NearestNeighbor is much faster
		img = imaging.Resize(img, pixelWidth, pixelHeight, imaging.NearestNeighbor)
	}
	
	// Normalize to 8-bit NRGBA (gofpdf requirement)
	nrgba := imaging.Clone(img)
	
	// Get buffer from pool and pre-allocate to avoid reallocations
	// Estimate: width * height * 4 bytes for RGBA
	estimatedSize := pixelWidth * pixelHeight * 4
	if estimatedSize < 1024 {
		estimatedSize = 1024 // Minimum buffer size
	}
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	if buf.Cap() < estimatedSize {
		buf.Grow(estimatedSize)
	}
	defer func() {
		buf.Reset()
		bufferPool.Put(buf)
	}()
	
	// Encode as PNG in memory
	err = imaging.Encode(buf, nrgba, imaging.PNG)
	if err != nil {
		return nil, fmt.Errorf("failed to encode PNG: %w", err)
	}
	
	// Get processed bytes (copy since buffer will be returned to pool)
	processedBytes := make([]byte, buf.Len())
	copy(processedBytes, buf.Bytes())
	
	// Cache the processed bytes
	imageDataCache.Set(cacheKey, processedBytes, gocache.DefaultExpiration)
	
	return processedBytes, nil
}

// PreloadImagesDirect downloads and processes multiple images in parallel
// Returns map of URL -> raw PNG bytes (not base64, not file paths)
func PreloadImagesDirect(requests []ImageRequest) map[string][]byte {
	results := make(map[string][]byte)
	var mu sync.Mutex
	var wg sync.WaitGroup
	
	// Limit concurrent downloads/processing
	sem := make(chan struct{}, 50)
	
	for _, req := range requests {
		if req.URL == "" {
			continue
		}
		
		wg.Add(1)
		go func(r ImageRequest) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			
			imageData, err := GetImageDataDirect(r.URL, r.Width, r.Height, r.DPI)
			if err == nil {
				mu.Lock()
				results[r.URL] = imageData
				mu.Unlock()
			}
			// Errors are silently ignored in production for performance
		}(req)
	}
	
	wg.Wait()
	return results
}

// ============ QR CODE CACHING ============

// GetQRCodePath returns path to cached QR code
func GetQRCodePath(content string) string {
	hash := md5.Sum([]byte(content))
	cacheKey := hex.EncodeToString(hash[:])
	return filepath.Join(fileCacheDir, "qrcodes", cacheKey+".png")
}

// ============ TEMPLATE CACHING ============

// CacheTemplateBackground caches the background image for a template
func CacheTemplateBackground(templateID int, url string) (string, error) {
	if url == "" {
		return "", nil
	}
	
	cacheKey := fmt.Sprintf("template_bg_%d", templateID)
	
	// Check memory cache
	if cached, found := memCache.Get(cacheKey); found {
		path := cached.(string)
		if fileExists(path) {
			return path, nil
		}
	}
	
	// Download and cache
	path, err := GetImagePath(url)
	if err != nil {
		return "", err
	}
	
	memCache.Set(cacheKey, path, gocache.NoExpiration) // Never expire templates
	return path, nil
}

// ============ HELPER FUNCTIONS ============

// isWebP detects if image data is in WebP format by checking magic bytes
func isWebP(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	// WebP files start with "RIFF" and contain "WEBP"
	return string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}

func downloadFile(url, destPath string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}
	
	// Create temp file first
	tmpPath := destPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	
	_, err = io.Copy(out, resp.Body)
	out.Close()
	
	if err != nil {
		os.Remove(tmpPath)
		return err
	}
	
	// Atomic rename
	return os.Rename(tmpPath, destPath)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ClearCache removes all cached files
func ClearCache() error {
	memCache.Flush()
	return os.RemoveAll(fileCacheDir)
}

// GetCacheStats returns cache statistics
func GetCacheStats() map[string]interface{} {
	return map[string]interface{}{
		"memory_items": memCache.ItemCount(),
		"cache_dir":    fileCacheDir,
	}
}
