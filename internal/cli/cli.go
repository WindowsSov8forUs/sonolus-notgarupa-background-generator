// Package cli holds the flags and helpers every generator command shares.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/internal/bestdori"
	"github.com/WindowsSov8forUs/sonolus-pack-go/packer"
)

type Common struct {
	Out     string
	Cache   string
	Regions []string
	Offline bool
	Verbose bool
	Version bool
	Workers int
}

// Register adds the shared flags to fs.
func (c *Common) Register(fs *flag.FlagSet) {
	fs.StringVar(&c.Out, "out", "out", "output directory: <out>/source (sonolus-pack source tree, recreated) and <out>/pack (servable pack)")
	fs.StringVar(&c.Cache, "cache", ".cache", "download cache directory")
	fs.Func("regions", "Bestdori server priority order, comma-separated; each bundle comes from the first server that has it (default "+strings.Join(bestdori.DefaultRegions, ",")+")", func(v string) error {
		c.Regions = nil
		for _, r := range strings.Split(v, ",") {
			if r = strings.TrimSpace(r); r != "" {
				c.Regions = append(c.Regions, r)
			}
		}
		return nil
	})
	fs.BoolVar(&c.Offline, "offline", false, "only use cached downloads")
	fs.BoolVar(&c.Verbose, "v", false, "log downloads")
	fs.BoolVar(&c.Version, "version", false, "print the version and exit")
	fs.IntVar(&c.Workers, "j", 0, "items built in parallel (0: the CPU count)")
}

// Logf returns the download logger (nil unless -v).
func (c *Common) Logf() func(string, ...any) {
	if !c.Verbose {
		return nil
	}
	return func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }
}

type logger struct{}

func (logger) Info(args ...any) { fmt.Fprintln(os.Stderr, append([]any{"[pack]"}, args...)...) }
func (logger) Warning(args ...any) {
	fmt.Fprintln(os.Stderr, append([]any{"[pack][warn]"}, args...)...)
}

// Pack runs sonolus-pack-go on a source tree.
func Pack(source, output string) error {
	return packer.Pack(context.Background(), packer.Options{Input: source, Output: output, Logger: logger{}})
}

// WriteJSON writes v as indented JSON, creating the parent directory.
func WriteJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Fatal prints an error and exits.
func Fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
