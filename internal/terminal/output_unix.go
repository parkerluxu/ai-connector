//go:build !windows

package terminal

import "os"

func enableOutput(_ *os.File) (func(), error) {
	return func() {}, nil
}
