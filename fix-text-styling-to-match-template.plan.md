# Fix Text Styling to Match Template Exactly

## Problem
Text styling is not displaying correctly according to template specifications. Text should render exactly as defined in the template object with proper font size, alignment, color, weight, and positioning.

## Root Cause Analysis

### Issues Identified:

1. **Font Size Conversion May Be Incorrect**
   - Current: `fontSize * (72.0 / float64(g.dpi))` converts px to pt
   - For 300 DPI: `60.23px * 0.24 = 14.45pt` (seems too small)
   - Template might expect direct point values or different conversion

2. **Font Size Clamping Too Restrictive**
   - Max clamped to 72pt, but template might need larger sizes
   - Min clamped to 4pt, might be too small for some templates

3. **Auto Font Size Interfering**
   - Auto font size might override template's intended size
   - Should only apply when explicitly enabled AND respect template max

4. **Text Alignment Issues**
   - Vertical alignment always "M" (middle), might need top/bottom
   - Horizontal alignment might not be working correctly
   - gofpdf alignment string format: "CM" = Center/Middle

5. **Text Wrapping Not Handled**
   - Long text might overflow or truncate
   - No word wrapping for text that exceeds cell width

6. **Text Positioning Within Cell**
   - SetXY sets position, but CellFormat might not position text correctly
   - Need to ensure text is properly centered/positioned within bounds

7. **Font Weight Limited**
   - Only handles "bold", might need "normal", "light", etc.

8. **Multi-line Text Handling**
   - Line height calculation might be incorrect
   - Vertical spacing might not match template

## Solution Plan

### 1. Fix Font Size Conversion
**File**: `internal/generator/generator.go` - `renderText()` function

**Current Issue**: Font size conversion might be wrong. Template font sizes (e.g., 60.23) might already be in points, not pixels.

**Fix**:
- Test if template font sizes are in points or pixels
- If template uses CSS pixels (px), conversion is: `pt = px * (72 / DPI)`
- If template uses points (pt), use directly: `pt = px`
- Based on payload analysis, font sizes like 60.23 seem like they should be points, not pixels
- **Decision**: Use direct conversion: `fontSize := layer.Style.FontSize` (assume template provides points)
- **Alternative**: Add environment variable to toggle conversion method for testing

### 2. Remove or Adjust Font Size Clamping
**File**: `internal/generator/generator.go` - `renderText()` function

**Current**: Clamps to 4-72pt
**Fix**:
- Remove max clamp (or increase to 200pt)
- Keep min clamp at 4pt for safety
- Let template control font size directly

### 3. Fix Auto Font Size Logic
**File**: `internal/generator/generator.go` - `renderText()` and `calculateAutoFontSize()`

**Current Issue**: Auto font size might ignore template's base fontSize
**Fix**:
- Ensure auto font size respects template's max fontSize
- Only apply auto sizing when `autoFontSize: true`
- Use template fontSize as maximum limit
- Fix `calculateAutoFontSize()` to properly respect maxFontSize parameter

### 4. Fix Text Alignment
**File**: `internal/generator/generator.go` - `renderText()` function

**Current Issue**: Alignment might not work correctly
**Fix**:
- Verify gofpdf alignment string format:
  - Horizontal: L (left), C (center), R (right)
  - Vertical: T (top), M (middle), B (bottom)
  - Combined: "CM" = center horizontal, middle vertical
- Ensure "center" maps to "C" for horizontal
- Add vertical alignment support if needed (default to "M")
- Test that text is actually centered/left/right aligned

### 5. Add Text Wrapping Support
**File**: `internal/generator/generator.go` - `renderText()` function

**Current Issue**: Long text might overflow
**Fix**:
- Use `MultiCell()` instead of `CellFormat()` for text that might wrap
- Calculate if text width exceeds cell width
- If exceeds, use MultiCell with word wrapping
- Otherwise, use CellFormat for single-line text

### 6. Fix Text Positioning
**File**: `internal/generator/generator.go` - `renderText()` function

**Current Issue**: Text might not be positioned correctly within cell
**Fix**:
- Ensure SetXY sets correct position
- Use CellFormat with proper width/height
- Verify text is within layer bounds
- Test with different alignments and positions

### 7. Enhance Font Weight Support
**File**: `internal/generator/generator.go` - `renderText()` function

**Current**: Only "bold" -> "B"
**Fix**:
- Map font weights correctly:
  - "bold" or "700" -> "B"
  - "normal" or "400" -> ""
  - "light" or "300" -> "" (gofpdf doesn't support, use normal)
- Default to "" (normal) if not specified

### 8. Fix Multi-line Text
**File**: `internal/generator/generator.go` - `renderText()` function

**Current Issue**: Line height calculation might be wrong
**Fix**:
- Calculate line height based on font size, not just dividing height
- Use proper line spacing (typically 1.2x font size)
- Ensure lines don't overlap
- Use MultiCell for better multi-line support

### 9. Add Text Opacity Support
**File**: `internal/generator/generator.go` - `renderText()` function

**Current**: Opacity not applied to text
**Fix**:
- gofpdf doesn't directly support text opacity
- Would need to pre-render text to image with alpha channel (complex)
- For now, skip opacity for text (most templates use opacity: 1)

### 10. Verify All Style Properties
**File**: `internal/generator/generator.go` - `renderText()` function

**Checklist**:
- ✅ fontSize - Fix conversion
- ✅ fontFamily - Working (with fallback)
- ✅ fontWeight - Enhance support
- ✅ color - Working (hexToRGB)
- ✅ textAlign - Verify alignment
- ⚠️ opacity - Not supported by gofpdf
- ⚠️ rotation - Not supported by gofpdf (would need image pre-processing)

## Implementation Steps

### Step 1: Fix Font Size (Critical)
1. Change font size conversion to use direct value (assume template provides points)
2. Remove max clamp or increase significantly
3. Test with payload font sizes (60.23, 49.6)

### Step 2: Fix Auto Font Size
1. Ensure calculateAutoFontSize respects maxFontSize
2. Only apply when autoFontSize is true
3. Use template fontSize as maximum

### Step 3: Fix Text Alignment
1. Verify alignment string format
2. Test center, left, right alignment
3. Ensure text is properly positioned

### Step 4: Add Text Wrapping
1. Detect if text exceeds cell width
2. Use MultiCell for wrapping text
3. Use CellFormat for single-line text

### Step 5: Fix Multi-line Text
1. Improve line height calculation
2. Use proper line spacing
3. Test with multi-line content

### Step 6: Test and Verify
1. Test with payload.json
2. Verify all text layers render correctly
3. Compare with template expectations

## Files to Modify

1. `internal/generator/generator.go`
   - `renderText()` function - Main text rendering logic
   - `calculateAutoFontSize()` function - Auto sizing logic

## Expected Outcomes

- Text displays at correct font size (matching template)
- Text is properly aligned (center, left, right)
- Text wraps correctly when too long
- Multi-line text has proper spacing
- Font weight (bold/normal) works correctly
- Text color matches template
- Auto font size respects template limits
- All text layers render exactly as template specifies

## Testing

- Test with payload.json provided
- Verify text layers:
  - Layer 1763558919084: fontSize 60.23, bold, center, autoFontSize true
  - Layer 1763559051656: fontSize 60.23, bold, center, autoFontSize true
  - Layer 1763559103536: fontSize 60.23, bold, center, autoFontSize true
- Verify text content resolves placeholders correctly
- Verify text fits within layer bounds
- Verify alignment matches template

## Notes

- gofpdf limitations:
  - Text opacity not directly supported
  - Text rotation not directly supported
  - Limited font weight options (normal, bold)
- For opacity/rotation, would need to pre-render text to image (complex, not recommended)
- Focus on getting core styling (size, alignment, color, weight) working perfectly first

