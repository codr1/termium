package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type runtimeDependency struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	StripPrefix string `json:"stripPrefix"`
}

const maxDependencyArchive = 512 * 1024 * 1024
const maxDependencyExtracted = 2 * 1024 * 1024 * 1024

func installDependencies(root, cache string, dependencies []runtimeDependency) error {
	if len(dependencies) == 0 {
		return nil
	} // Older complete bundles remain installable.
	if len(dependencies) != 2 {
		return fmt.Errorf("expected browser and Vimium dependencies")
	}
	seen := map[string]bool{}
	for _, dep := range dependencies {
		destination := "browser"
		if dep.Name == "vimium" {
			destination = "server/dist/extensions/vimium"
		} else if dep.Name != "browser" {
			return fmt.Errorf("unknown dependency %q", dep.Name)
		}
		if seen[dep.Name] {
			return fmt.Errorf("duplicate dependency %q", dep.Name)
		}
		seen[dep.Name] = true
		fmt.Printf("→ Preparing %s %s\n", dep.Name, dep.Version)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		archive, err := downloadDependency(ctx, http.DefaultClient, cache, dep)
		cancel()
		if err != nil {
			return err
		}
		if err := extractDependency(archive, filepath.Join(root, destination), dep.StripPrefix); err != nil {
			return fmt.Errorf("extract %s: %w", dep.Name, err)
		}
	}
	return nil
}

func dependencyHash(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, maxDependencyArchive+1))
	if err != nil {
		return "", err
	}
	if n > maxDependencyArchive {
		return "", fmt.Errorf("cached dependency exceeds size limit")
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func downloadDependency(ctx context.Context, client *http.Client, cache string, dep runtimeDependency) (string, error) {
	u, err := url.Parse(dep.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || !archiveHash.MatchString(dep.SHA256) {
		return "", fmt.Errorf("invalid dependency URL or checksum")
	}
	if err := os.MkdirAll(cache, 0700); err != nil {
		return "", err
	}
	destination := filepath.Join(cache, dep.SHA256+".zip")
	if digest, err := dependencyHash(destination); err == nil && digest == dep.SHA256 {
		return destination, nil
	}
	// Preserve the caller's transport (including proxy/TLS configuration), while
	// preventing redirects from downgrading the authenticated download to HTTP.
	downloadClient := *client
	downloadClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || req.URL.User != nil || len(via) >= 10 {
			return fmt.Errorf("unsafe dependency redirect")
		}
		if client.CheckRedirect != nil {
			return client.CheckRedirect(req, via)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", dep.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d", dep.Name, resp.StatusCode)
	}
	if resp.ContentLength > maxDependencyArchive {
		return "", fmt.Errorf("dependency archive exceeds size limit")
	}
	f, err := os.CreateTemp(cache, ".download-")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxDependencyArchive+1))
	closeErr := f.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if n > maxDependencyArchive || fmt.Sprintf("%x", h.Sum(nil)) != dep.SHA256 {
		return "", fmt.Errorf("%s checksum mismatch or archive too large", dep.Name)
	}
	if err := os.Rename(f.Name(), destination); err != nil {
		return "", err
	}
	return destination, nil
}

func extractDependency(archive, root, prefix string) error {
	z, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer z.Close()
	if len(z.File) > 50000 {
		return fmt.Errorf("too many dependency files")
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	var total uint64
	links := map[string]string{}
	for _, file := range z.File {
		if !strings.HasPrefix(file.Name, prefix) {
			return fmt.Errorf("unexpected archive prefix")
		}
		name := strings.TrimPrefix(file.Name, prefix)
		if name == "" {
			continue
		}
		if !filepath.IsLocal(name) || strings.Contains(name, "\\") {
			return fmt.Errorf("unsafe dependency path: %s", name)
		}
		for _, part := range strings.Split(name, "/") {
			if part == ".." {
				return fmt.Errorf("dependency path traversal")
			}
		}
		if file.UncompressedSize64 > maxDependencyExtracted-total {
			return fmt.Errorf("expanded dependency exceeds size limit")
		}
		total += file.UncompressedSize64
		destination := filepath.Join(root, name)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(destination, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		if file.Mode()&os.ModeSymlink != 0 {
			if file.UncompressedSize64 > 4096 {
				return fmt.Errorf("dependency symlink too long")
			}
			r, err := file.Open()
			if err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(r, 4097))
			r.Close()
			if err != nil {
				return err
			}
			target := string(data)
			if filepath.IsAbs(target) || !filepath.IsLocal(filepath.Join(filepath.Dir(name), target)) {
				return fmt.Errorf("dependency symlink escapes installation")
			}
			if _, exists := links[destination]; exists {
				return fmt.Errorf("duplicate dependency symlink")
			}
			links[destination] = target
			continue
		}
		if !file.Mode().IsRegular() {
			return fmt.Errorf("unsupported dependency file")
		}
		r, err := file.Open()
		if err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if file.Mode().Perm()&0111 != 0 {
			mode = 0755
		}
		w, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			r.Close()
			return err
		}
		n, copyErr := io.Copy(w, io.LimitReader(r, int64(file.UncompressedSize64)+1))
		r.Close()
		closeErr := w.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if uint64(n) != file.UncompressedSize64 {
			return fmt.Errorf("dependency file size mismatch")
		}
	}
	// Create links last so ZIP order cannot route file writes through a symlink.
	for name, target := range links {
		if err := os.Symlink(target, name); err != nil {
			return err
		}
	}
	return nil
}
