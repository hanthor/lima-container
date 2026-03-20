package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	bootcBuildsDir = "/var/lib/lima-bootc-builds"
	bootcDiskSize  = "20G"
)

type BuildStatus string

const (
	BuildPending  BuildStatus = "pending"
	BuildRunning  BuildStatus = "running"
	BuildComplete BuildStatus = "complete"
	BuildFailed   BuildStatus = "failed"
)

// Customizations describes optional image modifications applied before building
// the disk image. A derived container image is built from a generated Containerfile,
// then used as the source for `bootc install to-disk`.
type Customizations struct {
	// EnableSSH ensures sshd is installed and enabled.
	EnableSSH bool `json:"enable_ssh"`
	// EnableRDP installs xrdp and enables it so the VM is reachable via RDP.
	EnableRDP bool `json:"enable_rdp"`
	// ExtraPackages is a list of extra dnf/apt package names to install.
	ExtraPackages []string `json:"extra_packages,omitempty"`
	// ExtraContainerfile is appended verbatim to the generated Containerfile,
	// allowing arbitrary RUN/COPY/etc. instructions.
	ExtraContainerfile string `json:"extra_containerfile,omitempty"`
}

func (c *Customizations) hasWork() bool {
	if c == nil {
		return false
	}
	return c.EnableSSH || c.EnableRDP || len(c.ExtraPackages) > 0 || strings.TrimSpace(c.ExtraContainerfile) != ""
}

type BootcBuild struct {
	ID             string          `json:"id"`
	SourceImage    string          `json:"source_image"`
	VMName         string          `json:"vm_name"`
	Customizations *Customizations `json:"customizations,omitempty"`
	Status         BuildStatus     `json:"status"`
	StartedAt      time.Time       `json:"started_at"`
	FinishedAt     *time.Time      `json:"finished_at,omitempty"`
	Error          string          `json:"error,omitempty"`
	LogPath        string          `json:"-"`
	OutputPath     string          `json:"output_path,omitempty"`
}

type BootcManager struct {
	mu        sync.RWMutex
	builds    map[string]*BootcBuild
	lima      *LimaCtl
	buildsDir string
}

func NewBootcManager(lima *LimaCtl, buildsDir string) *BootcManager {
	if buildsDir == "" {
		buildsDir = bootcBuildsDir
	}
	return &BootcManager{
		builds:    make(map[string]*BootcBuild),
		lima:      lima,
		buildsDir: buildsDir,
	}
}

// StartBuild kicks off a bootc-image-builder build in a goroutine.
func (b *BootcManager) StartBuild(sourceImage, vmName string, customizations *Customizations) (*BootcBuild, error) {
	id := fmt.Sprintf("build-%d", time.Now().UnixMilli())
	if vmName == "" {
		vmName = id
	}

	outDir := filepath.Join(b.buildsDir, id)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("creating build dir: %w", err)
	}

	logPath := filepath.Join(outDir, "build.log")

	build := &BootcBuild{
		ID:             id,
		SourceImage:    sourceImage,
		VMName:         vmName,
		Customizations: customizations,
		Status:         BuildPending,
		StartedAt:      time.Now(),
		LogPath:        logPath,
	}

	b.mu.Lock()
	b.builds[id] = build
	b.mu.Unlock()

	go b.runBuild(build, outDir)
	return build, nil
}

// generateContainerfile produces a Containerfile that applies the requested
// customizations on top of the source image.
func generateContainerfile(sourceImage string, c *Customizations) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("FROM %s\n\n", sourceImage))

	// Collect systemd units to enable
	var enableUnits []string

	if c.EnableSSH {
		sb.WriteString("# Ensure sshd is installed and enabled\n")
		sb.WriteString("RUN command -v sshd >/dev/null 2>&1 || (dnf install -y openssh-server 2>/dev/null || apt-get install -y openssh-server 2>/dev/null || true)\n")
		enableUnits = append(enableUnits, "sshd")
	}

	if c.EnableRDP {
		sb.WriteString("# Install and enable xrdp for RDP access\n")
		sb.WriteString("RUN dnf install -y xrdp 2>/dev/null || apt-get install -y xrdp 2>/dev/null || true\n")
		enableUnits = append(enableUnits, "xrdp")
	}

	if len(c.ExtraPackages) > 0 {
		pkgs := strings.Join(c.ExtraPackages, " ")
		sb.WriteString(fmt.Sprintf("\n# Extra packages\n"))
		sb.WriteString(fmt.Sprintf("RUN dnf install -y %s 2>/dev/null || apt-get install -y %s 2>/dev/null || true\n", pkgs, pkgs))
	}

	if len(enableUnits) > 0 {
		units := strings.Join(enableUnits, " ")
		sb.WriteString(fmt.Sprintf("\n# Enable systemd services\n"))
		sb.WriteString(fmt.Sprintf("RUN systemctl enable %s\n", units))
	}

	if extra := strings.TrimSpace(c.ExtraContainerfile); extra != "" {
		sb.WriteString("\n# Custom instructions\n")
		sb.WriteString(extra)
		sb.WriteString("\n")
	}

	return sb.String()
}

// buildDerivedImage generates a Containerfile, builds a local image, and returns
// the local image tag. The caller is responsible for removing the image when done.
func (b *BootcManager) buildDerivedImage(build *BootcBuild, outDir string, logFile *os.File) (string, error) {
	containerfilePath := filepath.Join(outDir, "Containerfile")
	contents := generateContainerfile(build.SourceImage, build.Customizations)

	if err := os.WriteFile(containerfilePath, []byte(contents), 0644); err != nil {
		return "", fmt.Errorf("writing Containerfile: %w", err)
	}

	fmt.Fprintf(logFile, "[lima-bootc] Generated Containerfile:\n%s\n", contents)
	fmt.Fprintf(logFile, "[lima-bootc] Building derived image...\n")

	localTag := fmt.Sprintf("localhost/lima-bootc-custom-%s:latest", build.ID)

	cmd := exec.Command("podman", "build",
		"--tag", localTag,
		"--file", containerfilePath,
		"--network=host", // avoids netavark/nftables issues inside a privileged container
		"--no-cache",
		outDir, // build context (Containerfile only, no ADD/COPY needed)
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("podman build failed: %w", err)
	}

	fmt.Fprintf(logFile, "[lima-bootc] Derived image built: %s\n", localTag)
	return localTag, nil
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

	// Determine which image to pass to bootc-image-builder.
	// If customizations are requested, build a derived image first.
	buildImage := build.SourceImage
	var derivedTag string

	if build.Customizations.hasWork() {
		fmt.Fprintf(logFile, "[lima-bootc] Customizations requested — building derived image\n")
		tag, err := b.buildDerivedImage(build, outDir, logFile)
		if err != nil {
			b.markFailed(build, fmt.Sprintf("customization build failed: %v", err))
			fmt.Fprintf(logFile, "[lima-bootc] Customization FAILED: %v\n", err)
			return
		}
		buildImage = tag
		derivedTag = tag
	}

	// Ensure derived image is cleaned up after the build regardless of outcome.
	if derivedTag != "" {
		defer func() {
			fmt.Fprintf(logFile, "[lima-bootc] Cleaning up derived image %s\n", derivedTag)
			exec.Command("podman", "rmi", "--force", derivedTag).Run()
		}()
	}

	// --- Phase 2: create a raw disk and install the bootc image to it ---

	rawPath := filepath.Join(outDir, "disk.raw")
	qcow2Path := filepath.Join(outDir, "disk.qcow2")

	// Allocate a sparse raw disk image.
	fmt.Fprintf(logFile, "[lima-bootc] Allocating %s raw disk: %s\n", bootcDiskSize, rawPath)
	if out, err := exec.Command("truncate", "-s", bootcDiskSize, rawPath).CombinedOutput(); err != nil {
		b.markFailed(build, fmt.Sprintf("truncate failed: %v: %s", err, out))
		return
	}
	defer os.Remove(rawPath)

	// bootc checks /run/udev exists to verify it's not installing over the running OS.
	os.MkdirAll("/run/udev", 0755)

	// The nested container is --privileged with --pid=host, so losetup works inside
	// it even when the outer container is rootless. We mount the raw disk file in
	// and use a shell wrapper to: losetup → bootc install → losetup cleanup.
	podmanArgs := []string{
		"run", "--rm",
		"--privileged",
		"--pid=host",
		"--network=host",
		"--cgroup-manager=cgroupfs",
		"--security-opt", "label=type:unconfined_t",
		"-v", "/dev:/dev",
		"-v", rawPath + ":/tmp/disk.raw",
	}

	var sourceImgref string
	if derivedTag != "" {
		ociPath := filepath.Join(outDir, "source.oci.tar")
		fmt.Fprintf(logFile, "[lima-bootc] Saving derived image as OCI archive...\n")
		saveCmd := exec.Command("podman", "save", "--format", "oci-archive", "-o", ociPath, derivedTag)
		saveCmd.Stdout = logFile
		saveCmd.Stderr = logFile
		if err := saveCmd.Run(); err != nil {
			b.markFailed(build, fmt.Sprintf("podman save failed: %v", err))
			fmt.Fprintf(logFile, "[lima-bootc] podman save FAILED: %v\n", err)
			return
		}
		podmanArgs = append(podmanArgs, "-v", ociPath+":/tmp/source.oci.tar:ro")
		sourceImgref = "oci-archive:/tmp/source.oci.tar"
	} else {
		sourceImgref = "docker://" + buildImage
	}

	// Shell wrapper: attach raw file as loop device, run bootc, detach.
	// losetup works inside the privileged nested container even under rootless Podman.
	bootcScript := fmt.Sprintf(
		`set -e; DEV=$(losetup --find --show /tmp/disk.raw); `+
			`echo "Loop device: $DEV"; `+
			`trap "losetup -d $DEV" EXIT; `+
			`bootc install to-disk --source-imgref %s --target-no-signature-verification --filesystem xfs "$DEV"`,
		sourceImgref,
	)

	podmanArgs = append(podmanArgs, buildImage, "bash", "-c", bootcScript)

	fmt.Fprintf(logFile, "[lima-bootc] Running bootc install to-disk (source: %s)...\n", sourceImgref)
	installCmd := exec.Command("podman", podmanArgs...)
	installCmd.Stdout = logFile
	installCmd.Stderr = logFile
	if err := installCmd.Run(); err != nil {
		b.markFailed(build, fmt.Sprintf("bootc install to-disk failed: %v", err))
		fmt.Fprintf(logFile, "[lima-bootc] bootc install FAILED: %v\n", err)
		return
	}

	// Convert raw → qcow2.
	fmt.Fprintf(logFile, "[lima-bootc] Converting raw → qcow2: %s\n", qcow2Path)
	if out, err := exec.Command("qemu-img", "convert",
		"-f", "raw", "-O", "qcow2", rawPath, qcow2Path,
	).CombinedOutput(); err != nil {
		b.markFailed(build, fmt.Sprintf("qemu-img convert failed: %v: %s", err, out))
		return
	}

	fmt.Fprintf(logFile, "[lima-bootc] Build complete: %s\n", qcow2Path)
	fmt.Fprintf(logFile, "[lima-bootc] Starting Lima VM: %s\n", build.VMName)

	// Start Lima VM from the built qcow2.
	// bootc images don't run cloud-init, so Lima's SSH provisioning step will
	// time out. We use a short timeout and consider the VM started if limactl
	// reports it as Running even when the provisioning wait times out.
	if err := b.lima.CreateBootc(qcow2Path, build.VMName); err != nil {
		// Check if the VM actually started despite the error (limactl timeout).
		running, checkErr := b.lima.IsRunning(build.VMName)
		if checkErr != nil || !running {
			b.markFailed(build, fmt.Sprintf("lima start failed: %v", err))
			fmt.Fprintf(logFile, "[lima-bootc] Lima start FAILED: %v\n", err)
			return
		}
		fmt.Fprintf(logFile, "[lima-bootc] Lima provisioning timed out but VM is running (expected for bootc images)\n")
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
