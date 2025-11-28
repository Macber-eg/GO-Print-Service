# Optimize S3 Direct Image Loading for < 30ms Performance

## Goal

Load images directly from S3 compressed/resized to exact size needed, with zero file I/O, targeting < 30ms per request.

## Strategy: Direct S3 Image Loading with In-Memory Processing

**Key Principles**:
1. **No base64** - Use raw image bytes directly
2. **No file I/O** - Everything in memory
3. **Resize to exact size** - Download and resize in one step
4. **Aggressive caching** - In-memory cache with size-specific keys
5. **Parallel processing** - Download and process multiple images concurrently
6. **Minimal conversions** - Only convert WebP if absolutely necessary
7. **Direct gofpdf registration** - Register images from memory bytes

## Implementation Plan

### 1. Direct In-Memory Image Loading with Resize

**Approach**: Download full image, resize to exact size in memory, register directly with gofpdf

**No URL transformation needed** - We'll resize after download for maximum compatibility

### 2. In-Memory Image Processing (No File I/O)

**File**: `internal/cache/cache.go`

**New Function**: `GetImageDataDirect(url string, widthMM, heightMM float64, dpi int) ([]byte, string, error)`
- Downloads image directly to memory using `io.ReadAll()`
- Calculates exact pixel dimensions: `pixelWidth = int(widthMM * dpi / 25.4)`
- Resizes to exact dimensions in memory using `imaging.Resize()` (fast algorithm)
- Converts WebP to PNG in memory (if needed) using `imaging.Decode()` and `imaging.Encode()`
- Normalizes to 8-bit NRGBA in memory (gofpdf requirement)
- Returns raw PNG bytes and format type
- **Zero file I/O** - everything in memory

**Optimizations**:
- Use `imaging.Resize()` with `imaging.Lanczos` (fast, good quality) or `imaging.NearestNeighbor` (fastest)
- Skip resize if image is already smaller than needed
- Skip WebP conversion if gofpdf supports it (check version)
- Process in parallel for multiple images

### 3. Aggressive In-Memory Caching

**File**: `internal/cache/cache.go`

**Cache Strategy**:
- **Memory cache**: Store processed image bytes by URL+dimensions key
- **Cache key**: `img_data:{url_hash}_{widthMM}_{heightMM}_{dpi}` for size-specific caching
- **TTL**: Long TTL (10 minutes) for processed images
- **Size limit**: Cache all images (memory is fast, disk is slow)
- **Cache hit optimization**: Check cache before any download/processing

**Implementation**:
```go
var imageDataCache *gocache.Cache // URL+dimensions -> []byte (raw PNG bytes)
```

**Cache Key Format**:
```go
cacheKey := fmt.Sprintf("img_data:%s_%.1f_%.1f_%d", urlHash, widthMM, heightMM, dpi)
```

### 4. Direct gofpdf Image Registration (No Base64)

**File**: `internal/generator/generator.go` - `renderImage()`

**New Approach**:
- Get image bytes directly from cache or download (raw bytes, not base64)
- Register image with gofpdf using `RegisterImageOptionsReader()` with raw bytes
- **No base64 encoding/decoding** - direct bytes
- **No file paths** - everything in memory
- **No file I/O** during PDF generation

**Flow**:
1. Calculate exact pixel dimensions from layer size and DPI
2. Check memory cache for processed image bytes (by URL + dimensions)
3. If not cached, download and process in memory (resize, convert, normalize)
4. Cache processed bytes
5. Register with gofpdf using raw bytes (not base64)
6. Render image

**Key Change**: Replace base64 cache with raw bytes cache

### 5. Parallel Preloading with Size Information

**File**: `internal/handlers/handlers.go`

**Enhanced Preloading**:
- Collect image URLs with their target dimensions from layers (recursively check all layers)
- Preload images at exact sizes needed
- Process in parallel (50+ concurrent downloads/processing)
- Cache processed bytes in memory
- Return map: `URL -> raw image bytes` (not base64, not file paths)

**New Structure**:
```go
type ImageRequest struct {
    URL    string
    Width  float64 // in mm
    Height float64 // in mm
    DPI    int     // from template settings
}
```

**Function**: `PreloadImagesDirect(requests []ImageRequest) map[string][]byte`
- Takes image requests with dimensions
- Downloads and processes in parallel
- Returns map of URL -> raw PNG bytes
- All processing in memory, no files

### 6. Skip Unnecessary Processing

**Optimizations**:
- **Skip resize if image is already smaller than needed** (check dimensions after decode)
- **Skip WebP conversion if image is already PNG/JPG** (only convert WebP)
- **Fast resize algorithm**: Use `imaging.Lanczos` (good quality) or `imaging.NearestNeighbor` (fastest)
- **Skip normalization check**: Always normalize to 8-bit NRGBA (fast operation, ensures compatibility)
- **Early cache check**: Check cache before any download/processing
- **Parallel processing**: Process multiple images concurrently (50+ goroutines)

### 7. Connection Pooling and HTTP Optimization

**File**: `internal/cache/cache.go`

**HTTP Client Optimization**:
- **Connection pooling**: Reuse connections (already done, increase limits)
- **Keep-alive**: Enable HTTP keep-alive (default, verify)
- **Compression**: Accept gzip/deflate for faster downloads (add Accept-Encoding header)
- **Timeout**: Reduce timeout to 5s for faster failure detection
- **Max connections**: Increase to 100+ for parallel downloads
- **Max connections per host**: Increase to 50+ for S3
- **Idle connection timeout**: 90s (keep connections alive)

**Implementation**:
```go
transport := &http.Transport{
    MaxIdleConns:        200,
    MaxIdleConnsPerHost: 50,
    IdleConnTimeout:     90 * time.Second,
    DisableCompression:  false, // Enable compression
}
httpClient = &http.Client{
    Timeout:   5 * time.Second,
    Transport: transport,
}
```

### 8. Image Size Optimization

**Resize Strategy**:
- Calculate exact pixel dimensions from mm dimensions and DPI
- Request/resize to exact size (no larger)
- Use fast resize algorithm (nearest neighbor for speed, or bilinear for quality)
- Skip resize if image is already smaller than needed

**Formula**:
```go
pixelWidth := int(widthMM * dpi / 25.4)
pixelHeight := int(heightMM * dpi / 25.4)
```

### 9. Batch Processing

**File**: `internal/cache/cache.go`

**Batch Download Function**:
- Download multiple images in parallel
- Process (resize/convert) in parallel
- Return map of URL -> image bytes
- Use worker pool pattern for efficiency

### 10. Performance Monitoring

**Add Timing**:
- Log download time
- Log processing time
- Log cache hit rate
- Target: < 30ms total per request

## Files to Modify

1. `internal/cache/cache.go`:
   - Add `GetImageDataDirect(url, widthMM, heightMM, dpi)` function
   - Add `PreloadImagesDirect(requests []ImageRequest)` function
   - Add in-memory image data cache (raw bytes, not base64)
   - Optimize HTTP client (compression, more connections)
   - Remove base64 functions (not needed)

2. `internal/generator/generator.go`:
   - Replace `imageBase64Cache` with `imageDataCache map[string][]byte` (raw bytes)
   - Modify `renderImage()` to use direct image bytes (not base64)
   - Register images from memory bytes using `RegisterImageOptionsReader()`
   - Calculate exact pixel dimensions from layer size and DPI
   - Remove base64 encoding/decoding

3. `internal/handlers/handlers.go`:
   - Collect image URLs with dimensions from all layers (recursive)
   - Create `ImageRequest` structs with URL, width, height, DPI
   - Use `PreloadImagesDirect()` instead of base64 preloading
   - Pass raw bytes cache to generator

## Performance Targets

- **Single image load (cached)**: < 1ms (memory cache hit)
- **Single image load (download + process)**: < 20ms (parallel, optimized)
- **Multiple images (5-10, cached)**: < 5ms total
- **Multiple images (5-10, download)**: < 30ms total (parallel processing)
- **PDF generation (with images)**: < 30ms total
- **Cache hit rate**: > 90% after warmup

## Implementation Steps

1. **Add direct image loading function** (no file I/O)
2. **Add in-memory image data cache**
3. **Optimize HTTP client** (connection pooling, compression)
4. **Modify renderImage()** to use direct bytes
5. **Add size-aware preloading** (collect dimensions from layers)
6. **Implement parallel batch processing**
7. **Add performance monitoring**
8. **Test and optimize**

## Key Optimizations

1. **No File I/O**: Everything in memory
2. **Exact Size**: Request/resize to exact dimensions needed
3. **Aggressive Caching**: In-memory cache with long TTL
4. **Parallel Processing**: Download and process multiple images concurrently
5. **Skip Unnecessary Work**: Only convert/resize if needed
6. **Fast Algorithms**: Use fastest resize/convert algorithms
7. **Connection Reuse**: Reuse HTTP connections
8. **Compression**: Accept gzip for faster downloads

## Expected Performance

- **First request (cold cache)**: ~50-100ms (download + process in parallel)
- **Subsequent requests (warm cache)**: < 30ms (memory cache hit, no processing)
- **Concurrent requests**: < 30ms each (parallel processing, connection reuse)
- **Single image (cached)**: < 1ms (direct memory access)
- **Single image (download)**: < 20ms (optimized download + resize)

## Key Performance Optimizations

1. **Zero File I/O**: All processing in memory
2. **No Base64**: Direct raw bytes (no encoding/decoding overhead)
3. **Exact Size**: Resize to exact dimensions (smaller files, faster processing)
4. **Aggressive Caching**: Size-specific cache keys (reuse resized images)
5. **Parallel Processing**: 50+ concurrent downloads/processing
6. **Connection Reuse**: HTTP keep-alive, connection pooling
7. **Compression**: Accept gzip for faster downloads
8. **Fast Algorithms**: Use fastest resize algorithms
9. **Early Cache Check**: Check cache before any work
10. **Skip Unnecessary Work**: Only resize/convert if needed

