package update

import (
	"context"
	"errors"
	"path/filepath"
)

// Apply replaces the running executable with the binary of the release sel names, keeping the
// previous binary as a backup. It runs, in order and stopping at the first error, which it returns
// as the step returned it:
//
//  1. [Config.CheckManaged], so an install an external manager owns is refused.
//  2. [Config.CheckWritable], so an update that cannot be installed is not fetched.
//  3. [Config.Download] of sel with opts, which verifies the checksums file's signature with the
//     trusted keys and the archive against its checksum.
//  4. [Config.Target], the file the update replaces.
//  5. [Config.Extract] of the archive into Target's directory, so that the rename into place
//     stays on one filesystem.
//  6. [Config.Install] of the staged binary.
//
// Steps 1 and 2 make no request, so a refused install never reaches the network. Nothing at the
// executable's path changes before step 6, and no file Apply creates is left behind by a failure:
// Extract closes the archive and Install removes the staged binary, whether they succeed or fail,
// and Apply closes the archive itself when Target fails.
//
// The returned [*Installed] still holds the backup. The caller runs its [PostUpdate] hooks with
// [Installed.Path] and then settles the update with [Installed.Discard] or [Installed.Rollback];
// Apply does neither.
func (c Config) Apply(ctx context.Context, sel Selection, opts ...DownloadOption) (*Installed, error) {
	return c.apply(ctx, sel, defaultSteps(), opts...)
}

// apply is [Config.Apply] through s.
func (c Config) apply(ctx context.Context, sel Selection, s fsSteps, opts ...DownloadOption) (*Installed, error) {
	if err := c.CheckManaged(); err != nil {
		return nil, err
	}

	if err := c.checkWritable(s); err != nil {
		return nil, err
	}

	arc, err := c.Download(ctx, sel, opts...)
	if err != nil {
		return nil, err
	}

	target, err := c.target(s)
	if err != nil {
		return nil, errors.Join(err, arc.Close())
	}

	staged, err := c.Extract(ctx, arc, filepath.Dir(target))
	if err != nil {
		return nil, err
	}

	return c.install(staged, s)
}
