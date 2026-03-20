package main

import (
	_ "embed"
	"log"
	"net/http"
	"os"
)

//go:embed static/openapi.yaml
var openAPISpec []byte

func main() {
	port := os.Getenv("LIMA_WEB_PORT")
	if port == "" {
		port = "8080"
	}

	limaHome := os.Getenv("LIMA_HOME")
	if limaHome == "" {
		limaHome = "/var/lib/lima"
	}

	lima := NewLimaCtl(limaHome)
	vnc := NewVNCManager(limaHome)
	grdMgr := NewGRDManager(limaHome)

	enabled := os.Getenv("LIMA_BOOTC_ENABLED") == "true"
	var bootcMgr *BootcManager
	if enabled {
		bootcMgr = NewBootcManager(lima)
		log.Println("bootc-image-builder support enabled")
	}
	h := NewHandler(lima, vnc, bootcMgr)

	// Scan for already-running instances and start their VNC bridges.
	if instances, err := lima.List(); err == nil {
		for _, inst := range instances {
			if inst.Status == "Running" {
				log.Printf("startup: starting VNC bridge for running instance %q", inst.Name)
				if err := vnc.StartVNC(inst.Name); err != nil {
					log.Printf("startup: VNC bridge for %q failed: %v", inst.Name, err)
				}
			}
		}
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/instances", h.ListInstances)
	mux.HandleFunc("GET /api/instances/{name}", h.GetInstance)
	mux.HandleFunc("POST /api/instances/{name}/start", h.StartInstance)
	mux.HandleFunc("POST /api/instances/{name}/stop", h.StopInstance)
	mux.HandleFunc("POST /api/instances/{name}/restart", h.RestartInstance)
	mux.HandleFunc("DELETE /api/instances/{name}", h.DeleteInstance)
	mux.HandleFunc("GET /api/instances/{name}/vnc", h.GetVNC)
	mux.HandleFunc("GET /api/instances/{name}/rdp", grdMgr.HandleRDPStatus)
	mux.HandleFunc("POST /api/instances/{name}/rdp/enable", grdMgr.HandleRDPEnable)
	mux.HandleFunc("GET /api/instances/{name}/shell", h.ShellWS)
	mux.HandleFunc("GET /websockify/{name}", vnc.HandleVNCProxy)
	mux.HandleFunc("POST /api/instances/create", h.CreateInstance)
	mux.HandleFunc("GET /api/templates", h.ListTemplates)
	mux.HandleFunc("GET /api/info", h.GetInfo)
	mux.HandleFunc("GET /api/bootc/builds", h.ListBootcBuilds)
	mux.HandleFunc("POST /api/bootc/builds", h.CreateBootcBuild)
	mux.HandleFunc("GET /api/bootc/builds/{id}", h.GetBootcBuild)
	mux.HandleFunc("GET /api/bootc/builds/{id}/log", h.StreamBootcBuildLog)

	// Serve static dashboard files.
	staticDir := "/usr/share/lima-web/static/"
	mux.Handle("/dashboard/", http.StripPrefix("/dashboard/", http.FileServer(http.Dir(staticDir))))

	// Serve OpenAPI spec.
	mux.HandleFunc("GET /api/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Write(openAPISpec)
	})

	logged := loggingMiddleware(mux)

	log.Printf("lima-web listening on :%s", port)
	if err := http.ListenAndServe(":"+port, logged); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// loggingMiddleware logs every request to stderr.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
