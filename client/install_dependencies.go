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
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	URL         string            `json:"url"`
	SHA256      string            `json:"sha256"`
	StripPrefix string            `json:"stripPrefix"`
	Files       map[string]string `json:"files,omitempty"`
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
			if len(dep.Files) == 0 {
				return fmt.Errorf("missing reviewed Vimium file checksums")
			}
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
		if err := extractVerifiedDependency(archive, filepath.Join(root, destination), dep.StripPrefix, dep.Files); err != nil {
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
	return extractVerifiedDependency(archive, root, prefix, nil)
}

func dependencyDirectory(root, directory string) error {
	rel, err := filepath.Rel(root, directory)
	if err != nil || (rel != "." && !filepath.IsLocal(rel)) {
		return fmt.Errorf("dependency directory escapes root")
	}
	current := root
	parts := append([]string{"."}, strings.Split(rel, string(filepath.Separator))...)
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0755); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe dependency directory: %s", current)
		}
	}
	return nil
}

func extractVerifiedDependency(archive, root, prefix string, expected map[string]string) error {
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
	if err := dependencyDirectory(root, root); err != nil {
		return err
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	var total uint64
	links := map[string]string{}
	written := map[string]bool{}
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
		// Install only the extension files reviewed and exercised in this
		// release's source tree. Preserve the separately packaged welcome overlay.
		if expected != nil {
			if _, included := expected[name]; !included {
				continue
			}
			if !archiveHash.MatchString(expected[name]) || !file.Mode().IsRegular() {
				return fmt.Errorf("invalid reviewed dependency file: %s", name)
			}
		}
		if file.UncompressedSize64 > maxDependencyExtracted-total {
			return fmt.Errorf("expanded dependency exceeds size limit")
		}
		total += file.UncompressedSize64
		destination := filepath.Join(root, name)
		if file.FileInfo().IsDir() {
			if err := dependencyDirectory(root, destination); err != nil {
				return err
			}
			continue
		}
		if err := dependencyDirectory(root, filepath.Dir(destination)); err != nil {
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
			if uint64(len(data)) != file.UncompressedSize64 {
				return fmt.Errorf("dependency symlink size mismatch")
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
		hash := sha256.New()
		var output io.Writer = w
		if expected != nil {
			output = io.MultiWriter(w, hash)
		}
		n, copyErr := io.Copy(output, io.LimitReader(r, int64(file.UncompressedSize64)+1))
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
		if expected != nil && fmt.Sprintf("%x", hash.Sum(nil)) != expected[name] {
			return fmt.Errorf("downloaded dependency differs from reviewed source: %s", name)
		}
		written[name] = true
	}
	// Create links last so ZIP order cannot route file writes through a symlink.
	for name, target := range links {
		if err := os.Symlink(target, name); err != nil {
			return err
		}
	}
	// Lexical '..' checks alone miss escapes through other links in the ZIP.
	// Resolve the complete graph after creation, rejecting escapes and cycles.
	for name := range links {
		resolved, err := filepath.EvalSymlinks(name)
		if err != nil {
			return fmt.Errorf("invalid dependency symlink: %w", err)
		}
		rel, err := filepath.Rel(canonicalRoot, resolved)
		if err != nil || (rel != "." && !filepath.IsLocal(rel)) {
			return fmt.Errorf("dependency symlink resolves outside installation")
		}
	}
	for name := range expected {
		if !written[name] {
			return fmt.Errorf("reviewed dependency file missing: %s", name)
		}
	}
	return nil
}
