//go:build unix

package server

import (
	"os"
	"syscall"
)

const inventoryReadFlags = os.O_RDONLY | syscall.O_NONBLOCK
