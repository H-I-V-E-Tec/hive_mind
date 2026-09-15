//go:build !unix

package server

import "os"

const inventoryReadFlags = os.O_RDONLY
