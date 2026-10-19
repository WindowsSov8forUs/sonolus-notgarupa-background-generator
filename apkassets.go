package background

// Assets built into the original game's APK (not available as downloadable
// asset bundles), shipped with the generator under assets/apk: the stage's
// DarkCover texture.

import "embed"

//go:embed assets/apk
var apkAssets embed.FS

// apkFile returns an APK asset by its path under assets/apk.
func apkFile(rel string) ([]byte, error) { return apkAssets.ReadFile("assets/apk/" + rel) }
