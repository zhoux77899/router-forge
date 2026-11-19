package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestNormalizeFontFamilies(t *testing.T) {
	fonts := []ui.FontFamily{
		{Name: "App Font", Registered: true, Monospaced: true},
		{Name: "Arial", Registered: true, Monospaced: true},
		{Name: "思源黑体"},
		{Name: " Noto Sans SC "},
		{Name: "微软雅黑"},
		{Name: " Arial "},
		{Name: "arial"},
		{Name: "Calibri"},
		{Name: "calibri", Monospaced: true},
		{Name: "Font  With  Spaces"},
		{Name: `A "Quoted", Font`},
		{Name: "\t\n"},
		{},
	}
	original := append([]ui.FontFamily(nil), fonts...)
	want := []FontFamily{
		{Name: `A "Quoted", Font`},
		{Name: "Arial"},
		{Name: "Calibri", Monospaced: true},
		{Name: "Font  With  Spaces"},
		{Name: "Noto Sans SC"},
		{Name: "微软雅黑"},
		{Name: "思源黑体"},
	}
	if got := normalizeFontFamilies(fonts); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized families = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(fonts, original) {
		t.Fatal("normalization mutated the framework's cached font list")
	}
}

func TestNormalizeFontFamiliesEmptyListIsAnArray(t *testing.T) {
	for _, fonts := range [][]ui.FontFamily{nil, {}, {{Name: "App Font", Registered: true}, {Name: " "}}} {
		families := normalizeFontFamilies(fonts)
		encoded, err := json.Marshal(families)
		if err != nil || string(encoded) != "[]" {
			t.Fatalf("empty system families encoded as %s: %v", encoded, err)
		}
	}
}

func TestFontFamilyJSONContainsOnlyPickerFields(t *testing.T) {
	encoded, err := json.Marshal(FontFamily{Name: "Localized Family", Monospaced: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"name":"Localized Family","monospaced":true}` {
		t.Fatalf("unexpected appearance DTO: %s", encoded)
	}
}

func TestParseAppearanceThemeSource(t *testing.T) {
	for _, source := range []mygo.ThemeSource{mygo.ThemeLight, mygo.ThemeDark, mygo.ThemeSystem} {
		t.Run(string(source), func(t *testing.T) {
			got, err := parseAppearanceThemeSource(string(source))
			if err != nil || got != source {
				t.Fatalf("theme source = %q, error = %v", got, err)
			}
		})
	}
	for _, source := range []string{"", "Light", "SYSTEM", " light", "dark\n", "system ", "auto", "dark\x00"} {
		t.Run("invalid/"+source, func(t *testing.T) {
			got, err := parseAppearanceThemeSource(source)
			if err == nil || got != "" {
				t.Fatalf("accepted invalid source %q as %q", source, got)
			}
		})
	}
}

func TestAppearanceValidatesSourceBeforeCallerLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, ctx := range []context.Context{context.Background(), ctx} {
		err := (Appearance{}).SetTheme(ctx, "Light")
		if err == nil || err.Error() != "theme source must be light, dark, or system" {
			t.Fatalf("invalid source was not rejected first: %v", err)
		}
	}
}

func TestAppearanceRejectsCancelledContextsAndMissingCaller(t *testing.T) {
	appearance := Appearance{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if families, err := appearance.FontFamilies(ctx); !errors.Is(err, context.Canceled) || families != nil {
		t.Fatalf("font enumeration did not honor cancellation: %#v, %v", families, err)
	}
	if err := appearance.SetTheme(ctx, "light"); !errors.Is(err, context.Canceled) {
		t.Fatalf("theme did not honor cancellation: %v", err)
	}
	if err := appearance.SetTheme(context.Background(), "system"); err == nil || !strings.Contains(err.Error(), "calling desktop window") {
		t.Fatalf("accepted a theme call without a desktop caller: %v", err)
	}
}

func TestApplyAppearanceThemeUpdatesAppAndCaptionOnMainThread(t *testing.T) {
	for _, source := range []mygo.ThemeSource{mygo.ThemeLight, mygo.ThemeDark, mygo.ThemeSystem} {
		t.Run(string(source), func(t *testing.T) {
			var events []string
			onMainThread := false
			window := &appearanceTestWindow{setTheme: func(got mygo.ThemeSource) {
				if !onMainThread || got != source {
					t.Errorf("caption theme set off main thread or to %q", got)
				}
				events = append(events, "caption")
			}}
			err := applyAppearanceTheme(context.Background(), source, window, func(fn func()) {
				events = append(events, "dispatch")
				onMainThread = true
				defer func() { onMainThread = false }()
				fn()
			}, func(got mygo.ThemeSource) {
				if !onMainThread || got != source {
					t.Errorf("app theme set off main thread or to %q", got)
				}
				events = append(events, "app")
			})
			if err != nil || !reflect.DeepEqual(events, []string{"dispatch", "app", "caption"}) {
				t.Fatalf("theme operations = %v, error = %v", events, err)
			}
		})
	}
}

func TestApplyAppearanceThemeRejectsStaleNativeCalls(t *testing.T) {
	for _, scenario := range []string{"cancelled", "destroyed", "cancelled-in-queue", "destroyed-in-queue", "application-stopped"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			nativeCalls, dispatches := 0, 0
			window := &appearanceTestWindow{setTheme: func(mygo.ThemeSource) { nativeCalls++ }}
			if scenario == "cancelled" {
				cancel()
			}
			if scenario == "destroyed" {
				window.destroyed = true
			}
			err := applyAppearanceTheme(ctx, mygo.ThemeDark, window, func(fn func()) {
				dispatches++
				switch scenario {
				case "cancelled-in-queue":
					cancel()
				case "destroyed-in-queue":
					window.destroyed = true
				case "application-stopped":
					return
				}
				fn()
			}, func(mygo.ThemeSource) { nativeCalls++ })
			if err == nil || nativeCalls != 0 {
				t.Fatalf("stale theme call returned %v with %d native mutations", err, nativeCalls)
			}
			if strings.HasPrefix(scenario, "cancelled") && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation was not preserved: %v", err)
			}
			if strings.HasPrefix(scenario, "destroyed") && !strings.Contains(err.Error(), "destroyed") {
				t.Fatalf("destroyed window was not reported: %v", err)
			}
			if scenario == "application-stopped" && !strings.Contains(err.Error(), "stopped") {
				t.Fatalf("dropped main-thread operation was not reported: %v", err)
			}
			if (scenario == "cancelled" || scenario == "destroyed") && dispatches != 0 {
				t.Fatal("stale call was unnecessarily dispatched")
			}
		})
	}
}

type appearanceTestWindow struct {
	destroyed bool
	setTheme  func(mygo.ThemeSource)
}

func (w *appearanceTestWindow) IsDestroyed() bool { return w.destroyed }

func (w *appearanceTestWindow) SetWindowButtonsTheme(source mygo.ThemeSource) { w.setTheme(source) }
