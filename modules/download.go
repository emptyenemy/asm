package modules

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	downloadAttempts = 4
	downloadTimeout  = 600 * time.Second
	partialMaxAge    = 7 * 24 * time.Hour
	partialDirectory = ".asm-partial"
)

var (
	errStalled   = errors.New("no data received")
	contentRange = regexp.MustCompile(`^bytes (\d+)-`)
)

// retryableError marks a failure that another attempt may fix.
type retryableError struct{ err error }

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// corruptError marks a finished download that fails verification.
type corruptError string

func (e corruptError) Error() string { return string(e) }

func retryableStatus(code int) bool {
	return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500
}

// watchReader restarts the stall timer whenever data arrives and remembers
// read failures, which are the only body errors worth retrying.
type watchReader struct {
	reader io.Reader
	timer  *time.Timer
	stall  time.Duration
	err    error
}

func (w *watchReader) Read(data []byte) (int, error) {
	n, err := w.reader.Read(data)
	if n > 0 {
		w.timer.Reset(w.stall)
	}
	if err != nil && err != io.EOF {
		w.err = err
	}
	return n, err
}

// archiveDownload writes an archive to a partial file. The file and the hash
// always describe the same bytes, so an attempt can continue where the
// previous one stopped.
type archiveDownload struct {
	a        *app
	info     archiveInfo
	method   string
	name     string
	file     *os.File
	hash     hash.Hash
	size     int64
	progress *transfer
	resumed  bool
}

func (d *archiveDownload) reset() error {
	d.hash.Reset()
	d.size = 0
	d.progress.received.Store(0)
	d.progress.base.Store(0)
	if err := d.file.Truncate(0); err != nil {
		return err
	}
	_, err := d.file.Seek(0, io.SeekStart)
	return err
}

// interrupted classifies a failure of the network part of an attempt.
func (d *archiveDownload) interrupted(ctx context.Context, err error) error {
	if d.a.ctx.Err() != nil {
		return d.a.ctx.Err()
	}
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		err = cause
	}
	return &retryableError{err}
}

// attempt sends one request and appends the response to the partial file.
func (d *archiveDownload) attempt() error {
	if d.info.Size > 0 && d.size == d.info.Size {
		return nil
	}
	limited, cancelLimit := context.WithTimeout(d.a.ctx, downloadTimeout)
	defer cancelLimit()
	ctx, cancel := context.WithCancelCause(limited)
	defer cancel(nil)
	timer := time.AfterFunc(d.a.stallTimeout, func() { cancel(fmt.Errorf("%w for %s", errStalled, d.a.stallTimeout)) })
	defer timer.Stop()

	response, err := d.a.send(ctx, d.method, d.info.URL, d.size)
	if err != nil {
		var status *httpError
		if errors.As(err, &status) {
			if status.code == http.StatusRequestedRangeNotSatisfiable {
				if err := d.reset(); err != nil {
					return err
				}
				return &retryableError{err}
			}
			if !retryableStatus(status.code) {
				return err
			}
		}
		return d.interrupted(ctx, err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusPartialContent {
		match := contentRange.FindStringSubmatch(response.Header.Get("Content-Range"))
		start := int64(-1)
		if len(match) > 1 {
			start, _ = strconv.ParseInt(match[1], 10, 64)
		}
		if start != d.size {
			if err := d.reset(); err != nil {
				return err
			}
			return &retryableError{errors.New("server returned an unexpected byte range")}
		}
		d.resumed = true
	} else if d.size > 0 {
		if err := d.reset(); err != nil {
			return err
		}
	}
	if d.info.Size <= 0 && response.ContentLength > 0 {
		d.progress.total.Store(d.size + response.ContentLength)
	}
	body := &watchReader{reader: response.Body, timer: timer, stall: d.a.stallTimeout}
	count, err := io.Copy(io.MultiWriter(d.file, d.hash, d.progress), body)
	d.size += count
	if err != nil {
		if body.err != nil {
			return d.interrupted(ctx, body.err)
		}
		return err
	}
	return nil
}

func (d *archiveDownload) verify() error {
	if d.info.Size > 0 && d.size != d.info.Size {
		return corruptError("download size mismatch for " + d.name)
	}
	if !strings.EqualFold(hex.EncodeToString(d.hash.Sum(nil)), d.info.Checksum) {
		return corruptError("SHA-256 mismatch for " + d.name)
	}
	return nil
}

func (a *app) sleep(delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-a.ctx.Done():
		return a.ctx.Err()
	case <-timer.C:
		return nil
	}
}

// removeStalePartials deletes abandoned partial downloads.
func removeStalePartials(directory, keep string) {
	entries, _ := os.ReadDir(directory)
	for _, entry := range entries {
		info, err := entry.Info()
		if entry.Name() != keep && err == nil && time.Since(info.ModTime()) > partialMaxAge {
			_ = os.Remove(filepath.Join(directory, entry.Name()))
		}
	}
}

// download fetches an archive into destination and returns its path. Retries
// and interrupted runs continue from the bytes already stored in
// AIR_SDKS/.asm-partial, which is named after the expected SHA-256 and
// removed on success or when the data fails verification.
func (a *app) download(info archiveInfo, method, name, destination string) (string, error) {
	info.Checksum = strings.TrimSpace(info.Checksum)
	checksum, err := hex.DecodeString(info.Checksum)
	if err != nil || len(checksum) != sha256.Size {
		return "", fmt.Errorf("invalid SHA-256 for %s", name)
	}
	root, err := a.root()
	if err != nil {
		return "", err
	}
	directory := filepath.Join(root, partialDirectory)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", err
	}
	defer os.Remove(directory)
	partial := filepath.Join(directory, strings.ToLower(info.Checksum)+".part")
	removeStalePartials(directory, filepath.Base(partial))

	file, err := os.OpenFile(partial, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return "", err
	}
	progress := &transfer{started: time.Now()}
	progress.total.Store(info.Size)
	d := &archiveDownload{a: a, info: info, method: method, name: name, file: file, hash: sha256.New(), progress: progress}
	existing, err := io.Copy(d.hash, file)
	if err != nil {
		file.Close()
		return "", err
	}
	d.size = existing
	progress.received.Store(existing)
	progress.base.Store(existing)
	d.resumed = existing > 0
	if info.Size > 0 && existing > info.Size {
		if err := d.reset(); err != nil {
			file.Close()
			return "", err
		}
		d.resumed = false
	}
	closed := false
	finish := func(err error) (string, error) {
		if !closed {
			file.Close()
			closed = true
		}
		var corrupt corruptError
		if d.size == 0 || errors.As(err, &corrupt) {
			_ = os.Remove(partial)
		}
		return "", err
	}

	if a.ui.interactive {
		a.ui.notice("Downloading " + name)
	}
	stop := a.ui.activity("Downloading "+name, progress)
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	restartedClean := false
	for attempt := 1; ; {
		err := d.attempt()
		if err == nil {
			err = d.verify()
			if err == nil {
				break
			}
			if d.resumed && !restartedClean {
				// A stale or damaged partial file: start over once.
				restartedClean, d.resumed = true, false
				if err := d.reset(); err != nil {
					return finish(err)
				}
				continue
			}
			return finish(err)
		}
		var retry *retryableError
		if !errors.As(err, &retry) {
			return finish(err)
		}
		if attempt == downloadAttempts {
			return finish(fmt.Errorf("%w (after %d attempts; run again to continue the download)", err, attempt))
		}
		delay := a.retryDelay << (attempt - 1)
		stop()
		stop = nil
		a.ui.notice(fmt.Sprintf("Download interrupted: %v. Retrying %d/%d in %s.", err, attempt+1, downloadAttempts, delay))
		if err := a.sleep(delay); err != nil {
			return finish(err)
		}
		attempt++
		stop = a.ui.activity("Downloading "+name, progress)
	}
	if err := file.Close(); err != nil {
		closed = true
		return finish(err)
	}
	closed = true
	output, err := os.CreateTemp(destination, ".asm-download-*.zip")
	if err != nil {
		return finish(err)
	}
	target := output.Name()
	output.Close()
	if err := os.Rename(partial, target); err != nil {
		_ = os.Remove(target)
		return finish(err)
	}
	return target, nil
}
