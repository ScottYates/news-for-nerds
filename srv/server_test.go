package srv

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func testConfig(dbPath string) *Config {
	cfg := DefaultConfig()
	cfg.DBPath = dbPath
	return cfg
}

func TestServerStartup(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test.sqlite3")
	t.Cleanup(func() { os.Remove(tempDB) })

	server, err := New(testConfig(tempDB), "test-host")
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	// Close the DB before the test framework tries to remove the temp file.
	// On Windows an open SQLite file can't be unlinked, so without this the
	// t.Cleanup(os.Remove) above and t.TempDir's RemoveAll both fail.
	// Registered after the os.Remove cleanup so t.Cleanup's LIFO order runs
	// DB.Close first.
	t.Cleanup(func() { server.DB.Close() })

	if server.DB == nil {
		t.Fatal("expected DB to be initialized")
	}
}

func TestRootRedirects(t *testing.T) {
	tempDB := filepath.Join(t.TempDir(), "test.sqlite3")
	t.Cleanup(func() { os.Remove(tempDB) })

	server, err := New(testConfig(tempDB), "test-host")
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	t.Cleanup(func() { server.DB.Close() })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	server.HandleRoot(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected redirect (302), got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc == "" {
		t.Error("expected Location header on redirect")
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		expected string
	}{
		{"Scott Yates", "scott@example.com", "scott-yates"},
		{"", "beernutz@gmail.com", "beernutz"},
		{"John", "", "john"},
		{"", "", "page"},
		{"  Spaces  Everywhere  ", "", "spaces-everywhere"},
		{"UPPER case MiXeD", "", "upper-case-mixed"},
		{"special!@#chars$%", "", "special"},
	}

	for _, tt := range tests {
		result := slugify(tt.name, tt.email)
		if result != tt.expected {
			t.Errorf("slugify(%q, %q) = %q, want %q", tt.name, tt.email, result, tt.expected)
		}
	}
}

func TestIsValidSlug(t *testing.T) {
	valid := []string{"hello", "my-page", "test_123", "a"}
	for _, s := range valid {
		if !isValidSlug(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}

	invalid := []string{"", "has spaces", "has/slash", "has@at"}
	for _, s := range invalid {
		if isValidSlug(s) {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}
