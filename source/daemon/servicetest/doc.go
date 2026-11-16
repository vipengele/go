// Package servicetest proves that a Linux service binary installed through
// go.vipengele.org/daemon/service behaves under a real systemd: it boots a
// podman container with systemd as PID 1 for the service's scope, system or
// user, and drives the service there through install, kill, reinstall, crash
// loop and uninstall.
//
// A consumer calls Run from a test:
//
//	func TestService(t *testing.T) {
//		servicetest.Run(t, servicetest.Target{
//			Binary: "testdata/myservice", // built with GOOS=linux
//			Config: service.Config{
//				Name:   "myservice",
//				Binary: "/usr/local/bin/myservice",
//				Scope:  service.ScopeUser,
//			},
//		})
//	}
//
// Run builds the container image from the Containerfile embedded in this
// package, Debian stable pinned by digest with systemd, dbus and a lingering
// account svc for user-scope services, and tags it with a hash of that file
// and Target.BaseImage, so an unchanged image is built once and reused. The
// container is started with --privileged and --systemd=always and labelled
// servicetest, and is removed when the scope's subtest ends. The service is
// driven by a driver program, built from this module's
// servicetest/driver package unless Target.Driver names a prebuilt one, that
// calls service.New in the container as the consumer's program would; for a
// user-scope service it runs as svc.
//
// # Environment
//
// SERVICETEST must be "1" for any container to run; otherwise Run and
// SkipUnlessEnabled skip the test, so the containers stay out of a plain
// go test run. With SERVICETEST=1, a podman that does not answer fails the
// test rather than skipping it.
//
// SERVICETEST_PODMAN is the podman command line, split on whitespace; it
// defaults to "podman". Set it to "sudo podman" where rootless podman cannot
// run systemd in a container.
//
// # Requirements
//
// The host needs podman able to run a container with systemd as PID 1:
// --systemd=always, a cgroup v2 hierarchy the container's systemd can manage,
// and --privileged. Rootless podman needs cgroup delegation for that; rootful
// podman has it.
package servicetest
