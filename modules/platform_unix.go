//go:build linux || darwin

package modules

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

func terminalWidth(file *os.File) int {
	var size struct{ Rows, Columns, X, Y uint16 }
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&size)))
	if err != 0 {
		return 0
	}
	return int(size.Columns)
}

func enableColor(files ...*os.File) bool { return true }

func lockSDKRoot(root string) (func(), error) {
	path := filepath.Join(root, ".asm-update.lock")
	for attempt := 0; attempt < 3; attempt++ {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("SDK lock cannot be a symbolic link")
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			file.Close()
			return nil, errors.New("another install or update is running in this SDK directory")
		}
		opened, openErr := file.Stat()
		current, statErr := os.Stat(path)
		if openErr == nil && statErr == nil && os.SameFile(opened, current) {
			return func() { os.Remove(path); syscall.Flock(int(file.Fd()), syscall.LOCK_UN); file.Close() }, nil
		}
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
	}
	return nil, errors.New("SDK lock changed while acquiring it; retry the operation")
}

func moveNewDirectory(source, destination string) error {
	if err := os.Mkdir(destination, 0700); err != nil {
		return err
	}
	reserved, err := os.Stat(destination)
	if err != nil {
		return err
	}
	if err := syscall.Rename(source, destination); err != nil {
		if current, statErr := os.Stat(destination); statErr == nil && os.SameFile(current, reserved) {
			_ = os.Remove(destination)
		}
		return err
	}
	return nil
}

func copyIfMissing(source, destination string) error {
	if occupied(destination) {
		return nil
	}
	data, err := os.ReadFile(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0644)
}

func configureSDK(ctx context.Context, directory, arch string) error {
	for _, path := range []string{"bin/adt", "bin/adl", "bin/compc", "bin/mxmlc", "bin/configure_linux.sh"} {
		path = filepath.Join(directory, filepath.FromSlash(path))
		if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() {
			if err := os.Chmod(path, info.Mode().Perm()|0111); err != nil {
				return err
			}
		}
	}
	if runtime.GOOS == "darwin" {
		attributes, err := exec.CommandContext(ctx, "/usr/bin/xattr", "-r", directory).CombinedOutput()
		if err != nil {
			return fmt.Errorf("cannot inspect SDK quarantine: %w: %s", err, strings.TrimSpace(string(attributes)))
		}
		if strings.Contains(string(attributes), "com.apple.quarantine") {
			output, err := exec.CommandContext(ctx, "/usr/bin/xattr", "-r", "-d", "com.apple.quarantine", directory).CombinedOutput()
			if err != nil {
				return fmt.Errorf("cannot clear SDK quarantine: %w: %s", err, strings.TrimSpace(string(output)))
			}
		}
		return nil
	}
	script := filepath.Join(directory, "bin", "configure_linux.sh")
	if !occupied(script) {
		if arch == "arm64" {
			return errors.New("this SDK has no Linux ARM64 configuration script")
		}
		return ctx.Err()
	}
	if err := copyIfMissing(filepath.Join(directory, "lib", "linux_arm64", "FlashRuntimeExtensions.so"), filepath.Join(directory, "lib", "FlashRuntimeExtensions_linux_arm64.so")); err != nil {
		return err
	}
	original := filepath.Join(directory, "lib", "FlashRuntimeExtensions.so")
	if info, err := os.Lstat(original); err == nil && info.Mode().IsRegular() {
		if err := copyIfMissing(original, filepath.Join(directory, "lib", "FlashRuntimeExtensions_linux64.so")); err != nil {
			return err
		}
	}
	runtimeDir := filepath.Join(directory, "runtimes", "air", "linux")
	legacy := filepath.Join(directory, "runtimes", "air", "linux-x64")
	if info, err := os.Lstat(runtimeDir); err == nil && info.IsDir() && !occupied(legacy) {
		if err := os.Rename(runtimeDir, legacy); err != nil {
			return err
		}
	}
	naip := filepath.Join(directory, "lib", "nai", "bin", "naip")
	if info, err := os.Lstat(naip); err == nil && info.Mode().IsRegular() {
		if err := os.Remove(naip); err != nil {
			return err
		}
	}
	args := []string{script}
	if arch == "arm64" {
		args = append(args, "arm64")
	}
	command := exec.CommandContext(ctx, "/bin/sh", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot configure Linux SDK: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
