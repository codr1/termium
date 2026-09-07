package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func installerFixture(t *testing.T) (string, string, string) {
	t.Helper()
	t.Setenv("ZDOTDIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("TERMIUM_NO_MODIFY_PATH", "")
	base := filepath.Join(t.TempDir(), "space and 'quote")
	source, home, user := filepath.Join(base, "source"), filepath.Join(base, "install"), filepath.Join(base, "user")
	for _, dir := range []string{filepath.Join(source, "bin"), user} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "bundle.json"), []byte(`{"version":"test-1"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "bin", "termium"), []byte("#!/bin/sh\nprintf 'installed command works\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return source, home, user
}

func TestInstallFromAnyDirectoryAndRepeatPreservesShellConfig(t *testing.T) {
	source, home, user := installerFixture(t)
	rc := filepath.Join(user, ".bashrc")
	original := []byte("# My existing shell configuration\nexport PRESERVED=yes\n")
	if err := os.WriteFile(rc, original, 0640); err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	for i := 0; i < 2; i++ {
		if err := installBundle(source, home, user, digest, func(string) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, original) || bytes.Count(data, []byte("# >>> termium >>>")) != 1 {
		t.Fatalf("shell config was damaged or duplicated: %s", data)
	}
	info, _ := os.Stat(rc)
	if info.Mode().Perm() != 0640 {
		t.Fatal("shell permissions changed")
	}
	cmd := exec.Command("bash", "--noprofile", "--norc", "-c", `source "$1"; termium`, "test", rc)
	cmd.Dir, cmd.Env = t.TempDir(), []string{"PATH=/usr/bin:/bin"}
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "installed command works") {
		t.Fatalf("command unavailable outside checkout: %s %v", output, err)
	}
	entries, err := os.ReadDir(filepath.Join(home, "versions"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("repeat duplicated installation: %v %v", entries, err)
	}
}

func TestFailedUpgradeAndBusySessionPreserveWorkingVersion(t *testing.T) {
	source, home, user := installerFixture(t)
	if err := installBundle(source, home, user, strings.Repeat("a", 64), func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(home, "current")
	before, _ := os.Readlink(current)
	profile, _ := os.ReadFile(filepath.Join(user, ".bashrc"))
	err := installBundle(source, home, user, strings.Repeat("b", 64), func(string) error { return errors.New("browser cannot start") })
	if err == nil {
		t.Fatal("invalid release activated")
	}
	lease, err := fileLock(filepath.Join(home, before, ".active"), unix.LOCK_SH)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	err = installBundle(source, home, user, strings.Repeat("b", 64), func(string) error { t.Fatal("validated while session active"); return nil })
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("active session was ignored: %v", err)
	}
	after, _ := os.Readlink(current)
	got, _ := os.ReadFile(filepath.Join(user, ".bashrc"))
	if after != before || !bytes.Equal(profile, got) {
		t.Fatal("failed update changed installation")
	}
}

func TestInstallerRefusesForeignCommandAndEscapingLinks(t *testing.T) {
	for _, failure := range []string{"foreign command", "escaping symlink", "read-only profile", "unverified archive"} {
		t.Run(failure, func(t *testing.T) {
			source, home, user := installerFixture(t)
			digest := strings.Repeat("a", 64)
			switch failure {
			case "foreign command":
				bin := filepath.Join(user, ".local", "bin")
				_ = os.MkdirAll(bin, 0755)
				_ = os.WriteFile(filepath.Join(bin, "termium"), []byte("keep me"), 0755)
			case "escaping symlink":
				_ = os.Symlink("../../outside", filepath.Join(source, "escape"))
			case "read-only profile":
				_ = os.WriteFile(filepath.Join(user, ".bashrc"), []byte("keep me"), 0444)
			case "unverified archive":
				digest = ""
			}
			if err := installBundle(source, home, user, digest, func(string) error { return nil }); err == nil {
				t.Fatal("unsafe installation accepted")
			}
			if _, err := os.Lstat(filepath.Join(home, "current")); !os.IsNotExist(err) {
				t.Fatal("failed install activated a version")
			}
		})
	}
}

func TestInstallerSerializesConcurrentUpdates(t *testing.T) {
	source, home, user := installerFixture(t)
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- installBundle(source, home, user, strings.Repeat("a", 64), func(string) error { close(started); <-release; return nil })
	}()
	<-started
	err := installBundle(source, home, user, strings.Repeat("b", 64), func(string) error { return nil })
	close(release)
	if first := <-done; first != nil {
		t.Fatal(first)
	}
	if err == nil || !strings.Contains(err.Error(), "busy") {
		t.Fatalf("concurrent update accepted: %v", err)
	}
}

func TestInstalledShells(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			command, err := exec.LookPath(shell)
			if err != nil {
				t.Skip("shell not installed on this test host")
			}
			source, home, user := installerFixture(t)
			if err := installBundle(source, home, user, strings.Repeat("a", 64), func(string) error { return nil }); err != nil {
				t.Fatal(err)
			}
			args := []string{"--noprofile", "-ic", "termium"}
			if shell == "zsh" {
				args = []string{"-d", "-ic", "termium"}
			}
			if shell == "fish" {
				args = []string{"-c", "termium"}
			}
			cmd := exec.Command(command, args...)
			cmd.Dir, cmd.Env = t.TempDir(), []string{"HOME=" + user, "PATH=/usr/bin:/bin", "TERM=xterm-256color"}
			output, err := cmd.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "installed command works") {
				t.Fatalf("%s startup: %v\n%s", shell, err, output)
			}
		})
	}
}
