package instance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseInstanceNum(t *testing.T) {
	good := map[string]int{"1": 1, "02": 2, "7": 7, "9999": 9999}
	for raw, want := range good {
		n, err := parseInstanceNum(raw)
		if err != nil || n != want {
			t.Errorf("parseInstanceNum(%q) = %d, %v; want %d", raw, n, err, want)
		}
	}
	bad := []string{"", "abc", "0", "10000", "1a", " 2", "-1", "12345"}
	for _, raw := range bad {
		if _, err := parseInstanceNum(raw); err == nil {
			t.Errorf("parseInstanceNum(%q) 应报错", raw)
		}
	}
}

func TestDefaultRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DEEPX_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // windows 走 USERPROFILE
	base, err := baseRoot()
	if err != nil {
		t.Fatalf("baseRoot: %v", err)
	}
	want := filepath.Join(home, ".deepx")
	if base != want {
		t.Errorf("baseRoot = %s; want %s", base, want)
	}
}

func TestDEEPXHOME(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())

	// 绝对路径原样
	abs := filepath.Join(t.TempDir(), "insts")
	t.Setenv("DEEPX_HOME", abs)
	if b, _ := baseRoot(); b != abs {
		t.Errorf("abs DEEPX_HOME = %s; want %s", b, abs)
	}

	// 相对路径 → 绝对化
	t.Setenv("DEEPX_HOME", "./rel-home")
	b, err := baseRoot()
	if err != nil {
		t.Fatalf("rel DEEPX_HOME: %v", err)
	}
	if !filepath.IsAbs(b) {
		t.Errorf("rel DEEPX_HOME 未绝对化: %s", b)
	}
	if filepath.Base(b) != "rel-home" {
		t.Errorf("rel DEEPX_HOME 基名错: %s", b)
	}
}

func TestHomeFallback(t *testing.T) {
	// 未 Init 时 Home 回退父根(不带 deepx_NN),保测试/旧行为
	home := t.TempDir()
	t.Setenv("DEEPX_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if h := Home(); h != filepath.Join(home, ".deepx") {
		t.Errorf("Home 未 Init 应回退父根,got %s", h)
	}
	if id := InstanceID(); id != "" {
		t.Errorf("InstanceID 未 Init 应为空,got %s", id)
	}
}

func TestSharedDirWhitelist(t *testing.T) {
	if d := SharedDir("ocr"); d == "" {
		t.Error("SharedDir(ocr) 不应为空")
	}
	if d := SharedDir("sessions"); d != "" {
		t.Errorf("SharedDir(sessions) 不在共享清单应返回空,got %s", d)
	}
}

// TestAllocateRegistryBinding 验证编号与 workspace 绑定:新项目递增分配,同名项目稳定复用。
func TestAllocateRegistryBinding(t *testing.T) {
	base := filepath.Join(t.TempDir(), ".deepx") // TempDir 先注册
	resetInstanceState(t)
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}

	type step struct {
		wd   string
		want string
	}
	steps := []step{
		{"C:/proj/a", "deepx_01"}, // 项目 A 首启
		{"C:/proj/b", "deepx_02"}, // 项目 B 首启
		{"C:/proj/a", "deepx_01"}, // 项目 A 退出后重开 → 复用登记编号
		{"C:/proj/c", "deepx_03"}, // 项目 C 首启 → 递增,不复用 01/02
	}
	for i, s := range steps {
		if err := allocate(base, s.wd); err != nil {
			t.Fatalf("step %d (%s): %v", i, s.wd, err)
		}
		if currentID != s.want {
			t.Fatalf("step %d (%s): 应分配 %s, got %s", i, s.wd, s.want, currentID)
		}
		if !dirExists(filepath.Join(base, s.want)) {
			t.Fatalf("step %d: %s 目录应已创建", i, s.want)
		}
		simulateExit(t) // 模拟该实例进程退出,释放锁,下次分配可复用编号
	}

	// 登记表应有 3 条且映射正确
	reg := loadRegistry(base)
	if len(reg) != 3 {
		t.Fatalf("登记表应有 3 条, got %d: %v", len(reg), reg)
	}
	if reg["C:/proj/a"] != "deepx_01" || reg["C:/proj/c"] != "deepx_03" {
		t.Errorf("登记表内容不符: %v", reg)
	}
}

// TestAllocateBusyReportsError 验证:登记过的项目,其编号被占用(锁未释放)时明确报错。
func TestAllocateBusyReportsError(t *testing.T) {
	base := filepath.Join(t.TempDir(), ".deepx")
	resetInstanceState(t)
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := allocate(base, "C:/proj/a"); err != nil {
		t.Fatal(err)
	}
	// 第一次的锁还在 heldLocks 里(模拟实例仍在运行) → 同项目再 allocate 应报"正在运行"
	if err := allocate(base, "C:/proj/a"); err == nil {
		t.Fatal("编号被占用时 allocate 应报错")
	}
}

// TestInstanceNum 验证编号解析与默认值。
func TestInstanceNum(t *testing.T) {
	base := filepath.Join(t.TempDir(), ".deepx") // TempDir 先注册
	resetInstanceState(t)
	if n := InstanceNum(); n != 1 {
		t.Errorf("未 Init 时 InstanceNum 应为 1(默认实例), got %d", n)
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := allocate(base, "C:/proj/x"); err != nil {
		t.Fatal(err)
	}
	if n := InstanceNum(); n != 1 {
		t.Errorf("deepx_01 → InstanceNum 应为 1, got %d", n)
	}
}

// TestAcquireExplicitBusy 验证:显式指定被占用的编号 → 报错,不 fallback。
func TestAcquireExplicitBusy(t *testing.T) {
	base := filepath.Join(t.TempDir(), ".deepx")
	resetInstanceState(t)
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := tryLock(lockPath(base, 3))
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLock(f)

	if err := acquire(base, 3); err == nil {
		t.Fatal("显式指定被占编号应报错")
	}
}

func TestAcquireExplicitCreates(t *testing.T) {
	base := filepath.Join(t.TempDir(), ".deepx")
	resetInstanceState(t)
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := acquire(base, 5); err != nil {
		t.Fatalf("acquire(5): %v", err)
	}
	if currentID != "deepx_05" {
		t.Errorf("currentID = %s, want deepx_05", currentID)
	}
	if !dirExists(filepath.Join(base, "deepx_05")) {
		t.Error("deepx_05 目录应被创建")
	}
}

// resetInstanceState 释放本测试成功获取的全局锁并清空 state,
// 避免 TempDir 清理时锁文件被句柄占用(Windows 无法删除在用文件)。
func resetInstanceState(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		simulateExit(t)
	})
}

// simulateExit 模拟进程退出:释放全部实例锁(OS 在进程结束时同样会释放)并清空 state。
// 之后 allocate 可复用已释放的编号。
func simulateExit(t *testing.T) {
	t.Helper()
	for _, f := range heldLocks {
		_ = f.Close()
	}
	heldLocks = nil
	currentRoot, currentID = "", ""
}