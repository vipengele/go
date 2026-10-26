// Package updatetest builds a fake release for tests of [go.vipengele.org/update]: the archive a
// release publishes for a platform, the checksums file that lists its digest, and an HTTP server
// that serves them as GitHub does.
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
// [NewRelease] builds a [Release] for the running platform, and [NewServer] starts a [Server] that
// publishes it, closed when the test ends. The server answers the latest-release redirect and the
// REST API from one address, so a source reads the release by the redirect unless [APIOnly] makes
// that page answer 404. [BadChecksum] lists a wrong digest, [MissingAsset] removes an asset, and
// [Slow] holds every request to a [Target] until a gate channel closes. [Server.Requests] counts the
// requests a Target received.
//
// A test points its source at the server; a GitHubSource, for example, is built from its addresses:
//
//	rel := updatetest.NewRelease(t, "owner", "repo", "v1.2.3", "tool")
//	srv := updatetest.NewServer(t, rel, updatetest.BadChecksum())
//	src := update.GitHubSource{
//		Owner: "owner", Repo: "repo",
//		WebURL: srv.WebURL(), APIURL: srv.APIURL(), HTTPClient: srv.Client(),
//	}
//
// The package does not import [go.vipengele.org/update], so tests of that package can import it.
package updatetest
