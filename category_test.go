package goed2k

import "testing"

func TestMatchCategoryByExtension(t *testing.T) {
	categories := []Category{
		{Name: "video", OutputDir: "/videos", AutoExtension: ".mp4,.mkv"},
		{Name: "music", OutputDir: "/music", AutoExtension: "mp3"},
	}
	if cat := MatchCategory(categories, "movie.MP4"); cat == nil || cat.Name != "video" {
		t.Fatalf("expected video category, got %#v", cat)
	}
	if cat := MatchCategory(categories, "song.mp3"); cat == nil || cat.Name != "music" {
		t.Fatalf("expected music category, got %#v", cat)
	}
	if cat := MatchCategory(categories, "readme.txt"); cat != nil {
		t.Fatalf("expected no category, got %#v", cat)
	}
}

func TestResolveCategoryOutputDir(t *testing.T) {
	categories := []Category{
		{Name: "video", OutputDir: "/videos", AutoExtension: ".mp4"},
	}
	if got := ResolveCategoryOutputDir(categories, "clip.mp4", "/downloads"); got != "/videos" {
		t.Fatalf("expected /videos, got %q", got)
	}
	if got := ResolveCategoryOutputDir(categories, "clip.avi", "/downloads"); got != "/downloads" {
		t.Fatalf("expected /downloads, got %q", got)
	}
}
