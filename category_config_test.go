package goed2k

import (
	"testing"
)

func TestParseCategoriesConfig(t *testing.T) {
	cats, err := ParseCategoriesConfig("video:mp4,mkv:/videos;music:mp3:/music")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cats) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(cats))
	}
	if cats[0].Name != "video" || cats[0].OutputDir != "/videos" {
		t.Fatalf("unexpected first category: %+v", cats[0])
	}
	formatted := FormatCategoriesConfig(cats)
	if formatted != "video:mp4,mkv:/videos;music:mp3:/music" {
		t.Fatalf("unexpected formatted: %q", formatted)
	}
}

func TestEnsureIdentityKeyForSecIdent(t *testing.T) {
	st := NewSettings()
	st.EnableSecIdent = true
	path := EnsureIdentityKeyForSecIdent(&st)
	if path != DefaultIdentityKeyPath() {
		t.Fatalf("expected default path, got %q", path)
	}
	st.EnableSecIdent = false
	if got := EnsureIdentityKeyForSecIdent(&st); got != "" {
		t.Fatalf("expected empty when disabled, got %q", got)
	}
}
