// Package deepcheck performs an on-demand, comprehensive health probe across
// the database, storage, XMLTV source reachability, and data freshness.
package deepcheck

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// CheckStatus is the SUCCESS/FAILURE indicator for a single check or the
// overall report.
type CheckStatus string

const (
	StatusSuccess CheckStatus = "SUCCESS"
	StatusFailure CheckStatus = "FAILURE"
)

// xmltvProbeTimeout is the per-request timeout applied to the XMLTV upstream
// HEAD/GET probe so a slow source cannot stall the whole endpoint.
const xmltvProbeTimeout = 5 * time.Second

// CheckResult is the outcome of a single named check.
type CheckResult struct {
	Name   string      `json:"name"`
	Status CheckStatus `json:"status"`
	Error  string      `json:"error,omitempty"`
	Info   string      `json:"info,omitempty"`
}

// Report aggregates the results of all checks plus a top-level status.
type Report struct {
	Status CheckStatus   `json:"status"`
	Checks []CheckResult `json:"checks"`
}

// DBProbe is the narrow interface deepcheck needs from the database layer.
type DBProbe interface {
	Ping(ctx context.Context) error
	DeepCheck(ctx context.Context) DBCheckResults
}

// DBCheckResults holds the data the DB layer can gather. Per-field errors are
// stored so the caller can render every check independently — DBCheckResults
// itself never carries a fatal error.
type DBCheckResults struct {
	WritableErr     error
	FTSErr          error
	ChannelCount    int
	AiringCount     int
	LastRefresh     time.Time
	ChannelCountErr error
	AiringCountErr  error
}

// Checker runs the suite of deep checks.
type Checker struct {
	DB            DBProbe
	HTTPClient    *http.Client
	XMLTVURL      string
	PollInterval  time.Duration
	DBPath        string
	ImageCacheDir string
	Now           func() time.Time
}

// Run executes every check in order and returns the aggregated report. One
// failing check never stops subsequent checks from running.
func (c *Checker) Run(ctx context.Context) Report {
	now := c.Now
	if now == nil {
		now = time.Now
	}

	checks := make([]CheckResult, 0, 9)

	// 1. database — SELECT 1 via Ping.
	checks = append(checks, runCheck("database", func() (string, error) {
		return "", c.DB.Ping(ctx)
	}))

	// Pull the bulk DB results once; they cover the next 4 checks.
	dbResults := c.DB.DeepCheck(ctx)

	// 2. database_writable
	checks = append(checks, runCheck("database_writable", func() (string, error) {
		return "", dbResults.WritableErr
	}))

	// 3. fts
	checks = append(checks, runCheck("fts", func() (string, error) {
		return "", dbResults.FTSErr
	}))

	// 4. data_presence
	checks = append(checks, runCheck("data_presence", func() (string, error) {
		if dbResults.ChannelCountErr != nil {
			return "", fmt.Errorf("channels: %w", dbResults.ChannelCountErr)
		}
		if dbResults.AiringCountErr != nil {
			return "", fmt.Errorf("airings: %w", dbResults.AiringCountErr)
		}
		if dbResults.ChannelCount == 0 {
			return "", fmt.Errorf("no channels in database")
		}
		if dbResults.AiringCount == 0 {
			return "", fmt.Errorf("no airings in database")
		}
		return "", nil
	}))

	// 5. data_freshness
	checks = append(checks, runCheck("data_freshness", func() (string, error) {
		if dbResults.LastRefresh.IsZero() {
			return "", fmt.Errorf("no refresh has completed yet")
		}
		threshold := 2 * c.PollInterval
		age := now().Sub(dbResults.LastRefresh)
		if age > threshold {
			return "", fmt.Errorf("last refresh %s ago exceeds %s", age.Truncate(time.Second), threshold)
		}
		return "", nil
	}))

	// 6. xmltv_url
	checks = append(checks, c.probeXMLTV(ctx))

	// 7. disk_data — stat parent of DBPath, then write-probe.
	checks = append(checks, probeDisk("disk_data", filepath.Dir(c.DBPath)))

	// 8. disk_tmp
	checks = append(checks, probeDisk("disk_tmp", os.TempDir()))

	// 9. image_cache — write-probe in IMAGE_CACHE_DIR/channels.
	checks = append(checks, writeProbe("image_cache", filepath.Join(c.ImageCacheDir, "channels")))

	overall := StatusSuccess
	for _, r := range checks {
		if r.Status != StatusSuccess {
			overall = StatusFailure
			break
		}
	}
	return Report{Status: overall, Checks: checks}
}

// runCheck wraps a probe function and turns the (info, err) result into a
// CheckResult with the supplied name.
func runCheck(name string, fn func() (string, error)) CheckResult {
	info, err := fn()
	if err != nil {
		return CheckResult{Name: name, Status: StatusFailure, Error: err.Error(), Info: info}
	}
	return CheckResult{Name: name, Status: StatusSuccess, Info: info}
}

// probeXMLTV issues a HEAD request to c.XMLTVURL with a fresh 5s timeout. The
// HEAD and GET fallback requests both carry the same Accept and User-Agent
// headers the real XMLTV fetcher uses (see internal/xmltv/parser.go) so hosts
// that key on those don't reject the probe with 406. If HEAD returns anything
// outside the 2xx/3xx success range it falls back to a single GET with
// Range: bytes=0-0. Final response status outside 2xx/3xx is a failure.
func (c *Checker) probeXMLTV(parent context.Context) CheckResult {
	const name = "xmltv_url"
	ctx, cancel := context.WithTimeout(parent, xmltvProbeTimeout)
	defer cancel()

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	wrapErr := func(method string, err error) error {
		return fmt.Errorf("%s %s: %w", method, c.XMLTVURL, err)
	}
	wrapStatus := func(method string, status int) error {
		return fmt.Errorf("%s %s: unexpected status %d", method, c.XMLTVURL, status)
	}

	baseHeaders := map[string]string{
		"Accept":     "text/xml, application/xml, */*",
		"User-Agent": "xmltvguide/1.0",
	}

	doRequest := func(method string, extraHeaders map[string]string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, c.XMLTVURL, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range baseHeaders {
			req.Header.Set(k, v)
		}
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}
		return client.Do(req)
	}

	resp, err := doRequest(http.MethodHead, nil)
	if err != nil {
		return CheckResult{Name: name, Status: StatusFailure, Error: wrapErr(http.MethodHead, err).Error()}
	}
	// Drain and close to allow connection reuse, then decide.
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	method := http.MethodHead
	status := resp.StatusCode

	if status < 200 || status >= 400 {
		// HEAD failed — some servers reject HEAD with 400, 403, 405, 406, 501,
		// etc. Fall back to a single GET with Range: bytes=0-0 to keep the
		// probe cheap.
		resp2, err2 := doRequest(http.MethodGet, map[string]string{"Range": "bytes=0-0"})
		if err2 != nil {
			return CheckResult{Name: name, Status: StatusFailure, Error: wrapErr(http.MethodGet, err2).Error()}
		}
		_, _ = io.Copy(io.Discard, resp2.Body)
		_ = resp2.Body.Close()
		method = http.MethodGet
		status = resp2.StatusCode
	}

	if status < 200 || status >= 400 {
		return CheckResult{Name: name, Status: StatusFailure, Error: wrapStatus(method, status).Error()}
	}
	return CheckResult{Name: name, Status: StatusSuccess}
}

// probeDisk stats the directory, reports its mode+uid in Info, then performs
// a write probe.
func probeDisk(name, dir string) CheckResult {
	fi, err := os.Stat(dir)
	if err != nil {
		return CheckResult{Name: name, Status: StatusFailure, Error: fmt.Sprintf("stat %s: %s", dir, err.Error())}
	}
	info := fmt.Sprintf("mode=%04o uid=%s", fi.Mode().Perm()|fi.Mode()&os.ModeSticky, statUID(fi))
	wp := writeProbe(name, dir)
	if wp.Status != StatusSuccess {
		wp.Info = info
		return wp
	}
	wp.Info = info
	return wp
}

// writeProbe creates a temp file in dir, writes a byte, then removes it.
// Always attempts to Remove, even when the write fails.
func writeProbe(name, dir string) CheckResult {
	f, err := os.CreateTemp(dir, ".deepcheck-*")
	if err != nil {
		return CheckResult{Name: name, Status: StatusFailure, Error: fmt.Sprintf("create %s: %s", dir, err.Error())}
	}
	path := f.Name()
	_, writeErr := f.Write([]byte{0})
	_ = f.Close()
	removeErr := os.Remove(path)
	if writeErr != nil {
		return CheckResult{Name: name, Status: StatusFailure, Error: fmt.Sprintf("write %s: %s", dir, writeErr.Error())}
	}
	if removeErr != nil {
		return CheckResult{Name: name, Status: StatusFailure, Error: fmt.Sprintf("remove %s: %s", dir, removeErr.Error())}
	}
	return CheckResult{Name: name, Status: StatusSuccess}
}

// statUID returns the numeric owner UID of fi as a string. On platforms that
// don't expose syscall.Stat_t, returns "?".
func statUID(fi os.FileInfo) string {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d", st.Uid)
	}
	return "?"
}
