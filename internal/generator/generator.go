package generator

import (
	"badge-service/internal/cache"
	"badge-service/internal/models"
	"bytes"
	"crypto/md5"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jung-kurt/gofpdf"
	"github.com/skip2/go-qrcode"
	_ "golang.org/x/image/webp"
)

// Pre-compiled regex patterns for better performance
var (
	placeholderRegex = regexp.MustCompile(`\{\{customFields\.([a-f0-9-]+)\}\}`)
	whitespaceRegex  = regexp.MustCompile(`\s+`)
)

// PDFGenerator handles PDF generation for badges
type PDFGenerator struct {
	template    *models.Template
	user        *models.User
	pdf         *gofpdf.Fpdf
	imageCache  map[string]string // URL -> local path (for backward compatibility)
	imageDataCache map[string][]byte // URL -> raw PNG bytes (preferred, fastest - no base64, no files)
	scaleFactor float64           // Scale from mm to points
	dpi         int               // DPI from template settings for font size conversion
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
	
	return &PDFGenerator{
		template:       template,
		user:           user,
		pdf:            pdf,
		imageCache:     make(map[string]string),
		imageDataCache: make(map[string][]byte),
		scaleFactor:    1.0,
		dpi:            dpi,
	}
}

// SetImageCache sets pre-fetched image paths (for backward compatibility)
func (g *PDFGenerator) SetImageCache(cache map[string]string) {
	g.imageCache = cache
}

// SetImageDataCache sets pre-fetched images as raw PNG bytes (preferred, fastest - no base64, no files)
func (g *PDFGenerator) SetImageDataCache(cache map[string][]byte) {
	g.imageDataCache = cache
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
			// Log errors to stderr for debugging (production: remove or use proper logging)
			if layer.Type == "qrcode" {
				fmt.Fprintf(os.Stderr, "QR code error for layer %s: %v\n", layer.ID, err)
			}
			// Continue rendering other layers even if one fails
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
	
	// Set font style - support bold and normal
	fontStyle := ""
	if layer.Style.FontWeight == "bold" || layer.Style.FontWeight == "700" {
		fontStyle = "B"
	}
	// "normal", "400", or empty = regular weight (default)
	
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
	} else if fontSizeUnit == "auto" {
		// Test both - use px for now but log both
		fontSize = fontSizeAsPx
	} else {
		// Default: px conversion
		fontSize = fontSizeAsPx
	}
	
	// Clamp font size to reasonable bounds
	if fontSize < 4 {
		fontSize = 4
	}
	if fontSize > 72 {
		fontSize = 72
	}
	
	// Set font first (needed for width calculations)
	fontFamily := layer.Style.FontFamily
	if fontFamily == "" {
		fontFamily = "Arial"
	}
	
	// Auto font size: fit text to box
	// Pass the base fontSize (converted) as maximum to respect template intent
	if layer.AutoFontSize {
		// Use the converted fontSize as maximum - don't ignore template's intent
		fontSize = g.calculateAutoFontSize(text, layer.Size.Width, layer.Size.Height, fontFamily, fontStyle, fontSize)
	}
	
	g.pdf.SetFont(fontFamily, fontStyle, fontSize)
	
	// Set text color
	r, gr, b := hexToRGB(layer.Style.Color)
	g.pdf.SetTextColor(r, gr, b)
	
	// Determine alignment - gofpdf format: Horizontal + Vertical
	// Horizontal: L (left), C (center), R (right)
	// Vertical: T (top), M (middle), B (bottom)
	alignStr := "LM" // Left, Middle (vertical) - default
	switch layer.Style.TextAlign {
	case "center":
		alignStr = "CM" // Center, Middle
	case "right":
		alignStr = "RM" // Right, Middle
	case "left":
		alignStr = "LM" // Left, Middle
	}
	
	// Set position
	g.pdf.SetXY(x, y)
	
	// Check if text needs wrapping (exceeds cell width)
	textWidth := g.pdf.GetStringWidth(text)
	needsWrapping := textWidth > layer.Size.Width*0.95
	
	// Handle multi-line text (explicit newlines)
	if strings.Contains(text, "\n") {
		lines := strings.Split(text, "\n")
		// Calculate proper line height based on font size (1.2x for spacing)
		lineHeight := fontSize * 1.2
		if lineHeight > layer.Size.Height/float64(len(lines)) {
			lineHeight = layer.Size.Height / float64(len(lines))
		}
		
		for i, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue // Skip empty lines
			}
			currentY := y + float64(i)*lineHeight
			g.pdf.SetXY(x, currentY)
			
			// Check if this line needs wrapping
			lineWidth := g.pdf.GetStringWidth(line)
			if lineWidth > layer.Size.Width*0.95 {
				// Use MultiCell for wrapping
				g.pdf.MultiCell(layer.Size.Width, lineHeight, line, "", alignStr, false)
			} else {
				// Use CellFormat for single line
				g.pdf.CellFormat(layer.Size.Width, lineHeight, line, "", 0, alignStr, false, 0, "")
			}
		}
	} else if needsWrapping {
		// Text is too long, use MultiCell for automatic word wrapping
		// Calculate line height based on font size
		lineHeight := fontSize * 1.2
		if lineHeight > layer.Size.Height {
			lineHeight = layer.Size.Height
		}
		g.pdf.MultiCell(layer.Size.Width, lineHeight, text, "", alignStr, false)
	} else {
		// Single line text that fits - use CellFormat
		g.pdf.CellFormat(layer.Size.Width, layer.Size.Height, text, "", 0, alignStr, false, 0, "")
	}
	
	return nil
}

// renderQRCode generates and renders a QR code
func (g *PDFGenerator) renderQRCode(layer models.Layer, x, y float64) error {
	// Note: visibility is already checked in renderLayer, but check opacity here
	opacity := layer.Style.Opacity
	if opacity == 0 {
		return nil // Fully transparent, skip rendering
	}
	
	// Generate QR content - resolve placeholders first
	qrContent := g.resolvePlaceholders(layer.Content)
	
	// If content is empty or still has unresolved placeholders, use user identifier
	if qrContent == "" || strings.Contains(qrContent, "{{") {
		qrContent = g.user.Identifier
	}
	
	// Fallback to user ID if identifier is also empty
	if qrContent == "" {
		qrContent = g.user.ID
	}
	
	// Ensure we have content to encode
	if qrContent == "" {
		return fmt.Errorf("QR code content is empty")
	}
	
	// Calculate QR size in pixels based on DPI and layer size (mm to pixels)
	// Use the larger dimension to ensure quality
	widthMM := layer.Size.Width
	heightMM := layer.Size.Height
	if heightMM > widthMM {
		widthMM = heightMM
	}
	
	// Convert mm to pixels: pixels = (mm * DPI) / 25.4
	qrSize := int((widthMM * float64(g.dpi)) / 25.4)
	
	// Ensure minimum size for quality
	if qrSize < 100 {
		qrSize = 100
	}
	// Cap maximum size for performance
	if qrSize > 1024 {
		qrSize = 1024
	}
	
	// Generate QR code directly to bytes (in-memory, no file I/O)
	qrBytes, err := qrcode.Encode(qrContent, qrcode.Medium, qrSize)
	if err != nil {
		return fmt.Errorf("failed to generate QR code: %w", err)
	}
	
	// Generate unique image name for gofpdf registration
	// Include layer ID to ensure uniqueness even if content is the same
	hash := md5.Sum([]byte(qrContent + layer.ID))
	imageName := fmt.Sprintf("qr_%s_%x", layer.ID, hash[:8])
	
	// Register the QR code image with gofpdf using raw bytes (no file I/O)
	info := g.pdf.RegisterImageOptionsReader(imageName, gofpdf.ImageOptions{
		ImageType: "PNG", // QR codes are always PNG
	}, bytes.NewReader(qrBytes))
	
	if info == nil {
		return fmt.Errorf("failed to register QR code image (layer: %s, content: %s)", layer.ID, qrContent)
	}
	
	// Draw the registered QR code image
	g.pdf.ImageOptions(
		imageName,
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
	
	// Check if this is an asset reference
	if strings.HasPrefix(layer.Content, "asset_") {
		// Try exact match first (for cases like "asset_0" matching "asset_0")
		if url, ok := g.template.Assets[layer.Content]; ok {
			imageURL = url
		} else {
			// Fallback: find asset URL with contains match (for timestamped keys like "asset_0_1763558759124")
			for key, url := range g.template.Assets {
				if strings.Contains(key, layer.Content) {
					imageURL = url
					break
				}
			}
		}
	} else if layer.DataBinding != "" {
		// Get image URL from user data binding
		fieldID := strings.TrimPrefix(layer.DataBinding, "customFields.")
		imageURL = g.user.GetFieldValue(fieldID)
	} else if layer.Content != "" && (strings.HasPrefix(layer.Content, "http://") || strings.HasPrefix(layer.Content, "https://")) {
		imageURL = layer.Content
	}
	
	// If image layer expects an image but URL is empty, skip rendering
	// (some layers might be optional)
	if imageURL == "" {
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
	
	// PREFERRED: Use direct image data cache (raw bytes, fastest - no base64, no files)
	if imageData, ok := g.imageDataCache[imageURL]; ok {
		
		// Generate unique image name for gofpdf registration
		imageName := fmt.Sprintf("img_%s", strings.ReplaceAll(imageURL, "/", "_"))
		imageName = strings.ReplaceAll(imageName, ":", "_")
		imageName = strings.ReplaceAll(imageName, ".", "_")
		// Add hash to ensure uniqueness (use first 8 bytes of MD5)
		hash := md5.Sum([]byte(imageURL))
		imageName = fmt.Sprintf("%s_%x", imageName, hash[:8])
		
		// Register the image with gofpdf using raw bytes (no base64 encoding/decoding)
		info := g.pdf.RegisterImageOptionsReader(imageName, gofpdf.ImageOptions{
			ImageType: "PNG", // All processed images are PNG
		}, bytes.NewReader(imageData))
		
		if info == nil {
			return fmt.Errorf("layer '%s': failed to register image data", layer.ID)
		}
		
		// Draw the registered image
		g.pdf.ImageOptions(
			imageName,
			x, y,
			layer.Size.Width, layer.Size.Height,
			false,
			gofpdf.ImageOptions{ImageType: "PNG"},
			0, "",
		)
		return nil
	}
	
	// FALLBACK: Download and process on-demand if not in cache
	imageData, err2 := cache.GetImageDataDirect(imageURL, layer.Size.Width, layer.Size.Height, g.dpi)
	if err2 != nil {
		return fmt.Errorf("layer '%s': failed to get image data on-demand: %w", layer.ID, err2)
	}
	
	// Generate unique image name
	imageName := fmt.Sprintf("img_%s", strings.ReplaceAll(imageURL, "/", "_"))
	imageName = strings.ReplaceAll(imageName, ":", "_")
	imageName = strings.ReplaceAll(imageName, ".", "_")
	hash := md5.Sum([]byte(imageURL))
	imageName = fmt.Sprintf("%s_%x", imageName, hash[:8])
	
	// Register the image with gofpdf
	info := g.pdf.RegisterImageOptionsReader(imageName, gofpdf.ImageOptions{
		ImageType: "PNG",
	}, bytes.NewReader(imageData))
	
	if info == nil {
		return fmt.Errorf("layer '%s': failed to register image data on-demand", layer.ID)
	}
	
	// Draw the registered image
	g.pdf.ImageOptions(
		imageName,
		x, y,
		layer.Size.Width, layer.Size.Height,
		false,
		gofpdf.ImageOptions{ImageType: "PNG"},
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
			// Continue rendering other children even if one fails
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
	
	// Use pre-compiled regex pattern
	result = placeholderRegex.ReplaceAllStringFunc(result, func(match string) string {
		matches := placeholderRegex.FindStringSubmatch(match)
		if len(matches) < 2 {
			return ""
		}
		fieldID := matches[1]
		return g.user.GetFieldValue(fieldID)
	})
	
	// Clean up extra spaces using pre-compiled regex
	result = strings.TrimSpace(result)
	result = whitespaceRegex.ReplaceAllString(result, " ")
	
	return result
}

// calculateAutoFontSize calculates the optimal font size to fit text within given dimensions
func (g *PDFGenerator) calculateAutoFontSize(text string, width, height float64, fontFamily, fontStyle string, maxFontSize float64) float64 {
	if maxFontSize <= 0 {
		maxFontSize = 72 // Default max if not specified
	}
	
	// Start with a reasonable size based on height (in mm, convert to points)
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

