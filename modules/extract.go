package modules

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func childPath(root, path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("path is outside the SDK directory: %s", path)
	}
	return absolute, nil
}

func removeChild(root, path string) error {
	safe, err := childPath(root, path)
	if err != nil {
		return err
	}
	return os.RemoveAll(safe)
}

func regularParents(root, target string) error {
	for path := target; path != root; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("ZIP entry traverses a symbolic link: %s", path)
		}
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(data)
}

func (a *app) extract(file, destination string) error {
	defer os.Remove(file)
	stop := a.ui.activity("Extracting the SDK", nil)
	defer stop()
	archive, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer archive.Close()
	type link struct{ path, target string }
	var links []link
	for _, entry := range archive.File {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		name := strings.ReplaceAll(entry.Name, "\\", "/")
		if strings.Contains(name, ":") || strings.HasPrefix(name, "/") {
			return fmt.Errorf("invalid ZIP entry: %s", name)
		}
		target, err := childPath(destination, filepath.Join(destination, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		if err := regularParents(destination, target); err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if entry.Mode()&os.ModeSymlink != 0 {
			if a.os == "windows" {
				return fmt.Errorf("unexpected symbolic link in a Windows SDK: %s", name)
			}
			source, err := entry.Open()
			if err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(source, 8193))
			source.Close()
			if err != nil {
				return err
			}
			text := string(data)
			if len(data) > 8192 || text == "" || strings.ContainsAny(text, "\x00:") || strings.HasPrefix(text, "/") || strings.HasPrefix(text, "\\") {
				return fmt.Errorf("invalid ZIP symbolic link: %s", name)
			}
			if _, err := childPath(destination, filepath.Join(filepath.Dir(target), filepath.FromSlash(strings.ReplaceAll(text, "\\", "/")))); err != nil {
				return err
			}
			links = append(links, link{target, text})
			continue
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("unsupported ZIP entry: %s", name)
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		mode := entry.Mode().Perm()
		if mode == 0 {
			mode = 0644
		}
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(output, contextReader{a.ctx, source})
		closeErr := output.Close()
		source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := os.Chmod(target, mode); err != nil {
			return err
		}
	}
	for _, item := range links {
		if err := regularParents(destination, filepath.Dir(item.path)); err != nil {
			return err
		}
		if target, err := os.Readlink(item.path); err == nil && target == item.target {
			continue
		}
		if err := os.Symlink(item.target, item.path); err != nil {
			return err
		}
	}
	return nil
}
