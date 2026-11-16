package service

import (
	"errors"
	"fmt"
	"math"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// hostSCM is the service control manager of the local machine, reached
// through golang.org/x/sys/windows/svc/mgr and the registry.
func hostSCM() scm {
	return windowsSCM{}
}

// windowsSCM is the scm of a Windows host.
type windowsSCM struct{}

// scmWin32Error is an error that carries a Win32 error code. Its text is the
// original error's, and errors.Is matches both that error and code.
type scmWin32Error struct {
	err  error
	code scmErrno
}

func (e *scmWin32Error) Error() string   { return e.err.Error() }
func (e *scmWin32Error) Unwrap() []error { return []error{e.err, e.code} }

// scmError returns err with the Win32 error code it carries, if any, wrapped
// as an scmErrno. A nil err stays nil.
func scmError(err error) error {
	if err == nil {
		return nil
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) || uint64(errno) > math.MaxUint32 {
		return err
	}
	return &scmWin32Error{err: err, code: scmErrno(uint32(errno))}
}

func (windowsSCM) elevated() (bool, error) {
	return windows.GetCurrentProcessToken().IsElevated(), nil
}

func (windowsSCM) connect(access scmAccess) (scmManager, error) {
	if access == scmControl {
		m, err := mgr.Connect()
		if err != nil {
			return nil, scmError(err)
		}
		return &windowsSCMManager{m: m, access: access}, nil
	}
	h, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return nil, scmError(err)
	}
	return &windowsSCMManager{m: &mgr.Mgr{Handle: h}, access: access}, nil
}

// windowsSCMManager is a connection to the SCM, opened with access, which
// decides the rights open asks for on a service: mgr.Mgr.OpenService asks for
// SERVICE_ALL_ACCESS, which an account without elevation is denied.
type windowsSCMManager struct {
	m      *mgr.Mgr
	access scmAccess
}

func (w *windowsSCMManager) open(name string) (scmService, error) {
	if w.access == scmControl {
		s, err := w.m.OpenService(name)
		if err != nil {
			return nil, scmError(err)
		}
		return &windowsSCMService{s: s}, nil
	}
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, scmError(err)
	}
	h, err := windows.OpenService(w.m.Handle, namePtr, windows.SERVICE_QUERY_CONFIG|windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return nil, scmError(err)
	}
	return &windowsSCMService{s: &mgr.Service{Name: name, Handle: h}}, nil
}

// create calls CreateService itself, as mgr.Mgr.CreateService does, so that
// cfg.BinaryPathName is the command line as given: mgr.Mgr.CreateService
// quotes its executable path, which would quote the command line a second
// time. The description, the SID type and the delayed start, which
// CreateService does not take, are then written by UpdateConfig; a failure
// there deletes the service it created.
func (w *windowsSCMManager) create(name string, cfg scmConfig) (scmService, error) {
	ptr := func(s string) (*uint16, error) {
		if s == "" {
			return nil, nil
		}
		return syscall.UTF16PtrFromString(s)
	}
	namePtr, err := ptr(name)
	if err != nil {
		return nil, scmError(err)
	}
	displayPtr, err := ptr(cfg.DisplayName)
	if err != nil {
		return nil, scmError(err)
	}
	binaryPtr, err := ptr(cfg.BinaryPathName)
	if err != nil {
		return nil, scmError(err)
	}
	groupPtr, err := ptr(cfg.LoadOrderGroup)
	if err != nil {
		return nil, scmError(err)
	}
	accountPtr, err := ptr(cfg.ServiceStartName)
	if err != nil {
		return nil, scmError(err)
	}
	deps, err := windowsStringBlock(cfg.Dependencies)
	if err != nil {
		return nil, scmError(err)
	}
	h, err := windows.CreateService(w.m.Handle, namePtr, displayPtr, windows.SERVICE_ALL_ACCESS,
		cfg.ServiceType, cfg.StartType, cfg.ErrorControl, binaryPtr, groupPtr, nil, deps, accountPtr, nil)
	if err != nil {
		return nil, scmError(err)
	}
	s := &mgr.Service{Name: name, Handle: h}
	if cfg.Description != "" || cfg.SidType != windows.SERVICE_SID_TYPE_NONE || cfg.DelayedAutoStart {
		if err := s.UpdateConfig(mgrConfig(cfg)); err != nil {
			_ = s.Delete()
			_ = s.Close()
			return nil, scmError(err)
		}
	}
	return &windowsSCMService{s: s}, nil
}

func (w *windowsSCMManager) close() error {
	return scmError(w.m.Disconnect())
}

// windowsStringBlock is ss as the double-NUL-terminated block of
// NUL-terminated UTF-16 strings CreateService takes for its dependencies, nil
// when ss holds no non-empty string.
func windowsStringBlock(ss []string) (*uint16, error) {
	var block []uint16
	for _, s := range ss {
		if s == "" {
			continue
		}
		u, err := syscall.UTF16FromString(s)
		if err != nil {
			return nil, err
		}
		block = append(block, u...)
	}
	if len(block) == 0 {
		return nil, nil
	}
	block = append(block, 0)
	return &block[0], nil
}

// mgrConfig is cfg as an mgr.Config, with no password.
func mgrConfig(cfg scmConfig) mgr.Config {
	return mgr.Config{
		ServiceType:      cfg.ServiceType,
		StartType:        cfg.StartType,
		ErrorControl:     cfg.ErrorControl,
		BinaryPathName:   cfg.BinaryPathName,
		LoadOrderGroup:   cfg.LoadOrderGroup,
		TagId:            cfg.TagID,
		Dependencies:     cfg.Dependencies,
		ServiceStartName: cfg.ServiceStartName,
		DisplayName:      cfg.DisplayName,
		Description:      cfg.Description,
		SidType:          cfg.SidType,
		DelayedAutoStart: cfg.DelayedAutoStart,
	}
}

// windowsSCMService is an open service.
type windowsSCMService struct {
	s *mgr.Service
}

func (w *windowsSCMService) config() (scmConfig, error) {
	c, err := w.s.Config()
	if err != nil {
		return scmConfig{}, scmError(err)
	}
	return scmConfig{
		ServiceType:      c.ServiceType,
		StartType:        c.StartType,
		ErrorControl:     c.ErrorControl,
		BinaryPathName:   c.BinaryPathName,
		LoadOrderGroup:   c.LoadOrderGroup,
		TagID:            c.TagId,
		Dependencies:     c.Dependencies,
		ServiceStartName: c.ServiceStartName,
		DisplayName:      c.DisplayName,
		Description:      c.Description,
		SidType:          c.SidType,
		DelayedAutoStart: c.DelayedAutoStart,
	}, nil
}

func (w *windowsSCMService) updateConfig(cfg scmConfig) error {
	return scmError(w.s.UpdateConfig(mgrConfig(cfg)))
}

func (w *windowsSCMService) setRecoveryActions(actions []scmRecoveryAction, resetPeriod uint32) error {
	ra := make([]mgr.RecoveryAction, 0, len(actions))
	for _, a := range actions {
		ra = append(ra, mgr.RecoveryAction{Type: a.Type, Delay: a.Delay})
	}
	return scmError(w.s.SetRecoveryActions(ra, resetPeriod))
}

func (w *windowsSCMService) setRecoveryOnNonCrashFailures(on bool) error {
	return scmError(w.s.SetRecoveryActionsOnNonCrashFailures(on))
}

// registryKey opens the service's key under
// HKLM\SYSTEM\CurrentControlSet\Services for setting values.
func (w *windowsSCMService) registryKey() (registry.Key, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Services\`+w.s.Name, registry.SET_VALUE)
	if err != nil {
		return 0, scmError(fmt.Errorf("open the registry key of %s: %w", w.s.Name, err))
	}
	return k, nil
}

func (w *windowsSCMService) setEnvironment(entries []string) error {
	k, err := w.registryKey()
	if err != nil {
		return err
	}
	defer func() { _ = k.Close() }()
	return scmError(k.SetStringsValue("Environment", entries))
}

func (w *windowsSCMService) deleteEnvironment() error {
	k, err := w.registryKey()
	if err != nil {
		return err
	}
	defer func() { _ = k.Close() }()
	if err := k.DeleteValue("Environment"); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return scmError(err)
	}
	return nil
}

func (w *windowsSCMService) start() error {
	return scmError(w.s.Start())
}

func (w *windowsSCMService) stop() (scmStatus, error) {
	st, err := w.s.Control(svc.Stop)
	return windowsStatus(st), scmError(err)
}

func (w *windowsSCMService) query() (scmStatus, error) {
	st, err := w.s.Query()
	if err != nil {
		return scmStatus{}, scmError(err)
	}
	return windowsStatus(st), nil
}

func (w *windowsSCMService) delete() error {
	return scmError(w.s.Delete())
}

func (w *windowsSCMService) close() error {
	return scmError(w.s.Close())
}

// windowsStatus is st as an scmStatus.
func windowsStatus(st svc.Status) scmStatus {
	return scmStatus{
		State:                   scmState(st.State),
		ProcessID:               st.ProcessId,
		Win32ExitCode:           st.Win32ExitCode,
		ServiceSpecificExitCode: st.ServiceSpecificExitCode,
	}
}
