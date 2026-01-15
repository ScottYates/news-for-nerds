package srv

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"srv.exe.dev/db"
	"srv.exe.dev/db/dbgen"
)

type Server struct {
	DB           *sql.DB
	Hostname     string
	TemplatesDir string
	StaticDir    string
	httpClient   *http.Client
}

func New(dbPath, hostname string) (*Server, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(thisFile)
	srv := &Server{
		Hostname:     hostname,
		TemplatesDir: filepath.Join(baseDir, "templates"),
		StaticDir:    filepath.Join(baseDir, "static"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	if err := srv.setUpDatabase(dbPath); err != nil {
		return nil, err
	}
	return srv, nil
}

func (s *Server) HandleRoot(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
	if userID == "" {
		userID = "anonymous"
	}

	q := dbgen.New(s.DB)
	pages, err := q.GetPagesByUserID(r.Context(), userID)
	if err != nil {
		slog.Warn("failed to get pages", "error", err)
	}

	// Create default page if none exists
	if len(pages) == 0 {
		pageID := uuid.New().String()
		bgColor := "#1a1a2e"
		bgImage := ""
		now := time.Now()
		err := q.CreatePage(r.Context(), dbgen.CreatePageParams{
			ID:        pageID,
			UserID:    userID,
			Name:      "My Page",
			BgColor:   &bgColor,
			BgImage:   &bgImage,
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			slog.Warn("failed to create default page", "error", err)
		}
		// Redirect to the new page
		http.Redirect(w, r, "/page/"+pageID, http.StatusFound)
		return
	}

	// Redirect to first page
	http.Redirect(w, r, "/page/"+pages[0].ID, http.StatusFound)
}

func (s *Server) HandlePage(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	userID := strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
	if userID == "" {
		userID = "anonymous"
	}

	q := dbgen.New(s.DB)
	page, err := q.GetPageByID(r.Context(), pageID)
	if err != nil {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}

	// Check ownership
	if page.UserID != userID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.renderTemplate(w, "dashboard.html", page); err != nil {
		slog.Warn("render template", "url", r.URL.Path, "error", err)
	}
}

// API Handlers

type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) HandleAPIGetWidgets(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("pageId")
	userID := strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
	if userID == "" {
		userID = "anonymous"
	}

	q := dbgen.New(s.DB)

	// Verify page ownership
	page, err := q.GetPageByID(r.Context(), pageID)
	if err != nil || page.UserID != userID {
		s.writeJSON(w, http.StatusForbidden, APIResponse{Error: "forbidden"})
		return
	}

	widgets, err := q.GetWidgetsByPageID(r.Context(), pageID)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: widgets})
}

func (s *Server) HandleAPICreateWidget(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("pageId")
	userID := strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
	if userID == "" {
		userID = "anonymous"
	}

	q := dbgen.New(s.DB)

	// Verify page ownership
	page, err := q.GetPageByID(r.Context(), pageID)
	if err != nil || page.UserID != userID {
		s.writeJSON(w, http.StatusForbidden, APIResponse{Error: "forbidden"})
		return
	}

	var input struct {
		Title       string `json:"title"`
		WidgetType  string `json:"widget_type"`
		PosX        int64  `json:"pos_x"`
		PosY        int64  `json:"pos_y"`
		Width       int64  `json:"width"`
		Height      int64  `json:"height"`
		BgColor     string `json:"bg_color"`
		TextColor   string `json:"text_color"`
		HeaderColor string `json:"header_color"`
		Config      string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid json"})
		return
	}

	// Defaults
	if input.Title == "" {
		input.Title = "New Widget"
	}
	if input.WidgetType == "" {
		input.WidgetType = "rss"
	}
	if input.Width == 0 {
		input.Width = 300
	}
	if input.Height == 0 {
		input.Height = 400
	}
	if input.BgColor == "" {
		input.BgColor = "#16213e"
	}
	if input.TextColor == "" {
		input.TextColor = "#ffffff"
	}
	if input.HeaderColor == "" {
		input.HeaderColor = "#0f3460"
	}
	if input.Config == "" {
		input.Config = "{}"
	}

	widgetID := uuid.New().String()
	now := time.Now()

	err = q.CreateWidget(r.Context(), dbgen.CreateWidgetParams{
		ID:          widgetID,
		PageID:      pageID,
		Title:       input.Title,
		WidgetType:  input.WidgetType,
		PosX:        input.PosX,
		PosY:        input.PosY,
		Width:       input.Width,
		Height:      input.Height,
		BgColor:     &input.BgColor,
		TextColor:   &input.TextColor,
		HeaderColor: &input.HeaderColor,
		Config:      input.Config,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	widget, _ := q.GetWidgetByID(r.Context(), widgetID)
	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: widget})
}

func (s *Server) HandleAPIUpdateWidget(w http.ResponseWriter, r *http.Request) {
	widgetID := r.PathValue("id")
	userID := strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
	if userID == "" {
		userID = "anonymous"
	}

	q := dbgen.New(s.DB)

	// Get widget and verify ownership
	widget, err := q.GetWidgetByID(r.Context(), widgetID)
	if err != nil {
		s.writeJSON(w, http.StatusNotFound, APIResponse{Error: "not found"})
		return
	}

	page, err := q.GetPageByID(r.Context(), widget.PageID)
	if err != nil || page.UserID != userID {
		s.writeJSON(w, http.StatusForbidden, APIResponse{Error: "forbidden"})
		return
	}

	var input struct {
		Title       *string `json:"title"`
		WidgetType  *string `json:"widget_type"`
		PosX        *int64  `json:"pos_x"`
		PosY        *int64  `json:"pos_y"`
		Width       *int64  `json:"width"`
		Height      *int64  `json:"height"`
		BgColor     *string `json:"bg_color"`
		TextColor   *string `json:"text_color"`
		HeaderColor *string `json:"header_color"`
		Config      *string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid json"})
		return
	}

	// Apply updates
	if input.Title != nil {
		widget.Title = *input.Title
	}
	if input.WidgetType != nil {
		widget.WidgetType = *input.WidgetType
	}
	if input.PosX != nil {
		widget.PosX = *input.PosX
	}
	if input.PosY != nil {
		widget.PosY = *input.PosY
	}
	if input.Width != nil {
		widget.Width = *input.Width
	}
	if input.Height != nil {
		widget.Height = *input.Height
	}
	if input.BgColor != nil {
		widget.BgColor = input.BgColor
	}
	if input.TextColor != nil {
		widget.TextColor = input.TextColor
	}
	if input.HeaderColor != nil {
		widget.HeaderColor = input.HeaderColor
	}
	if input.Config != nil {
		widget.Config = *input.Config
	}

	err = q.UpdateWidget(r.Context(), dbgen.UpdateWidgetParams{
		Title:       widget.Title,
		WidgetType:  widget.WidgetType,
		PosX:        widget.PosX,
		PosY:        widget.PosY,
		Width:       widget.Width,
		Height:      widget.Height,
		BgColor:     widget.BgColor,
		TextColor:   widget.TextColor,
		HeaderColor: widget.HeaderColor,
		Config:      widget.Config,
		UpdatedAt:   time.Now(),
		ID:          widgetID,
	})
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	widget, _ = q.GetWidgetByID(r.Context(), widgetID)
	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: widget})
}

func (s *Server) HandleAPIDeleteWidget(w http.ResponseWriter, r *http.Request) {
	widgetID := r.PathValue("id")
	userID := strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
	if userID == "" {
		userID = "anonymous"
	}

	q := dbgen.New(s.DB)

	// Get widget and verify ownership
	widget, err := q.GetWidgetByID(r.Context(), widgetID)
	if err != nil {
		s.writeJSON(w, http.StatusNotFound, APIResponse{Error: "not found"})
		return
	}

	page, err := q.GetPageByID(r.Context(), widget.PageID)
	if err != nil || page.UserID != userID {
		s.writeJSON(w, http.StatusForbidden, APIResponse{Error: "forbidden"})
		return
	}

	err = q.DeleteWidget(r.Context(), widgetID)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Success: true})
}

func (s *Server) HandleAPIGetFeed(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "url required"})
		return
	}

	feed, err := s.GetFeed(r.Context(), url)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: feed})
}

func (s *Server) HandleAPIRefreshFeed(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "url required"})
		return
	}

	// Force refresh
	s.fetchAndStoreFeed(r.Context(), url)

	feed, err := s.GetFeed(r.Context(), url)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: feed})
}

func (s *Server) HandleAPIUpdatePage(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	userID := strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
	if userID == "" {
		userID = "anonymous"
	}

	q := dbgen.New(s.DB)

	page, err := q.GetPageByID(r.Context(), pageID)
	if err != nil || page.UserID != userID {
		s.writeJSON(w, http.StatusForbidden, APIResponse{Error: "forbidden"})
		return
	}

	var input struct {
		Name    *string `json:"name"`
		BgColor *string `json:"bg_color"`
		BgImage *string `json:"bg_image"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid json"})
		return
	}

	if input.Name != nil {
		page.Name = *input.Name
	}
	if input.BgColor != nil {
		page.BgColor = input.BgColor
	}
	if input.BgImage != nil {
		page.BgImage = input.BgImage
	}

	err = q.UpdatePage(r.Context(), dbgen.UpdatePageParams{
		Name:      page.Name,
		BgColor:   page.BgColor,
		BgImage:   page.BgImage,
		UpdatedAt: time.Now(),
		ID:        pageID,
	})
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	page, _ = q.GetPageByID(r.Context(), pageID)
	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: page})
}

func (s *Server) renderTemplate(w http.ResponseWriter, name string, data any) error {
	path := filepath.Join(s.TemplatesDir, name)
	tmpl, err := template.ParseFiles(path)
	if err != nil {
		return fmt.Errorf("parse template %q: %w", name, err)
	}
	if err := tmpl.Execute(w, data); err != nil {
		return fmt.Errorf("execute template %q: %w", name, err)
	}
	return nil
}

func (s *Server) setUpDatabase(dbPath string) error {
	wdb, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}
	s.DB = wdb
	if err := db.RunMigrations(wdb); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

func (s *Server) Serve(addr string) error {
	mux := http.NewServeMux()

	// Pages
	mux.HandleFunc("GET /{$}", s.HandleRoot)
	mux.HandleFunc("GET /page/{id}", s.HandlePage)

	// API
	mux.HandleFunc("GET /api/pages/{pageId}/widgets", s.HandleAPIGetWidgets)
	mux.HandleFunc("POST /api/pages/{pageId}/widgets", s.HandleAPICreateWidget)
	mux.HandleFunc("PATCH /api/widgets/{id}", s.HandleAPIUpdateWidget)
	mux.HandleFunc("DELETE /api/widgets/{id}", s.HandleAPIDeleteWidget)
	mux.HandleFunc("PATCH /api/pages/{id}", s.HandleAPIUpdatePage)
	mux.HandleFunc("GET /api/feed", s.HandleAPIGetFeed)
	mux.HandleFunc("POST /api/feed/refresh", s.HandleAPIRefreshFeed)

	// Static files
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(s.StaticDir))))

	// Start feed refresher
	ctx := context.Background()
	s.StartFeedRefresher(ctx)

	slog.Info("starting server", "addr", addr)
	return http.ListenAndServe(addr, mux)
}
