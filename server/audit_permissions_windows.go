//go:build windows

package server

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"golang.org/x/sys/windows"
)

// FileMode.Perm does not describe Windows ACLs. Protect private files and the
// audit directory with a DACL for the current user, SYSTEM and local
// administrators, and disable inheritance from a potentially shared parent.
func auditModeIsPrivate(os.FileInfo) bool { return true }

func secureAuditDirectory(root *os.Root, path string) error {
	dir, err := root.Open(".")
	if err != nil {
		return fmt.Errorf("open directory handle: %w", err)
	}
	defer dir.Close()
	return setPrivateACL(dir, path, true)
}

func secureAuditFile(file *os.File, path string) error {
	return setPrivateACL(file, path, false)
}

func secureConvertedFile(file *os.File) error {
	return setPrivateACL(file, file.Name(), false)
}

func setPrivateACL(file *os.File, path string, directory bool) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("read current user: %w", err)
	}
	if user == nil || user.User.Sid == nil {
		return errors.New("current Windows user has no SID")
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return fmt.Errorf("identify local system: %w", err)
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return fmt.Errorf("identify administrators: %w", err)
	}
	var pinner runtime.Pinner
	defer pinner.Unpin()
	pinner.Pin(user)
	pinner.Pin(system)
	pinner.Pin(admins)

	inheritance := uint32(windows.NO_INHERITANCE)
	if directory {
		inheritance = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
	}
	entries := make([]windows.EXPLICIT_ACCESS, 0, 3)
	for _, trustee := range []struct {
		sid  *windows.SID
		kind windows.TRUSTEE_TYPE
	}{
		{user.User.Sid, windows.TRUSTEE_IS_USER},
		{system, windows.TRUSTEE_IS_USER},
		{admins, windows.TRUSTEE_IS_GROUP},
	} {
		entries = append(entries, windows.EXPLICIT_ACCESS{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       inheritance,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  trustee.kind,
				TrusteeValue: windows.TrusteeValueFromSID(trustee.sid),
			},
		})
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return fmt.Errorf("construct private ACL: %w", err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode private path: %w", err)
	}
	flags := uint32(windows.FILE_ATTRIBUTE_NORMAL)
	if directory {
		flags = windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := windows.CreateFile(name, windows.WRITE_DAC|windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		return fmt.Errorf("open ACL write handle: %w", err)
	}
	defer windows.CloseHandle(handle)
	var opened, target windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &opened); err != nil {
		return fmt.Errorf("inspect original handle: %w", err)
	}
	if err := windows.GetFileInformationByHandle(handle, &target); err != nil {
		return fmt.Errorf("inspect ACL write handle: %w", err)
	}
	if opened.VolumeSerialNumber != target.VolumeSerialNumber || opened.FileIndexHigh != target.FileIndexHigh || opened.FileIndexLow != target.FileIndexLow {
		return errors.New("private path changed while securing it")
	}
	if err := windows.SetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil,
	); err != nil {
		return fmt.Errorf("apply private ACL: %w", err)
	}
	return nil
}
