package instance

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHasLegacyEnv(t *testing.T) {
	base := t.TempDir()

	if hasLegacyEnv(base) {
		t.Fatal("空目录不应判定为有旧环境")
	}
	mkDir(base, "ocr")     // 共享资产
	mkDir(base, "router")  // 共享资产
	mkDir(base, "deepx_01") // 实例目录
	if hasLegacyEnv(base) {
		t.Fatal("只有共享资产/实例目录不应判定为有旧环境")
	}
	// 任意一个非排除条目即判定为有旧环境
	if err := os.WriteFile(filepath.Join(base, "router.yaml"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !hasLegacyEnv(base) {
		t.Fatal("存在 router.yaml 应判定为有旧环境")
	}
}

// TestMigrateLegacy 验证:全量复制(排除共享资产与 deepx_*)、保文件权限、无 tmp 残留。
func TestMigrateLegacy(t *testing.T) {
	base := t.TempDir()

	// 旧环境:model.yaml(0600 模拟 api key)+ sessions 目录 + 共享资产 + 锁文件
	modelPath := filepath.Join(base, "model.yaml")
	if err := os.WriteFile(modelPath, []byte("flash: {api_key: sk-secret}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mkdirWrite(t, filepath.Join(base, "sessions", "abc"), "2026-01-01.jsonl", `{"ts":"x","role":"user","content":"hi"}`)
	mkDir(base, "ocr")     // 共享,不复制
	mkDir(base, "router")  // 共享,不复制
	mkDir(base, "deepx_99") // 实例,不复制
	if err := os.WriteFile(filepath.Join(base, "deepx_01.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(base, "deepx_01")
	if err := migrateLegacy(base, root); err != nil {
		t.Fatalf("migrateLegacy: %v", err)
	}

	// 复制的文件 + 内容 + 权限(0600 不能丢)
	migrated, err := os.ReadFile(filepath.Join(root, "model.yaml"))
	if err != nil {
		t.Fatalf("model.yaml 未迁移: %v", err)
	}
	if string(migrated) != "flash: {api_key: sk-secret}\n" {
		t.Errorf("model.yaml 内容不符: %s", migrated)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(filepath.Join(root, "model.yaml"))
		if perms := fi.Mode().Perm(); perms != 0o600 {
			t.Errorf("迁移后权限 %o,应保持 0600", perms)
		}
	}
	// sessions 递归复制
	if _, err := os.Stat(filepath.Join(root, "sessions", "abc", "2026-01-01.jsonl")); err != nil {
		t.Errorf("sessions 未递归复制: %v", err)
	}
	// 排除项不得复制
	for _, name := range []string{"ocr", "router", "deepx_99", "deepx_01.lock"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Errorf("%s 不应被迁移复制", name)
		}
	}
	// 无 tmp 残留
	if _, err := os.Stat(filepath.Join(base, "deepx_01.tmp")); err == nil {
		t.Error("迁移后不应残留 tmp 目录")
	}
}

// TestMigrateIdempotent 验证:目标已存在时迁移应失败(调用方靠"目录不存在"判首次,
// 迁移不覆盖已有实例)。
func TestMigrateIdempotent(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "model.yaml"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "deepx_01")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "model.yaml"), []byte("new-config"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacy(base, root); err == nil {
		t.Fatal("目标已存在时迁移应失败(不覆盖用户改动)")
	}
}

// TestMigrateForEveryNewInstance 验证:任意编号的首次创建实例都会从父根复制旧环境
// (问题:新项目 02/03 不应要求重新配置 provider)——先开 A(01)再开 B(02),B 应继承旧配置。
func TestMigrateForEveryNewInstance(t *testing.T) {
	base := filepath.Join(t.TempDir(), ".deepx")
	resetInstanceState(t)
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	// 父根旧环境:model.yaml(旧版位置)
	if err := os.WriteFile(filepath.Join(base, "model.yaml"), []byte("flash: {api_key: sk-old}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 项目 A → 01(迁移旧配置)
	if err := allocate(base, "C:/proj/a"); err != nil {
		t.Fatalf("allocate A: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "deepx_01", "model.yaml")); err != nil {
		t.Errorf("deepx_01 应继承旧配置: %v", err)
	}
	simulateExit(t)

	// 项目 B → 02(同样迁移旧配置,而不是空初始化)
	if err := allocate(base, "C:/proj/b"); err != nil {
		t.Fatalf("allocate B: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(base, "deepx_02", "model.yaml"))
	if err != nil {
		t.Fatalf("deepx_02 应继承旧配置(问题修复验证): %v", err)
	}
	if string(data) != "flash: {api_key: sk-old}\n" {
		t.Errorf("deepx_02 model.yaml 内容不符: %s", data)
	}
	// 父根旧环境保留不动
	if _, err := os.Stat(filepath.Join(base, "model.yaml")); err != nil {
		t.Error("父根旧 model.yaml 不应被移动/删除")
	}
}

// TestMigrateClearsWebPort 验证:迁移复制 meta.json 后清除 web_port(旧固定端口
// 会被所有实例继承导致端口冲突),其它字段保留。
func TestMigrateClearsWebPort(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "meta.json"),
		[]byte(`{"web_port": 50529,"lang":"zh"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "deepx_01")
	if err := migrateLegacy(base, root); err != nil {
		t.Fatalf("migrateLegacy: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "meta.json"))
	if err != nil {
		t.Fatalf("meta.json 未迁移: %v", err)
	}
	if strings.Contains(string(data), "web_port") {
		t.Errorf("迁移后 meta.json 应清除 web_port: %s", data)
	}
	if !strings.Contains(string(data), "lang") {
		t.Errorf("其它字段应保留: %s", data)
	}
}

// TestAcquireMigratesAndSanitizes 走完整 acquireOne 路径(锁+迁移+sanitize),
// 验证 meta.json 的 web_port 被清除。
func TestAcquireMigratesAndSanitizes(t *testing.T) {
	base := filepath.Join(t.TempDir(), ".deepx") // TempDir 先注册
	resetInstanceState(t)
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "meta.json"),
		[]byte(`{"web_port": 50529,"lang":"zh"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err := acquireOne(base, 1)
	if err != nil || !ok {
		t.Fatalf("acquireOne: ok=%v err=%v", ok, err)
	}
	data, err := os.ReadFile(filepath.Join(base, "deepx_01", "meta.json"))
	if err != nil {
		t.Fatalf("meta.json 未迁移: %v", err)
	}
	if strings.Contains(string(data), "web_port") {
		t.Errorf("acquireOne 迁移后 meta.json 应清除 web_port: %s", data)
	}
}

func mkDir(base, name string) {
	if os.MkdirAll(filepath.Join(base, name), 0o755) != nil {
		panic("mkdir " + name)
	}
}

func mkdirWrite(t *testing.T, dir, file, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}