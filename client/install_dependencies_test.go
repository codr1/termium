package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDependencyDownloadIntegrityAndCache(t *testing.T) {
	body := []byte("verified archive")
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/downgrade" {
			http.Redirect(w, r, "http://example.com/", http.StatusFound)
			return
		}
		if r.URL.Path == "/missing" {
			w.WriteHeader(404)
			return
		}
		w.Write(body)
	}))
	defer server.Close()
	cache := t.TempDir()
	dep := runtimeDependency{Name: "browser", URL: server.URL, SHA256: fmt.Sprintf("%x", sha256.Sum256(body))}
	get := func(dep runtimeDependency) (string, error) {
		return downloadDependency(context.Background(), server.Client(), cache, dep)
	}
	file, err := get(dep)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(file); !bytes.Equal(data, body) {
		t.Fatal("wrong cached data")
	}
	if _, err := get(dep); err != nil || requests.Load() != 1 {
		t.Fatal("verified cache not reused", err)
	}
	if err := os.WriteFile(file, []byte("corrupt cache"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := get(dep); err != nil || requests.Load() != 2 {
		t.Fatal("corrupt cache not replaced", err)
	}
	bad := dep
	bad.SHA256 = strings.Repeat("a", 64)
	if _, err := get(bad); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	if _, err := os.Stat(filepath.Join(cache, bad.SHA256+".zip")); !os.IsNotExist(err) {
		t.Fatal("unverified download cached")
	}
	for _, route := range []string{"/downgrade", "/missing"} {
		bad.URL = server.URL + route
		if _, err := get(bad); err == nil {
			t.Fatal("unsafe/failed download accepted", route)
		}
	}
	bad.URL = "http://example.com/archive.zip"
	if _, err := get(bad); err == nil {
		t.Fatal("plaintext source accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	bad.URL = server.URL
	if _, err := downloadDependency(ctx, server.Client(), cache, bad); err == nil {
		t.Fatal("cancelled download completed")
	}
	entries, _ := os.ReadDir(cache)
	if len(entries) != 1 {
		t.Fatal("partial downloads leaked", entries)
	}
}

type zipEntry struct {
	name, body string
	mode       os.FileMode
}

func dependencyZip(t *testing.T, entries ...zipEntry) string {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		h.SetMode(e.mode)
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "dependency.zip")
	if err := os.WriteFile(file, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestDependencyExtractionAndTraversal(t *testing.T) {
	file := dependencyZip(t, zipEntry{"upstream/bin/browser", "binary", 0755}, zipEntry{"upstream/Current", "bin", os.ModeSymlink | 0777})
	root := filepath.Join(t.TempDir(), "runtime")
	if err := extractDependency(file, root, "upstream/"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(root, "Current/browser")); err != nil || info.Mode().Perm() != 0755 {
		t.Fatal("executable/framework symlink not preserved", info, err)
	}
	for _, tc := range []struct {
		name    string
		entries []zipEntry
	}{
		{"traversal", []zipEntry{{"upstream/../../outside", "bad", 0644}}},
		{"absolute", []zipEntry{{"/outside", "bad", 0644}}},
		{"wrong prefix", []zipEntry{{"another/file", "bad", 0644}}},
		{"escaping link", []zipEntry{{"upstream/link", "../outside", os.ModeSymlink | 0777}}},
		{"link used as parent", []zipEntry{{"upstream/link", "dir", os.ModeSymlink | 0777}, {"upstream/link/file", "bad", 0644}}},
		{"duplicate file", []zipEntry{{"upstream/file", "one", 0644}, {"upstream/file", "two", 0644}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			if err := extractDependency(dependencyZip(t, tc.entries...), filepath.Join(base, "runtime"), "upstream/"); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			if _, err := os.Stat(filepath.Join(base, "outside")); !os.IsNotExist(err) {
				t.Fatal("archive escaped extraction root")
			}
		})
	}
}

func TestDependencyFailurePreservesInstallation(t *testing.T) {
	source, home, user := installerFixture(t)
	if err := installBundle(source, home, user, strings.Repeat("a", 64), func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Readlink(filepath.Join(home, "current"))
	profile, _ := os.ReadFile(filepath.Join(user, ".bashrc"))
	manifest := bundleManifest{Version: "broken-upgrade", Dependencies: []runtimeDependency{{Name: "browser", URL: "http://invalid", SHA256: strings.Repeat("a", 64)}, {Name: "vimium"}}}
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(source, "bundle.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := installBundle(source, home, user, strings.Repeat("b", 64), func(string) error { t.Fatal("executed incomplete runtime"); return nil }); err == nil {
		t.Fatal("incomplete upgrade activated")
	}
	after, _ := os.Readlink(filepath.Join(home, "current"))
	got, _ := os.ReadFile(filepath.Join(user, ".bashrc"))
	if before != after || !bytes.Equal(profile, got) {
		t.Fatal("download failure damaged installed app")
	}
}
