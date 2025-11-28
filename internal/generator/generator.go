package generator

import (
	"badge-service/internal/cache"
	"badge-service/internal/models"
	"bytes"
	"fmt"
	"image"
	"image/color"
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
	template    *models.Template
	user        *models.User
	pdf         *gofpdf.Fpdf
	imageCache  map[string]string // URL -> local path
	scaleFactor float64           // Scale from mm to points
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
	
	return &PDFGenerator{
		template:    template,
		user:        user,
		pdf:         pdf,
		imageCache:  make(map[string]string),
		scaleFactor: 1.0,
	}
}

// SetImageCache sets pre-fetched image paths
func (g *PDFGenerator) SetImageCache(cache map[string]string) {
	g.imageCache = cache
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
	
	// Calculate font size (convert from design units to PDF points)
	// The design uses px-like units, we need to convert to mm-appropriate size
	fontSize := layer.Style.FontSize / 5.0 // Rough conversion factor
	if fontSize < 4 {
		fontSize = 8
	}
	if fontSize > 72 {
		fontSize = 72
	}
	
	// Auto font size: fit text to box
	if layer.AutoFontSize {
		fontSize = g.calculateAutoFontSize(text, layer.Size.Width, layer.Size.Height, layer.Style.FontFamily, fontStyle)
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
	
	// Normalize QR code PNG to 8-bit depth (gofpdf doesn't support 16-bit PNGs)
	normalizedQRPath, err := normalizePNGTo8Bit(qrPath)
	if err != nil {
		return fmt.Errorf("failed to normalize QR code PNG: %w", err)
	}
	
	// Draw QR code image
	g.pdf.ImageOptions(
		normalizedQRPath,
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
		// Find asset URL from template assets
		for key, url := range g.template.Assets {
			if strings.Contains(key, layer.Content) {
				imageURL = url
				break
			}
		}
		// If not found with full key, try direct match
		if imageURL == "" {
			if url, ok := g.template.Assets[layer.Content]; ok {
				imageURL = url
			}
		}
	} else if layer.DataBinding != "" {
		// Get image URL from user data binding
		fieldID := strings.TrimPrefix(layer.DataBinding, "customFields.")
		imageURL = g.user.GetFieldValue(fieldID)
	} else if layer.Content != "" && (strings.HasPrefix(layer.Content, "http://") || strings.HasPrefix(layer.Content, "https://")) {
		imageURL = layer.Content
	}
	
	if imageURL == "" {
		return nil // No image to render
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
			return fmt.Errorf("failed to get image: %w", err)
		}
	}
	
	// Determine image type
	imageType := getImageType(imagePath)
	
	// Handle WebP conversion
	if imageType == "WEBP" {
		convertedPath, err := convertWebPToPNG(imagePath)
		if err != nil {
			return fmt.Errorf("failed to convert WebP: %w", err)
		}
		imagePath = convertedPath
		imageType = "PNG"
	}
	
	// Normalize PNG to 8-bit depth (gofpdf doesn't support 16-bit PNGs)
	if imageType == "PNG" {
		normalizedPath, err := normalizePNGTo8Bit(imagePath)
		if err != nil {
			return fmt.Errorf("failed to normalize PNG: %w", err)
		}
		imagePath = normalizedPath
	}
	
	// Draw image
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
func (g *PDFGenerator) calculateAutoFontSize(text string, width, height float64, fontFamily, fontStyle string) float64 {
	// Start with a reasonable size and scale down
	fontSize := height * 0.8
	if fontSize > 24 {
		fontSize = 24
	}
	
	for fontSize > 4 {
		g.pdf.SetFont(fontFamily, fontStyle, fontSize)
		textWidth := g.pdf.GetStringWidth(text)
		
		if textWidth <= width*0.95 {
			return fontSize
		}
		
		fontSize -= 0.5
	}
	
	return 4 // Minimum font size
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

func convertWebPToPNG(webpPath string) (string, error) {
	pngPath := webpPath + ".png"
	
	// Check if already converted
	if _, err := os.Stat(pngPath); err == nil {
		// Still normalize to ensure 8-bit depth
		return normalizePNGTo8Bit(pngPath)
	}
	
	// Open WebP file
	file, err := os.Open(webpPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	
	// Decode image
	img, _, err := image.Decode(file)
	if err != nil {
		return "", err
	}
	
	// Save as PNG using imaging library (will be 8-bit)
	err = imaging.Save(img, pngPath)
	if err != nil {
		return "", err
	}
	
	return pngPath, nil
}

// normalizePNGTo8Bit converts a PNG image to 8-bit depth if needed
// gofpdf doesn't support 16-bit PNG files
func normalizePNGTo8Bit(pngPath string) (string, error) {
	// Use a normalized path with a suffix to avoid conflicts
	normalizedPath := pngPath + ".8bit.png"
	
	// Check if already normalized
	if _, err := os.Stat(normalizedPath); err == nil {
		return normalizedPath, nil
	}
	
	// Open PNG file
	file, err := os.Open(pngPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	
	// Decode image
	img, _, err := image.Decode(file)
	if err != nil {
		return "", err
	}
	
	// Convert to NRGBA format (8-bit per channel) to ensure 8-bit depth
	// This explicitly converts any 16-bit images to 8-bit
	// Use imaging to convert to NRGBA which guarantees 8-bit per channel
	bounds := img.Bounds()
	nrgba := image.NewNRGBA(bounds)
	
	// Copy pixels, which will convert from any format (including 16-bit) to 8-bit NRGBA
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			// Convert from 16-bit RGBA to 8-bit NRGBA
			nrgba.SetNRGBA(x, y, color.NRGBA{
				R: uint8(r >> 8),
				G: uint8(g >> 8),
				B: uint8(b >> 8),
				A: uint8(a >> 8),
			})
		}
	}
	
	// Save as 8-bit PNG using imaging library
	// NRGBA format ensures 8-bit depth
	err = imaging.Save(nrgba, normalizedPath)
	if err != nil {
		return "", err
	}
	
	return normalizedPath, nil
}
