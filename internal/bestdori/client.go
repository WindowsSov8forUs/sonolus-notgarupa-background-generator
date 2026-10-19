// Package bestdori downloads asset bundle rips from bestdori.com and keeps a
// local disk cache so that repeated generator runs work offline.
//
// Servers have exclusive assets, so a bundle is looked up on every server in
// priority order (Regions) and taken from the first one that has it; all
// files of a bundle then come from that server.
package bestdori

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DefaultRoot      = "https://bestdori.com"
	defaultUserAgent = "Mozilla/5.0 (compatible; sonolus-notgarupa-generator)"
)

// DefaultRegions is the server priority order: jp first, then the servers
// that have assets jp lacks.
var DefaultRegions = []string{"jp", "en", "tw", "cn", "kr"}

// ErrNotFound reports an asset that a server does not have.
var ErrNotFound = errors.New("not found")

// Client fetches Bestdori explorer manifests and ripped bundle files.
type Client struct {
	Root string
	// Regions is the server priority order (nil: DefaultRegions).
	Regions  []string
	CacheDir string
	// Offline makes every request fail unless it is already cached.
	Offline bool
	HTTP    *http.Client
	Log     func(format string, args ...any)
	// Ctx cancels downloads (nil: context.Background()).
	Ctx context.Context

	mu        sync.Mutex
	manifests map[string]manifest // bundle → manifest of the server that has it
	files     map[string][]byte   // URL → body, so one run downloads a file once
}

type manifest struct {
	region string
	files  []string
}

func New(cacheDir string) *Client {
	return &Client{
		Root:     DefaultRoot,
		CacheDir: cacheDir,
		HTTP:     &http.Client{Timeout: 90 * time.Second},
	}
}

func (c *Client) regions() []string {
	if len(c.Regions) == 0 {
		return DefaultRegions
	}
	return c.Regions
}

// Manifest returns the file list of a bundle, e.g. "ingameskin/noteskin/skin00",
// from the first server in priority order that has it.
func (c *Client) Manifest(bundle string) ([]string, error) {
	m, err := c.manifest(bundle)
	return m.files, err
}

// Region returns the server a bundle is taken from.
func (c *Client) Region(bundle string) (string, error) {
	m, err := c.manifest(bundle)
	return m.region, err
}

func (c *Client) manifest(bundle string) (manifest, error) {
	c.mu.Lock()
	m, ok := c.manifests[bundle]
	c.mu.Unlock()
	if ok {
		return m, nil
	}
	for _, region := range c.regions() {
		u := fmt.Sprintf("%s/api/explorer/%s/assets/%s.json", c.Root, region, bundle)
		data, err := c.get(u, filepath.Join("manifest", region, bundle+".json"))
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return manifest{}, err
		}
		m = manifest{region: region}
		if err := json.Unmarshal(data, &m.files); err != nil {
			return manifest{}, fmt.Errorf("bestdori manifest %s/%s: %w", region, bundle, err)
		}
		c.mu.Lock()
		if c.manifests == nil {
			c.manifests = map[string]manifest{}
		}
		c.manifests[bundle] = m
		c.mu.Unlock()
		return m, nil
	}
	return manifest{}, fmt.Errorf("bestdori bundle %s is on none of the servers %v: %w", bundle, c.regions(), ErrNotFound)
}

// File returns one ripped file of a bundle, from the server the bundle is
// taken from.
func (c *Client) File(bundle, file string) ([]byte, error) {
	region, err := c.Region(bundle)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(file, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	u := fmt.Sprintf("%s/assets/%s/%s_rip/%s", c.Root, region, bundle, strings.Join(parts, "/"))
	return c.get(u, filepath.Join("assets", region, bundle+"_rip", file))
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

// missingSuffix marks a cached "server does not have this" answer, so that an
// offline run resolves bundles to the same server as the online run did.
const missingSuffix = ".missing"

func (c *Client) get(u, cacheRel string) ([]byte, error) {
	c.mu.Lock()
	data, ok := c.files[u]
	c.mu.Unlock()
	if ok {
		return data, nil
	}
	data, err := c.load(u, cacheRel)
	if err == nil {
		c.mu.Lock()
		if c.files == nil {
			c.files = map[string][]byte{}
		}
		c.files[u] = data
		c.mu.Unlock()
	}
	return data, err
}

func (c *Client) load(u, cacheRel string) ([]byte, error) {
	var cachePath string
	if c.CacheDir != "" {
		cachePath = filepath.Join(c.CacheDir, cacheRel)
		if data, err := os.ReadFile(cachePath); err == nil {
			return data, nil
		}
	}
	if c.Offline {
		if cachePath != "" {
			if _, err := os.Stat(cachePath + missingSuffix); err == nil {
				return nil, fmt.Errorf("%s: %w (cached)", u, ErrNotFound)
			}
		}
		return nil, fmt.Errorf("offline and not cached: %s", u)
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		data, retry, err := c.fetch(u)
		if err == nil {
			if cachePath != "" {
				writeCache(cachePath, data)
				_ = os.Remove(cachePath + missingSuffix)
			}
			return data, nil
		}
		if errors.Is(err, ErrNotFound) {
			if cachePath != "" {
				writeCache(cachePath+missingSuffix, nil)
			}
			return nil, err
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
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, fmt.Errorf("GET %s: %w", u, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode >= 500, fmt.Errorf("GET %s: HTTP %d", u, resp.StatusCode)
	}
	// Bestdori answers unknown asset paths with its HTML app shell.
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		return nil, false, fmt.Errorf("GET %s: got HTML instead of an asset: %w", u, ErrNotFound)
	}
	return data, false, nil
}
