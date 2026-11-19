package main

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type FontFamily struct {
	Name       string `json:"name"`
	Monospaced bool   `json:"monospaced"`
}

// Appearance exposes desktop-only fonts and native appearance controls.
type Appearance struct{}

// FontFamilies returns installed system families, refreshed after an app restart.
func (Appearance) FontFamilies(ctx context.Context) ([]FontFamily, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	families := normalizeFontFamilies(ui.FontFamilies())
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return families, nil
}

// SetTheme applies the preference to the app and the calling window's native controls.
func (Appearance) SetTheme(ctx context.Context, source string) error {
	themeSource, err := parseAppearanceThemeSource(source)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	window := mygo.CallerWindow(ctx)
	if window == nil {
		return errors.New("native appearance requires a calling desktop window")
	}
	return applyAppearanceTheme(ctx, themeSource, window, mygo.RunOnMain, mygo.Theme.SetSource)
}

func normalizeFontFamilies(fonts []ui.FontFamily) []FontFamily {
	families := make([]FontFamily, 0, len(fonts))
	indices := make(map[string]int, len(fonts))
	for _, font := range fonts {
		if font.Registered {
			continue
		}
		name := strings.TrimSpace(font.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if index, exists := indices[key]; exists {
			families[index].Monospaced = families[index].Monospaced || font.Monospaced
			continue
		}
		indices[key] = len(families)
		families = append(families, FontFamily{Name: name, Monospaced: font.Monospaced})
	}
	sort.Slice(families, func(i, j int) bool {
		return strings.ToLower(families[i].Name) < strings.ToLower(families[j].Name)
	})
	return families
}

func parseAppearanceThemeSource(source string) (mygo.ThemeSource, error) {
	switch source {
	case "light":
		return mygo.ThemeLight, nil
	case "dark":
		return mygo.ThemeDark, nil
	case "system":
		return mygo.ThemeSystem, nil
	default:
		return "", errors.New("theme source must be light, dark, or system")
	}
}

type appearanceThemeWindow interface {
	IsDestroyed() bool
	SetWindowButtonsTheme(mygo.ThemeSource)
}

func applyAppearanceTheme(ctx context.Context, source mygo.ThemeSource, window appearanceThemeWindow, onMain func(func()), setSource func(mygo.ThemeSource)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if window.IsDestroyed() {
		return errors.New("native appearance window has been destroyed")
	}

	err := errors.New("native appearance is unavailable after the application has stopped")
	// Check lifecycle and update both native states in one main-thread operation.
	onMain(func() {
		if err = ctx.Err(); err != nil {
			return
		}
		if window.IsDestroyed() {
			err = errors.New("native appearance window has been destroyed")
			return
		}
		setSource(source)
		window.SetWindowButtonsTheme(source)
		err = nil
	})
	return err
}
