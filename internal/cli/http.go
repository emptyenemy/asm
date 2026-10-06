package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type httpError struct {
	code            int
	status, address string
}

// Error reports the status line of the response that was rejected.
func (e *httpError) Error() string { return fmt.Sprintf("HTTP %s from %s", e.status, e.address) }

// requestError keeps a failed request's cause for errors.Is and errors.As
// but reads like a sentence. Go's *url.Error repeats the method and the
// whole address, and a refused connection carries the operating system's
// own paragraph; the host and what went wrong is what a person can act on.
type requestError struct {
	text string
	err  error
}

func (e *requestError) Error() string { return e.text }

func (e *requestError) Unwrap() error { return e.err }

func shortRequestError(err error) error {
	var failed *url.Error
	if !errors.As(err, &failed) || errors.Is(err, context.Canceled) {
		return err
	}
	host := failed.URL
	if parsed, parseErr := url.Parse(failed.URL); parseErr == nil && parsed.Host != "" {
		host = parsed.Host
	}
	var dns *net.DNSError
	var dial *net.OpError
	switch {
	case failed.Timeout():
		return &requestError{"no answer from " + host, err}
	case errors.As(err, &dns):
		return &requestError{"cannot resolve " + host, err}
	case errors.As(err, &dial) && dial.Op == "dial":
		return &requestError{"cannot connect to " + host, err}
	}
	return &requestError{fmt.Sprintf("request to %s failed: %v", host, failed.Err), err}
}

// request performs a metadata request, which never continues a partial body.
func (a *app) request(ctx context.Context, method, address string) (*http.Response, error) {
	return a.send(ctx, method, address, -1)
}

// send performs a request. A negative offset marks a metadata request; a
// non-negative one is an archive download that continues at that byte.
func (a *app) send(ctx context.Context, method, address string, offset int64) (*http.Response, error) {
	var body io.Reader
	if method == "POST" {
		body = strings.NewReader("acceptedLicense=true")
	}
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "asm/"+a.version)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if offset >= 0 {
		req.Header.Set("Accept-Encoding", "identity")
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, shortRequestError(err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, &httpError{response.StatusCode, response.Status, address}
	}
	return response, nil
}

// metadata fetches a JSON document that is known to be small, under the given
// timeout and activity label, and refuses a body larger than 16 MiB.
func (a *app) metadata(address, label string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(a.ctx, timeout)
	defer cancel()
	stop := a.ui.activity(label, nil)
	defer stop()
	response, err := a.request(ctx, "GET", address)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16*1024*1024+1))
	if len(data) > 16*1024*1024 {
		return nil, errors.New("SDK metadata response is too large")
	}
	return data, err
}
