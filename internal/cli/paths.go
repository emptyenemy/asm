package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// occupied reports whether anything at all exists at path. Lstat is used so a
// dangling symbolic link still counts, which is what the install and clean
// paths want when they refuse to touch a destination.
func occupied(path string) bool { _, err := os.Lstat(path); return !errors.Is(err, os.ErrNotExist) }

// childPath resolves a path that is meant to live inside root and rejects
// anything that escapes it, so an archive entry or a recorded SDK path cannot
// reach the rest of the filesystem.
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

// removeChild deletes a path, but only once childPath has confirmed that it
// lies inside root.
func removeChild(root, path string) error {
	safe, err := childPath(root, path)
	if err != nil {
		return err
	}
	return os.RemoveAll(safe)
}

// clearDirectory empties a directory without removing it, so the staging
// directory can be reused for the next source in the fallback chain.
func clearDirectory(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(directory, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
