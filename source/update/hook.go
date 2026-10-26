package update

import "errors"

// PostUpdate is a step to run after an update has replaced the installed binary. It runs in the
// old process, which is still the one executing, so installedPath names the new binary:
// [os.Executable] would name the wrong file. A failure is non-fatal by contract, as the update has
// been applied; the caller reports it and carries on.
type PostUpdate func(installedPath string) error

// RunPostUpdate calls each hook in order with installedPath. A hook that fails does not stop the
// ones after it, and a nil hook is skipped. It returns the errors of the failed hooks joined with
// [errors.Join], so [errors.Is] and [errors.As] reach each one, and nil when none failed.
func RunPostUpdate(hooks []PostUpdate, installedPath string) error {
	var errs []error
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		if err := hook(installedPath); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
