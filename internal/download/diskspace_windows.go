//go:build windows

package download

import "errors"

func freeDiskSpace(_ string) (int64, error) {
	return 0, errors.New("disk space check not implemented on Windows")
}
