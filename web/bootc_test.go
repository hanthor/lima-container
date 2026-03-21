package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 1. Containerfile generation
// ---------------------------------------------------------------------------

func TestContainerfile_NoCustomizations(t *testing.T) {
	cf := generateContainerfile("quay.io/fedora/fedora-bootc:42", &Customizations{})
	if !strings.HasPrefix(cf, "FROM quay.io/fedora/fedora-bootc:42\n") {
		t.Fatalf("expected FROM line first, got:\n%s", cf)
	}
	if strings.Contains(cf, "RUN") {
		t.Fatalf("expected no RUN lines for empty customizations, got:\n%s", cf)
	}
}

func TestContainerfile_SSHOnly(t *testing.T) {
	cf := generateContainerfile("img:latest", &Customizations{EnableSSH: true})
	if !strings.HasPrefix(cf, "FROM img:latest\n") {
		t.Fatal("missing FROM line")
	}
	if !strings.Contains(cf, "openssh-server") {
		t.Fatal("expected openssh-server install")
	}
	if !strings.Contains(cf, "systemctl enable sshd") {
		t.Fatal("expected systemctl enable sshd")
	}
	if strings.Contains(cf, "xrdp") {
		t.Fatal("xrdp should not appear when only SSH is enabled")
	}
}

func TestContainerfile_RDPOnly(t *testing.T) {
	cf := generateContainerfile("img:latest", &Customizations{EnableRDP: true})
	if !strings.Contains(cf, "gnome-remote-desktop") {
		t.Fatal("expected gnome-remote-desktop install")
	}
	if !strings.Contains(cf, "setup-rdp.sh") {
		t.Fatal("expected setup-rdp.sh script")
	}
	if !strings.Contains(cf, "gnome-rdp-setup.service") {
		t.Fatal("expected gnome-rdp-setup.service")
	}
	if !strings.Contains(cf, "gnome-remote-desktop.service") {
		t.Fatal("expected gnome-remote-desktop.service enabled")
	}
	if strings.Contains(cf, "openssh-server") {
		t.Fatal("openssh-server should not appear when only RDP is enabled")
	}
}

func TestContainerfile_SSHAndRDP(t *testing.T) {
	cf := generateContainerfile("img:latest", &Customizations{EnableSSH: true, EnableRDP: true})
	if !strings.Contains(cf, "openssh-server") {
		t.Fatal("expected openssh-server")
	}
	if !strings.Contains(cf, "gnome-remote-desktop") {
		t.Fatal("expected gnome-remote-desktop")
	}
	if !strings.Contains(cf, "systemctl enable sshd gnome-rdp-setup.service gnome-remote-desktop.service") {
		t.Fatal("expected all units enabled together")
	}
}

func TestContainerfile_ExtraPackages(t *testing.T) {
	cf := generateContainerfile("img:latest", &Customizations{
		ExtraPackages: []string{"vim", "htop", "tmux"},
	})
	if !strings.Contains(cf, "dnf install -y vim htop tmux") {
		t.Fatalf("expected dnf install line with packages, got:\n%s", cf)
	}
}

func TestContainerfile_EmptyExtraPackages(t *testing.T) {
	cf := generateContainerfile("img:latest", &Customizations{
		ExtraPackages: []string{},
	})
	if strings.Contains(cf, "dnf install") {
		t.Fatalf("should not produce dnf install for empty package list, got:\n%s", cf)
	}
}

func TestContainerfile_ExtraContainerfile(t *testing.T) {
	extra := "RUN echo hello\nCOPY foo /bar"
	cf := generateContainerfile("img:latest", &Customizations{
		ExtraContainerfile: extra,
	})
	if !strings.Contains(cf, "RUN echo hello") {
		t.Fatal("expected extra containerfile lines verbatim")
	}
	if !strings.Contains(cf, "COPY foo /bar") {
		t.Fatal("expected COPY line from extra containerfile")
	}
}

func TestContainerfile_AllCustomizations(t *testing.T) {
	cf := generateContainerfile("base:v1", &Customizations{
		EnableSSH:          true,
		EnableRDP:          true,
		ExtraPackages:      []string{"curl", "jq"},
		ExtraContainerfile: "RUN touch /marker",
	})
	if !strings.HasPrefix(cf, "FROM base:v1\n") {
		t.Fatal("missing FROM line")
	}
	if !strings.Contains(cf, "openssh-server") {
		t.Fatal("missing openssh-server")
	}
	if !strings.Contains(cf, "gnome-remote-desktop") {
		t.Fatal("missing gnome-remote-desktop")
	}
	if !strings.Contains(cf, "dnf install -y curl jq") {
		t.Fatal("missing extra packages")
	}
	if !strings.Contains(cf, "systemctl enable sshd gnome-rdp-setup.service gnome-remote-desktop.service") {
		t.Fatal("missing systemctl enable")
	}
	if !strings.Contains(cf, "RUN touch /marker") {
		t.Fatal("missing extra containerfile")
	}

	// Extra containerfile should be last
	markerIdx := strings.Index(cf, "RUN touch /marker")
	enableIdx := strings.Index(cf, "systemctl enable")
	if markerIdx < enableIdx {
		t.Fatal("extra containerfile should appear after systemctl enable")
	}
}

// ---------------------------------------------------------------------------
// 2. Build state management
// ---------------------------------------------------------------------------

func newTestManager(t *testing.T) *BootcManager {
	t.Helper()
	return NewBootcManager(nil, t.TempDir())
}

func TestBootcManager_StartBuild(t *testing.T) {
	mgr := newTestManager(t)
	build, err := mgr.StartBuild("img:latest", "", nil, "", 0, "")
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}
	if build.ID == "" {
		t.Fatal("build ID should not be empty")
	}
	if build.Status != BuildPending {
		t.Fatalf("expected status pending, got %s", build.Status)
	}
	if build.SourceImage != "img:latest" {
		t.Fatalf("expected source_image img:latest, got %s", build.SourceImage)
	}
}

func TestBootcManager_StartBuild_DefaultVMName(t *testing.T) {
	mgr := newTestManager(t)
	build, err := mgr.StartBuild("img:latest", "", nil, "", 0, "")
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}
	if build.VMName != build.ID {
		t.Fatalf("expected VMName to default to build ID %q, got %q", build.ID, build.VMName)
	}
}

func TestBootcManager_StartBuild_CustomVMName(t *testing.T) {
	mgr := newTestManager(t)
	build, err := mgr.StartBuild("img:latest", "my-vm", nil, "", 0, "")
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}
	if build.VMName != "my-vm" {
		t.Fatalf("expected VMName my-vm, got %s", build.VMName)
	}
}

func TestBootcManager_UniqueIDs(t *testing.T) {
	mgr := newTestManager(t)
	b1, err := mgr.StartBuild("img:v1", "", nil, "", 0, "")
	if err != nil {
		t.Fatalf("StartBuild 1: %v", err)
	}
	// Ensure unique IDs even when called in rapid succession.
	time.Sleep(2 * time.Millisecond)
	b2, err := mgr.StartBuild("img:v2", "", nil, "", 0, "")
	if err != nil {
		t.Fatalf("StartBuild 2: %v", err)
	}
	if b1.ID == b2.ID {
		t.Fatalf("build IDs should be unique, both are %q", b1.ID)
	}
}

func TestBootcManager_ListBuilds(t *testing.T) {
	mgr := newTestManager(t)
	if builds := mgr.ListBuilds(); len(builds) != 0 {
		t.Fatalf("expected 0 builds, got %d", len(builds))
	}

	mgr.StartBuild("img:v1", "", nil, "", 0, "")
	time.Sleep(2 * time.Millisecond)
	mgr.StartBuild("img:v2", "", nil, "", 0, "")

	builds := mgr.ListBuilds()
	if len(builds) != 2 {
		t.Fatalf("expected 2 builds, got %d", len(builds))
	}
}

func TestBootcManager_GetBuild(t *testing.T) {
	mgr := newTestManager(t)
	b, _ := mgr.StartBuild("img:latest", "", nil, "", 0, "")

	got, ok := mgr.GetBuild(b.ID)
	if !ok {
		t.Fatal("expected to find build")
	}
	if got.ID != b.ID {
		t.Fatalf("expected ID %q, got %q", b.ID, got.ID)
	}
}

func TestBootcManager_GetBuild_NotFound(t *testing.T) {
	mgr := newTestManager(t)
	_, ok := mgr.GetBuild("nonexistent")
	if ok {
		t.Fatal("expected not found for nonexistent ID")
	}
}

func TestBootcManager_StartBuild_WithCustomizations(t *testing.T) {
	mgr := newTestManager(t)
	cust := &Customizations{EnableSSH: true, ExtraPackages: []string{"vim"}}
	build, err := mgr.StartBuild("img:latest", "", cust, "", 0, "")
	if err != nil {
		t.Fatalf("StartBuild: %v", err)
	}
	if build.Customizations == nil {
		t.Fatal("expected customizations to be stored")
	}
	if !build.Customizations.EnableSSH {
		t.Fatal("expected EnableSSH to be true")
	}
}

// ---------------------------------------------------------------------------
// 3. HTTP API handler tests
// ---------------------------------------------------------------------------

func newTestHandler(t *testing.T, enableBootc bool) *Handler {
	t.Helper()
	var mgr *BootcManager
	if enableBootc {
		mgr = newTestManager(t)
	}
	return &Handler{bootc: mgr}
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var body map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}

// --- POST /api/bootc/builds ---

func TestCreateBootcBuild_Disabled(t *testing.T) {
	h := newTestHandler(t, false)
	body := `{"image":"img:latest"}`
	req := httptest.NewRequest("POST", "/api/bootc/builds", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateBootcBuild(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	resp := decodeBody(t, rec)
	if !strings.Contains(string(resp["error"]), "bootc not available") {
		t.Fatalf("expected bootc not available error, got %s", resp["error"])
	}
}

func TestCreateBootcBuild_MissingImage(t *testing.T) {
	h := newTestHandler(t, true)
	body := `{"image":""}`
	req := httptest.NewRequest("POST", "/api/bootc/builds", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateBootcBuild(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	resp := decodeBody(t, rec)
	if !strings.Contains(string(resp["error"]), "image is required") {
		t.Fatalf("expected 'image is required' error, got %s", resp["error"])
	}
}

func TestCreateBootcBuild_EmptyBody(t *testing.T) {
	h := newTestHandler(t, true)
	req := httptest.NewRequest("POST", "/api/bootc/builds", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	h.CreateBootcBuild(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateBootcBuild_InvalidJSON(t *testing.T) {
	h := newTestHandler(t, true)
	req := httptest.NewRequest("POST", "/api/bootc/builds", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	h.CreateBootcBuild(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateBootcBuild_Valid(t *testing.T) {
	h := newTestHandler(t, true)
	body := `{"image":"quay.io/fedora/fedora-bootc:42"}`
	req := httptest.NewRequest("POST", "/api/bootc/builds", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateBootcBuild(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data BootcBuild `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.ID == "" {
		t.Fatal("expected build ID")
	}
	if resp.Data.Status != BuildPending {
		t.Fatalf("expected pending status, got %s", resp.Data.Status)
	}
	if resp.Data.SourceImage != "quay.io/fedora/fedora-bootc:42" {
		t.Fatalf("unexpected source_image: %s", resp.Data.SourceImage)
	}
}

func TestCreateBootcBuild_WithCustomizations(t *testing.T) {
	h := newTestHandler(t, true)
	body := `{"image":"img:latest","customizations":{"enable_ssh":true,"extra_packages":["vim"]}}`
	req := httptest.NewRequest("POST", "/api/bootc/builds", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateBootcBuild(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data BootcBuild `json:"data"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Data.Customizations == nil {
		t.Fatal("expected customizations in response")
	}
	if !resp.Data.Customizations.EnableSSH {
		t.Fatal("expected EnableSSH true")
	}
}

func TestCreateBootcBuild_WithVMName(t *testing.T) {
	h := newTestHandler(t, true)
	body := `{"image":"img:latest","vm_name":"my-bootc-vm"}`
	req := httptest.NewRequest("POST", "/api/bootc/builds", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateBootcBuild(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}

	var resp struct {
		Data BootcBuild `json:"data"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Data.VMName != "my-bootc-vm" {
		t.Fatalf("expected vm_name my-bootc-vm, got %s", resp.Data.VMName)
	}
}

// --- GET /api/bootc/builds ---

func TestListBootcBuilds_Disabled(t *testing.T) {
	h := newTestHandler(t, false)
	req := httptest.NewRequest("GET", "/api/bootc/builds", nil)
	rec := httptest.NewRecorder()
	h.ListBootcBuilds(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestListBootcBuilds_Empty(t *testing.T) {
	h := newTestHandler(t, true)
	req := httptest.NewRequest("GET", "/api/bootc/builds", nil)
	rec := httptest.NewRecorder()
	h.ListBootcBuilds(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Data []BootcBuild `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Fatalf("expected empty list, got %d items", len(resp.Data))
	}
}

func TestListBootcBuilds_AfterCreating(t *testing.T) {
	h := newTestHandler(t, true)

	// Create two builds via the manager directly.
	h.bootc.StartBuild("img:v1", "", nil, "", 0, "")
	time.Sleep(2 * time.Millisecond)
	h.bootc.StartBuild("img:v2", "", nil, "", 0, "")

	req := httptest.NewRequest("GET", "/api/bootc/builds", nil)
	rec := httptest.NewRecorder()
	h.ListBootcBuilds(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Data []BootcBuild `json:"data"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 builds, got %d", len(resp.Data))
	}
}

// --- GET /api/bootc/builds/{id} ---

func TestGetBootcBuild_Disabled(t *testing.T) {
	h := newTestHandler(t, false)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/bootc/builds/{id}", h.GetBootcBuild)

	req := httptest.NewRequest("GET", "/api/bootc/builds/build-123", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetBootcBuild_NotFound(t *testing.T) {
	h := newTestHandler(t, true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/bootc/builds/{id}", h.GetBootcBuild)

	req := httptest.NewRequest("GET", "/api/bootc/builds/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	resp := decodeBody(t, rec)
	if !strings.Contains(string(resp["error"]), "build not found") {
		t.Fatalf("expected 'build not found' error, got %s", resp["error"])
	}
}

func TestGetBootcBuild_Exists(t *testing.T) {
	h := newTestHandler(t, true)
	build, _ := h.bootc.StartBuild("img:latest", "test-vm", nil, "", 0, "")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/bootc/builds/{id}", h.GetBootcBuild)

	req := httptest.NewRequest("GET", "/api/bootc/builds/"+build.ID, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data BootcBuild `json:"data"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Data.ID != build.ID {
		t.Fatalf("expected ID %q, got %q", build.ID, resp.Data.ID)
	}
	if resp.Data.VMName != "test-vm" {
		t.Fatalf("expected vm_name test-vm, got %s", resp.Data.VMName)
	}
}

// --- GET /api/bootc/builds/{id}/log ---

func TestStreamBootcBuildLog_Disabled(t *testing.T) {
	h := newTestHandler(t, false)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/bootc/builds/{id}/log", h.StreamBootcBuildLog)

	req := httptest.NewRequest("GET", "/api/bootc/builds/build-123/log", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestStreamBootcBuildLog_MissingBuild(t *testing.T) {
	h := newTestHandler(t, true)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/bootc/builds/{id}/log", h.StreamBootcBuildLog)

	req := httptest.NewRequest("GET", "/api/bootc/builds/nonexistent/log", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// --- Customizations.hasWork ---

func TestCustomizations_HasWork(t *testing.T) {
	tests := []struct {
		name string
		c    *Customizations
		want bool
	}{
		{"nil", nil, false},
		{"empty", &Customizations{}, false},
		{"ssh", &Customizations{EnableSSH: true}, true},
		{"rdp", &Customizations{EnableRDP: true}, true},
		{"packages", &Customizations{ExtraPackages: []string{"vim"}}, true},
		{"extra cf", &Customizations{ExtraContainerfile: "RUN echo hi"}, true},
		{"whitespace-only extra cf", &Customizations{ExtraContainerfile: "   "}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.hasWork(); got != tt.want {
				t.Fatalf("hasWork() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- JSON response format ---

func TestCreateBootcBuild_ResponseFormat(t *testing.T) {
	h := newTestHandler(t, true)
	body := `{"image":"img:latest"}`
	req := httptest.NewRequest("POST", "/api/bootc/builds", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateBootcBuild(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected application/json, got %s", ct)
	}

	// Verify top-level "data" key exists
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["data"]; !ok {
		t.Fatal("expected 'data' key in response")
	}
}

func TestListBootcBuilds_ResponseFormat(t *testing.T) {
	h := newTestHandler(t, true)
	req := httptest.NewRequest("GET", "/api/bootc/builds", nil)
	rec := httptest.NewRecorder()
	h.ListBootcBuilds(rec, req)

	var raw map[string]json.RawMessage
	json.Unmarshal(rec.Body.Bytes(), &raw)
	if _, ok := raw["data"]; !ok {
		t.Fatal("expected 'data' key in response")
	}
}

func TestErrorResponse_Format(t *testing.T) {
	h := newTestHandler(t, false)
	req := httptest.NewRequest("GET", "/api/bootc/builds", nil)
	rec := httptest.NewRecorder()
	h.ListBootcBuilds(rec, req)

	var raw map[string]json.RawMessage
	json.Unmarshal(rec.Body.Bytes(), &raw)
	if _, ok := raw["error"]; !ok {
		t.Fatal("expected 'error' key in error response")
	}
}

// --- Edge case: request with no body at all ---

func TestCreateBootcBuild_NoBody(t *testing.T) {
	h := newTestHandler(t, true)
	req := httptest.NewRequest("POST", "/api/bootc/builds", &bytes.Buffer{})
	rec := httptest.NewRecorder()
	h.CreateBootcBuild(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
