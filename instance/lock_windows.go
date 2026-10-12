//go:build windows

package instance

import (
	"os"

	"golang.org/x/sys/windows"
)

// tryLock 以非阻塞独占方式锁住 path(LockFileEx 锁 1 字节区域)。
// 锁被占用返回非 nil err。返回的句柄必须包级持有(见 instance.go 的 heldLocks
// 与 runtime.KeepAlive)——句柄被 GC 关闭会提前解锁,编号会被误判空闲。
func tryLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var ol windows.Overlapped
	err = windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &ol,
	)
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
