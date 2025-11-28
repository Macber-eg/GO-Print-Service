package generator

import (
	"badge-service/internal/cache"
	"badge-service/internal/models"
	"bytes"
	"encoding/base64"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/jung-kurt/gofpdf"
	"github.com/skip2/go-qrcode"
	_ "golang.org/x/image/webp"
)

// PDFGenerator handles PDF generation for badges
type PDFGenerator struct {
	template        *models.Template
	user            *models.User
	pdf             *gofpdf.Fpdf
	imageCache      map[string]string // URL -> local path (for backward compatibility)
	imageBase64Cache map[string]string // URL -> base64 string (preferred, faster)
	scaleFactor     float64           // Scale from mm to points
	dpi             int               // DPI from template settings for font size conversion
	debugLog        bool              // Enable debug logging
}

// NewPDFGenerator creates a new PDF generator instance
func NewPDFGenerator(template *models.Template, user *models.User) *PDFGenerator {
	settings := template.Design.Settings
	
	// Use template dimensions (in mm)
	width := settings.PaperWidth
	height := settings.PaperHeight
	
	if width == 0 {
		width = template.Width
	}
	if height == 0 {
		height = template.Height
	}
	
	// Default to A4 if not specified
	if width == 0 {
		width = 210
	}
	if height == 0 {
		height = 297
	}
	
	// Create PDF with exact dimensions
	pdf := gofpdf.NewCustom(&gofpdf.InitType{
		OrientationStr: "P",
		UnitStr:        "mm",
		Size: gofpdf.SizeType{
			Wd: width,
			Ht: height,
		},
	})
	
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)
	pdf.AddPage()
	
	// Add Unicode font support if font files exist
	if _, err := os.Stat("fonts/arial.ttf"); err == nil {
		pdf.AddUTF8Font("Arial", "", "fonts/arial.ttf")
	}
	if _, err := os.Stat("fonts/arialbd.ttf"); err == nil {
		pdf.AddUTF8Font("Arial", "B", "fonts/arialbd.ttf")
	}
	
	// Get DPI from template settings (default to 300 if not set)
	dpi := settings.DPI
	if dpi == 0 {
		dpi = 300 // Standard print DPI
	}
	
	// Check if debug logging is enabled
	debugLog := os.Getenv("DEBUG_PDF") == "true"
	
	return &PDFGenerator{
		template:         template,
		user:             user,
		pdf:              pdf,
		imageCache:       make(map[string]string),
		imageBase64Cache: make(map[string]string),
		scaleFactor:      1.0,
		dpi:              dpi,
		debugLog:         debugLog,
	}
}

// SetImageCache sets pre-fetched image paths (for backward compatibility)
func (g *PDFGenerator) SetImageCache(cache map[string]string) {
	g.imageCache = cache
}

// SetImageBase64Cache sets pre-fetched images as base64 strings (preferred, faster)
func (g *PDFGenerator) SetImageBase64Cache(cache map[string]string) {
	g.imageBase64Cache = cache
}

// Generate creates the PDF and returns the bytes
func (g *PDFGenerator) Generate() ([]byte, error) {
	// 1. Get all layers and sort by zIndex
	layers := g.template.Design.Layers
	sort.Slice(layers, func(i, j int) bool {
		return layers[i].ZIndex < layers[j].ZIndex
	})
	
	// 2. Render each layer
	for _, layer := range layers {
		if !layer.Visible {
			continue
		}
		if err := g.renderLayer(layer, models.Position{X: 0, Y: 0}); err != nil {
			// Log error but continue rendering other layers
			fmt.Printf("Warning: failed to render layer %s: %v\n", layer.ID, err)
		}
	}
	
	// 3. Output PDF to buffer
	var buf bytes.Buffer
	if err := g.pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("failed to output PDF: %w", err)
	}
	
	return buf.Bytes(), nil
}

// renderLayer renders a single layer at the given parent position
func (g *PDFGenerator) renderLayer(layer models.Layer, parentPos models.Position) error {
	// Calculate absolute position
	absX := parentPos.X + layer.Position.X
	absY := parentPos.Y + layer.Position.Y
	
	switch layer.Type {
	case "text":
		return g.renderText(layer, absX, absY)
	case "qrcode":
		return g.renderQRCode(layer, absX, absY)
	case "image":
		return g.renderImage(layer, absX, absY)
	case "container":
		return g.renderContainer(layer, absX, absY)
	case "shape":
		return g.renderShape(layer, absX, absY)
	default:
		// Unknown layer type, skip
		return nil
	}
}

// renderText renders a text layer
func (g *PDFGenerator) renderText(layer models.Layer, x, y float64) error {
	// Resolve placeholders in content
	text := g.resolvePlaceholders(layer.Content)
	
	if strings.TrimSpace(text) == "" {
		return nil
	}
	
	// Set font style
	fontStyle := ""
	if layer.Style.FontWeight == "bold" {
		fontStyle = "B"
	}
	
	// Calculate font size - test both conversion methods
	// Template font sizes might be in pixels (px) or points (pt)
	// Method 1 (if px): pt = px * (72 / DPI) = px * 0.24 for 300 DPI
	// Method 2 (if pt): use directly without conversion
	fontSizeAsPx := layer.Style.FontSize * (72.0 / float64(g.dpi))
	fontSizeAsPt := layer.Style.FontSize // Direct use (if already in points)
	
	// Check environment variable to determine which method to use
	// FONT_SIZE_UNIT=pt means use directly, =px means convert, =auto means test both
	fontSizeUnit := os.Getenv("FONT_SIZE_UNIT")
	if fontSizeUnit == "" {
		fontSizeUnit = "px" // Default to px conversion
	}
	
	var fontSize float64
	if fontSizeUnit == "pt" {
		// Use directly as points (no conversion)
		fontSize = fontSizeAsPt
		fmt.Printf("Debug: Layer '%s' using PT method: %.2fpt (original=%.2f)\n", layer.ID, fontSize, layer.Style.FontSize)
	} else if fontSizeUnit == "auto" {
		// Test both - use px for now but log both
		fontSize = fontSizeAsPx
		fmt.Printf("Debug: Layer '%s' fontSize: original=%.2f, as_px=%.2fpt, as_pt=%.2fpt, DPI=%d, using=%.2fpt\n", 
			layer.ID, layer.Style.FontSize, fontSizeAsPx, fontSizeAsPt, g.dpi, fontSize)
	} else {
		// Default: px conversion
		fontSize = fontSizeAsPx
		fmt.Printf("Debug: Layer '%s' using PX method: %.2fpt (original=%.2fpx, DPI=%d)\n", layer.ID, fontSize, layer.Style.FontSize, g.dpi)
	}
	
	// Clamp font size to reasonable bounds
	if fontSize < 4 {
		fontSize = 4
	}
	if fontSize > 72 {
		fontSize = 72
	}
	
	// Auto font size: fit text to box
	// Pass the base fontSize (converted) as maximum to respect template intent
	if layer.AutoFontSize {
		// Use the converted fontSize as maximum - don't ignore template's intent
		originalFontSize := fontSize
		fontSize = g.calculateAutoFontSize(text, layer.Size.Width, layer.Size.Height, layer.Style.FontFamily, fontStyle, fontSize)
		if g.debugLog {
			fmt.Printf("Debug: Auto font size for layer '%s': calculated=%.2fpt (max was %.2fpt)\n", layer.ID, fontSize, originalFontSize)
		}
	}
	
	// Set font
	fontFamily := layer.Style.FontFamily
	if fontFamily == "" {
		fontFamily = "Arial"
	}
	
	// Try to use the font, fall back to Helvetica if not available
	// If Arial wasn't loaded (font file missing), gofpdf will automatically use Helvetica
	g.pdf.SetFont(fontFamily, fontStyle, fontSize)
	
	// Set text color
	r, gr, b := hexToRGB(layer.Style.Color)
	g.pdf.SetTextColor(r, gr, b)
	
	// Determine alignment
	alignStr := "LM" // Left, Middle (vertical)
	switch layer.Style.TextAlign {
	case "center":
		alignStr = "CM"
	case "right":
		alignStr = "RM"
	}
	
	// Draw text cell
	g.pdf.SetXY(x, y)
	
	// Handle multi-line text
	if strings.Contains(text, "\n") {
		lines := strings.Split(text, "\n")
		lineHeight := layer.Size.Height / float64(len(lines))
		for i, line := range lines {
			g.pdf.SetXY(x, y+float64(i)*lineHeight)
			g.pdf.CellFormat(layer.Size.Width, lineHeight, line, "", 0, alignStr, false, 0, "")
		}
	} else {
		g.pdf.CellFormat(layer.Size.Width, layer.Size.Height, text, "", 0, alignStr, false, 0, "")
	}
	
	return nil
}

// renderQRCode generates and renders a QR code
func (g *PDFGenerator) renderQRCode(layer models.Layer, x, y float64) error {
	// Generate QR content - use user identifier or custom content
	qrContent := layer.Content
	if qrContent == "" || strings.Contains(qrContent, "{{") {
		qrContent = g.user.Identifier
	}
	
	if qrContent == "" {
		qrContent = g.user.ID
	}
	
	// Check for cached QR code
	qrPath := cache.GetQRCodePath(qrContent)
	
	// Generate QR code if not cached
	if _, err := os.Stat(qrPath); os.IsNotExist(err) {
		// Calculate QR size in pixels (use larger size for quality)
		qrSize := int(layer.Size.Width * 10)
		if qrSize < 100 {
			qrSize = 256
		}
		if qrSize > 1024 {
			qrSize = 1024
		}
		
		qrBytes, err := qrcode.Encode(qrContent, qrcode.Medium, qrSize)
		if err != nil {
			return fmt.Errorf("failed to generate QR code: %w", err)
		}
		
		if err := os.WriteFile(qrPath, qrBytes, 0644); err != nil {
			return fmt.Errorf("failed to save QR code: %w", err)
		}
	}
	
	// QR codes are always generated as 8-bit PNG, no normalization needed
	// Draw QR code image
	g.pdf.ImageOptions(
		qrPath,
		x, y,
		layer.Size.Width, layer.Size.Height,
		false,
		gofpdf.ImageOptions{ImageType: "PNG"},
		0, "",
	)
	
	return nil
}

// renderImage renders an image layer
func (g *PDFGenerator) renderImage(layer models.Layer, x, y float64) error {
	var imageURL string
	var imageSource string // Track where the image URL came from for debugging
	
	// Check if this is an asset reference
	if strings.HasPrefix(layer.Content, "asset_") {
		// Try exact match first (for cases like "asset_0" matching "asset_0")
		if url, ok := g.template.Assets[layer.Content]; ok {
			imageURL = url
			imageSource = "asset:" + layer.Content
		} else {
			// Fallback: find asset URL with contains match (for timestamped keys like "asset_0_1763558759124")
			for key, url := range g.template.Assets {
				if strings.Contains(key, layer.Content) {
					imageURL = url
					imageSource = "asset:" + key
					if g.debugLog {
						fmt.Printf("Debug: Matched asset key '%s' to layer content '%s'\n", key, layer.Content)
					}
					break
				}
			}
		}
	} else if layer.DataBinding != "" {
		// Get image URL from user data binding
		fieldID := strings.TrimPrefix(layer.DataBinding, "customFields.")
		imageURL = g.user.GetFieldValue(fieldID)
		imageSource = "dataBinding:" + fieldID
		
		// Debug logging if field not found
		if imageURL == "" {
			fmt.Printf("Warning: dataBinding field '%s' not found or empty for layer '%s'\n", fieldID, layer.ID)
			// Check if field exists but has empty value
			if g.debugLog {
				for _, cf := range g.user.CustomFieldValues {
					if cf.FieldID == fieldID {
						fmt.Printf("Debug: Field '%s' exists but value is empty or not a valid URL\n", fieldID)
						break
					}
				}
			}
		}
	} else if layer.Content != "" && (strings.HasPrefix(layer.Content, "http://") || strings.HasPrefix(layer.Content, "https://")) {
		imageURL = layer.Content
		imageSource = "direct:" + layer.Content
	}
	
	// If image layer expects an image but URL is empty, log error but don't fail
	// (some layers might be optional)
	if imageURL == "" {
		if layer.DataBinding != "" || strings.HasPrefix(layer.Content, "asset_") {
			// This layer was expected to have an image, log warning
			fmt.Printf("Warning: Image layer '%s' has no image URL (source: %s)\n", layer.ID, imageSource)
		}
		return nil // No image to render
	}
	
	// Apply opacity check (gofpdf doesn't directly support opacity in ImageOptions)
	// For opacity < 1, we would need to pre-process the image, but for now we'll render
	// Opacity of 0 means fully transparent, skip rendering
	opacity := layer.Style.Opacity
	if opacity == 0 {
		return nil // Fully transparent, skip rendering
	}
	
	// Note: Rotation is not directly supported by gofpdf's ImageOptions
	// For rotation support, we would need to pre-process the image using imaging library
	// For now, we'll render without rotation (most templates use rotation: 0)
	rotation := layer.Style.Rotation
	if rotation != 0 {
		fmt.Printf("Warning: Image rotation (%f degrees) not yet implemented for layer '%s'\n", rotation, layer.ID)
		// TODO: Implement rotation using imaging library to pre-rotate the image
	}
	
	// PREFERRED: Use base64 cache if available (much faster, no file I/O)
	if base64Data, ok := g.imageBase64Cache[imageURL]; ok {
		if g.debugLog {
			fmt.Printf("Debug: Using base64 image for layer '%s' (size: %dx%dmm)\n", 
				layer.ID, int(layer.Size.Width), int(layer.Size.Height))
		}
		// Determine image type from URL or base64 data
		imageType := getImageTypeFromURL(imageURL)
		if imageType == "" {
			imageType = "PNG" // Default for base64 (already processed)
		}
		
		// Register image from base64 and get name
		imageName := fmt.Sprintf("img_%s", strings.ReplaceAll(imageURL, "/", "_"))
		imageName = strings.ReplaceAll(imageName, ":", "_")
		imageName = strings.ReplaceAll(imageName, ".", "_")
		
		// Decode base64 to bytes
		imageData, err := base64.StdEncoding.DecodeString(base64Data)
		if err != nil {
			return fmt.Errorf("layer '%s': failed to decode base64 image: %w", layer.ID, err)
		}
		
		// Register the image with gofpdf
		info := g.pdf.RegisterImageOptionsReader(imageName, gofpdf.ImageOptions{
			ImageType: imageType,
		}, bytes.NewReader(imageData))
		
		if info == nil {
			return fmt.Errorf("layer '%s': failed to register base64 image", layer.ID)
		}
		
		// Draw the registered image
		g.pdf.ImageOptions(
			imageName,
			x, y,
			layer.Size.Width, layer.Size.Height,
			false,
			gofpdf.ImageOptions{ImageType: imageType},
			0, "",
		)
		return nil
	}
	
	// FALLBACK: Use file path (backward compatibility, slower)
	if g.debugLog {
		fmt.Printf("Debug: Using file path for layer '%s' (base64 not available)\n", layer.ID)
	}
	
	// Get cached image path
	var imagePath string
	var err error
	
	// Check if we have it in the pre-fetched cache
	if path, ok := g.imageCache[imageURL]; ok {
		imagePath = path
	} else {
		// Download and cache
		imagePath, err = cache.GetImagePath(imageURL)
		if err != nil {
			return fmt.Errorf("layer '%s': failed to get image from %s: %w", layer.ID, imageURL, err)
		}
	}
	
	// Validate file exists and is readable
	if stat, err := os.Stat(imagePath); os.IsNotExist(err) || stat == nil || stat.Size() == 0 {
		return fmt.Errorf("layer '%s': image file does not exist or is empty: %s (from %s)", layer.ID, imagePath, imageURL)
	}
	
	// Determine image type
	imageType := getImageType(imagePath)
	
	// Handle WebP conversion (only if not using base64)
	if imageType == "WEBP" {
		if g.debugLog {
			fmt.Printf("Debug: Converting WebP to PNG for layer '%s': %s\n", layer.ID, imagePath)
		}
		convertedPath, err := convertWebPToPNG(imagePath)
		if err != nil {
			return fmt.Errorf("layer '%s': failed to convert WebP to PNG: %w", layer.ID, err)
		}
		// Validate converted file exists
		if stat, err := os.Stat(convertedPath); os.IsNotExist(err) || stat == nil || stat.Size() == 0 {
			return fmt.Errorf("layer '%s': WebP conversion failed, converted file missing: %s", layer.ID, convertedPath)
		}
		imagePath = convertedPath
		imageType = "PNG"
	}
	
	// Draw image from file path
	if g.debugLog {
		fmt.Printf("Debug: Rendering image for layer '%s' at (%.2f, %.2f) size (%.2f x %.2f)mm from: %s\n", 
			layer.ID, x, y, layer.Size.Width, layer.Size.Height, imagePath)
	}
	g.pdf.ImageOptions(
		imagePath,
		x, y,
		layer.Size.Width, layer.Size.Height,
		false,
		gofpdf.ImageOptions{ImageType: imageType},
		0, "",
	)
	
	return nil
}

// renderContainer renders a container with child layers
func (g *PDFGenerator) renderContainer(layer models.Layer, x, y float64) error {
	if len(layer.Children) == 0 {
		return nil
	}
	
	// Calculate child positions based on container layout
	layout := layer.ContainerLayout
	if layout == nil {
		// Default layout: stack vertically
		layout = &models.ContainerLayout{
			Type:          "flex",
			FlexDirection: "column",
		}
	}
	
	// Calculate positions for flex layout
	childPositions := g.calculateFlexPositions(layer, layout)
	
	// Render children
	for i, child := range layer.Children {
		if !child.Visible {
			continue
		}
		
		childX := x
		childY := y
		
		if i < len(childPositions) {
			childX = x + childPositions[i].X
			childY = y + childPositions[i].Y
		}
		
		if err := g.renderLayer(child, models.Position{X: childX, Y: childY}); err != nil {
			fmt.Printf("Warning: failed to render child layer: %v\n", err)
		}
	}
	
	return nil
}

// renderShape renders a shape layer (rectangle, etc.)
func (g *PDFGenerator) renderShape(layer models.Layer, x, y float64) error {
	if layer.Style.BackgroundColor == "" || layer.Style.BackgroundColor == "transparent" {
		return nil
	}
	
	r, gr, b := hexToRGB(layer.Style.BackgroundColor)
	g.pdf.SetFillColor(r, gr, b)
	g.pdf.Rect(x, y, layer.Size.Width, layer.Size.Height, "F")
	
	return nil
}

// calculateFlexPositions calculates positions for children in a flex container
func (g *PDFGenerator) calculateFlexPositions(container models.Layer, layout *models.ContainerLayout) []models.Position {
	positions := make([]models.Position, len(container.Children))
	
	if len(container.Children) == 0 {
		return positions
	}
	
	isRow := layout.FlexDirection == "row"
	gap := float64(layout.FlexGap)
	
	// Calculate total size of children
	var totalSize float64
	for _, child := range container.Children {
		if isRow {
			totalSize += child.Size.Width
		} else {
			totalSize += child.Size.Height
		}
	}
	totalSize += gap * float64(len(container.Children)-1)
	
	// Calculate starting position based on justify-content
	containerSize := container.Size.Width
	if !isRow {
		containerSize = container.Size.Height
	}
	
	var startPos float64
	var spacing float64
	
	switch layout.JustifyContent {
	case "center":
		startPos = (containerSize - totalSize) / 2
	case "flex-end":
		startPos = containerSize - totalSize
	case "space-between":
		if len(container.Children) > 1 {
			spacing = (containerSize - totalSize + gap*float64(len(container.Children)-1)) / float64(len(container.Children)-1)
		}
	case "space-around":
		spacing = (containerSize - totalSize + gap*float64(len(container.Children)-1)) / float64(len(container.Children)*2)
		startPos = spacing
	case "space-evenly":
		spacing = (containerSize - totalSize + gap*float64(len(container.Children)-1)) / float64(len(container.Children)+1)
		startPos = spacing
	default: // flex-start
		startPos = 0
	}
	
	// Calculate cross-axis alignment
	crossAlign := func(childSize, containerCrossSize float64) float64 {
		switch layout.AlignItems {
		case "center":
			return (containerCrossSize - childSize) / 2
		case "flex-end":
			return containerCrossSize - childSize
		default: // flex-start
			return 0
		}
	}
	
	// Assign positions
	currentPos := startPos
	for i, child := range container.Children {
		if isRow {
			positions[i] = models.Position{
				X: currentPos,
				Y: crossAlign(child.Size.Height, container.Size.Height),
			}
			currentPos += child.Size.Width + gap
			if spacing > 0 {
				currentPos += spacing - gap
			}
		} else {
			positions[i] = models.Position{
				X: crossAlign(child.Size.Width, container.Size.Width),
				Y: currentPos,
			}
			currentPos += child.Size.Height + gap
			if spacing > 0 {
				currentPos += spacing - gap
			}
		}
	}
	
	return positions
}

// resolvePlaceholders replaces {{customFields.xxx}} with actual values
func (g *PDFGenerator) resolvePlaceholders(content string) string {
	if content == "" {
		return ""
	}
	
	result := content
	
	// Match {{customFields.uuid}} pattern
	re := regexp.MustCompile(`\{\{customFields\.([a-f0-9-]+)\}\}`)
	
	result = re.ReplaceAllStringFunc(result, func(match string) string {
		matches := re.FindStringSubmatch(match)
		if len(matches) < 2 {
			return ""
		}
		fieldID := matches[1]
		return g.user.GetFieldValue(fieldID)
	})
	
	// Clean up extra spaces
	result = strings.TrimSpace(result)
	result = regexp.MustCompile(`\s+`).ReplaceAllString(result, " ")
	
	return result
}

// calculateAutoFontSize finds the best font size to fit text in a box
// Uses DPI-aware calculations to match template behavior
// maxFontSize: The template's intended fontSize (after conversion) - don't exceed this
func (g *PDFGenerator) calculateAutoFontSize(text string, width, height float64, fontFamily, fontStyle string, maxFontSize float64) float64 {
	// Start with a reasonable size based on height (in mm, convert to points)
	// Height is in mm, we want to use most of it for text
	// BUT: Don't exceed the template's intended fontSize
	fontSizeFromHeight := height * 2.83 // Convert mm to points: 1mm ≈ 2.83pt
	
	// Use the minimum of height-based size and template's max fontSize
	// This respects the template's intent while fitting to box
	maxSize := fontSizeFromHeight
	if maxFontSize > 0 && maxFontSize < maxSize {
		maxSize = maxFontSize
	}
	if maxSize > 72 {
		maxSize = 72
	}
	
	// Binary search for optimal font size
	minSize := 4.0
	
	for maxSize-minSize > 0.1 {
		testSize := (minSize + maxSize) / 2
		g.pdf.SetFont(fontFamily, fontStyle, testSize)
		textWidth := g.pdf.GetStringWidth(text)
		
		if textWidth <= width*0.95 {
			minSize = testSize
		} else {
			maxSize = testSize
		}
	}
	
	return minSize
}

// ============ HELPER FUNCTIONS ============

func hexToRGB(hex string) (int, int, int) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0
	}
	r, _ := strconv.ParseInt(hex[0:2], 16, 64)
	gr, _ := strconv.ParseInt(hex[2:4], 16, 64)
	b, _ := strconv.ParseInt(hex[4:6], 16, 64)
	return int(r), int(gr), int(b)
}

func getImageType(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(strings.ToLower(path[strings.LastIndex(path, "."):]), "."))
	switch ext {
	case "jpg", "jpeg":
		return "JPG"
	case "png":
		return "PNG"
	case "gif":
		return "GIF"
	case "webp":
		return "WEBP"
	default:
		return "PNG"
	}
}

func getImageTypeFromURL(url string) string {
	// Extract extension from URL
	lastDot := strings.LastIndex(url, ".")
	if lastDot == -1 {
		return "PNG" // Default
	}
	ext := strings.ToLower(url[lastDot+1:])
	// Remove query parameters if any
	if qIdx := strings.Index(ext, "?"); qIdx != -1 {
		ext = ext[:qIdx]
	}
	switch ext {
	case "jpg", "jpeg":
		return "JPG"
	case "png":
		return "PNG"
	case "gif":
		return "GIF"
	case "webp":
		return "PNG" // WebP converted to PNG in base64
	default:
		return "PNG"
	}
}

func convertWebPToPNG(webpPath string) (string, error) {
	pngPath := webpPath + ".png"
	
	// Check if already converted (single os.Stat call)
	if stat, err := os.Stat(pngPath); err == nil && stat.Size() > 0 {
		// WebP conversion already produces 8-bit PNG via imaging library
		// No need to normalize again
		return pngPath, nil
	}
	
	// Open WebP file using imaging library
	img, err := imaging.Open(webpPath)
	if err != nil {
		return "", fmt.Errorf("failed to open WebP: %w", err)
	}
	
	// Save as 8-bit PNG using imaging library (imaging.Save creates 8-bit PNGs)
	err = imaging.Save(img, pngPath)
	if err != nil {
		return "", fmt.Errorf("failed to save PNG: %w", err)
	}
	
	return pngPath, nil
}

// normalizePNGTo8Bit converts a PNG image to 8-bit depth if needed
// gofpdf doesn't support 16-bit PNG files
// Uses imaging library for fast conversion
// Optimized: checks file existence first, caches results
func normalizePNGTo8Bit(pngPath string) (string, error) {
	// Use a normalized path with a suffix to avoid conflicts
	normalizedPath := pngPath + ".8bit.png"
	
	// Check if already normalized (single os.Stat call)
	if stat, err := os.Stat(normalizedPath); err == nil && stat.Size() > 0 {
		return normalizedPath, nil
	}
	
	// Check if source file exists
	if _, err := os.Stat(pngPath); os.IsNotExist(err) {
		return "", fmt.Errorf("source PNG file does not exist: %s", pngPath)
	}
	
	// Open and decode image
	img, err := imaging.Open(pngPath)
	if err != nil {
		return "", fmt.Errorf("failed to open PNG: %w", err)
	}
	
	// Convert to NRGBA using imaging library (much faster than pixel-by-pixel)
	// This automatically handles 16-bit to 8-bit conversion
	nrgba := imaging.Clone(img)
	
	// Save as 8-bit PNG using imaging library
	// NRGBA format ensures 8-bit depth
	err = imaging.Save(nrgba, normalizedPath)
	if err != nil {
		return "", fmt.Errorf("failed to save normalized PNG: %w", err)
	}
	
	return normalizedPath, nil
}
