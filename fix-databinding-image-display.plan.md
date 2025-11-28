# Fix DataBinding Image Display Issue

## Problem Statement

Images from `dataBinding` fields are not displaying in PDFs. The flow should be:

1. **Template Layer**: Has `dataBinding: "customFields.d1c3dc73-86ef-47b0-af79-bc5047f2c100"`
2. **User Data**: Has `CustomFieldValues` with `fieldId: "d1c3dc73-86ef-47b0-af79-bc5047f2c100"` and `value: "https://s3...webp"`
3. **Expected**: Image should be retrieved from S3 and displayed in the layer

## Current Implementation Analysis

### Current Flow

1. **Image Preloading** (`handlers.go`):
   - Collects template assets (from `template.Assets`)
   - Collects user photos from `customFieldValues` where `fieldType == "file"`
   - Checks `dataBinding` fields in layers to get image URLs
   - Uses `PreloadImages()` (file-based) or `PreloadImagesAsBase64()` (base64)

2. **Image Rendering** (`generator.go` - `renderImage()`):
   - Checks if layer has `dataBinding`
   - Extracts fieldID: `strings.TrimPrefix(layer.DataBinding, "customFields.")`
   - Gets value: `g.user.GetFieldValue(fieldID)`
   - Uses base64 cache if available, falls back to file path

### Issues Identified

1. **Handlers Rejected**: The handlers still use `PreloadImages()` (file-based) instead of `PreloadImagesAsBase64()`
   - This means images are processed as files, not base64
   - The base64 cache is empty, so it falls back to file paths
   - File-based approach has the performance issues we saw

2. **Image Preloading Logic**: 
   - Current code checks `layer.DataBinding` but might miss some cases
   - Need to ensure ALL image URLs from dataBinding are collected
   - Need to handle both assets and customFields properly

3. **Field Lookup**: 
   - `GetFieldValue()` should work correctly
   - But need to verify the fieldID matching is exact
   - Need to add better debugging

4. **Base64 Cache Not Set**: 
   - Since handlers use `SetImageCache()` instead of `SetImageBase64Cache()`
   - The base64 cache is empty
   - Falls back to slow file-based approach

## Solution Plan

### 1. Fix Handlers to Use Base64 (Respect Rejected Changes)

**File**: `internal/handlers/handlers.go`

Since the handlers changes were rejected, we need to:
- Keep the existing structure but update to use base64
- Change `PreloadImages()` to `PreloadImagesAsBase64()`
- Change `SetImageCache()` to `SetImageBase64Cache()`
- Ensure all image URLs are collected correctly

**Changes**:
- Line 97: Change `cache.PreloadImages(imageURLs)` to `cache.PreloadImagesAsBase64(imageURLs)`
- Line 104: Change `gen.SetImageCache(imageCache)` to `gen.SetImageBase64Cache(imageBase64Cache)`
- Update variable name from `imageCache` to `imageBase64Cache`

### 2. Improve Image URL Collection

**File**: `internal/handlers/handlers.go` - `GenerateBadge()`

**Current Logic**:
- Collects assets from `template.Assets`
- Collects from `customFieldValues` where `fieldType == "file"`
- Checks `dataBinding` in layers

**Improvements Needed**:
- Ensure ALL layers with `dataBinding` are checked (not just `type == "image"`)
- Verify the field lookup works correctly
- Add deduplication to avoid downloading same image twice
- Log collected URLs for debugging

### 3. Verify DataBinding Field Lookup

**File**: `internal/generator/generator.go` - `renderImage()`

**Current Logic**:
```go
fieldID := strings.TrimPrefix(layer.DataBinding, "customFields.")
imageURL = g.user.GetFieldValue(fieldID)
```

**Verification**:
- Ensure `fieldID` extraction is correct
- Verify `GetFieldValue()` matches exactly
- Add detailed logging to trace the lookup process
- Log the fieldID, found value, and whether it's a valid URL

### 4. Add Better Error Handling and Debugging

**File**: `internal/generator/generator.go` - `renderImage()`

- Add logging when dataBinding field is found
- Log the imageURL retrieved
- Log whether base64 cache has the image
- Log any errors during image processing

**File**: `internal/handlers/handlers.go`

- Log collected image URLs
- Log which URLs came from assets vs dataBinding
- Log any missing fields

### 5. Ensure Base64 Cache is Populated

**File**: `internal/generator/generator.go` - `renderImage()`

- Verify base64 cache is checked first
- If base64 cache is empty, log warning
- Ensure fallback to file path works correctly

## Implementation Steps

1. **Update handlers.go**:
   - Change to use `PreloadImagesAsBase64()`
   - Change to use `SetImageBase64Cache()`
   - Improve image URL collection logic
   - Add logging

2. **Verify generator.go**:
   - Ensure dataBinding lookup is correct
   - Add detailed logging for debugging
   - Verify base64 cache usage

3. **Test with payload**:
   - Use the provided payload.json
   - Verify fieldID `d1c3dc73-86ef-47b0-af79-bc5047f2c100` is found
   - Verify image URL is retrieved
   - Verify image is preloaded as base64
   - Verify image displays in PDF

## Expected Outcomes

- Images from `dataBinding` fields display correctly in PDFs
- All images (assets and customFields) are preloaded as base64
- Performance is optimized (using base64 instead of files)
- Better debugging information to trace any issues
- Field lookup works correctly for all UUIDs

## Testing Checklist

- [ ] Template assets display correctly
- [ ] Images from `dataBinding: "customFields.{uuid}"` display correctly
- [ ] Field lookup finds the correct fieldID
- [ ] Image URL is retrieved from customFieldValues
- [ ] Image is preloaded as base64
- [ ] Image appears in PDF at correct position
- [ ] Performance is acceptable (< 1s for cached images)

## Files to Modify

1. `internal/handlers/handlers.go` - Update to use base64 preloading
2. `internal/generator/generator.go` - Add better debugging for dataBinding

