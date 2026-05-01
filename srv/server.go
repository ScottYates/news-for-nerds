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
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"srv.exe.dev/db"
	"srv.exe.dev/db/dbgen"
)

var slugRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func isValidSlug(slug string) bool {
	if len(slug) < 1 || len(slug) > 64 {
		return false
	}
	return slugRegex.MatchString(slug)
}

// slugify converts a name or email into a URL-friendly slug.
// "Scott Yates" -> "scott-yates", "beernutz@gmail.com" -> "beernutz"
func slugify(name, email string) string {
	raw := name
	if raw == "" {
		raw = email
	}
	// Use part before @ for emails
	if idx := strings.Index(raw, "@"); idx > 0 {
		raw = raw[:idx]
	}
	// Lowercase, replace non-alphanum with hyphens, collapse runs, trim
	var b strings.Builder
	lastHyphen := true // avoid leading hyphen
	for _, r := range strings.ToLower(raw) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastHyphen = false
		} else if !lastHyphen {
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if s == "" {
		s = "page"
	}
	if len(s) > 48 {
		s = s[:48]
	}
	return s
}

// assignSlug picks a unique slug for a page based on name/email.
// Tries "scott-yates", then "scott-yates-2", "scott-yates-3", etc.
func (s *Server) assignSlug(ctx context.Context, q *dbgen.Queries, pageID, name, email string) string {
	base := slugify(name, email)
	candidate := base
	for i := 2; i < 100; i++ {
		count, err := q.CheckSlugExists(ctx, dbgen.CheckSlugExistsParams{
			Slug: &candidate,
			ID:   pageID,
		})
		if err != nil || count == 0 {
			break
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	now := time.Now()
	_ = q.UpdatePageSlug(ctx, dbgen.UpdatePageSlugParams{
		Slug:      &candidate,
		UpdatedAt: now,
		ID:        pageID,
	})
	// Enable slug access so the owner can reach the page via slug URL
	slugAccess := int64(1)
	_ = q.UpdatePageSlugAccess(ctx, dbgen.UpdatePageSlugAccessParams{
		SlugAccess: &slugAccess,
		UpdatedAt:  now,
		ID:         pageID,
	})
	return candidate
}

type Server struct {
	DB           *sql.DB
	Config       *Config
	Hostname     string
	TemplatesDir string
	StaticDir    string
	httpClient   *http.Client
}

func New(cfg *Config, hostname string) (*Server, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(thisFile)
	srv := &Server{
		Config:       cfg,
		Hostname:     hostname,
		TemplatesDir: filepath.Join(baseDir, "templates"),
		StaticDir:    filepath.Join(baseDir, "static"),
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.FeedFetchTimeout) * time.Second,
		},
	}
	if err := srv.setUpDatabase(cfg.DBPath); err != nil {
		return nil, err
	}
	return srv, nil
}

func (s *Server) HandleRoot(w http.ResponseWriter, r *http.Request) {
	// Use GetOrCreateVisitorID so unauthenticated visitors get a stable
	// cookie-based identity before we create a default page for them.
	userID := s.GetUserID(r)
	if userID == "anonymous" {
		userID = s.GetOrCreateVisitorID(w, r)
	}

	q := dbgen.New(s.DB)
	pages, err := q.GetPagesByUserID(r.Context(), userID)
	if err != nil {
		slog.Warn("failed to get pages", "error", err)
	}

	// Create default page if none exists
	if len(pages) == 0 {
		pageID := uuid.New().String()
		bgColor := s.Config.DefaultBgColor
		bgImage := ""
		now := time.Now()
		err := q.CreatePage(r.Context(), dbgen.CreatePageParams{
			ID:        pageID,
			UserID:    userID,
			Name:      "My Page",
			BgColor:   &bgColor,
			BgImage:   &bgImage,
			Config:    "{}",
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			slog.Warn("failed to create default page", "error", err)
		}
		// Auto-assign slug from user name/email
		slug := ""
		if session, err := s.GetUserFromRequest(r); err == nil {
			slug = s.assignSlug(r.Context(), q, pageID, session.UserName, session.Email)
		}
		if slug != "" {
			http.Redirect(w, r, "/page/"+slug, http.StatusFound)
		} else {
			http.Redirect(w, r, "/page/"+pageID, http.StatusFound)
		}
		return
	}

	// Redirect to slug if available, otherwise ID
	page := pages[0]
	if page.Slug != nil && *page.Slug != "" {
		http.Redirect(w, r, "/page/"+*page.Slug, http.StatusFound)
	} else {
		// Try to assign a slug now if user is logged in
		if session, err := s.GetUserFromRequest(r); err == nil {
			slug := s.assignSlug(r.Context(), q, page.ID, session.UserName, session.Email)
			http.Redirect(w, r, "/page/"+slug, http.StatusFound)
			return
		}
		http.Redirect(w, r, "/page/"+page.ID, http.StatusFound)
	}
}

func (s *Server) HandlePage(w http.ResponseWriter, r *http.Request) {
	idOrSlug := r.PathValue("id")
	userID := s.GetUserID(r)

	q := dbgen.New(s.DB)
	
	// Try to find by ID first, then by slug
	page, err := q.GetPageByID(r.Context(), idOrSlug)
	if err != nil {
		// Try by slug
		page, err = q.GetPageBySlug(r.Context(), &idOrSlug)
		if err != nil {
			http.Error(w, "Page not found", http.StatusNotFound)
			return
		}
	}

	// Check ownership or public access
	isOwner := page.UserID == userID
	isPublic := page.IsPublic != nil && *page.IsPublic == 1
	// Allow access via slug if slug_access is enabled and accessed by slug (not UUID)
	hasSlugAccess := page.SlugAccess != nil && *page.SlugAccess == 1 && page.Slug != nil && *page.Slug == idOrSlug
	
	if !isOwner && !isPublic && !hasSlugAccess {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Pass ownership info to template
	type pageData struct {
		dbgen.Page
		IsOwner bool
	}
	
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.renderTemplate(w, "dashboard.html", pageData{Page: page, IsOwner: isOwner}); err != nil {
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
	userID := s.GetUserID(r)

	q := dbgen.New(s.DB)

	// Verify page ownership or public access
	page, err := q.GetPageByID(r.Context(), pageID)
	if err != nil {
		s.writeJSON(w, http.StatusForbidden, APIResponse{Error: "forbidden"})
		return
	}
	
	isOwner := page.UserID == userID
	isPublic := page.IsPublic != nil && *page.IsPublic == 1
	hasSlugAccess := page.SlugAccess != nil && *page.SlugAccess == 1 && page.Slug != nil
	
	if !isOwner && !isPublic && !hasSlugAccess {
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
	userID := s.GetUserID(r)

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
		input.BgColor = s.Config.DefaultWidgetBg
	}
	if input.TextColor == "" {
		input.TextColor = s.Config.DefaultWidgetText
	}
	if input.HeaderColor == "" {
		input.HeaderColor = s.Config.DefaultWidgetHeader
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
	userID := s.GetUserID(r)

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
	userID := s.GetUserID(r)

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

func (s *Server) HandleAPIImportWidgets(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("pageId")
	userID := s.GetUserID(r)

	q := dbgen.New(s.DB)

	// Verify page ownership
	page, err := q.GetPageByID(r.Context(), pageID)
	if err != nil || page.UserID != userID {
		s.writeJSON(w, http.StatusForbidden, APIResponse{Error: "forbidden"})
		return
	}

	var input struct {
		Widgets []struct {
			Title       string          `json:"title"`
			WidgetType  string          `json:"widget_type"`
			Type        string          `json:"type"`
			PosX        int64           `json:"pos_x"`
			PosY        int64           `json:"pos_y"`
			X           int64           `json:"x"`
			Y           int64           `json:"y"`
			Width       int64           `json:"width"`
			Height      int64           `json:"height"`
			BgColor     string          `json:"bg_color"`
			TextColor   string          `json:"text_color"`
			HeaderColor string          `json:"header_color"`
			Config      json.RawMessage `json:"config"`
		} `json:"widgets"`
		PageSettings *json.RawMessage `json:"page_settings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid json"})
		return
	}

	if len(input.Widgets) == 0 {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "no widgets to import"})
		return
	}

	// Do everything in a single transaction
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}
	defer tx.Rollback()

	qtx := q.WithTx(tx)

	// Delete all existing widgets for this page
	if err := qtx.DeleteWidgetsByPageID(r.Context(), pageID); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	// Import page settings if present
	if input.PageSettings != nil {
		var ps struct {
			BgColor        string  `json:"bg_color"`
			BgImage        string  `json:"bg_image"`
			GridSize       int     `json:"grid_size"`
			ShowGrid       bool    `json:"show_grid"`
			HeaderSize     string  `json:"header_size"`
			ItemPadding    string  `json:"item_padding"`
			TextBrightness string  `json:"text_brightness"`
			AutoRefresh    int     `json:"auto_refresh"`
			ProxyURL       string  `json:"proxy_url"`
			ProxyUser      string  `json:"proxy_user"`
			ProxyPass      string  `json:"proxy_pass"`
		}
		if err := json.Unmarshal(*input.PageSettings, &ps); err == nil {
			configMap := map[string]interface{}{
				"grid_size":       ps.GridSize,
				"show_grid":       ps.ShowGrid,
				"header_size":     ps.HeaderSize,
				"item_padding":    ps.ItemPadding,
				"text_brightness": ps.TextBrightness,
				"auto_refresh":    ps.AutoRefresh,
				"proxy_url":       ps.ProxyURL,
				"proxy_user":      ps.ProxyUser,
				"proxy_pass":      ps.ProxyPass,
			}
			configJSON, _ := json.Marshal(configMap)
			bgColor := ps.BgColor
			bgImage := ps.BgImage
			_ = qtx.UpdatePage(r.Context(), dbgen.UpdatePageParams{
				Name:      page.Name,
				BgColor:   &bgColor,
				BgImage:   &bgImage,
				Config:    string(configJSON),
				UpdatedAt: time.Now(),
				ID:        pageID,
			})
		}
	}

	// Create all widgets
	now := time.Now()
	var created []dbgen.Widget
	for _, iw := range input.Widgets {
		widgetID := uuid.New().String()

		wtype := iw.WidgetType
		if wtype == "" {
			wtype = iw.Type
		}
		if wtype == "" {
			wtype = "rss"
		}

		posX := iw.PosX
		if posX == 0 {
			posX = iw.X
		}
		posY := iw.PosY
		if posY == 0 {
			posY = iw.Y
		}

		title := iw.Title
		if title == "" {
			title = "Imported Widget"
		}
		width := iw.Width
		if width == 0 {
			width = 300
		}
		height := iw.Height
		if height == 0 {
			height = 400
		}
		bgColor := iw.BgColor
		if bgColor == "" {
			bgColor = s.Config.DefaultWidgetBg
		}
		textColor := iw.TextColor
		if textColor == "" {
			textColor = s.Config.DefaultWidgetText
		}
		headerColor := iw.HeaderColor
		if headerColor == "" {
			headerColor = s.Config.DefaultWidgetHeader
		}

		// Normalize config to a JSON string
		configStr := "{}"
		if len(iw.Config) > 0 {
			// If it's already a JSON object/string, use as-is
			var obj interface{}
			if json.Unmarshal(iw.Config, &obj) == nil {
				// If it unmarshalled to a string, it was a double-encoded JSON string
				if str, ok := obj.(string); ok {
					configStr = str
				} else {
					configStr = string(iw.Config)
				}
			}
		}

		err := qtx.CreateWidget(r.Context(), dbgen.CreateWidgetParams{
			ID:          widgetID,
			PageID:      pageID,
			Title:       title,
			WidgetType:  wtype,
			PosX:        posX,
			PosY:        posY,
			Width:       width,
			Height:      height,
			BgColor:     &bgColor,
			TextColor:   &textColor,
			HeaderColor: &headerColor,
			Config:      configStr,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		if err != nil {
			slog.Error("failed to create widget during import", "error", err, "title", title)
			s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: fmt.Sprintf("failed to create widget %q: %v", title, err)})
			return
		}
		// Build response widget
		widget, _ := qtx.GetWidgetByID(r.Context(), widgetID)
		created = append(created, widget)
	}

	if err := tx.Commit(); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
		"imported": len(created),
		"widgets":  created,
	}})
}

func (s *Server) HandleAPIGetFeed(w http.ResponseWriter, r *http.Request) {
	feedURL := r.URL.Query().Get("url")
	if feedURL == "" {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "url required"})
		return
	}
	proxy := ProxyConfig{
		URL:      r.URL.Query().Get("proxy"),
		Username: r.URL.Query().Get("proxy_user"),
		Password: r.URL.Query().Get("proxy_pass"),
	}

	feed, err := s.GetFeedWithProxy(r.Context(), feedURL, proxy)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: feed})
}

func (s *Server) HandleAPIRefreshFeed(w http.ResponseWriter, r *http.Request) {
	feedURL := r.URL.Query().Get("url")
	if feedURL == "" {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "url required"})
		return
	}
	proxy := ProxyConfig{
		URL:      r.URL.Query().Get("proxy"),
		Username: r.URL.Query().Get("proxy_user"),
		Password: r.URL.Query().Get("proxy_pass"),
	}

	// Force refresh with optional proxy
	if proxy.URL != "" {
		s.fetchAndStoreFeedWithProxy(r.Context(), feedURL, proxy)
	} else {
		s.fetchAndStoreFeed(r.Context(), feedURL)
	}

	feed, err := s.GetFeedWithProxy(r.Context(), feedURL, proxy)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: feed})
}

// HandleAPISubmitFeed receives feed data fetched by the client and stores it
func (s *Server) HandleAPISubmitFeed(w http.ResponseWriter, r *http.Request) {
	var input struct {
		URL   string     `json:"url"`
		Title string     `json:"title"`
		Items []FeedItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid json"})
		return
	}

	if input.URL == "" {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "url required"})
		return
	}

	// Store the feed data
	err := s.StoreFeedFromClient(r.Context(), input.URL, input.Title, input.Items)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	// Return the stored feed
	feed, err := s.GetFeed(r.Context(), input.URL)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: feed})
}

func (s *Server) HandleAPIGetFavicon(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "url required"})
		return
	}

	favicon, err := s.GetFavicon(r.Context(), url)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: favicon})
}

func (s *Server) HandleAPIMarkVisited(w http.ResponseWriter, r *http.Request) {
	userID := s.GetUserID(r)

	var input struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid json"})
		return
	}

	q := dbgen.New(s.DB)
	err := q.MarkLinkVisited(r.Context(), dbgen.MarkLinkVisitedParams{
		UserID:    userID,
		LinkUrl:   input.URL,
		VisitedAt: time.Now(),
	})
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Success: true})
}

func (s *Server) HandleAPIGetVisitedLinks(w http.ResponseWriter, r *http.Request) {
	userID := s.GetUserID(r)

	q := dbgen.New(s.DB)
	
	// Clean up old links first
	cutoff := time.Now().Add(-time.Duration(s.Config.VisitedLinkMaxDays) * 24 * time.Hour)
	_ = q.CleanupOldVisitedLinks(r.Context(), cutoff)

	// Get visited links from last 30 days
	links, err := q.GetVisitedLinks(r.Context(), dbgen.GetVisitedLinksParams{
		UserID:    userID,
		VisitedAt: cutoff,
	})
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: links})
}

func (s *Server) HandleAPIUnmarkVisited(w http.ResponseWriter, r *http.Request) {
	userID := s.GetUserID(r)

	var input struct {
		URLs []string `json:"urls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "invalid json"})
		return
	}

	q := dbgen.New(s.DB)
	err := q.UnmarkLinksVisited(r.Context(), dbgen.UnmarkLinksVisitedParams{
		UserID: userID,
		Urls:   input.URLs,
	})
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Success: true})
}

func (s *Server) HandleAPIUpdatePage(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	userID := s.GetUserID(r)

	q := dbgen.New(s.DB)

	page, err := q.GetPageByID(r.Context(), pageID)
	if err != nil || page.UserID != userID {
		s.writeJSON(w, http.StatusForbidden, APIResponse{Error: "forbidden"})
		return
	}

	var input struct {
		Name       *string `json:"name"`
		BgColor    *string `json:"bg_color"`
		BgImage    *string `json:"bg_image"`
		Config     *string `json:"config"`
		Slug       *string `json:"slug"`
		IsPublic   *bool   `json:"is_public"`
		SlugAccess *bool   `json:"slug_access"`
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
	if input.Config != nil {
		page.Config = *input.Config
	}
	
	// Handle slug update with uniqueness check
	if input.Slug != nil {
		slugVal := *input.Slug
		if slugVal == "" {
			// Clear the slug
			page.Slug = nil
		} else {
			// Validate slug format (alphanumeric, hyphens, underscores only)
			if !isValidSlug(slugVal) {
				s.writeJSON(w, http.StatusBadRequest, APIResponse{Error: "Invalid slug format. Use only letters, numbers, hyphens, and underscores."})
				return
			}
			// Check uniqueness
			count, err := q.CheckSlugExists(r.Context(), dbgen.CheckSlugExistsParams{
				Slug: &slugVal,
				ID:   pageID,
			})
			if err != nil {
				s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
				return
			}
			if count > 0 {
				s.writeJSON(w, http.StatusConflict, APIResponse{Error: "This URL is already taken. Please choose a different one."})
				return
			}
			page.Slug = &slugVal
		}
		
		// Update slug separately
		err = q.UpdatePageSlug(r.Context(), dbgen.UpdatePageSlugParams{
			Slug:      page.Slug,
			UpdatedAt: time.Now(),
			ID:        pageID,
		})
		if err != nil {
			s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
			return
		}
	}
	
	// Handle is_public update
	if input.IsPublic != nil {
		var val int64 = 0
		if *input.IsPublic {
			val = 1
		}
		err = q.UpdatePagePublic(r.Context(), dbgen.UpdatePagePublicParams{
			IsPublic:  &val,
			UpdatedAt: time.Now(),
			ID:        pageID,
		})
		if err != nil {
			s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
			return
		}
	}

	// Handle slug_access update
	if input.SlugAccess != nil {
		var val int64 = 0
		if *input.SlugAccess {
			val = 1
		}
		err = q.UpdatePageSlugAccess(r.Context(), dbgen.UpdatePageSlugAccessParams{
			SlugAccess: &val,
			UpdatedAt:  time.Now(),
			ID:         pageID,
		})
		if err != nil {
			s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
			return
		}
	}

	err = q.UpdatePage(r.Context(), dbgen.UpdatePageParams{
		Name:      page.Name,
		BgColor:   page.BgColor,
		BgImage:   page.BgImage,
		Config:    page.Config,
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

func (s *Server) HandleAPICheckSlug(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	userID := s.GetUserID(r)
	slug := r.URL.Query().Get("slug")

	q := dbgen.New(s.DB)

	// Verify page ownership
	page, err := q.GetPageByID(r.Context(), pageID)
	if err != nil || page.UserID != userID {
		s.writeJSON(w, http.StatusForbidden, APIResponse{Error: "forbidden"})
		return
	}

	if slug == "" {
		s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]bool{"available": true}})
		return
	}

	if !isValidSlug(slug) {
		s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
			"available": false,
			"reason":    "Invalid format. Use only letters, numbers, hyphens, and underscores (max 64 chars).",
		}})
		return
	}

	count, err := q.CheckSlugExists(r.Context(), dbgen.CheckSlugExistsParams{
		Slug: &slug,
		ID:   pageID,
	})
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, APIResponse{Error: err.Error()})
		return
	}

	if count > 0 {
		s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]interface{}{
			"available": false,
			"reason":    "This URL is already taken.",
		}})
		return
	}

	s.writeJSON(w, http.StatusOK, APIResponse{Success: true, Data: map[string]bool{"available": true}})
}

func (s *Server) renderTemplate(w http.ResponseWriter, name string, data any) error {
	path := filepath.Join(s.TemplatesDir, name)
	funcMap := template.FuncMap{
		"derefInt64": func(p *int64) int64 {
			if p == nil {
				return 0
			}
			return *p
		},
	}
	tmpl, err := template.New(name).Funcs(funcMap).ParseFiles(path)
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

	// Auth routes
	mux.HandleFunc("GET /auth/login", s.HandleLogin)
	mux.HandleFunc("GET /auth/callback", s.HandleCallback)
	mux.HandleFunc("GET /auth/logout", s.HandleLogout)
	mux.HandleFunc("GET /api/auth/status", s.HandleAuthStatus)

	// Pages
	mux.HandleFunc("GET /{$}", s.HandleRoot)
	mux.HandleFunc("GET /page/{id}", s.HandlePage)

	// API
	mux.HandleFunc("GET /api/pages/{pageId}/widgets", s.HandleAPIGetWidgets)
	mux.HandleFunc("POST /api/pages/{pageId}/widgets", s.HandleAPICreateWidget)
	mux.HandleFunc("POST /api/pages/{pageId}/import", s.HandleAPIImportWidgets)
	mux.HandleFunc("PATCH /api/widgets/{id}", s.HandleAPIUpdateWidget)
	mux.HandleFunc("DELETE /api/widgets/{id}", s.HandleAPIDeleteWidget)
	mux.HandleFunc("PATCH /api/pages/{id}", s.HandleAPIUpdatePage)
	mux.HandleFunc("GET /api/pages/{id}/check-slug", s.HandleAPICheckSlug)
	mux.HandleFunc("GET /api/feed", s.HandleAPIGetFeed)
	mux.HandleFunc("POST /api/feed/refresh", s.HandleAPIRefreshFeed)
	mux.HandleFunc("POST /api/feed/submit", s.HandleAPISubmitFeed)
	mux.HandleFunc("GET /api/favicon", s.HandleAPIGetFavicon)
	mux.HandleFunc("GET /api/proxy", s.HandleAPIProxy)
	mux.HandleFunc("POST /api/visited", s.HandleAPIMarkVisited)
	mux.HandleFunc("DELETE /api/visited", s.HandleAPIUnmarkVisited)
	mux.HandleFunc("GET /api/visited", s.HandleAPIGetVisitedLinks)

	// Static files
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(s.StaticDir))))

	// Start feed refresher
	ctx := context.Background()
	s.StartFeedRefresher(ctx)

	slog.Info("starting server",
		"addr", addr,
		"db", s.Config.DBPath,
		"log_file", s.Config.LogFile,
		"log_level", s.Config.LogLevel,
		"oauth", s.Config.GoogleClientID != "",
		"canonical_domain", s.Config.CanonicalDomain,
		"feed_refresh_min", s.Config.FeedRefreshInterval,
		"feed_stale_min", s.Config.FeedStaleMinutes,
	)

	var handler http.Handler = mux

	// If a canonical domain is configured, redirect all other hosts to it.
	if s.Config.CanonicalDomain != "" {
		canonical := s.Config.CanonicalDomain
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
				host = fwdHost
			}
			// Strip port for comparison
			hostNoPort := host
			if idx := strings.LastIndex(hostNoPort, ":"); idx != -1 {
				hostNoPort = hostNoPort[:idx]
			}
			canonicalNoPort := canonical
			if idx := strings.LastIndex(canonicalNoPort, ":"); idx != -1 {
				canonicalNoPort = canonicalNoPort[:idx]
			}
			if hostNoPort != canonicalNoPort {
				scheme := "https"
				if fwdProto := r.Header.Get("X-Forwarded-Proto"); fwdProto != "" {
					scheme = fwdProto
				}
				target := scheme + "://" + canonical + r.RequestURI
				slog.Debug("redirecting to canonical domain", "from", host, "to", canonical)
				http.Redirect(w, r, target, http.StatusMovedPermanently)
				return
			}
			mux.ServeHTTP(w, r)
		})
	}

	return http.ListenAndServe(addr, handler)
}
