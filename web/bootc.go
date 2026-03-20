package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const (
	bootcBuildsDir = "/var/lib/lima-bootc-builds"
	bibImage       = "quay.io/centos-bootc/bootc-image-builder:latest"
)

type BuildStatus string

const (
	BuildPending  BuildStatus = "pending"
	BuildRunning  BuildStatus = "running"
	BuildComplete BuildStatus = "complete"
	BuildFailed   BuildStatus = "failed"
)

type BootcBuild struct {
	ID          string      `json:"id"`
	SourceImage string      `json:"source_image"`
	VMName      string      `json:"vm_name"`
	Status      BuildStatus `json:"status"`
	StartedAt   time.Time   `json:"started_at"`
	FinishedAt  *time.Time  `json:"finished_at,omitempty"`
	Error       string      `json:"error,omitempty"`
	LogPath     string      `json:"-"`
	OutputPath  string      `json:"output_path,omitempty"`
}

type BootcManager struct {
	mu     sync.RWMutex
	builds map[string]*BootcBuild
	lima   *LimaCtl
}

func NewBootcManager(lima *LimaCtl) *BootcManager {
	return &BootcManager{
		builds: make(map[string]*BootcBuild),
		lima:   lima,
	}
}

// StartBuild kicks off a bootc-image-builder build in a goroutine.
func (b *BootcManager) StartBuild(sourceImage, vmName string) (*BootcBuild, error) {
	id := fmt.Sprintf("build-%d", time.Now().UnixMilli())
	if vmName == "" {
		vmName = id
	}

	outDir := filepath.Join(bootcBuildsDir, id)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("creating build dir: %w", err)
	}

	logPath := filepath.Join(outDir, "build.log")

	build := &BootcBuild{
		ID:          id,
		SourceImage: sourceImage,
		VMName:      vmName,
		Status:      BuildPending,
		StartedAt:   time.Now(),
		LogPath:     logPath,
	}

	b.mu.Lock()
	b.builds[id] = build
	b.mu.Unlock()

	go b.runBuild(build, outDir)
	return build, nil
}

func (b *BootcManager) runBuild(build *BootcBuild, outDir string) {
	b.mu.Lock()
	build.Status = BuildRunning
	b.mu.Unlock()

	logFile, err := os.Create(build.LogPath)
	if err != nil {
		b.markFailed(build, fmt.Sprintf("creating log file: %v", err))
		return
	}
	defer logFile.Close()

	// Run bootc-image-builder via podman
	cmd := exec.Command("podman", "run",
		"--rm",
		"--privileged",
		"--pull=newer",
		"-v", outDir+":/output",
		bibImage,
		"--type", "qcow2",
		"--output", "/output",
		build.SourceImage,
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	fmt.Fprintf(logFile, "[lima-bootc] Starting build: %s → %s\n", build.SourceImage, outDir)

	if err := cmd.Run(); err != nil {
		b.markFailed(build, fmt.Sprintf("bootc-image-builder failed: %v", err))
		fmt.Fprintf(logFile, "[lima-bootc] Build FAILED: %v\n", err)
		return
	}

	// Expected output path from bootc-image-builder
	qcow2Path := filepath.Join(outDir, "qcow2", "disk.qcow2")
	if _, err := os.Stat(qcow2Path); err != nil {
		b.markFailed(build, fmt.Sprintf("qcow2 not found at expected path: %v", err))
		return
	}

	fmt.Fprintf(logFile, "[lima-bootc] Build complete: %s\n", qcow2Path)
	fmt.Fprintf(logFile, "[lima-bootc] Starting Lima VM: %s\n", build.VMName)

	// Start Lima VM from the built qcow2
	if err := b.lima.Create(qcow2Path, build.VMName); err != nil {
		b.markFailed(build, fmt.Sprintf("lima start failed: %v", err))
		fmt.Fprintf(logFile, "[lima-bootc] Lima start FAILED: %v\n", err)
		return
	}

	fmt.Fprintf(logFile, "[lima-bootc] VM started: %s\n", build.VMName)

	now := time.Now()
	b.mu.Lock()
	build.Status = BuildComplete
	build.FinishedAt = &now
	build.OutputPath = qcow2Path
	b.mu.Unlock()

	log.Printf("bootc build %s complete, VM %s started", build.ID, build.VMName)
}

func (b *BootcManager) markFailed(build *BootcBuild, msg string) {
	now := time.Now()
	b.mu.Lock()
	build.Status = BuildFailed
	build.Error = msg
	build.FinishedAt = &now
	b.mu.Unlock()
	log.Printf("bootc build %s failed: %s", build.ID, msg)
}

func (b *BootcManager) ListBuilds() []*BootcBuild {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]*BootcBuild, 0, len(b.builds))
	for _, build := range b.builds {
		out = append(out, build)
	}
	return out
}

func (b *BootcManager) GetBuild(id string) (*BootcBuild, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	build, ok := b.builds[id]
	return build, ok
}

// StreamLog writes the build log to w, following new content until the build finishes.
func (b *BootcManager) StreamLog(id string, w http.ResponseWriter) {
	build, ok := b.GetBuild(id)
	if !ok {
		http.Error(w, "build not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, canFlush := w.(http.Flusher)

	f, err := os.Open(build.LogPath)
	if err != nil {
		fmt.Fprintf(w, "data: [error opening log: %v]\n\n", err)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for {
		for scanner.Scan() {
			fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
			if canFlush {
				flusher.Flush()
			}
		}
		// Check if build is done
		b.mu.RLock()
		done := build.Status == BuildComplete || build.Status == BuildFailed
		b.mu.RUnlock()
		if done {
			fmt.Fprintf(w, "data: [DONE status=%s]\n\n", build.Status)
			if canFlush {
				flusher.Flush()
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
