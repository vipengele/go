package update

import (
	"context"
	"errors"
	"path/filepath"

	"go.vipengele.org/cli/spec/version"
)

// ApplyOption configures one call to [Config.Apply].
type ApplyOption func(*applyOptions)

// applyOptions is what the [ApplyOption]s of one call to [Config.Apply] set.
type applyOptions struct {
	allowDowngrade bool
	download       []DownloadOption
}

// AllowDowngrade has [Config.Apply] install a release below the downgrade floor: it skips
// [Config.CheckDowngrade], and the State load it makes, entirely.
func AllowDowngrade() ApplyOption {
	return func(o *applyOptions) {
		o.allowDowngrade = true
	}
}

// WithDownloadOptions has [Config.Apply] pass opts to its [Config.Download]. Several calls add up,
// in the order given.
func WithDownloadOptions(opts ...DownloadOption) ApplyOption {
	return func(o *applyOptions) {
		o.download = append(o.download, opts...)
	}
}

// Apply replaces the running executable with the binary of the release sel names, keeping the
// previous binary as a backup. It resolves the Config, returning its validation error, and then
// runs, in order and stopping at the first error, which it returns as the step returned it:
//
//  1. [Config.CheckManaged], so an install an external manager owns is refused.
//  2. [Config.CheckWritable], so an update that cannot be installed is not fetched.
//  3. [Config.CheckDowngrade] of sel's Tag, so a release below the highest version applied or
//     running is refused unless [AllowDowngrade] is given, which skips this step.
//  4. [Config.Download] of sel with the options of [WithDownloadOptions], which verifies the
//     checksums file's signature with the trusted keys and the archive against its checksum.
//  5. [Config.Target], the file the update replaces.
//  6. [Config.Extract] of the archive into Target's directory, so that the rename into place
//     stays on one filesystem.
//  7. [Config.Install] of the staged binary.
//  8. The record: under the Locker, the State is loaded, its HighestApplied raised to sel's Tag
//     when the Tag is newer in the order of [version.Newer], and the whole State saved back, so
//     its other fields are kept.
//
// Steps 1 to 3 make no request, so a refused install never reaches the network. The downgrade
// guard fails closed: a State that cannot be loaded refuses the install with the StateStore's
// error, rather than guess at the floor. The record fails open: a failed lock, load or save is
// ignored and Apply still returns the [*Installed], since the installed binary's Version floors
// the next update as the watermark would. Nothing at the executable's path changes before step 7,
// and no file Apply creates is left behind by a failure: Extract closes the archive and Install
// removes the staged binary, whether they succeed or fail, and Apply closes the archive itself
// when Target fails.
//
// The returned [*Installed] still holds the backup. The caller runs its [PostUpdate] hooks with
// [Installed.Path] and then settles the update with [Installed.Discard] or [Installed.Rollback];
// Apply does neither.
func (c Config) Apply(ctx context.Context, sel Selection, opts ...ApplyOption) (*Installed, error) {
	return c.apply(ctx, sel, defaultSteps(), opts...)
}

// apply is [Config.Apply] through s.
func (c Config) apply(ctx context.Context, sel Selection, s fsSteps, opts ...ApplyOption) (*Installed, error) {
	var o applyOptions
	for _, opt := range opts {
		opt(&o)
	}

	r, err := c.resolve()
	if err != nil {
		return nil, err
	}

	if err := r.CheckManaged(); err != nil {
		return nil, err
	}

	if err := r.checkWritable(s); err != nil {
		return nil, err
	}

	if !o.allowDowngrade {
		if err := r.CheckDowngrade(ctx, sel.Tag); err != nil {
			return nil, err
		}
	}

	arc, err := r.Download(ctx, sel, o.download...)
	if err != nil {
		return nil, err
	}

	target, err := r.target(s)
	if err != nil {
		return nil, errors.Join(err, arc.Close())
	}

	staged, err := r.Extract(ctx, arc, filepath.Dir(target))
	if err != nil {
		return nil, err
	}

	installed, err := r.install(staged, s)
	if err != nil {
		return nil, err
	}

	recordApplied(ctx, r, sel.Tag)

	return installed, nil
}

// recordApplied raises the HighestApplied of r's State to tag when tag is newer, loading and saving
// the whole State under r's Locker. It ignores every failure, leaving the State as it was.
func recordApplied(ctx context.Context, r Config, tag string) {
	unlock, err := r.Locker.Lock(ctx)
	if err != nil {
		return
	}
	defer unlock()

	st, err := r.StateStore.Load(ctx)
	if err != nil || !version.Newer(st.HighestApplied, tag) {
		return
	}

	st.HighestApplied = tag
	_ = r.StateStore.Save(ctx, st) // a failed save never fails the update
}
