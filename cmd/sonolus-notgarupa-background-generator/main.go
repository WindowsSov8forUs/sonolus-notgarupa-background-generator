// Command sonolus-notgarupa-background-generator builds the NotGarupa
// background pack (see package background) as a sonolus-pack source tree and
// a servable pack.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	background "github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator"
	"github.com/WindowsSov8forUs/sonolus-notgarupa-background-generator/internal/cli"
)

var version = "dev"

func main() {
	var common cli.Common
	fs := flag.NewFlagSet("sonolus-notgarupa-background-generator", flag.ExitOnError)
	common.Register(fs)
	var o background.Options
	skins := fs.String("bg", strings.Join(background.DefaultBackgrounds, ","), "comma-separated ordinary background bundles (ingameskin/bgskin/<id>); the #HABAHIRO background ("+background.HabahiroBackground+") is always built")
	fever := fs.Bool("fever", true, "also emit the liveBG_fever variant when the bundle has one")
	dark := fs.Bool("dark-cover", true, "bake the DarkCover subtraction")
	fs.Float64Var(&o.PxPerUnit, "px-per-unit", 0, "output pixels per UI unit (default: native texture density)")
	fs.Float64Var(&o.Height, "height", 1640, "canvas height in UI units (symmetric around the screen centre; 1640 covers the whole widget)")
	_ = fs.Parse(os.Args[1:])
	if common.Version {
		fmt.Println(version)
		return
	}
	o.Backgrounds = strings.Split(*skins, ",")
	o.NoFever, o.NoDarkCover = !*fever, !*dark
	o.Source = background.Source{CacheDir: common.Cache, Regions: common.Regions, Offline: common.Offline, Log: common.Logf()}

	results, err := background.Generate(context.Background(), o)
	if err != nil {
		cli.Fatal(err)
	}
	if err := background.WriteSource(common.Out, results...); err != nil {
		cli.Fatal(err)
	}
	if common.Pack != "" {
		if err := cli.Pack(common.Out, common.Pack); err != nil {
			cli.Fatal(err)
		}
	}
	for _, r := range results {
		fmt.Printf("background %s: %s/%s (%.4f px per UI unit)\n", r.Item.Name, r.Bundle, r.File, r.PxPerUnit)
	}
	fmt.Printf("%d backgrounds -> %s\n", len(results), common.Out)
}
