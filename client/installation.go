package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type bundleManifest struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
	Browser  string `json:"browser"`
}

// Release dependencies are resolved beside the application, independent of cwd
// and the user's Node/Puppeteer configuration. Source builds use the toolchain.
func serverRuntime(loc *serverLocation) (string, []string, error) {
	root := filepath.Dir(loc.workDir)
	data, err := os.ReadFile(filepath.Join(root, "bundle.json"))
	if os.IsNotExist(err) {
		node, err := exec.LookPath("node")
		return node, os.Environ(), err
	}
	if err != nil {
		return "", nil, err
	}
	var manifest bundleManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", nil, err
	}
	if manifest.Platform != runtime.GOOS+"-"+runtime.GOARCH || !filepath.IsLocal(manifest.Browser) {
		return "", nil, fmt.Errorf("invalid or incompatible Termium bundle")
	}
	node := filepath.Join(root, "runtime", "bin", "node")
	browser := filepath.Join(root, manifest.Browser)
	for _, path := range []string{node, browser} {
		info, err := os.Stat(path)
		if err != nil {
			return "", nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return "", nil, fmt.Errorf("not executable: %s", path)
		}
	}
	env := os.Environ()
	set := func(key, value string) {
		filtered := env[:0]
		for _, item := range env {
			if !strings.HasPrefix(item, key+"=") {
				filtered = append(filtered, item)
			}
		}
		env = append(filtered, key+"="+value)
	}
	set("PUPPETEER_EXECUTABLE_PATH", browser)
	set("PUPPETEER_SKIP_DOWNLOAD", "true")
	if runtime.GOOS == "linux" {
		set("LD_LIBRARY_PATH", filepath.Join(root, "lib"))
		set("FONTCONFIG_FILE", filepath.Join(root, "fonts.conf"))
	}
	return node, env, nil
}

func checkInstallation() error {
	loc, err := findServerBinary()
	if err != nil {
		return err
	}
	node, env, err := serverRuntime(loc)
	if err != nil {
		return fmt.Errorf("runtime check failed: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join(filepath.Dir(loc.scriptPath), "check-install.js"))
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = loc.workDir, env, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("browser check failed: %w", err)
	}
	return nil
}
