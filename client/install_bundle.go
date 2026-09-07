package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/unix"
)

var releaseName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
var archiveHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

func fileLock(path string, mode int) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), mode|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("Termium is busy; close running sessions and retry: %w", err)
	}
	return f, nil
}

func executableBundle() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	return filepath.Dir(filepath.Dir(exe)), err
}

func lockInstalledSession() (func(), error) {
	root, err := executableBundle()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(root, "bundle.json")); os.IsNotExist(err) {
		return func() {}, nil
	}
	f, err := fileLock(filepath.Join(root, ".active"), unix.LOCK_SH)
	if err != nil {
		return nil, err
	}
	return func() { f.Close() }, nil
}

func installCurrentBundle() error {
	source, err := executableBundle()
	if err != nil {
		return err
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(userHome, ".local", "share")
	}
	appHome := os.Getenv("TERMIUM_HOME")
	if appHome == "" {
		appHome = filepath.Join(dataHome, "termium")
	}
	appHome, err = filepath.Abs(appHome)
	if err != nil {
		return err
	}
	return installBundle(source, appHome, userHome, os.Getenv("TERMIUM_INSTALL_SHA256"), func(root string) error {
		cmd := exec.Command(filepath.Join(root, "bin", "termium"), "--doctor")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	})
}

func installBundle(source, home, userHome, digest string, validate func(string) error) error {
	if !archiveHash.MatchString(digest) {
		return fmt.Errorf("installation requires a verified archive SHA-256")
	}
	data, err := os.ReadFile(filepath.Join(source, "bundle.json"))
	if err != nil {
		return err
	}
	var manifest bundleManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if !releaseName.MatchString(manifest.Version) {
		return fmt.Errorf("invalid bundle version")
	}
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	lock, err := fileLock(filepath.Join(home, ".install.lock"), unix.LOCK_EX)
	if err != nil {
		return err
	}
	defer lock.Close()
	bin := filepath.Join(userHome, ".local", "bin", "termium")
	launcherTarget := filepath.Join(home, "current", "bin", "termium")
	if _, err := os.Lstat(bin); err == nil {
		target, err := os.Readlink(bin)
		if err != nil || target != launcherTarget {
			return fmt.Errorf("refusing to overwrite unrelated command: %s", bin)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	current := filepath.Join(home, "current")
	previous := ""
	if target, err := os.Readlink(current); err == nil {
		if !filepath.IsLocal(target) || !strings.HasPrefix(target, "versions"+string(os.PathSeparator)) {
			return fmt.Errorf("unmanaged installation at %s", current)
		}
		previous = target
		lease, err := fileLock(filepath.Join(home, target, ".active"), unix.LOCK_EX)
		if err != nil {
			return err
		}
		defer lease.Close()
	} else if !os.IsNotExist(err) {
		return err
	}
	profiles, err := shellProfiles(userHome)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(home, ".staging-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	fmt.Println("→ Preparing Termium " + manifest.Version)
	if err := copyBundle(source, stage); err != nil {
		return err
	}
	if err := validate(stage); err != nil {
		return fmt.Errorf("installation check failed; previous version preserved: %w", err)
	}
	for i := range profiles {
		if err := profiles[i].apply(); err != nil {
			for j := i - 1; j >= 0; j-- {
				profiles[j].rollback()
			}
			return err
		}
	}
	committed := false
	defer func() {
		if !committed {
			for i := len(profiles) - 1; i >= 0; i-- {
				profiles[i].rollback()
			}
		}
	}()
	name := manifest.Version + "-" + digest[:12]
	versions := filepath.Join(home, "versions")
	if err := os.MkdirAll(versions, 0700); err != nil {
		return err
	}
	target := filepath.Join(versions, name)
	if _, err := os.Stat(target); os.IsNotExist(err) {
		if err := os.Rename(stage, target); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		installed, err := os.ReadFile(filepath.Join(target, "bundle.json"))
		if err != nil || !bytes.Equal(installed, data) {
			return fmt.Errorf("existing release metadata differs from the verified archive")
		}
		if err := validate(target); err != nil {
			return fmt.Errorf("existing release validation failed: %w", err)
		}
	}
	// The running binary is immutable. Repeated installation validates staging
	// but reuses the existing version, preserving files held by an old process.
	if err := atomicSymlink(filepath.Join("versions", name), current); err != nil {
		return err
	}
	if err := atomicSymlink(launcherTarget, bin); err != nil {
		if previous != "" {
			_ = atomicSymlink(previous, current)
		} else {
			_ = os.Remove(current)
		}
		return err
	}
	committed = true
	fmt.Println("✓ Installed. Run termium from any directory.")
	return nil
}

func atomicSymlink(target, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(filepath.Dir(destination), ".termium-link-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		return err
	}
	return os.Rename(link, destination)
}

func copyBundle(source, destination string) error {
	return filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dest := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.Mkdir(dest, 0755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			if filepath.IsAbs(target) || !filepath.IsLocal(filepath.Join(filepath.Dir(rel), target)) {
				return fmt.Errorf("bundle symlink escapes installation: %s", rel)
			}
			return os.Symlink(target, dest)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported bundle entry: %s", rel)
		}
		in, err := os.Open(name)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}

type profileChange struct {
	name          string
	before, after []byte
	existed       bool
	mode          fs.FileMode
}

func shellProfiles(home string) ([]profileChange, error) {
	if os.Getenv("TERMIUM_NO_MODIFY_PATH") == "1" {
		return nil, nil
	}
	bin := filepath.Join(home, ".local", "bin")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	block := "\n# >>> termium >>>\ncase \":$PATH:\" in *:" + quote(bin) + ":*) ;; *) export PATH=" + quote(bin) + ":\"$PATH\" ;; esac\n# <<< termium <<<\n"
	zdir := os.Getenv("ZDOTDIR")
	if zdir == "" {
		zdir = home
	}
	login := filepath.Join(home, ".profile")
	for _, name := range []string{".bash_profile", ".bash_login"} {
		if _, err := os.Stat(filepath.Join(home, name)); err == nil {
			login = filepath.Join(home, name)
			break
		}
	}
	paths := []string{filepath.Join(home, ".bashrc"), login, filepath.Join(zdir, ".zshrc"), filepath.Join(zdir, ".zprofile")}
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	fish := filepath.Join(config, "fish", "conf.d", "termium.fish")
	paths = append(paths, fish)
	var changes []profileChange
	for _, name := range paths {
		isFish := name == fish
		if resolved, err := filepath.EvalSymlinks(name); err == nil {
			name = resolved
		}
		before, err := os.ReadFile(name)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if bytes.Contains(before, []byte("# >>> termium >>>")) {
			continue
		}
		if isFish && err == nil {
			return nil, fmt.Errorf("refusing to overwrite unrelated shell file: %s", name)
		}
		content := block
		if isFish {
			quoted := "'" + strings.ReplaceAll(strings.ReplaceAll(bin, "\\", "\\\\"), "'", "\\'") + "'"
			content = "# >>> termium >>>\nfish_add_path --global --move -- " + quoted + "\n# <<< termium <<<\n"
		}
		mode := fs.FileMode(0644)
		if info, err := os.Stat(name); err == nil {
			mode = info.Mode().Perm()
			if mode&0222 == 0 {
				return nil, fmt.Errorf("shell configuration is read-only: %s", name)
			}
		}
		changes = append(changes, profileChange{name, before, append(bytes.Clone(before), []byte(content)...), err == nil, mode})
	}
	return changes, nil
}

func (p profileChange) apply() error {
	now, err := os.ReadFile(p.name)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if !bytes.Equal(now, p.before) {
		return fmt.Errorf("shell configuration changed during setup: %s", p.name)
	}
	return writeProfile(p.name, p.after, p.mode)
}
func (p profileChange) rollback() {
	now, err := os.ReadFile(p.name)
	if err == nil && bytes.Equal(now, p.after) {
		if p.existed {
			_ = writeProfile(p.name, p.before, p.mode)
		} else {
			_ = os.Remove(p.name)
		}
	}
}
func writeProfile(name string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(name), ".termium-profile-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), name)
}
