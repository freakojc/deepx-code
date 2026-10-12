//go:build !windows

package instance

import (
	"os"

	"golang.org/x/sys/unix"
)

// tryLock 以非阻塞独占方式锁住 path(flock)。锁被占用返回非 nil err。
// 返回的 *os.File 必须由调用方持有(instance 包级 heldLocks)——flock 锁
// 随 fd 关闭释放,句柄被 GC 回收等于提前解锁,会让编号被误判空闲。
func tryLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
