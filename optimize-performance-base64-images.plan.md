# Optimize Performance and Fix Image Display with Base64

## Critical Issues

1. **Performance Degraded**: Request time increased from 2.53s to 8.18s
2. **Images Not Displaying**: WebP images still not appearing in PDFs

## Root Causes

### Performance Issues

1. **Forced Normalization**: WebP-converted PNGs are always normalized, even if already 8-bit
   - `normalizePNGTo8Bit()` uses `imaging.Open()` and `imaging.Save()` which are expensive
   - Multiple file I/O operations per image

2. **Multiple File Validations**: 
   - `os.Stat()` called multiple times per image
   - Validation after conversion, after normalization, etc.

3. **Synchronous Processing**: All image operations happen sequentially
   - WebP conversion blocks
   - Normalization blocks
   - No parallel processing

4. **File I/O Overhead**: Reading/writing files from disk is slow
   - Each image: download → save → read → convert → save → read → normalize → save → read

### Image Display Issues

1. **Path Mismatch**: After conversion/normalization, path might not match what gofpdf expects
2. **File Not Found**: Normalized file might not exist when gofpdf tries to read it
3. **Timing Issue**: File might not be fully written when gofpdf tries to read it

## Solution: Use Base64/In-Memory Processing

**Benefits**:
- No file I/O during PDF generation (only during preload)
- Faster processing (in-memory operations)
- More reliable (no file path issues)
- Can process images in parallel

**Approach**:
1. Preload images and convert to base64 in memory
2. Use `gofpdf.RegisterImageOptions()` to register images from memory
3. Process WebP conversion and normalization in memory
4. Cache base64 strings instead of file paths

## Implementation Plan

### 1. Add Base64 Image Cache

**File**: `internal/generator/generator.go`

- **Add base64 cache field**: `imageBase64Cache map[string]string` (URL -> base64 string)
- **Modify SetImageCache**: Accept both file paths and base64 strings
- **New method**: `SetImageBase64Cache(cache map[string]string)` to set base64 images

### 2. Optimize Image Preloading

**File**: `internal/cache/cache.go`

- **Add base64 conversion**: After downloading, convert to base64 in memory
- **Return both**: Return map with both file paths (for compatibility) and base64 strings
- **Process in parallel**: Convert multiple images to base64 concurrently

**New function**: `PreloadImagesAsBase64(urls []string) map[string]string`
- Downloads images
- Converts WebP to PNG in memory (using imaging library)
- Normalizes PNG to 8-bit in memory
- Converts to base64
- Returns map: URL -> base64 string

### 3. Use gofpdf RegisterImageOptions

**File**: `internal/generator/generator.go` - `renderImage()`

- **Check for base64 first**: If image in base64 cache, use it
- **Register image**: Use `pdf.RegisterImageOptions()` with base64 data
- **Fallback to file**: If no base64, use file path (for backward compatibility)
- **Remove file operations**: No more `os.Stat()`, file reading during render

### 4. Optimize WebP Conversion

**File**: `internal/generator/generator.go` or new helper

- **In-memory conversion**: Convert WebP to PNG in memory (no file write)
- **Use imaging library**: `imaging.Decode()` and `imaging.Encode()` in memory
- **Skip file I/O**: Process entirely in memory

### 5. Optimize Normalization

**File**: `internal/generator/generator.go` or new helper

- **In-memory normalization**: Normalize PNG to 8-bit in memory
- **Skip if already 8-bit**: Check image format before normalizing
- **Fast path**: If already 8-bit NRGBA, skip normalization

### 6. Performance Optimizations

- **Remove unnecessary validations**: Don't validate files if using base64
- **Parallel processing**: Process multiple images concurrently during preload
- **Cache base64**: Store base64 strings in memory cache (faster than file cache)
- **Skip normalization check**: If using base64, normalization already done during preload

## Files to Modify

1. `internal/generator/generator.go` - Add base64 support, use RegisterImageOptions
2. `internal/cache/cache.go` - Add base64 conversion, in-memory processing
3. `internal/handlers/handlers.go` - Use base64 preloading

## Expected Outcomes

- **Performance**: < 1s for requests with images (from 8.18s)
- **Images Display**: All images (including WebP) appear correctly
- **Memory Usage**: Slightly higher (base64 strings in memory) but acceptable
- **Reliability**: No file path issues, no timing problems

## Implementation Steps

1. Add base64 cache to PDFGenerator
2. Create `PreloadImagesAsBase64()` function
3. Modify `renderImage()` to use base64 when available
4. Optimize WebP conversion to in-memory
5. Optimize normalization to in-memory
6. Update handlers to use base64 preloading
7. Test and measure performance

## Performance Targets

- Single request with images: < 1s (when cached)
- Single request with images: < 2s (when downloading)
- Concurrent requests: < 1ms per request (when cached)

