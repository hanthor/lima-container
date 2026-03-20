package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var allowedImageExts = map[string]bool{
	".qcow2": true,
	".img":   true,
	".raw":   true,
	".iso":   true,
}

var allowedTemplateExts = map[string]bool{
	".yaml": true,
	".yml":  true,
}

func isAllowedUploadExt(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return allowedImageExts[ext] || allowedTemplateExts[ext]
}

func isTemplateExt(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return allowedTemplateExts[ext]
}

// UploadManager handles disk image file storage for VM creation.
type UploadManager struct {
	uploadDir string
}

// NewUploadManager creates an UploadManager and ensures the upload directory exists.
func NewUploadManager(dir string) *UploadManager {
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("WARNING: could not create upload dir %s: %v", dir, err)
	}
	return &UploadManager{uploadDir: dir}
}

// SaveFile persists data from src into the upload directory with a timestamped
// filename prefix and returns the full path of the saved file.
func (u *UploadManager) SaveFile(filename string, src io.Reader) (string, error) {
	safeName := filepath.Base(filename)
	dest := filepath.Join(u.uploadDir, fmt.Sprintf("%d-%s", time.Now().UnixMilli(), safeName))
	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, src); err != nil {
		os.Remove(dest)
		return "", fmt.Errorf("writing file: %w", err)
	}
	return dest, nil
}

// instanceNameFromFile derives a VM instance name from a filename using the
// same convention as lima-up.sh: basename without extension.
func instanceNameFromFile(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// runLimaUp shells out to lima-up with the given image path and optional name.
// lima-up handles auto-template generation and VM startup via limactl.
func (h *Handler) runLimaUp(imagePath, instanceName string) error {
	args := []string{imagePath, "--tty=false"}
	if instanceName != "" {
		args = append(args, "--name="+instanceName)
	}

	cmd := exec.Command("/usr/local/bin/lima-up", args...)
	cmd.Env = append(os.Environ(), "LIMA_HOME="+h.lima.Home())

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("lima-up failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// HandleUploadImage handles POST /api/images/upload — multipart file upload.
// Accepts a disk image (.qcow2/.img/.raw) and an optional VM name, then
// creates and starts a VM using lima-up.
func (h *Handler) HandleUploadImage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(20 << 30); err != nil {
		writeError(w, http.StatusBadRequest, "failed to parse multipart form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'file' field")
		return
	}
	defer file.Close()

	if !isAllowedUploadExt(header.Filename) {
		writeError(w, http.StatusBadRequest,
			"unsupported file type: must be .qcow2, .img, .raw, .yaml, or .yml")
		return
	}

	name := r.FormValue("name")
	instanceName := name
	if instanceName == "" {
		instanceName = instanceNameFromFile(header.Filename)
	}

	savedPath, err := h.upload.SaveFile(header.Filename, file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save file: "+err.Error())
		return
	}

	// For YAML templates, copy to Lima templates dir so lima-up can find it.
	if isTemplateExt(header.Filename) {
		templateDir := "/usr/local/share/lima/templates"
		os.MkdirAll(templateDir, 0755)
		destPath := filepath.Join(templateDir, filepath.Base(header.Filename))
		if err := copyFile(savedPath, destPath); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to install template: "+err.Error())
			return
		}
		savedPath = destPath
	}

	if err := h.runLimaUp(savedPath, instanceName); err != nil {
		htmxToast(w, "Create failed: "+err.Error(), "error")
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := h.vnc.StartVNC(instanceName); err != nil {
		htmxToast(w, "Created "+instanceName+" (VNC warning: "+err.Error()+")", "success")
		writeData(w, map[string]any{
			"status":      "created",
			"instance":    instanceName,
			"vnc_warning": err.Error(),
		})
		return
	}
	htmxToast(w, "Created "+instanceName, "success")
	writeData(w, map[string]any{
		"status":   "created",
		"instance": instanceName,
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// HandleFetchImage handles POST /api/images/fetch — create a VM from a URL.
// Downloads the disk image asynchronously and returns 202 Accepted immediately.
// The VM appears in the instance list once download and creation complete.
func (h *Handler) HandleFetchImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL  string `json:"url"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	parsed, err := url.Parse(req.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		writeError(w, http.StatusBadRequest, "url must be a valid http or https URL")
		return
	}

	filename := filepath.Base(parsed.Path)
	if !isAllowedUploadExt(filename) {
		writeError(w, http.StatusBadRequest,
			"unsupported file type: URL must point to a .qcow2, .img, .raw, .yaml, or .yml file")
		return
	}

	instanceName := req.Name
	if instanceName == "" {
		instanceName = instanceNameFromFile(filename)
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":   "downloading",
		"instance": instanceName,
	})

	go h.fetchAndCreate(req.URL, filename, instanceName)
}

// fetchAndCreate downloads a disk image from a URL and creates a VM from it.
func (h *Handler) fetchAndCreate(imageURL, filename, instanceName string) {
	log.Printf("fetchAndCreate: downloading %s", imageURL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		log.Printf("fetchAndCreate: request creation failed: %v", err)
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("fetchAndCreate: download failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("fetchAndCreate: download returned HTTP %d", resp.StatusCode)
		return
	}

	savedPath, err := h.upload.SaveFile(filename, resp.Body)
	if err != nil {
		log.Printf("fetchAndCreate: save failed: %v", err)
		return
	}

	if err := h.runLimaUp(savedPath, instanceName); err != nil {
		log.Printf("fetchAndCreate: %v", err)
		return
	}

	if err := h.vnc.StartVNC(instanceName); err != nil {
		log.Printf("fetchAndCreate: VNC bridge for %q failed: %v", instanceName, err)
	}

	log.Printf("fetchAndCreate: VM %q created from %s", instanceName, imageURL)
}
