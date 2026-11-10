package installscript

import "errors"

// ErrZipAsset reports a Config whose AssetTemplate holds {ext} with the AssetExt "zip" given to
// [Render]. The sh script extracts with tar alone, so a zip asset has no sh script;
// [RenderPowerShell] renders one.
var ErrZipAsset = errors.New("a zip asset has no sh install script")
