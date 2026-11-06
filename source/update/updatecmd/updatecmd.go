package updatecmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.vipengele.org/cli/spec"
	"go.vipengele.org/cli/spec/version"
	"go.vipengele.org/update"
)

// ExitUpdateAvailable is the exit code of `update --check` when a newer release exists. It lies in
// the range a tool may give its own errors, so it never collides with 1, a failure, or 2, a
// command line the tool does not accept.
const ExitUpdateAvailable = 10

// Option configures the commands [Commands] returns.
type Option func(*settings)

// settings is what the [Option]s of one [Commands] call set.
type settings struct{}

// Commands returns the tool's `update` command, which updates the running executable as cfg
// describes it. cfg is not validated here: a Config that does not resolve fails the run.
func Commands(cfg update.Config, opts ...Option) []*spec.Command {
	var s settings
	for _, opt := range opts {
		if opt != nil {
			opt(&s)
		}
	}

	c := &command{
		cfg:            cfg,
		check:          spec.BoolFlag("check", "report whether a newer release exists, without installing it"),
		yes:            spec.BoolFlag("yes", "update without asking for confirmation", spec.Shorthand("y")),
		to:             spec.StringFlag("to", "install this release version, such as v1.2.3, instead of the latest"),
		allowDowngrade: spec.BoolFlag("allow-downgrade", "install a release below the highest version applied or running"),
		channel:        spec.StringFlag("channel", "follow this release channel for this run: stable, beta or edge"),
	}

	return []*spec.Command{{
		Name:        "update",
		Summary:     "Update " + cfg.Name + " to the latest release",
		Description: description(cfg.Name),
		Flags:       []spec.Flag{c.check, c.yes, c.to, c.allowDowngrade, c.channel},
		Run:         c.run,
		Annotations: map[string]string{spec.SkipUpdateCheck: ""},
	}}
}

// description is the long help of the `update` command of the tool name.
func description(name string) string {
	return fmt.Sprintf(`Update %[1]s by replacing the running executable with a release.

Without flags, %[1]s is updated to its latest release after a confirmation, which --yes
answers in advance. --to installs the named release instead. A release below the highest
version applied or running is refused unless --allow-downgrade is given; --to alone does not
lift that. A development build and an install an external manager owns, such as Homebrew or
Nix, are not updated.

Releases come in three channels, each receiving everything the ones before it do: stable, then
beta, then edge. --channel follows one of them for this run instead of the configured channel.
Switching to a lower channel never downgrades: a stable tool running v1.3.0-beta.2 stays there
until a stable release sorts above it.

With --check, nothing is installed: the result is printed on the standard output, and the
exit code is %[2]d when a newer release exists and 0 when %[1]s is up to date. A latest release
below the highest version applied or running counts as up to date.`, name, ExitUpdateAvailable)
}

// command is the `update` command of one Config: the Config and the handles of its flags.
type command struct {
	cfg            update.Config
	check          *spec.FlagOf[bool]
	yes            *spec.FlagOf[bool]
	to             *spec.FlagOf[string]
	allowDowngrade *spec.FlagOf[bool]
	channel        *spec.FlagOf[string]
}

// run is the `update` command's run function.
func (c *command) run(ctx context.Context, inv *spec.Invocation) error {
	check := c.check.Get(inv)
	to := c.to.Get(inv)
	pinned := c.to.IsSet(inv) || to != ""

	cfg := c.cfg
	if name := c.channel.Get(inv); c.channel.IsSet(inv) || name != "" {
		ch, err := update.ParseChannel(name)
		if err != nil {
			return fmt.Errorf("--channel: %w", err)
		}
		cfg.Channel = ch
	}

	if pinned {
		if check {
			return errors.New("--to and --check cannot be used together")
		}

		normalized, err := releaseVersion(to)
		if err != nil {
			return err
		}
		to = normalized
	}

	r, err := cfg.Resolved()
	if err != nil {
		return err
	}

	if err := r.CheckRelease(); err != nil {
		return err
	}

	var managed *update.ManagedInstallError
	if err := r.CheckManaged(); err != nil {
		if !check || !errors.As(err, &managed) {
			return err
		}
	}

	if check {
		return c.report(ctx, inv, r, managed)
	}

	return c.update(ctx, inv, r, to, pinned)
}

// releaseVersion returns v, given as "1.2.3" or "v1.2.3", as the release version "v1.2.3". A
// prerelease is accepted; a version that is not a full release version, such as "1.2" or one with
// build metadata, is an error.
func releaseVersion(v string) (string, error) {
	normalized := v
	if !strings.HasPrefix(normalized, "v") {
		normalized = "v" + normalized
	}

	if !version.IsRelease(normalized) {
		return "", fmt.Errorf("--to %q is not a release version: give one such as v1.2.3 or 1.2.3", v)
	}

	return normalized, nil
}

// report writes on the standard output whether a release newer than the running version exists.
// A newer release returns an error carrying [ExitUpdateAvailable], marked as reported, since the
// line written says all there is to say. managed, when not nil, adds what to run instead of an
// in-place update.
func (c *command) report(ctx context.Context, inv *spec.Invocation, r update.Config, managed *update.ManagedInstallError) error {
	latest, err := r.Source.Latest(ctx, r.Channel)
	if err != nil {
		return err
	}

	if !version.Newer(r.Version, latest.Tag) {
		return inv.Data("%s %s is up to date\n", r.Name, r.Version)
	}

	// A latest below the downgrade floor is not an update the command would install.
	if err := r.CheckDowngrade(ctx, latest.Tag); err != nil {
		var down *update.DowngradeError
		if errors.As(err, &down) {
			return inv.Data("%s %s is up to date\n", r.Name, r.Version)
		}
		return err
	}

	line := fmt.Sprintf("%s %s is available (current %s)", r.Name, latest.Tag, r.Version)
	if managed != nil {
		line += fmt.Sprintf(" (managed by %s: %s)", managed.Manager, managed.Instead)
	}

	if err := inv.Data("%s\n", line); err != nil {
		return err
	}

	return spec.Coded(ExitUpdateAvailable, spec.Reported(errors.New(line)))
}

// update installs the release tagged to when pinned, and the latest release otherwise, once the
// user has confirmed it or passed --yes.
func (c *command) update(ctx context.Context, inv *spec.Invocation, r update.Config, to string, pinned bool) error {
	rel := update.Release{Tag: to}

	if pinned {
		if to == r.Version {
			inv.Info("%s is already at %s", r.Name, to)
			return nil
		}
	} else {
		latest, err := r.Source.Latest(ctx, r.Channel)
		if err != nil {
			return err
		}

		if !version.Newer(r.Version, latest.Tag) {
			inv.Info("%s %s is already up to date", r.Name, r.Version)
			return nil
		}
		rel = latest
	}

	// The guard runs before the question, so a refused downgrade never prompts.
	allowDowngrade := c.allowDowngrade.Get(inv)
	if !allowDowngrade {
		if err := r.CheckDowngrade(ctx, rel.Tag); err != nil {
			return err
		}
	}

	if !c.yes.Get(inv) {
		ok, err := inv.Confirm(
			fmt.Sprintf("Update %s from %s to %s?", r.Name, r.Version, rel.Tag),
			spec.ConfirmOptions{Default: spec.DefaultNo, Required: true, Flag: "--yes"},
		)
		if err != nil {
			return err
		}

		// An interrupt during the question cancels ctx without ending the read.
		if err := ctx.Err(); err != nil {
			return err
		}

		if !ok {
			inv.Info("update cancelled")
			return nil
		}
	}

	sel, err := r.Select(rel)
	if err != nil {
		return err
	}

	var opts []update.ApplyOption
	if allowDowngrade {
		opts = append(opts, update.AllowDowngrade())
	}

	installed, err := r.Apply(ctx, sel, opts...)
	if err != nil {
		return err
	}

	// A hook failure leaves the update applied; it is reported and the update settled.
	if err := update.RunPostUpdate(r.PostUpdate, installed.Path()); err != nil {
		inv.Warning("a post-update step failed: %v", err)
	}

	backup := installed.Backup()
	if err := installed.Discard(); err != nil {
		inv.Warning("the previous binary is left at %s: %v", backup, err)
	}

	inv.Success("updated %s %s → %s", r.Name, r.Version, rel.Tag)
	return nil
}
