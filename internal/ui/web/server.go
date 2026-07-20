package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/analyzer"
	"github.com/clodura/linkedin-post-agent/internal/config"
	"github.com/clodura/linkedin-post-agent/internal/generator"
	"github.com/clodura/linkedin-post-agent/internal/models"
	"github.com/clodura/linkedin-post-agent/internal/research"
	"github.com/clodura/linkedin-post-agent/internal/storage"
)

// topicHistoryLookback bounds how many past topic titles we check new
// suggestions/drafts against, to avoid repeating an already-covered subject.
const topicHistoryLookback = 300

// suggestionDuplicateThreshold is more aggressive than the default (0.5) used
// for actual post-selection dedup. Here, being wrong just means one fewer
// suggestion shown — cheap to retry — so it's worth catching more loosely
// related rewordings (e.g. "Platform Engineering: The New DevOps Frontier?"
// vs "Platform Engineering: The Developer's New Superpower").
const suggestionDuplicateThreshold = 0.3

// filterFreshIdeas drops ideas that duplicate topic history or an earlier
// idea in the same batch (the LLM can suggest overlapping ideas within one
// response), so repeated suggestions never reach the user.
func filterFreshIdeas(ideas []generator.TopicIdea, history []string) []generator.TopicIdea {
	seen := append([]string{}, history...)
	var fresh []generator.TopicIdea
	for _, idea := range ideas {
		if research.IsDuplicateTitleAt(idea.Title, seen, suggestionDuplicateThreshold) {
			continue
		}
		fresh = append(fresh, idea)
		seen = append(seen, idea.Title)
	}
	return fresh
}

//go:embed static/*
var staticFiles embed.FS

// mediaDir holds user-uploaded media files, served under /media/.
const mediaDir = "media"

//go:embed templates/*
var templateFiles embed.FS

type Server struct {
	addr      string
	logger    *zap.Logger
	agent     AgentBridge
	examples  *analyzer.ExampleStore
	drafts    *storage.DraftRepo
	topics    *storage.TopicRepo
	published *storage.PublishedRepo
	cycles    *storage.CycleRepo
	users     *storage.UserRepo
	sessions  *storage.SessionRepo
	media     *storage.MediaRepo
	li        config.LinkedInConfig
	gen       *generator.LLMGenerator
	voice     generator.BrandVoice
	maxPosts  int

	// suggestedTitlesMu guards suggestedTitles: an in-memory record of every
	// idea title shown to the user this run (whether or not they clicked
	// "+Add"), so repeated "Generate Ideas" clicks stay fresh even though only
	// added topics get persisted to MongoDB. Reset on server restart, which is
	// fine — this is a diversity nicety, not a hard uniqueness guarantee (that
	// guarantee lives in the DB-backed dedup for actually-used topics).
	suggestedTitlesMu sync.Mutex
	suggestedTitles   []string

	templates *template.Template
}

const maxSuggestedTitlesTracked = 200

func (s *Server) recordSuggestedTitles(ideas []generator.TopicIdea) {
	if len(ideas) == 0 {
		return
	}
	s.suggestedTitlesMu.Lock()
	defer s.suggestedTitlesMu.Unlock()
	for _, idea := range ideas {
		s.suggestedTitles = append(s.suggestedTitles, idea.Title)
	}
	if len(s.suggestedTitles) > maxSuggestedTitlesTracked {
		s.suggestedTitles = s.suggestedTitles[len(s.suggestedTitles)-maxSuggestedTitlesTracked:]
	}
}

func (s *Server) recentSuggestedTitles() []string {
	s.suggestedTitlesMu.Lock()
	defer s.suggestedTitlesMu.Unlock()
	out := make([]string, len(s.suggestedTitles))
	copy(out, s.suggestedTitles)
	return out
}

type AgentBridge interface {
	Generate(ctx context.Context, userID string, topicCount int) ([]models.Draft, error)
	PublishScheduled(ctx context.Context) error
}

type layoutData struct {
	Title   string
	Page    string
	Error   string
	Success string
	User    *models.User
	Content template.HTML
}

func NewServer(addr string, logger *zap.Logger, agent AgentBridge, examples *analyzer.ExampleStore, draftRepo *storage.DraftRepo, topicRepo *storage.TopicRepo, publishedRepo *storage.PublishedRepo, cycleRepo *storage.CycleRepo, userRepo *storage.UserRepo, sessionRepo *storage.SessionRepo, mediaRepo *storage.MediaRepo, li config.LinkedInConfig, gen *generator.LLMGenerator, voice generator.BrandVoice, maxPosts int) *Server {
	if maxPosts < 1 {
		maxPosts = 1
	}
	s := &Server{
		addr:      addr,
		logger:    logger,
		agent:     agent,
		examples:  examples,
		drafts:    draftRepo,
		topics:    topicRepo,
		published: publishedRepo,
		cycles:    cycleRepo,
		users:     userRepo,
		sessions:  sessionRepo,
		media:     mediaRepo,
		li:        li,
		gen:       gen,
		voice:     voice,
		maxPosts:  maxPosts,
	}

	funcMap := template.FuncMap{
		"truncate":        truncate,
		"formatTime":      formatTime,
		"formatTimeIST":   formatTimeIST,
		"formatScheduled": formatScheduled,
		"nl2br":           nl2br,
		"join":            strings.Join,
		"firstChar": func(s string) string {
			runes := []rune(s)
			if len(runes) > 0 {
				return string(runes[:1])
			}
			return "?"
		},
	}

	s.templates = template.Must(template.New("").Funcs(funcMap).ParseFS(templateFiles, "templates/*.html"))
	return s
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Public (no auth)
	mux.HandleFunc("GET /static/", s.serveStatic)
	mux.HandleFunc("GET /media/", s.serveMedia)
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("GET /auth/linkedin", s.handleLinkedInStart)
	mux.HandleFunc("GET /auth/callback", s.handleLinkedInCallback)
	mux.HandleFunc("POST /logout", s.handleLogout)

	// Protected (require a logged-in user; scoped to that user)
	mux.HandleFunc("GET /{$}", s.requireAuth(s.handleDashboard))
	mux.HandleFunc("GET /drafts", s.requireAuth(s.handleDrafts))
	mux.HandleFunc("GET /drafts/{id}", s.requireAuth(s.handleDraftPreview))
	mux.HandleFunc("POST /drafts/{id}/approve", s.requireAuth(s.handleDraftApprove))
	mux.HandleFunc("POST /drafts/{id}/schedule", s.requireAuth(s.handleDraftSchedule))
	mux.HandleFunc("POST /drafts/{id}/cancel-schedule", s.requireAuth(s.handleDraftCancelSchedule))
	mux.HandleFunc("POST /drafts/{id}/edit", s.requireAuth(s.handleDraftEdit))
	mux.HandleFunc("POST /drafts/{id}/media", s.requireAuth(s.handleDraftMedia))
	mux.HandleFunc("POST /drafts/{id}/reject", s.requireAuth(s.handleDraftReject))
	mux.HandleFunc("GET /history", s.requireAuth(s.handleHistory))
	mux.HandleFunc("POST /history/add", s.requireAuth(s.handleAddExample))
	mux.HandleFunc("POST /history/clear", s.requireAuth(s.handleClearExamples))
	mux.HandleFunc("GET /scheduled", s.requireAuth(s.handleScheduled))
	mux.HandleFunc("GET /topics", s.requireAuth(s.handleTopics))
	mux.HandleFunc("GET /topics/search", s.requireAuth(s.handleTopicSearch))
	mux.HandleFunc("POST /topics/add", s.requireAuth(s.handleAddTopic))
	mux.HandleFunc("POST /topics/{id}/delete", s.requireAuth(s.handleDeleteTopic))
	mux.HandleFunc("POST /run", s.requireAuth(s.handleRunCycle))
	mux.HandleFunc("GET /generate-stream", s.requireAuth(s.handleGenerateStream))
	mux.HandleFunc("POST /generate-from-query", s.requireAuth(s.handleGenerateFromQuery))
	mux.HandleFunc("GET /cycles", s.requireAuth(s.handleCycles))
	mux.HandleFunc("GET /guide", s.requireAuth(s.handleGuide))

	srv := &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	s.logger.Info("Web UI server starting", zap.String("addr", s.addr))
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Fatal("Web UI server error", zap.Error(err))
		}
	}()

	return nil
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	file := strings.TrimPrefix(r.URL.Path, "/static/")
	data, err := staticFiles.ReadFile(path.Join("static", file))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct := "text/plain"
	if strings.HasSuffix(file, ".css") {
		ct = "text/css"
	} else if strings.HasSuffix(file, ".js") {
		ct = "application/javascript"
	}
	w.Header().Set("Content-Type", ct)
	// Local dev server: never let the browser serve stale CSS/JS after a rebuild.
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

func (s *Server) renderPage(w http.ResponseWriter, page, title string, data interface{}) {
	s.renderPageFlash(w, nil, page, title, data)
}

// renderPageFlash renders a page and, when the request is provided, surfaces
// one-shot flash messages passed via ?ok= / ?err= query params (set on redirects).
func (s *Server) renderPageFlash(w http.ResponseWriter, r *http.Request, page, title string, data interface{}) {
	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, page, data); err != nil {
		s.logger.Error("Content template error", zap.String("page", page), zap.Error(err))
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	ld := layoutData{
		Title:   title,
		Page:    page,
		Content: template.HTML(buf.String()),
	}
	if r != nil {
		ld.Success = r.URL.Query().Get("ok")
		ld.Error = r.URL.Query().Get("err")
		ld.User = userFromCtx(r.Context())
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "layout.html", ld); err != nil {
		s.logger.Error("Layout template error", zap.Error(err))
	}
}

// renderBare renders a standalone full-page template (no app chrome), used for
// the login page.
func (s *Server) renderBare(w http.ResponseWriter, page, title string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, page, data); err != nil {
		s.logger.Error("Bare template error", zap.String("page", page), zap.Error(err))
		http.Error(w, "Internal error", http.StatusInternalServerError)
	}
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request, url string) {
	http.Redirect(w, r, url, http.StatusSeeOther)
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("Jan 2, 2006 3:04 PM")
}

func formatTimeIST(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return t.Format("Jan 2, 2006 3:04 PM")
	}
	return t.In(loc).Format("Jan 2, 2006 3:04 PM IST")
}

func formatScheduled(t *time.Time, tz string) string {
	if t == nil || t.IsZero() {
		return ""
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	return t.In(loc).Format("Jan 2, 2006 3:04 PM MST")
}

func nl2br(s string) template.HTML {
	return template.HTML(strings.ReplaceAll(template.HTMLEscapeString(s), "\n", "<br>"))
}

// --- Handlers ---

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := userFromCtx(ctx).ID
	drafts, _ := s.drafts.List(ctx, uid, "", 20)
	if drafts == nil {
		drafts = []models.Draft{}
	}
	published, _ := s.published.List(ctx, uid, 5)
	if published == nil {
		published = []models.PublishedPost{}
	}
	recentCycles, _ := s.cycles.List(ctx, uid, 5)
	if recentCycles == nil {
		recentCycles = []models.Cycle{}
	}

	s.renderPageFlash(w, r, "dashboard", "Dashboard", map[string]interface{}{
		"Drafts":    drafts,
		"Published": published,
		"Cycles":    recentCycles,
		"UserCount": s.users.Count(ctx),
	})
}

func (s *Server) handleDrafts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	drafts, _ := s.drafts.List(ctx, userFromCtx(ctx).ID, "", 50)
	s.renderPageFlash(w, r, "drafts", "Drafts", drafts)
}

func (s *Server) handleScheduled(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	drafts, _ := s.drafts.List(ctx, userFromCtx(ctx).ID, string(models.DraftStatusApproved), 50)
	if drafts == nil {
		drafts = []models.Draft{}
	}

	// Separate into scheduled (has time) vs approved (no time yet)
	type scheduledGroup struct {
		Scheduled []models.Draft
		Approved  []models.Draft
	}
	data := scheduledGroup{}
	for _, d := range drafts {
		if d.ScheduledFor != nil && !d.ScheduledFor.IsZero() {
			data.Scheduled = append(data.Scheduled, d)
		} else {
			data.Approved = append(data.Approved, d)
		}
	}
	if data.Scheduled == nil {
		data.Scheduled = []models.Draft{}
	}
	if data.Approved == nil {
		data.Approved = []models.Draft{}
	}
	s.renderPageFlash(w, r, "scheduled", "Scheduled Posts", data)
}

// ownedDraft fetches a draft and confirms it belongs to the current user, so a
// user can never read or mutate another tenant's draft by guessing its id.
func (s *Server) ownedDraft(ctx context.Context, id string) (*models.Draft, bool) {
	d, err := s.drafts.GetByID(ctx, id)
	if err != nil {
		return nil, false
	}
	u := userFromCtx(ctx)
	if u == nil || d.UserID != u.ID {
		return nil, false
	}
	return d, true
}

func (s *Server) handleDraftPreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	draft, ok := s.ownedDraft(ctx, id)
	if !ok {
		s.redirect(w, r, "/drafts?err=Draft+not+found")
		return
	}

	topic, _ := s.topics.GetByID(ctx, draft.TopicID)
	s.renderPageFlash(w, r, "preview", "Preview Post", map[string]interface{}{
		"Draft": draft,
		"Topic": topic,
	})
}

func (s *Server) handleDraftApprove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	draft, ok := s.ownedDraft(ctx, id)
	if !ok {
		s.redirect(w, r, "/drafts?err=Draft+not+found")
		return
	}
	// Guard: never re-publish an already-published (or rejected) draft.
	if draft.Status == models.DraftStatusPublished {
		s.redirect(w, r, "/drafts/"+id+"?err=Already+published+—+skipped+to+avoid+a+duplicate+post")
		return
	}
	if draft.Status == models.DraftStatusRejected {
		s.redirect(w, r, "/drafts/"+id+"?err=Draft+was+rejected+—+edit+and+re-approve+from+preview")
		return
	}

	// Approve AND make it due now: the publish poller only picks up drafts whose
	// scheduled_for has passed, so "Publish Now" must set that too.
	if err := s.drafts.UpdateScheduledFor(ctx, id, time.Now().UTC()); err != nil {
		s.logger.Error("Failed to set publish time", zap.Error(err))
	}
	if err := s.drafts.UpdateStatus(ctx, id, models.DraftStatusApproved); err != nil {
		s.logger.Error("Failed to approve draft", zap.Error(err))
	}

	s.logger.Info("Draft approved for immediate publish", zap.String("draft_id", id))
	s.redirect(w, r, "/drafts/"+id+"?ok=Publishing+now+—+it+will+appear+on+LinkedIn+within+a+few+seconds")
}

func (s *Server) handleDraftSchedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if _, ok := s.ownedDraft(ctx, id); !ok {
		s.redirect(w, r, "/drafts?err=Draft+not+found")
		return
	}

	dt := r.FormValue("datetime")
	tz := r.FormValue("tz")
	if dt == "" || tz == "" {
		s.logger.Warn("Missing datetime or tz in schedule request")
		s.redirect(w, r, "/drafts/"+id)
		return
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		s.logger.Warn("Invalid timezone", zap.String("tz", tz), zap.Error(err))
		s.redirect(w, r, "/drafts/"+id)
		return
	}

	t, err := time.ParseInLocation("2006-01-02T15:04", dt, loc)
	if err != nil {
		s.logger.Warn("Invalid datetime format", zap.String("datetime", dt), zap.Error(err))
		s.redirect(w, r, "/drafts/"+id)
		return
	}

	utc := t.UTC()

	// Guard: reject times in the past (2-minute grace) so a date-picker typo
	// can't publish instantly by accident.
	if utc.Before(time.Now().UTC().Add(-2 * time.Minute)) {
		s.logger.Warn("Rejected past schedule time", zap.Time("scheduled_for", utc))
		s.redirect(w, r, "/drafts/"+id+"?err=That+time+is+in+the+past+—+pick+a+future+time+or+use+Publish+Now")
		return
	}

	// Guard: never schedule an already-published draft (duplicate post).
	if draft, derr := s.drafts.GetByID(ctx, id); derr == nil && draft.Status == models.DraftStatusPublished {
		s.redirect(w, r, "/drafts/"+id+"?err=Already+published+—+cannot+schedule+again")
		return
	}

	// One post per day: if the requested day already has another scheduled
	// post, move to the next free day (same time of day) — up to 60 days out.
	target := t // in the user's location
	bumped := false
	for i := 0; i < 60; i++ {
		dayStart := time.Date(target.Year(), target.Month(), target.Day(), 0, 0, 0, 0, loc)
		dayEnd := dayStart.Add(24 * time.Hour)
		n, cerr := s.drafts.CountScheduledBetween(ctx, userFromCtx(ctx).ID, id, dayStart.UTC(), dayEnd.UTC())
		if cerr != nil {
			s.logger.Warn("Day-conflict check failed, scheduling anyway", zap.Error(cerr))
			break
		}
		if n == 0 {
			break
		}
		target = target.Add(24 * time.Hour)
		bumped = true
	}
	utc = target.UTC()

	if err := s.drafts.UpdateScheduledFor(ctx, id, utc); err != nil {
		s.logger.Error("Failed to set scheduled_for", zap.Error(err))
	}
	if err := s.drafts.UpdateStatus(ctx, id, models.DraftStatusApproved); err != nil {
		s.logger.Error("Failed to approve draft", zap.Error(err))
	}

	s.logger.Info("Draft scheduled",
		zap.String("draft_id", id),
		zap.Time("scheduled_for", utc),
		zap.Bool("bumped_for_conflict", bumped),
		zap.String("tz", tz),
	)
	if bumped {
		s.redirect(w, r, "/scheduled?ok="+url.QueryEscape(
			"That day already had a post — scheduled for "+target.Format("Jan 2, 3:04 PM")+" instead"))
		return
	}
	s.redirect(w, r, "/scheduled?ok=Post+scheduled")
}

func (s *Server) handleDraftCancelSchedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if _, ok := s.ownedDraft(ctx, id); !ok {
		s.redirect(w, r, "/drafts?err=Draft+not+found")
		return
	}

	if err := s.drafts.ClearScheduledFor(ctx, id); err != nil {
		s.logger.Error("Failed to clear scheduled_for", zap.Error(err))
	}
	if err := s.drafts.UpdateStatus(ctx, id, models.DraftStatusDraft); err != nil {
		s.logger.Error("Failed to revert status", zap.Error(err))
	}

	s.logger.Info("Draft schedule cancelled", zap.String("draft_id", id))
	s.redirect(w, r, "/drafts/"+id)
}

func (s *Server) handleDraftEdit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if _, ok := s.ownedDraft(ctx, id); !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	var req struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := s.drafts.UpdateBody(ctx, id, req.Body); err != nil {
		http.Error(w, "Failed to update", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleDraftMedia(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if _, ok := s.ownedDraft(ctx, id); !ok {
		s.redirect(w, r, "/drafts?err=Draft+not+found")
		return
	}

	_ = r.ParseMultipartForm(20 << 20) // up to 20MB uploads

	mediaType := models.MediaType(r.FormValue("type"))

	// Explicit clear: "none" removes media entirely.
	if mediaType == models.MediaTypeNone {
		if err := s.drafts.UpdateMedia(ctx, id, &models.Media{Type: models.MediaTypeNone}); err != nil {
			s.logger.Error("Failed to clear media", zap.Error(err))
		}
		s.redirect(w, r, "/drafts/"+id+"?ok=Media+removed")
		return
	}

	// Start from the existing media so an empty submit can't wipe the
	// auto-generated image (prompt, alt text, source URL).
	media := &models.Media{Type: mediaType}
	if draft, derr := s.drafts.GetByID(ctx, id); derr == nil && draft.Media != nil {
		*media = *draft.Media
		media.Type = mediaType
	}

	// An uploaded file takes precedence over a pasted URL (URLs must be public;
	// private links like ChatGPT content URLs return 403 and can't be fetched).
	changed := false
	if file, hdr, err := r.FormFile("file"); err == nil {
		defer file.Close()
		if serr := s.saveUploadedMedia(ctx, userFromCtx(ctx).ID, id, hdr.Filename, file); serr != nil {
			s.logger.Error("Failed to save uploaded media", zap.Error(serr))
			s.redirect(w, r, "/drafts/"+id+"?err=Upload+failed+—+try+again")
			return
		}
		// Absolute, persistent URL: served from MongoDB, so it survives restarts
		// AND is what gets uploaded to LinkedIn at publish time (taking priority
		// over the auto-generated Pollinations image).
		media.LocalPath = ""
		media.SourceURL = baseURL(r) + "/media/" + id
		media.LinkedInAssetURN = ""
		changed = true
	} else if url := r.FormValue("url"); url != "" {
		media.SourceURL = url
		media.LocalPath = ""
		media.LinkedInAssetURN = ""
		changed = true
	}

	if !changed {
		s.redirect(w, r, "/drafts/"+id+"?err=Choose+a+file+or+paste+a+URL+first")
		return
	}

	if err := s.drafts.UpdateMedia(ctx, id, media); err != nil {
		s.logger.Error("Failed to update media", zap.Error(err))
	}

	s.redirect(w, r, "/drafts/"+id+"?ok=Media+updated")
}

// saveUploadedMedia stores an uploaded image in MongoDB under the given id, so
// it persists across restarts/ephemeral disks and is fetchable at publish time.
func (s *Server) saveUploadedMedia(ctx context.Context, userID, id, filename string, file io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(file, 8<<20)) // 8MB cap (LinkedIn allows ~6MB)
	if err != nil {
		return err
	}
	ct := "image/jpeg"
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png":
		ct = "image/png"
	case ".gif":
		ct = "image/gif"
	case ".webp":
		ct = "image/webp"
	}
	return s.media.Put(ctx, id, userID, ct, data)
}

func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request) {
	name := path.Base(strings.TrimPrefix(r.URL.Path, "/media/"))
	if name == "" || name == "." || name == "/" {
		http.NotFound(w, r)
		return
	}
	// Media id may carry a legacy extension (e.g. "<id>.png"); strip it.
	id := name
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		id = name[:i]
	}
	ct, data, err := s.media.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func (s *Server) handleDraftReject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	draft, ok := s.ownedDraft(ctx, id)
	if !ok {
		s.redirect(w, r, "/drafts?err=Draft+not+found")
		return
	}
	// Guard: rejecting a published draft would misrecord history (the post is
	// already live on LinkedIn).
	if draft.Status == models.DraftStatusPublished {
		s.redirect(w, r, "/drafts/"+id+"?err=Already+published+—+delete+it+on+LinkedIn+if+needed")
		return
	}

	// Clear any pending schedule so a rejected draft can never auto-publish.
	s.drafts.ClearScheduledFor(ctx, id)
	s.drafts.UpdateStatus(ctx, id, models.DraftStatusRejected)
	s.redirect(w, r, "/drafts?ok=Draft+rejected")
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	published, _ := s.published.List(ctx, userFromCtx(ctx).ID, 20)
	examples, _ := s.examples.List(ctx, 20)

	var pattern *analyzer.PostPattern
	if len(examples) > 0 {
		pattern = analyzer.Analyze(examples)
	}

	s.renderPageFlash(w, r, "history", "Post History", map[string]interface{}{
		"Published": published,
		"Examples":  examples,
		"Pattern":   pattern,
	})
}

func (s *Server) handleAddExample(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body := r.FormValue("body")
	imageURL := r.FormValue("image_url")
	if body == "" {
		s.redirect(w, r, "/history")
		return
	}

	if err := s.examples.Insert(ctx, body, imageURL); err != nil {
		s.logger.Error("Failed to add example", zap.Error(err))
	}
	s.redirect(w, r, "/history")
}

func (s *Server) handleClearExamples(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s.examples.DeleteAll(ctx)
	s.redirect(w, r, "/history")
}

func (s *Server) handleGenerateFromQuery(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("query")
	if query == "" {
		s.redirect(w, r, "/")
		return
	}

	uid := userFromCtx(r.Context()).ID
	s.logger.Info("Generating drafts from query", zap.String("query", query))
	s.redirect(w, r, "/")

	go func(q, uid string) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		examples, _ := s.examples.List(ctx, 10)
		var exampleBodies []string
		styleGuide := ""
		if len(examples) > 0 {
			for _, ex := range examples {
				exampleBodies = append(exampleBodies, ex.Body)
			}
			pattern := analyzer.Analyze(examples)
			styleGuide = pattern.PromptSuffix()
		}

		history, _ := s.topics.RecentTitles(ctx, uid, topicHistoryLookback)
		history = append(history, s.recentSuggestedTitles()...)

		ideas, err := s.gen.GenerateTopicIdeas(ctx, q, exampleBodies, history)
		if err != nil {
			s.logger.Error("Topic idea generation failed", zap.Error(err))
			return
		}
		ideas = filterFreshIdeas(ideas, history)
		s.recordSuggestedTitles(ideas)

		if len(ideas) > s.maxPosts {
			ideas = ideas[:s.maxPosts]
		}

		var generated int
		for _, idea := range ideas {
			topic := models.Topic{
				UserID:  uid,
				Title:   idea.Title,
				Summary: idea.Hook,
				Source:  "manual",
			}
			if err := s.topics.Insert(ctx, &topic); err != nil {
				s.logger.Warn("Failed to store topic", zap.Error(err))
				continue
			}

			draft, err := s.gen.GenerateFromTopic(ctx, topic, s.voice, styleGuide)
			if err != nil {
				s.logger.Warn("Failed to generate draft", zap.String("topic", idea.Title), zap.Error(err))
				continue
			}

			draft.UserID = uid
			draft.TopicID = topic.ID
			if err := s.drafts.Insert(ctx, draft); err != nil {
				s.logger.Warn("Failed to store draft", zap.Error(err))
				continue
			}
			generated++
		}

		s.logger.Info("Generated drafts from query", zap.Int("count", generated))
	}(query, uid)
}

func (s *Server) handleGenerateStream(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	if query == "" {
		http.Error(w, "Missing query", http.StatusBadRequest)
		return
	}

	uid := userFromCtx(r.Context()).ID

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	fmt.Fprintf(w, "event: start\ndata: {\"status\":\"started\"}\n\n")
	flusher.Flush()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	examples, _ := s.examples.List(ctx, 10)
	var exampleBodies []string
	styleGuide := ""
	if len(examples) > 0 {
		for _, ex := range examples {
			exampleBodies = append(exampleBodies, ex.Body)
		}
		pattern := analyzer.Analyze(examples)
		styleGuide = pattern.PromptSuffix()
	}

	history, _ := s.topics.RecentTitles(ctx, uid, topicHistoryLookback)
	history = append(history, s.recentSuggestedTitles()...)

	ideas, err := s.gen.GenerateTopicIdeas(ctx, query, exampleBodies, history)
	if err != nil {
		errData, _ := json.Marshal(map[string]string{"error": err.Error()})
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", errData)
		flusher.Flush()
		return
	}
	// The user's own typed query is always included as-is (they explicitly
	// want it); only the LLM's auto-suggestions get filtered against history.
	ideas = filterFreshIdeas(ideas, history)
	s.recordSuggestedTitles(ideas)

	// Always include the user's exact query as the first topic
	userIdea := generator.TopicIdea{
		Title:       query,
		Hook:        "Let's explore this topic in depth.",
		Explanation: "Generated based on your specific query.",
	}
	allIdeas := append([]generator.TopicIdea{userIdea}, ideas...)
	if len(allIdeas) > s.maxPosts {
		allIdeas = allIdeas[:s.maxPosts]
	}

	fmt.Fprintf(w, "event: ideas\ndata: {\"total\":%d}\n\n", len(allIdeas))
	flusher.Flush()

	for i, idea := range allIdeas {
		genEvent, _ := json.Marshal(map[string]interface{}{
			"index": i,
			"title": idea.Title,
			"hook":  idea.Hook,
		})
		fmt.Fprintf(w, "event: generating\ndata: %s\n\n", genEvent)
		flusher.Flush()

		topic := models.Topic{
			UserID:  uid,
			Title:   idea.Title,
			Summary: idea.Hook,
			Source:  "manual",
		}
		_ = s.topics.Insert(ctx, &topic)

		draft, err := s.gen.GenerateFromTopic(ctx, topic, s.voice, styleGuide)
		if err != nil {
			errEvent, _ := json.Marshal(map[string]interface{}{
				"index": i,
				"title": idea.Title,
				"error": err.Error(),
			})
			// Named "drafterror" so it doesn't collide with EventSource's
			// built-in "error" event (which would kill the whole stream UI).
			fmt.Fprintf(w, "event: drafterror\ndata: %s\n\n", errEvent)
			flusher.Flush()
			continue
		}

		draft.UserID = uid
		draft.TopicID = topic.ID
		draft.Title = idea.Title
		_ = s.drafts.Insert(ctx, draft)

		draftEvent, _ := json.Marshal(map[string]interface{}{
			"index":    i,
			"id":       draft.ID,
			"title":    idea.Title,
			"body":     draft.Body,
			"hashtags": draft.Hashtags,
		})
		fmt.Fprintf(w, "event: draft\ndata: %s\n\n", draftEvent)
		flusher.Flush()
	}

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

func (s *Server) handleRunCycle(w http.ResponseWriter, r *http.Request) {
	uid := userFromCtx(r.Context()).ID
	go func(uid string) {
		cycleCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if _, err := s.agent.Generate(cycleCtx, uid, s.maxPosts); err != nil {
			s.logger.Error("Cycle failed", zap.Error(err))
		}
	}(uid)
	s.redirect(w, r, "/")
}

func (s *Server) handleTopics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	topics, _ := s.topics.List(ctx, userFromCtx(ctx).ID, 50)
	s.renderPageFlash(w, r, "topics", "Topics", map[string]interface{}{
		"Topics":       topics,
		"SearchQuery":  "",
		"SearchResult": nil,
	})
}

func (s *Server) handleTopicSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := userFromCtx(ctx).ID
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Missing query", http.StatusBadRequest)
		return
	}

	topics, _ := s.topics.List(ctx, uid, 50)

	examples, _ := s.examples.List(ctx, 10)
	var exampleBodies []string
	for _, ex := range examples {
		exampleBodies = append(exampleBodies, ex.Body)
	}

	history, _ := s.topics.RecentTitles(ctx, uid, topicHistoryLookback)
	// Also avoid repeating anything shown in a previous search this session,
	// even if the user never clicked "+Add" (so it was never persisted to the
	// DB history above).
	history = append(history, s.recentSuggestedTitles()...)

	ideas, err := s.gen.GenerateTopicIdeas(ctx, query, exampleBodies, history)
	if err != nil {
		s.logger.Error("Topic idea generation failed", zap.Error(err))
		s.renderPageFlash(w, r, "topics", "Topics", map[string]interface{}{
			"Topics":      topics,
			"SearchError": fmt.Sprintf("Generation failed: %v", err),
			"SearchQuery": query,
		})
		return
	}
	ideas = filterFreshIdeas(ideas, history)
	s.recordSuggestedTitles(ideas)

	s.renderPageFlash(w, r, "topics", "Topics", map[string]interface{}{
		"Topics":       topics,
		"SearchResult": ideas,
		"SearchQuery":  query,
	})
}

func (s *Server) handleAddTopic(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		s.redirect(w, r, "/topics?err=Topic+title+cannot+be+empty")
		return
	}

	source := r.FormValue("source")
	if source == "" {
		source = "custom"
	}

	topic := &models.Topic{
		UserID:  userFromCtx(ctx).ID,
		Title:   title,
		URL:     r.FormValue("url"),
		Summary: r.FormValue("summary"),
		Source:  source,
	}
	if err := s.topics.Insert(ctx, topic); err != nil {
		s.logger.Error("Failed to add topic", zap.Error(err))
		s.redirect(w, r, "/topics?err=Failed+to+add+topic")
		return
	}

	// Generate the post right away instead of leaving the topic to sit until
	// someone clicks Run Cycle (or the once-daily auto job happens to pick
	// it — which prioritizes trending topics first and may never reach it).
	go s.generateFromAddedTopic(*topic)

	s.redirect(w, r, "/topics?ok=Topic+added+—+generating+a+post+now,+check+Drafts+in+a+bit")
}

// generateFromAddedTopic creates a draft from a just-added topic in the
// background, so "+Add" / "Add Topic" feels immediate while generation
// (15s-2min depending on provider) happens without blocking the request.
// Marks the topic used so it's never picked up again by Run Cycle or the
// daily auto-generate job, avoiding a duplicate post on the same topic.
func (s *Server) generateFromAddedTopic(topic models.Topic) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	examples, _ := s.examples.List(ctx, 10)
	styleGuide := ""
	if len(examples) > 0 {
		pattern := analyzer.Analyze(examples)
		styleGuide = pattern.PromptSuffix()
	}

	draft, err := s.gen.GenerateFromTopic(ctx, topic, s.voice, styleGuide)
	if err != nil {
		s.logger.Warn("Failed to generate draft for added topic", zap.String("topic", topic.Title), zap.Error(err))
		return
	}
	draft.UserID = topic.UserID
	draft.TopicID = topic.ID
	if err := s.drafts.Insert(ctx, draft); err != nil {
		s.logger.Warn("Failed to store draft for added topic", zap.String("topic", topic.Title), zap.Error(err))
		return
	}
	if err := s.topics.MarkUsed(ctx, topic.ID); err != nil {
		s.logger.Warn("Failed to mark added topic used", zap.String("topic", topic.Title), zap.Error(err))
	}
	s.logger.Info("Generated draft for added topic", zap.String("topic", topic.Title), zap.String("draft_id", draft.ID))
}

func (s *Server) handleDeleteTopic(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	// Ownership guard: only delete a topic the current user owns.
	if t, err := s.topics.GetByID(ctx, id); err != nil || t.UserID != userFromCtx(ctx).ID {
		s.redirect(w, r, "/topics?err=Topic+not+found")
		return
	}
	if err := s.topics.Delete(ctx, id); err != nil {
		s.logger.Error("Failed to delete topic", zap.Error(err))
	}
	s.redirect(w, r, "/topics")
}

func (s *Server) handleCycles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cycles, _ := s.cycles.List(ctx, userFromCtx(ctx).ID, 20)
	s.renderPageFlash(w, r, "cycles", "Cycles", cycles)
}

func (s *Server) handleGuide(w http.ResponseWriter, r *http.Request) {
	s.renderPageFlash(w, r, "guide", "Guide", nil)
}
