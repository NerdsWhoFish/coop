package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientAppReadsReportedBuild(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/parent/me", nil)
	request.Header.Set(clientBuildHeader, "10800")
	request.Header.Set(clientVersionHeader, "1.8.0")

	client := clientApp(request)
	if client.Build != "10800" || client.Version != "1.8.0" {
		t.Errorf("clientApp() = %+v, want 10800/1.8.0", client)
	}
	if !client.Reported() {
		t.Error("Reported() = false for a client that named a build")
	}
}

// A client too old to report is the state a migration needs to see, so it must
// be an absent build rather than an error or a guess.
func TestClientAppTreatsSilenceAsUnknown(t *testing.T) {
	client := clientApp(httptest.NewRequest(http.MethodGet, "/api/v1/parent/me", nil))
	if client.Reported() {
		t.Errorf("clientApp() = %+v, want an unreported client", client)
	}
}

func TestClientAppRejectsUnusableValues(t *testing.T) {
	tests := []struct {
		name  string
		build string
	}{
		{name: "empty", build: ""},
		{name: "whitespace", build: "   "},
		{name: "too long", build: strings.Repeat("9", 33)},
		{name: "path traversal", build: "../../etc/passwd"},
		{name: "html", build: "<script>alert(1)</script>"},
		{name: "newline", build: "14\nX-Injected: yes"},
		{name: "leading punctuation", build: ".14"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/parent/me", nil)
			request.Header.Set(clientBuildHeader, tt.build)

			if client := clientApp(request); client.Reported() {
				t.Errorf("clientApp() accepted %q as build %q", tt.build, client.Build)
			}
		})
	}
}

// A usable build with a junk version keeps the build, because the build is what
// the migration is tracked by and the version is only shown to a person.
func TestClientAppDropsOnlyTheUnusableVersion(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/parent/me", nil)
	request.Header.Set(clientBuildHeader, "10800")
	request.Header.Set(clientVersionHeader, "<b>1.8.0</b>")

	client := clientApp(request)
	if client.Build != "10800" {
		t.Errorf("Build = %q, want 10800", client.Build)
	}
	if client.Version != "" {
		t.Errorf("Version = %q, want it dropped", client.Version)
	}
}
