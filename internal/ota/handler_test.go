package ota

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseDescribesPublishedUpdate(t *testing.T) {
	handler, directory := testHandler(t)
	writePackage(t, directory, applications[1], "13", "signed-child-package")

	recorder := request(t, handler, http.MethodGet, "/releases/child.json")
	if recorder.Code != http.StatusOK {
		t.Fatalf("release status = %d, want 200", recorder.Code)
	}
	var got release
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.App != "child" || got.Build != "13" || got.Title != "Cooper Watch" {
		t.Fatalf("release = %+v", got)
	}
	if !strings.HasPrefix(got.InstallURL, "itms-services://?action=download-manifest") {
		t.Fatalf("install URL = %q", got.InstallURL)
	}
	if got.InstallerURL != "https://coop.example/install/" {
		t.Fatalf("installer URL = %q", got.InstallerURL)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestNewRequiresHTTPSAndExistingDirectory(t *testing.T) {
	if _, err := New("http://coop.example", t.TempDir()); err == nil {
		t.Fatal("New() accepted an insecure public URL")
	}
	if _, err := New("https://coop.example?redirect=elsewhere", t.TempDir()); err == nil {
		t.Fatal("New() accepted a public URL with a query")
	}
	if _, err := New("https://coop.example", filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("New() accepted a missing package directory")
	}
}

func TestIndexReflectsAvailablePackages(t *testing.T) {
	handler, directory := testHandler(t)
	writePackage(t, directory, applications[1], "10", "signed-child-package")

	recorder := request(t, handler, http.MethodGet, "/")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "itms-services://?action=download-manifest") {
		t.Fatal("available child package has no install link")
	}
	if strings.Count(body, "Package unavailable") != 1 {
		t.Fatalf("unavailable package count = %d, want 1", strings.Count(body, "Package unavailable"))
	}
	if got := recorder.Header().Get("Content-Security-Policy"); !strings.Contains(got, "style-src 'self'") {
		t.Fatalf("Content-Security-Policy = %q, want self-hosted styles", got)
	}
}

func TestManifestUsesPublicURLAndPackageVersion(t *testing.T) {
	handler, directory := testHandler(t)
	writePackage(t, directory, applications[1], "10", "signed-child-package")

	recorder := request(t, handler, http.MethodGet, "/manifests/child.plist")
	if recorder.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, want 200", recorder.Code)
	}
	for _, want := range []string{
		"https://coop.example/install/apps/CooperWatch.ipa",
		"fish.nerdswhofish.coop.child",
		"<string>10</string>",
	} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Errorf("manifest missing %q", want)
		}
	}
}

func TestIPAServingIsAllowlisted(t *testing.T) {
	handler, directory := testHandler(t)
	writePackage(t, directory, applications[1], "10", "signed-child-package")
	if err := os.WriteFile(filepath.Join(directory, "secret.txt"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}

	recorder := request(t, handler, http.MethodGet, "/apps/CooperWatch.ipa")
	if recorder.Code != http.StatusOK || recorder.Body.String() != "signed-child-package" {
		t.Fatalf("IPA response = %d %q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", got)
	}

	for _, path := range []string{"/apps/secret.txt", "/apps/../secret.txt", "/manifests/missing.plist"} {
		if got := request(t, handler, http.MethodGet, path).Code; got != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, got)
		}
	}
}

func TestInvalidOrMissingVersionDisablesPackage(t *testing.T) {
	handler, directory := testHandler(t)
	app := applications[1]
	if err := os.WriteFile(filepath.Join(directory, app.IPAName), []byte("package"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := request(t, handler, http.MethodGet, "/manifests/child.plist").Code; got != http.StatusNotFound {
		t.Fatalf("manifest without version status = %d, want 404", got)
	}
	if err := os.WriteFile(filepath.Join(directory, app.IPAName+".version"), []byte("<script>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := request(t, handler, http.MethodGet, "/apps/CooperWatch.ipa").Code; got != http.StatusNotFound {
		t.Fatalf("IPA with invalid version status = %d, want 404", got)
	}
}

func TestRejectsWrites(t *testing.T) {
	handler, _ := testHandler(t)
	recorder := request(t, handler, http.MethodPost, "/")
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST / status = %d, want 405", recorder.Code)
	}
	if got := recorder.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow = %q, want GET, HEAD", got)
	}
}

func testHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	directory := t.TempDir()
	handler, err := New("https://coop.example", directory)
	if err != nil {
		t.Fatal(err)
	}
	return handler, directory
}

func writePackage(t *testing.T, directory string, app application, version, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, app.IPAName), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, app.IPAName+".version"), []byte(version+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func request(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	handler.ServeHTTP(recorder, request)
	return recorder
}
