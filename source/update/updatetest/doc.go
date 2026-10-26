// Package updatetest builds the files of a fake release for tests of [go.vipengele.org/update]:
// the archive a release publishes for a platform, and the checksums file that lists its digest.
//
// [BuildArchive] writes a set of [Entry] values as a gzip-compressed tar archive or a zip archive,
// chosen by the same extension the updater is configured with, Config.AssetExt: [ExtTarGz] or
// [ExtZip]. Its output is deterministic, so building the same entries twice yields the same bytes
// and the same digest. [Checksums] renders the checksums file for a set of assets, one
// `<sha256 hex>  <name>` line each, as sha256sum writes it and as the updater parses it.
//
// Every entry is a regular file. An entry name is written as given, so the kit builds no
// symbolic link and does not guard against a name that leaves the extraction directory.
//
// A test builds an archive and lists it in a checksums file:
//
//	archive, err := updatetest.BuildArchive(updatetest.ExtTarGz, []updatetest.Entry{
//		{Name: "tool", Mode: 0o755, Body: []byte("#!/bin/sh\necho v1.2.3\n")},
//	})
//	if err != nil {
//		t.Fatal(err)
//	}
//	sums := updatetest.Checksums(map[string][]byte{"tool_linux_amd64.tar.gz": archive})
//
// The package does not import [go.vipengele.org/update], so tests of that package can import it.
package updatetest
