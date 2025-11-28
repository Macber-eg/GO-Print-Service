package models

import "strings"

// ============ TEMPLATE STRUCTURES ============

type Template struct {
	ID           int               `json:"id"`
	AdminID      string            `json:"adminId"`
	EventID      int               `json:"eventId"`
	Name         string            `json:"name"`
	Design       TemplateDesign    `json:"design"`
	Assets       map[string]string `json:"assets"`
	Placeholders map[string]string `json:"placeholders"`
	Width        float64           `json:"width"`
	Height       float64           `json:"height"`
}

type TemplateDesign struct {
	Layers   []Layer  `json:"layers"`
	Settings Settings `json:"settings"`
}

type Layer struct {
	ID              string          `json:"id"`
	Type            string          `json:"type"` // image, text, qrcode, container
	Position        Position        `json:"position"`
	Size            Size            `json:"size"`
	Style           Style           `json:"style"`
	Content         string          `json:"content"`
	DataBinding     string          `json:"dataBinding,omitempty"`
	Children        []Layer         `json:"children,omitempty"`
	ZIndex          int             `json:"zIndex"`
	Visible         bool            `json:"visible"`
	ParentID        string          `json:"parentId,omitempty"`
	ContainerLayout *ContainerLayout `json:"containerLayout,omitempty"`
	AutoFontSize    bool            `json:"autoFontSize,omitempty"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Size struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type Style struct {
	FontSize        float64 `json:"fontSize"`
	FontFamily      string  `json:"fontFamily"`
	FontWeight      string  `json:"fontWeight"`
	Color           string  `json:"color"`
	TextAlign       string  `json:"textAlign"`
	Opacity         float64 `json:"opacity"`
	BackgroundColor string  `json:"backgroundColor,omitempty"`
	Rotation        float64 `json:"rotation,omitempty"`
}

type Settings struct {
	PaperWidth      float64 `json:"paperWidth"`
	PaperHeight     float64 `json:"paperHeight"`
	DPI             int     `json:"dpi"`
	Orientation     string  `json:"orientation"`
	DefaultLanguage string  `json:"defaultLanguage"`
	RTLSupport      bool    `json:"rtlSupport"`
}

type ContainerLayout struct {
	Type           string `json:"type"`
	FlexDirection  string `json:"flexDirection"`
	JustifyContent string `json:"justifyContent"`
	AlignItems     string `json:"alignItems"`
	FlexGap        int    `json:"flexGap"`
	FlexWrap       string `json:"flexWrap"`
}

// ============ USER STRUCTURES ============

type User struct {
	ID                string             `json:"id"`
	FirstName         string             `json:"firstName"`
	LastName          string             `json:"lastName"`
	Email             string             `json:"email"`
	Identifier        string             `json:"identifier"`
	CustomFieldValues []CustomFieldValue `json:"customFieldValues"`
}

type CustomFieldValue struct {
	FieldID   string `json:"fieldId"`
	Name      string `json:"name"`
	FieldType string `json:"fieldType"`
	Value     string `json:"value"`
	Label     string `json:"label"`
}

func (u *User) GetFieldValue(fieldID string) string {
	for _, cf := range u.CustomFieldValues {
		if cf.FieldID == fieldID {
			return cf.Value
		}
	}
	return ""
}

func (u *User) GetFieldByName(name string) string {
	nameLower := strings.ToLower(name)
	for _, cf := range u.CustomFieldValues {
		if strings.ToLower(cf.Name) == nameLower || strings.ToLower(cf.Label) == nameLower {
			return cf.Value
		}
	}
	return ""
}

// ============ REQUEST/RESPONSE STRUCTURES ============

type GenerateBadgeRequest struct {
	Template Template `json:"template"`
	User     UserData `json:"user"`
}

type UserData struct {
	User User `json:"user"`
}

type BatchGenerateRequest struct {
	Template Template   `json:"template"`
	Users    []UserData `json:"users"`
}

type BatchGenerateResponse struct {
	Success bool           `json:"success"`
	Total   int            `json:"total"`
	Results []BadgeResult  `json:"results"`
}

type BadgeResult struct {
	UserID     string `json:"user_id"`
	Identifier string `json:"identifier"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
	PDFUrl     string `json:"pdf_url,omitempty"`
	PDFBase64  string `json:"pdf_base64,omitempty"`
}

type HealthResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Uptime    string `json:"uptime"`
}
