package update

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"go.vipengele.org/cli/spec/version"
)

// ApplyOption configures one call to [Config.Apply].
type ApplyOption func(*applyOptions)

// applyOptions is what the [ApplyOption]s of one call to [Config.Apply] set.
type applyOptions struct {
	allowDowngrade bool
	download       []DownloadOption
	beforeInstall  []func(ctx context.Context, plan InstallPlan) error
}

// InstallPlan is the install [Config.Apply] is about to make, as a [BeforeInstall] hook sees it.
type InstallPlan struct {
	// Target is the executable's resolved path, the file the staged binary is renamed onto.
	Target string
	// Backup is the path the executable is renamed to first: Target followed by ".old", which
	// [Installed.Backup] returns once the install has run.
	Backup string
	// Version is the tag of the release being installed, the [Selection]'s Tag.
	Version string
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

// BeforeInstall has [Config.Apply] call hook once the staged binary has passed
// [Config.CheckStaged] and before [Config.Install] renames anything, with the [InstallPlan] of the
// install about to run. An error from hook stops Apply: the staged binary is discarded, the
// executable is left as it was, HighestApplied is not raised, and the error is returned wrapped.
// Several calls add up: the hooks run in the order given, and the first error stops the rest.
//
// Unlike a [Preflight] hook, which must be free of side effects because a later hook or the install
// may still refuse, hook may have side effects, such as recording that an update is pending before
// the executable is swapped. Only a later BeforeInstall hook or Install can still refuse after it, so
// a hook with side effects handles an Apply that returns an error after hook succeeded: the
// executable is then as it was, or a [*RestoreError] names its backup.
func BeforeInstall(hook func(ctx context.Context, plan InstallPlan) error) ApplyOption {
	return func(o *applyOptions) {
		o.beforeInstall = append(o.beforeInstall, hook)
	}
}

// Apply replaces the running executable with the binary of the release sel names, keeping the
// previous binary as a backup. It resolves the Config, returning its validation error, and then
// runs, in order and stopping at the first error, which it returns as the step returned it, a
// BeforeInstall hook's wrapped:
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
//  7. [Config.CheckStaged] of the staged binary against sel, which runs it with "version --json"
//     and then the [Config.Preflight] hooks, so a binary that is not the selected release for the
//     platform, or that a hook vetoes, is never installed.
//  8. The [BeforeInstall] hooks, in order, with the [InstallPlan] of Target, its backup and sel's
//     Tag. Unlike a [Preflight] hook, which is free of side effects (ADR-0029), a BeforeInstall
//     hook may have them: it runs once every check has passed, with only the install left.
//  9. [Config.Install] of the staged binary.
//  10. The record: under the Locker, the State is loaded, its HighestApplied raised to sel's Tag
//     when the Tag is newer in the order of [version.Newer], and the whole State saved back, so
//     its other fields are kept.
//
// Steps 1 to 3 make no request, so a refused install never reaches the network. The downgrade
// guard fails closed: a State that cannot be loaded refuses the install with the StateStore's
// error, rather than guess at the floor. The record fails open: a failed lock, load or save is
// ignored and Apply still returns the [*Installed], since the installed binary's Version floors
// the next update as the watermark would. Nothing at the executable's path changes before step 9,
// so a binary CheckStaged refuses, or an install a BeforeInstall hook refuses, leaves the installed
// executable as it was and HighestApplied unchanged; [errors.Is] matches a hook's error through
// its wrapping. No file Apply creates is left behind by a failure: Extract closes the archive and
// Install removes the staged binary, whether they succeed or fail, Apply closes the archive itself
// when Target fails, and it discards the staged binary itself when CheckStaged or a BeforeInstall
// hook fails, joining an error removing it onto that failure. When ctx is done during CheckStaged,
// the error is ctx's own, not a [*PreflightError].
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

	if _, err := r.checkStaged(ctx, staged, sel, s); err != nil {
		return nil, discardStaged(staged, err)
	}

	plan := InstallPlan{Target: target, Backup: target + backupSuffix, Version: sel.Tag}
	for _, hook := range o.beforeInstall {
		if err := hook(ctx, plan); err != nil {
			return nil, discardStaged(staged, fmt.Errorf("update: before install: %w", err))
		}
	}

	installed, err := r.install(staged, s)
	if err != nil {
		return nil, err
	}

	recordApplied(ctx, r, sel.Tag)

	return installed, nil
}

// discardStaged discards staged and returns err, with an error discarding it joined on.
func discardStaged(staged *Staged, err error) error {
	if discardErr := staged.Discard(); discardErr != nil {
		return errors.Join(err, discardErr)
	}

	return err
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
