# Final Fix: WebP S3 Images Display and Font Sizing

## Critical Issues Remaining

1. **WebP Images Not Displaying**: User photos from S3 (WebP format) are still not appearing in PDFs
2. **Font Sizing Incorrect**: Font sizes don't match expected output - text appears smaller than template

## Root Cause Analysis

### WebP Display Issue

**Problem**: WebP images are converted to PNG but may not be rendering correctly

**Potential Causes**:
1. **Cache Path Mismatch**: After WebP→PNG conversion, the converted path isn't cached in `imageCache` map
   - Original: `imageCache[imageURL] = webpPath`
   - After conversion: `imagePath = convertedPNGPath` but cache not updated
   - Next render might use old cached WebP path

2. **Normalization Skipped**: Converted PNG from WebP might be skipped for normalization (if small)
   - User photos are typically small (26.1 x 39.6 mm)
   - Code skips normalization for images < 50mm
   - But converted PNG might still need normalization

3. **File Extension Detection**: `getImageType()` might not detect WebP correctly if file extension is wrong
   - Cache saves with extension from URL (`.webp`)
   - But `getImageType()` reads from file path

4. **Image Validation Timing**: Validation happens before conversion, but converted file might fail

### Font Sizing Issue

**Problem**: Font sizes appear smaller than expected in output PDF

**Root Cause**: Unknown whether template font sizes are in pixels (px) or points (pt)

**Two Possible Scenarios**:
1. **If in Pixels (px)**: Need conversion `pt = px * (72 / DPI) = px * 0.24` for 300 DPI
   - `60.23px → 14.455pt`
   - Current implementation uses this, but might still be wrong

2. **If in Points (pt)**: Should use directly without conversion
   - `60.23pt → 60.23pt` (no conversion)
   - This would make text much larger, matching expected output

**Additional Issues**:
1. **Auto Font Size Override**: When `autoFontSize: true`, it overrides the base fontSize
   - Payload shows `fontSize: 60.23` with `autoFontSize: true`
   - Current: `calculateAutoFontSize()` uses `height * 2.83` which ignores template fontSize
   - Should use template fontSize as maximum/starting point

2. **Auto Font Size Calculation**: The binary search might be finding too small a size
   - Starts with `height * 2.83` which might be too conservative
   - Should consider the base `fontSize` from template as a starting point
   - Need to respect the template's intended size even when auto-sizing

## Implementation Plan

### 1. Fix WebP Display Issue

**File**: `internal/generator/generator.go` - `renderImage()`

- **Update cache after conversion**: After WebP→PNG conversion, update `imageCache` with converted path
  - Store both: `imageCache[imageURL] = originalPath` and `imageCache[imageURL+".png"] = convertedPath`
  - Or update the cache entry to point to converted path

- **Force normalization for converted WebP**: Don't skip normalization for WebP-converted PNGs
  - Even if small, normalize converted PNGs to ensure compatibility
  - Add flag: `isConvertedFromWebP` to track this

- **Better WebP detection**: Improve `getImageType()` to detect WebP from file content if extension fails
  - Check file extension first
  - If ambiguous, try to read file header

- **Validate converted file before use**: Ensure converted PNG is valid before rendering
  - Check file size > 0
  - Try to open with imaging library to verify it's valid

**File**: `internal/cache/cache.go` - `GetImagePath()`

- **Preserve WebP extension**: Ensure downloaded WebP files keep `.webp` extension
  - Current code uses extension from URL, which should work
  - But verify it's not defaulting to `.png`

### 2. Fix Font Sizing Issue (Test Both Approaches)

**File**: `internal/generator/generator.go` - `renderText()`

- **Add font size unit detection**: 
  - Add environment variable or config flag: `FONT_SIZE_UNIT=px|pt|auto`
  - Default to `auto` which tests both approaches
  - Log both calculated sizes for comparison

- **Test both conversion methods**:
  - **Method 1 (Pixels)**: `fontSizePt = fontSize * (72.0 / DPI)` - current approach
  - **Method 2 (Points)**: `fontSizePt = fontSize` - direct use, no conversion
  - Add logging to show both values: `fmt.Printf("Font size: original=%.2f, as_px=%.2fpt, as_pt=%.2fpt\n", ...)`

- **Fix auto font size calculation**: 
  - When `autoFontSize: true`, use the template's `fontSize` as maximum/starting point
  - Don't ignore the base fontSize - it represents the template's intended size
  - Current: `height * 2.83` ignores the template fontSize completely
  - New: Use `min(convertedFontSize, height * 2.83)` where convertedFontSize respects template intent
  - Pass base fontSize to `calculateAutoFontSize()` as maximum

- **Improve conversion logic**:
  - If `autoFontSize: false`: 
    - Try both: `fontSize * (72.0 / DPI)` and `fontSize` directly
    - Log both for comparison
  - If `autoFontSize: true`: 
    - Use template fontSize (converted or direct) as maximum
    - Then fit to box, but don't exceed template's intended size

**File**: `internal/generator/generator.go` - `calculateAutoFontSize()`

- **Accept base fontSize parameter**: 
  - Add parameter: `baseFontSize float64` (the template's fontSize after optional conversion)
  - Use it as maximum size: `maxSize = min(baseFontSize, height * 2.83)`
  - Don't let auto-sizing make text larger than template intended
  - Don't let auto-sizing ignore template's base size completely

- **Respect template intent**:
  - Template provides `fontSize: 60.23` as a guide
  - Auto-sizing should fit within that constraint
  - Current implementation ignores this completely

### 3. Performance Enhancements

**File**: `internal/generator/generator.go`

- **Parallel image processing**: Process WebP conversion and normalization concurrently
- **Cache converted paths**: Store WebP→PNG conversions in cache to avoid re-conversion
- **Batch validation**: Validate multiple images in parallel

**File**: `internal/cache/cache.go`

- **Pre-validate URLs**: Check if URLs are accessible before downloading
- **Retry logic**: Add retry for failed downloads (with exponential backoff)

### 4. Debugging and Logging

- **Add detailed logging**: Log each step of image processing
  - Log when WebP is detected
  - Log conversion success/failure
  - Log final image path used
  - Log font size calculations

- **Add validation checks**: Verify images are actually rendered
  - Check if ImageOptions succeeds
  - Log image dimensions and positions

## Files to Modify

1. `internal/generator/generator.go` - Fix WebP conversion caching, fix font size calculation
2. `internal/cache/cache.go` - Ensure WebP extension preservation

## Expected Outcomes

- WebP user photos display correctly in PDFs (converted and cached properly)
- Font sizes match template exactly (text appears at correct size)
  - Will test both px→pt conversion and direct pt use
  - Logging will show which approach works better
- Auto font size respects template's base fontSize (doesn't ignore it)
- Better performance with parallel processing and caching
- Detailed logging for debugging (font sizes, image paths, conversions)

## Font Size Testing Strategy

1. **Add logging** to show both conversion methods:
   - `fontSize * 0.24` (if px)
   - `fontSize` (if pt)
   - Compare output PDFs with both

2. **Test with known values**:
   - Template: `fontSize: 60.23`
   - Method 1 (px): `60.23 * 0.24 = 14.455pt`
   - Method 2 (pt): `60.23pt` directly
   - See which matches expected PDF better

3. **Auto font size fix**:
   - Always respect template fontSize as maximum
   - Use it as starting point, not ignore it
   - Fit to box but don't exceed template intent

## Testing Strategy

1. **WebP Test**: 
   - Use payload with WebP user photo
   - Verify conversion succeeds
   - Verify converted PNG is cached
   - Verify image appears in PDF
   - Check logs for conversion path

2. **Font Size Test**: 
   - Generate PDF with both conversion methods (px and pt)
   - Compare with expected PDF
   - Review logs to see calculated sizes
   - Determine which method matches better

3. **Auto Font Size Test**:
   - Test with `autoFontSize: true` and `fontSize: 60.23`
   - Verify it respects the base fontSize
   - Verify it doesn't exceed template intent
   - Verify it fits text to box

4. **Performance Test**: 
   - Measure time for single request with images
   - Compare before/after optimizations
   - Target: < 500ms (cached), < 1.5s (downloading)

5. **Log Analysis**: 
   - Review logs for:
     - WebP conversion paths
     - Font size calculations (both methods)
     - Image cache hits/misses
     - Any errors or warnings

