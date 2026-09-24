//go:build windows

package server

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsConvertedOutputUsesPrivateACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "converted.json")
	if err := writeConvertedDocument(path, []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("converted output inherits broad permissions")
	}
}
