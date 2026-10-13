//go:build !linux && !darwin

package backup

import "errors"

func checkCaptureSpace(path string, budget int64) error {
	return errors.New("native capture requires Linux or macOS")
}
