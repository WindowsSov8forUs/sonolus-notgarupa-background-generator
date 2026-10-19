// Package bestdori downloads asset bundle rips from bestdori.com and keeps a
// local disk cache so that repeated generator runs work offline.
package bestdori

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultRoot      = "https://bestdori.com"
	DefaultRegion    = "jp"
	defaultUserAgent = "Mozilla/5.0 (compatible; sonolus-notgarupa-generator)"
)

// Client fetches Bestdori explorer manifests and ripped bundle files.
type Client struct {
	Root     string
	Region   string
	CacheDir string
	// Offline makes every request fail unless it is already cached.
	Offline bool
	HTTP    *http.Client
	Log     func(format string, args ...any)
	// Ctx cancels downloads (nil: context.Background()).
	Ctx context.Context
}

func New(cacheDir string) *Client {
	return &Client{
		Root:     DefaultRoot,
		Region:   DefaultRegion,
		CacheDir: cacheDir,
		HTTP:     &http.Client{Timeout: 90 * time.Second},
	}
}

// Manifest returns the file list of a bundle, e.g. "ingameskin/noteskin/skin00".
func (c *Client) Manifest(bundle string) ([]string, error) {
	u := fmt.Sprintf("%s/api/explorer/%s/assets/%s.json", c.Root, c.Region, bundle)
	data, err := c.get(u, filepath.Join("manifest", c.Region, bundle+".json"))
	if err != nil {
		return nil, err
	}
	var files []string
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("bestdori manifest %s: %w", bundle, err)
	}
	return files, nil
}

// File returns one ripped file of a bundle.
func (c *Client) File(bundle, file string) ([]byte, error) {
	parts := strings.Split(file, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	u := fmt.Sprintf("%s/assets/%s/%s_rip/%s", c.Root, c.Region, bundle, strings.Join(parts, "/"))
	return c.get(u, filepath.Join("assets", c.Region, bundle+"_rip", file))
}

// FindFile resolves a file name case-insensitively against a bundle manifest.
func (c *Client) FindFile(bundle, name string) (string, error) {
	files, err := c.Manifest(bundle)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if strings.EqualFold(f, name) {
			return f, nil
		}
	}
	return "", fmt.Errorf("bestdori bundle %s has no file %q", bundle, name)
}

// FileByName combines FindFile and File.
func (c *Client) FileByName(bundle, name string) ([]byte, error) {
	f, err := c.FindFile(bundle, name)
	if err != nil {
		return nil, err
	}
	return c.File(bundle, f)
}

func (c *Client) get(u, cacheRel string) ([]byte, error) {
	var cachePath string
	if c.CacheDir != "" {
		cachePath = filepath.Join(c.CacheDir, cacheRel)
		if data, err := os.ReadFile(cachePath); err == nil {
			return data, nil
		}
	}
	if c.Offline {
		return nil, fmt.Errorf("offline and not cached: %s", u)
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		data, retry, err := c.fetch(u)
		if err == nil {
			if cachePath != "" {
				writeCache(cachePath, data)
			}
			return data, nil
		}
		lastErr = err
		if !retry {
			break
		}
		select {
		case <-c.ctx().Done():
			return nil, c.ctx().Err()
		case <-time.After(time.Duration(attempt) * 700 * time.Millisecond):
		}
	}
	return nil, lastErr
}

func (c *Client) ctx() context.Context {
	if c.Ctx == nil {
		return context.Background()
	}
	return c.Ctx
}

// writeCache stores a download atomically so that concurrent generators
// sharing a cache directory never read a partial file.
func writeCache(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), path) != nil {
		_ = os.Remove(tmp.Name())
	}
}

func (c *Client) fetch(u string) ([]byte, bool, error) {
	if c.Log != nil {
		c.Log("GET %s", u)
	}
	req, err := http.NewRequestWithContext(c.ctx(), http.MethodGet, u, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if c.ctx().Err() != nil {
			return nil, false, c.ctx().Err()
		}
		return nil, true, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, fmt.Errorf("GET %s: %w", u, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode >= 500, fmt.Errorf("GET %s: HTTP %d", u, resp.StatusCode)
	}
	// Bestdori answers unknown asset paths with its HTML app shell.
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		return nil, false, fmt.Errorf("GET %s: got HTML instead of an asset (missing file?)", u)
	}
	return data, false, nil
}
