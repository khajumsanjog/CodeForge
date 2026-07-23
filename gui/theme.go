package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// CodeForgeTheme implements the fyne.Theme interface to define customized brand colors.
type CodeForgeTheme struct{}

// Color returns the brand-specific color palette for widgets.
func (t CodeForgeTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if variant == theme.VariantLight {
		switch name {
		case theme.ColorNamePrimary:
			return color.NRGBA{R: 0x4F, G: 0x46, B: 0xE5, A: 0xFF} // #4F46E5 Indigo
		case theme.ColorNameBackground:
			return color.NRGBA{R: 0xF8, G: 0xFA, B: 0xFC, A: 0xFF} // Clean Light Slate
		case theme.ColorNameForeground:
			return color.NRGBA{R: 0x0F, G: 0x17, B: 0x2A, A: 0xFF} // Dark Slate Text
		case theme.ColorNameSuccess:
			return color.NRGBA{R: 0x05, G: 0x96, B: 0x69, A: 0xFF} // Emerald Green
		case theme.ColorNameError:
			return color.NRGBA{R: 0xDC, G: 0x26, B: 0x26, A: 0xFF} // Red Error
		case theme.ColorNameWarning:
			return color.NRGBA{R: 0xD9, G: 0x77, B: 0x06, A: 0xFF} // Amber Warning
		case theme.ColorNameInputBackground:
			return color.NRGBA{R: 0xE2, G: 0xE8, B: 0xF0, A: 0xFF} // Light Input Box
		case theme.ColorNameButton:
			return color.NRGBA{R: 0x63, G: 0x66, B: 0xF1, A: 0xFF} // Indigo Button
		case theme.ColorNameShadow:
			return color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x20}
		default:
			return theme.DefaultTheme().Color(name, theme.VariantLight)
		}
	}

	switch name {
	case theme.ColorNamePrimary:
		return color.NRGBA{R: 0x63, G: 0x66, B: 0xF1, A: 0xFF} // #6366F1 (Indigo Accent)
	case theme.ColorNameBackground:
		return color.NRGBA{R: 0x0F, G: 0x17, B: 0x2A, A: 0xFF} // #0F172A (Dark Slate Navy)
	case theme.ColorNameForeground:
		return color.NRGBA{R: 0xF8, G: 0xFA, B: 0xFC, A: 0xFF} // #F8FAFC (Crisp White)
	case theme.ColorNameSuccess:
		return color.NRGBA{R: 0x10, G: 0xB9, B: 0x81, A: 0xFF} // #10B981 (Emerald Green)
	case theme.ColorNameError:
		return color.NRGBA{R: 0xEF, G: 0x44, B: 0x44, A: 0xFF} // #EF4444 (Vibrant Red)
	case theme.ColorNameWarning:
		return color.NRGBA{R: 0xF5, G: 0x9E, B: 0x0B, A: 0xFF} // #F59E0B (Amber Warning)
	case theme.ColorNameInputBackground:
		return color.NRGBA{R: 0x1E, G: 0x29, B: 0x3B, A: 0xFF} // #1E293B (Slate Input)
	case theme.ColorNameButton:
		return color.NRGBA{R: 0x4F, G: 0x46, B: 0xE5, A: 0xFF} // #4F46E5 (Deep Indigo Button)
	case theme.ColorNameShadow:
		return color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x80}
	default:
		return theme.DefaultTheme().Color(name, theme.VariantDark)
	}
}

// Font delegates typography loading to the Fyne default fonts.
func (t CodeForgeTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

// Icon delegates system icon loading to the Fyne default icons.
func (t CodeForgeTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

// Size returns standard layout dimension guidelines.
func (t CodeForgeTheme) Size(name fyne.ThemeSizeName) float32 {
	return theme.DefaultTheme().Size(name)
}
