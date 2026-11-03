package installscript

import "errors"

// ErrZipAsset reports a Config whose AssetTemplate holds {ext} with the AssetExt "zip". The script
// extracts with tar alone, so a zip asset has no rendered script.
var ErrZipAsset = errors.New("a zip asset has no install script")
