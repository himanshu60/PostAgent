package web

import (
	"bytes"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/config"
	"github.com/clodura/linkedin-post-agent/internal/generator"
	"github.com/clodura/linkedin-post-agent/internal/models"
)

func newTestServer() *Server {
	return NewServer(":0", zap.NewNop(), nil, nil, nil, nil, nil, nil, nil, nil, nil, config.LinkedInConfig{}, nil, generator.BrandVoice{}, 1)
}

func TestDraftImageURL(t *testing.T) {
	if got := draftImageURL(nil); got != "" {
		t.Errorf("nil draft: %q", got)
	}
	if got := draftImageURL(&models.Draft{ID: "a"}); got != "" {
		t.Errorf("no media: %q", got)
	}
	card := &models.Draft{ID: "abc", Media: &models.Media{Card: &models.NoteCard{Seed: 42}, SourceURL: "https://ignored"}}
	if got := draftImageURL(card); got != "/drafts/abc/image.png?v=42" {
		t.Errorf("card draft: %q", got)
	}
	src := &models.Draft{ID: "abc", Media: &models.Media{SourceURL: "https://example.com/x.png"}}
	if got := draftImageURL(src); got != "https://example.com/x.png" {
		t.Errorf("source draft: %q", got)
	}
}

func TestPreviewTemplateImages(t *testing.T) {
	s := newTestServer()
	render := func(d *models.Draft) string {
		var buf bytes.Buffer
		if err := s.templates.ExecuteTemplate(&buf, "preview", map[string]interface{}{"Draft": d, "Topic": nil}); err != nil {
			t.Fatalf("execute preview: %v", err)
		}
		return buf.String()
	}

	cardDraft := &models.Draft{ID: "d1", Title: "T", Body: "B", Status: models.DraftStatusDraft,
		Media: &models.Media{Type: models.MediaTypeImage, AltText: "Handwritten notes: T", Card: &models.NoteCard{Headline: "T", Seed: 7}}}
	html := render(cardDraft)
	if strings.Count(html, `src="/drafts/d1/image.png?v=7"`) != 2 {
		t.Errorf("card image should appear in preview and media card:\n%s", html)
	}
	if !strings.Contains(html, "Try another handwritten style") || !strings.Contains(html, `action="/drafts/d1/handwritten"`) {
		t.Error("restyle button missing for card draft")
	}

	urlDraft := &models.Draft{ID: "d2", Status: models.DraftStatusDraft, Media: &models.Media{Type: models.MediaTypeImage, SourceURL: "https://example.com/x.png"}}
	html = render(urlDraft)
	if !strings.Contains(html, `src="https://example.com/x.png"`) || !strings.Contains(html, "Use a handwritten image") {
		t.Error("source-URL draft should show its image and offer the handwritten option")
	}

	bare := &models.Draft{ID: "d3", Status: models.DraftStatusPublished}
	html = render(bare)
	if strings.Contains(html, "<img") || strings.Contains(html, "/handwritten") {
		t.Error("published draft without media should show no image and no handwritten action")
	}
}

func TestDashboardTemplateRenders(t *testing.T) {
	s := newTestServer()
	var buf bytes.Buffer
	err := s.templates.ExecuteTemplate(&buf, "dashboard", map[string]interface{}{
		"Drafts": []models.Draft{}, "Published": []models.PublishedPost{}, "Cycles": []models.Cycle{},
		"UserCount": int64(1), "NeedsPrefs": false,
	})
	if err != nil {
		t.Fatalf("execute dashboard: %v", err)
	}
	if !strings.Contains(buf.String(), "gen-thumb") {
		t.Error("dashboard stream script should render handwritten thumbnails")
	}
}
