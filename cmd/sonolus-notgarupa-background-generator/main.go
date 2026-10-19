// Command sonolus-notgarupa-background-generator builds every NotGarupa
// background item (see package background) as a sonolus-pack source tree and
// a servable pack.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	background "github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/internal/cli"
)

var version = "dev"

func main() {
	var common cli.Common
	fs := flag.NewFlagSet("sonolus-notgarupa-background-generator", flag.ExitOnError)
	common.Register(fs)
	assetVersion := fs.String("asset-version", background.PinnedAssetVersion, "content CDN asset version of the stage-effect bundles")
	_ = fs.Parse(os.Args[1:])
	if common.Version {
		fmt.Println(version)
		return
	}
	source := filepath.Join(common.Out, "source")
	tree, err := background.CreateSourceTree(source)
	if err != nil {
		cli.Fatal(err)
	}
	opts := background.Options{
		Source:  background.Source{CacheDir: common.Cache, Regions: common.Regions, AssetVersion: *assetVersion, Offline: common.Offline, Log: common.Logf()},
		Workers: common.Workers,
	}
	count := 0
	err = background.Generate(context.Background(), opts, func(r *background.Result) error {
		count++
		extra := ""
		if len(r.StageEffect) > 0 {
			extra = fmt.Sprintf(", stage effect %v", r.StageEffect)
		}
		fmt.Printf("background %s: %s/%s (%.4f px per UI unit%s)\n", r.Item.Name, r.Bundle, r.File, r.PxPerUnit, extra)
		return tree.Add(r)
	})
	if err != nil {
		cli.Fatal(err)
	}
	if err := cli.Pack(source, filepath.Join(common.Out, "pack")); err != nil {
		cli.Fatal(err)
	}
	fmt.Printf("%d backgrounds -> %s\n", count, common.Out)
}
