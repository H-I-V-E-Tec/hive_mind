//go:build windows

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsAuditUsesPrivateACL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "audit")
	audit, err := OpenFileAudit(Config{AuditDirectory: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer audit.Close()
	for _, path := range []string{dir, filepath.Join(dir, audit.name)} {
		sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		control, _, err := sd.Control()
		if err != nil {
			t.Fatal(err)
		}
		if control&windows.SE_DACL_PROTECTED == 0 {
			t.Fatalf("audit ACL inherits permissions: %s", path)
		}
		permissions := sd.String()
		for _, broadSID := range []string{";;;WD)", ";;;BU)", ";;;AU)"} {
			if strings.Contains(permissions, broadSID) {
				t.Fatalf("audit ACL grants broad access: %s", path)
			}
		}
	}
	if _, err := os.ReadFile(filepath.Join(dir, audit.name)); err != nil {
		t.Fatal(err)
	}
}
