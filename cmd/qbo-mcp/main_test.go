package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadmeSection(t *testing.T) {
	got, ok := readmeSection(readmeText, "APPROVAL")
	if !ok || !strings.HasPrefix(got, "## The approval rule") {
		t.Fatalf("approval section not found: %q", got)
	}
	if strings.Contains(got, "## Matching names") {
		t.Fatal("section ran into the next heading")
	}
	if _, ok := readmeSection(readmeText, "no such heading"); ok {
		t.Fatal("found a section that does not exist")
	}
}

func TestReadmeNamesEveryTool(t *testing.T) {
	for _, tool := range []string{"readme", "search_customers", "search_items", "list_open_invoices", "draft_invoice", "create_invoice"} {
		if !strings.Contains(readmeText, "`"+tool+"`") {
			t.Errorf("readme.md does not mention %s", tool)
		}
	}
}

func TestRequireBearer(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := requireBearer("s3cret", ok)
	for _, tc := range []struct {
		header string
		want   int
	}{
		{"", http.StatusUnauthorized},
		{"Bearer wrong", http.StatusUnauthorized},
		{"Bearer s3cret", http.StatusNoContent},
	} {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if tc.header != "" {
			r.Header.Set("Authorization", tc.header)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("Authorization %q: got %d, want %d", tc.header, w.Code, tc.want)
		}
	}
	if requireBearer("", ok) == nil {
		t.Fatal("no token should pass the handler through")
	}
}

func TestIsLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"localhost:8090": true,
		"127.0.0.1:8090": true,
		"[::1]:8090":     true,
		"0.0.0.0:8090":   false,
		":8090":          false,
		"10.0.0.5:8090":  false,
	} {
		if got := isLoopback(addr); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}
