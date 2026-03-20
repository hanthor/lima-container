package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	lima  *LimaCtl
	vnc   *VNCManager
	rdp   *RDPManager
	bootc *BootcManager
	tmpl  *Templates
}

func NewHandler(lima *LimaCtl, vnc *VNCManager, rdp *RDPManager, bootc *BootcManager, tmpl *Templates) *Handler {
	return &Handler{lima: lima, vnc: vnc, rdp: rdp, bootc: bootc, tmpl: tmpl}
}

// --- response helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("ERROR: writeJSON encode failed: %v", err)
	}
}

func writeData(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// --- htmx helpers ---

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func htmxToast(w http.ResponseWriter, message, level string) {
	msg := strings.ReplaceAll(message, `\`, `\\`)
	msg = strings.ReplaceAll(msg, `"`, `\"`)
	msg = strings.ReplaceAll(msg, "\n", `\n`)
	w.Header().Set("HX-Trigger", fmt.Sprintf(`{"showToast":{"message":"%s","level":"%s"}}`, msg, level))
}

// --- template handlers ---

// RenderDashboard serves the full page via Go template.
func (h *Handler) RenderDashboard(w http.ResponseWriter, r *http.Request) {
	h.tmpl.RenderHTML(w, "base", map[string]any{
		"BootcEnabled": h.bootc != nil,
	})
}

// PartialInstances returns just the instance card grid HTML.
func (h *Handler) PartialInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := h.lima.List()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	h.tmpl.RenderHTML(w, "instances", instances)
}

// PartialBuilds returns the bootc builds list HTML.
func (h *Handler) PartialBuilds(w http.ResponseWriter, r *http.Request) {
	if h.bootc == nil {
		w.WriteHeader(204)
		return
	}
	builds := h.bootc.ListBuilds()
	for i, j := 0, len(builds)-1; i < j; i, j = i+1, j-1 {
		builds[i], builds[j] = builds[j], builds[i]
	}
	h.tmpl.RenderHTML(w, "builds", builds)
}

// PartialTemplateOptions returns <option> elements for the template select.
func (h *Handler) PartialTemplateOptions(w http.ResponseWriter, r *http.Request) {
	templateDir := "/opt/lima/templates"
	entries, err := os.ReadDir(templateDir)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	type tmplInfo struct {
		Name string
	}
	var templates []tmplInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		templates = append(templates, tmplInfo{
			Name: strings.TrimSuffix(e.Name(), ".yaml"),
		})
	}
	h.tmpl.RenderHTML(w, "template-options", templates)
}

// --- JSON API handlers ---

func (h *Handler) ListInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := h.lima.List()
	if err != nil {
		log.Printf("ListInstances error: %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if instances == nil {
		instances = []Instance{}
	}
	log.Printf("ListInstances: found %d instances", len(instances))
	writeData(w, instances)
}

func (h *Handler) GetInstance(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	inst, err := h.lima.Get(name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeData(w, inst)
}

func (h *Handler) StartInstance(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.lima.Start(name); err != nil {
		htmxToast(w, "Start failed: "+err.Error(), "error")
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Attempt to start VNC bridge after starting the instance.
	if err := h.vnc.StartVNC(name); err != nil {
		// Non-fatal: instance started but VNC may not be available yet.
		htmxToast(w, "Started "+name+" (VNC warning: "+err.Error()+")", "success")
		writeData(w, map[string]any{
			"status":      "started",
			"vnc_warning": err.Error(),
		})
		return
	}
	htmxToast(w, "Started "+name, "success")
	writeData(w, map[string]string{"status": "started"})
}

func (h *Handler) StopInstance(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	// Stop VNC bridge first.
	_ = h.vnc.StopVNC(name)

	if err := h.lima.Stop(name); err != nil {
		htmxToast(w, "Stop failed: "+err.Error(), "error")
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	htmxToast(w, "Stopped "+name, "success")
	writeData(w, map[string]string{"status": "stopped"})
}

func (h *Handler) RestartInstance(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	_ = h.vnc.StopVNC(name)

	if err := h.lima.Stop(name); err != nil {
		htmxToast(w, "Restart failed (stop): "+err.Error(), "error")
		writeError(w, http.StatusInternalServerError,
			fmt.Sprintf("stop failed: %s", err.Error()))
		return
	}
	if err := h.lima.Start(name); err != nil {
		htmxToast(w, "Restart failed (start): "+err.Error(), "error")
		writeError(w, http.StatusInternalServerError,
			fmt.Sprintf("start failed: %s", err.Error()))
		return
	}

	if err := h.vnc.StartVNC(name); err != nil {
		htmxToast(w, "Restarted "+name+" (VNC warning: "+err.Error()+")", "success")
		writeData(w, map[string]any{
			"status":      "restarted",
			"vnc_warning": err.Error(),
		})
		return
	}
	htmxToast(w, "Restarted "+name, "success")
	writeData(w, map[string]string{"status": "restarted"})
}

func (h *Handler) DeleteInstance(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	_ = h.vnc.StopVNC(name)

	if err := h.lima.Delete(name); err != nil {
		htmxToast(w, "Delete failed: "+err.Error(), "error")
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	htmxToast(w, "Deleted "+name, "success")
	writeData(w, map[string]string{"status": "deleted"})
}

func (h *Handler) GetVNC(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	info, err := h.vnc.GetVNCInfo(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeData(w, info)
}

func (h *Handler) GetRDP(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	info, err := h.rdp.GetRDPInfo(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeData(w, info)
}

func (h *Handler) CreateInstance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Template string `json:"template"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Template == "" {
		req.Template = "default"
	}

	// Resolve template name to path.
	templatePath := req.Template
	if !strings.Contains(templatePath, "/") {
		templatePath = fmt.Sprintf("/opt/lima/templates/%s.yaml", templatePath)
	}
	if _, err := os.Stat(templatePath); err != nil {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("template not found: %s", templatePath))
		return
	}

	if err := h.lima.Create(templatePath, req.Name); err != nil {
		htmxToast(w, "Create failed: "+err.Error(), "error")
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Determine instance name for VNC. If no name was given, limactl derives
	// it from the template filename.
	instanceName := req.Name
	if instanceName == "" {
		base := filepath.Base(templatePath)
		instanceName = strings.TrimSuffix(base, filepath.Ext(base))
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

func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	templateDir := "/opt/lima/templates"
	entries, err := os.ReadDir(templateDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError,
			fmt.Sprintf("reading template dir: %s", err.Error()))
		return
	}

	type tmplInfo struct {
		Name     string `json:"name"`
		Filename string `json:"filename"`
		Path     string `json:"path"`
	}
	var templates []tmplInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		templates = append(templates, tmplInfo{
			Name:     name,
			Filename: e.Name(),
			Path:     filepath.Join(templateDir, e.Name()),
		})
	}
	writeData(w, templates)
}

func (h *Handler) GetInfo(w http.ResponseWriter, r *http.Request) {
	info, err := h.lima.Info()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeData(w, map[string]any{
		"lima":          info,
		"bootc_enabled": h.bootc != nil,
	})
}

func (h *Handler) ListBootcBuilds(w http.ResponseWriter, r *http.Request) {
	if h.bootc == nil {
		writeError(w, http.StatusNotFound, "bootc not available in this image")
		return
	}
	writeData(w, h.bootc.ListBuilds())
}

func (h *Handler) CreateBootcBuild(w http.ResponseWriter, r *http.Request) {
	if h.bootc == nil {
		writeError(w, http.StatusNotFound, "bootc not available in this image")
		return
	}
	var req struct {
		Image          string          `json:"image"`
		VMName         string          `json:"vm_name"`
		Customizations *Customizations `json:"customizations,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Image == "" {
		htmxToast(w, "Image is required", "error")
		writeError(w, http.StatusBadRequest, "image is required")
		return
	}
	build, err := h.bootc.StartBuild(req.Image, req.VMName, req.Customizations)
	if err != nil {
		htmxToast(w, "Build failed: "+err.Error(), "error")
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	htmxToast(w, "Build started: "+build.ID, "success")
	writeJSON(w, http.StatusAccepted, map[string]any{"data": build})
}

func (h *Handler) GetBootcBuild(w http.ResponseWriter, r *http.Request) {
	if h.bootc == nil {
		writeError(w, http.StatusNotFound, "bootc not available in this image")
		return
	}
	id := r.PathValue("id")
	build, ok := h.bootc.GetBuild(id)
	if !ok {
		writeError(w, http.StatusNotFound, "build not found")
		return
	}
	writeData(w, build)
}

func (h *Handler) StreamBootcBuildLog(w http.ResponseWriter, r *http.Request) {
	if h.bootc == nil {
		writeError(w, http.StatusNotFound, "bootc not available")
		return
	}
	id := r.PathValue("id")
	h.bootc.StreamLog(id, w)
}
