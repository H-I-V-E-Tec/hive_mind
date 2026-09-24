//go:build !windows

package server

import "os"

func auditModeIsPrivate(info os.FileInfo) bool {
	return info.Mode().Perm()&0o077 == 0
}

func secureAuditDirectory(*os.Root, string) error { return nil }

func secureAuditFile(*os.File, string) error { return nil }

func secureConvertedFile(*os.File) error { return nil }
