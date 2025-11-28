# Fix S3 Images Display and Template Format Matching

## Issues Identified

1. **S3 Images Not Displaying**: User photos from S3 are not appearing in PDFs
2. **Template Format Mismatch**: PDF output doesn't match template format (font sizes, positioning, styling)
3. **Performance**: Needs further optimization

## Root Causes

### S3 Images Not Displaying

1. **Asset Key Mismatch**: Payload shows asset key is `"asset_0_1763558759124"` but layer content is `"asset_0"`. Current `strings.Contains()` should work, but needs verification
2. **Silent Failures**: When `imageURL` is empty, function returns `nil` silently without error
3. **Image Path Validation**: No verification that downloaded image file actually exists before rendering
4. **Error Handling**: Errors during image download/normalization might be swallowed
5. **Cache Miss Handling**: If image not in preload cache, download happens but might fail silently
6. **File Existence Check**: No validation that `imagePath` file exists before calling `ImageOptions()`
7. **WebP to PNG Conversion**: User photos are WebP format (`7882919302_d1c3dc73-86ef-47b0-af79-bc5047f2c100_20251128_103052.webp`) - conversion might be failing silently

### Template Format Mismatch

1. **Font Size Conversion**: Current code uses `fontSize / 5.0` which is incorrect:
   - Payload shows: `fontSize: 60.23` for text layers, `fontSize: 49.6` for containers
   - Current: `60.23 / 5.0 = 12.046pt` (WRONG)
   - Should be: `60.23 * 0.24 = 14.455pt` (for 300 DPI)
   - Template uses DPI 300, font sizes are in pixels (px) that need proper conversion
   - Conversion: `pt = px * (72 / DPI) = px * 0.24` for 300 DPI

2. **Text Alignment**: Current alignment might not match template exactly
   - `"center"` uses `"CM"` but vertical alignment might be wrong
   - Need to verify text positioning matches template

3. **Image Opacity**: Template has `opacity: 1` in style but it's not being applied to images

4. **Image Rotation**: Template supports `rotation` in style but it's not implemented

5. **Font Family Fallback**: When Arial not available, falls back to Helvetica which might look different

6. **Auto Font Size**: Calculation might not match template's auto-sizing behavior

## Implementation Plan

### 1. Fix S3 Images Not Displaying

**File**: `internal/generator/generator.go` - `renderImage()`

- **Fix asset key matching**: Verify `strings.Contains()` works correctly for timestamped keys like `"asset_0_1763558759124"` matching `"asset_0"`
- **Improve asset lookup**: Try exact match first, then fallback to contains match
- **Add file existence validation**: Before calling `ImageOptions()`, verify file exists and is readable with `os.Stat()`
- **Improve error handling**: Return detailed errors instead of silent failures - include layer ID, imageURL, and file path
- **Add image validation**: Check image file is valid (not corrupted) before rendering - verify file size > 0
- **Better logging**: Log image URL, path, asset key matching, and any errors for debugging
- **Verify image download**: Ensure downloaded images are complete and valid
- **WebP conversion validation**: Verify WebP to PNG conversion succeeds and file exists before using

**File**: `internal/cache/cache.go` - `GetImagePath()`

- **Validate downloaded files**: Check file size > 0 after download
- **Verify image format**: Ensure downloaded file is actually an image
- **Better error messages**: Include URL and path in error messages

### 2. Fix Template Format Matching

**File**: `internal/generator/generator.go` - `renderText()`

- **Fix font size conversion**: 
  - Payload shows font sizes: `60.23px` for text, `49.6px` for containers
  - Template uses DPI 300 (from `settings.dpi`)
  - Conversion: `fontSizePt = fontSizePx * (72 / DPI)`
  - For 300 DPI: `fontSizePt = fontSizePx * 0.24`
  - Examples from payload: 
    - `60.23px → 14.455pt` (not `12.046pt` from current `/5.0`)
    - `49.6px → 11.904pt` (not `9.92pt` from current `/5.0`)
  - Remove the `/ 5.0` conversion factor completely
  - Use `settings.DPI` from template (default to 300 if not set)

- **Improve text alignment**:
  - Verify vertical alignment matches template
  - Ensure text positioning is pixel-perfect

- **Fix auto font size**:
  - Use proper DPI-aware calculations
  - Match template's auto-sizing behavior

**File**: `internal/generator/generator.go` - `renderImage()`

- **Add opacity support**: Apply `layer.Style.Opacity` to images (gofpdf supports this via image options)
- **Add rotation support**: Apply `layer.Style.Rotation` if present
- **Verify image dimensions**: Ensure images render at exact template size

**File**: `internal/generator/generator.go` - `NewPDFGenerator()`

- **Use template DPI**: Use `settings.DPI` from template (payload shows `300`)
- **Store DPI in generator**: Add `dpi` field to PDFGenerator struct for use in conversions
- **Default DPI**: If DPI not set in settings, default to 300 (standard print DPI)

### 3. Performance Optimizations

**File**: `internal/generator/generator.go`

- **Parallel image processing**: Process multiple images concurrently during normalization
- **Cache image metadata**: Store image dimensions to avoid repeated reads
- **Optimize font size calculations**: Cache font metrics
- **Lazy image loading**: Only process images when actually needed

**File**: `internal/cache/cache.go`

- **Parallel downloads**: Already implemented, but can optimize further
- **Connection reuse**: Already added, verify it's working
- **Image validation optimization**: Quick format check without full decode

**File**: `internal/handlers/handlers.go`

- **Pre-validate image URLs**: Check URLs are valid before preloading
- **Batch image validation**: Validate all images in parallel

### 4. Additional Improvements

- **Better error messages**: Include layer ID, image URL, and file path in errors
- **Debug mode**: Add flag to enable detailed logging
- **Image format detection**: Better detection of image formats
- **Fallback handling**: Better fallbacks when images fail to load

## Files to Modify

1. `internal/generator/generator.go` - Fix image rendering, font sizes, add opacity/rotation
2. `internal/cache/cache.go` - Improve image validation and error handling
3. `internal/handlers/handlers.go` - Add image URL validation

## Expected Outcomes

- S3 images display correctly in PDFs (both background assets and user photos)
- Asset key matching works with timestamped keys (`asset_0_1763558759124` matches `asset_0`)
- WebP user photos convert and display correctly
- Font sizes match template exactly:
  - `60.23px → 14.455pt` (not `12.046pt`)
  - `49.6px → 11.904pt` (not `9.92pt`)
- Text positioning and alignment match template (containers with flex layouts)
- Image opacity and rotation work correctly
- Performance: < 500ms (cached), < 1.5s (downloading)
- Better error messages for debugging (include layer ID, asset key, image URL)

## Font Size Conversion Formula

For 300 DPI templates (as in payload):
- Pixels to Points: `pt = px * (72 / 300) = px * 0.24`
- Examples from payload:
  - `60.23px → 14.455pt` (currently becomes `12.046pt` - WRONG)
  - `49.6px → 11.904pt` (currently becomes `9.92pt` - WRONG)

For other DPIs:
- `pt = px * (72 / dpi)`
- Always read DPI from `template.Design.Settings.DPI` (default to 300)

## Asset Key Matching

Payload shows:
- Layer content: `"asset_0"`
- Asset key: `"asset_0_1763558759124"` (includes timestamp)

Current code uses `strings.Contains(key, layer.Content)` which should work, but:
- Need to verify it matches correctly
- Consider exact match first, then contains fallback
- Log matching process for debugging

## Image Opacity Implementation

Payload shows `opacity: 1` in image layer styles. gofpdf's `ImageOptions` supports opacity via:
1. Apply opacity during image normalization (pre-process with imaging library)
2. Or use gofpdf's transparency features if available
3. Note: Opacity of 1.0 means fully opaque (no change needed), but other values need handling

## Container Layout Support

Payload shows complex nested containers with flex layouts:
- `flexDirection: "row"` and `"column"`
- `justifyContent: "space-around"` and `"space-evenly"`
- `alignItems: "center"`
- `flexGap: 0` and `20`

Current implementation should handle this, but verify:
- Child positioning within containers matches template exactly
- Flex gap is applied correctly
- Alignment works as expected

