// Package instance 实现 deepx 的多实例环境隔离。
//
// 目录布局:
//
//	{DEEPX_HOME | ~/.deepx}/          父根 base(旧版环境文件原地保留,新版本不再读写)
//	├── deepx_01/                     实例1 环境根:配置/sessions/skills/workflows/AGENTS.md
//	├── deepx_02/ ... deepx_0N/       实例N,不限数量(上限见 maxInstances)
//	├── deepx_NN.lock                 实例 NN 的独占锁文件(文件锁,崩溃自动释放)
//	├── ocr/                          共享资产:OCR 模型(可重建,所有实例复用)
//	└── router/                       共享资产:语义路由 embedder(可重建,所有实例复用)
//
// 编号分配(Init):
//   - DEEPX_INSTANCE=<n> 显式指定 → 固定 deepx_NN,锁被占则报错退出。
//   - 否则自动:从 01 起逐个试锁 base/deepx_NN.lock,第一个锁成功的即最小空闲编号。
//     基于锁的探测式分配不需要解析已有目录名——用户手动建的非规范目录不挡锁,天然忽略。
//
// 迁移(仅编号 01 首次创建时):
//   - 父根顶层存在旧版环境(排除 ocr/router/deepx_* 后仍有条目)→ 复制到 deepx_01,
//     旧环境原地不动;deepx_01 已存在则永不迁移(幂等)。
//   - 复制先写 deepx_01.tmp、完成后 rename,中断不留半成品。
//
// 兼容性:Home() 未 Init 时回退父根(不含 deepx_NN)——测试(t.Setenv HOME 隔离)与
// 不经过 Init 的路径保持旧行为;生产入口(main)必须在读配置前调用 Init。
package instance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// maxInstances 是自动分配的上限,防御性设置(实际几乎不可能触达)。
const maxInstances = 9999

// 实例登记表与分配锁(都在父根,deepx_ 前缀 → 迁移排除规则自然跳过,不会被复制进实例)。
//
//	deepx_registry.json  workspace 绝对路径 → 实例目录名(如 "deepx_01"):
//	                      编号与项目绑定,项目重开稳定用同一实例,新项目递增分配不复用。
//	deepx_alloc.lock      "查表-分配-登记" 的串行化锁(文件锁)。
const (
	registryFile = "deepx_registry.json"
	regLockFile  = "deepx_alloc.lock"
)

// sharedAssets 是跨实例共享、不随实例目录走的资产名(在父根下,迁移时排除)。
// 与 SharedDir 的白名单同源,未来新增资产目录只改这里一处。
var sharedAssets = map[string]bool{"ocr": true, "router": true}

var (
	initOnce    sync.Once
	initErr     error
	currentRoot string // 当前实例环境根,Init 后非空
	currentID   string // 当前实例编号("01"),Init 后非空
	heldLocks   []*os.File // 持有的锁句柄,包级持有防 GC 提前回收(LockFileEx/flock 依赖句柄存活)
)

// Init 分配当前进程的实例编号、获取独占锁,必要时把旧环境迁移进实例目录。
// 幂等(sync.Once);失败(显式编号非法/被占用/父根不可写/迁移失败)返回 error,
// 调用方必须视为启动失败退出——绝不静默回退,否则多实例隔离静默失效。
func Init() (string, error) {
	initOnce.Do(func() { initErr = doInit() })
	return currentRoot, initErr
}

func doInit() error {
	base, err := baseRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return fmt.Errorf("创建配置根 %s: %w", base, err)
	}

	// 显式指定优先:固定编号,不写登记表(一次性覆盖)。
	if raw := strings.TrimSpace(os.Getenv("DEEPX_INSTANCE")); raw != "" {
		n, err := parseInstanceNum(raw)
		if err != nil {
			return err
		}
		return acquire(base, n)
	}

	// 自动分配:编号与 workspace 绑定。拿分配锁串行化"查表-登记",防两个进程
	// 并发给不同 workspace 分到同一编号。
	alloc, err := tryLock(filepath.Join(base, regLockFile))
	if err != nil {
		return fmt.Errorf("实例分配锁被占用(另一个进程正在分配实例): %w", err)
	}
	defer releaseLock(alloc)

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取工作目录失败: %w", err)
	}
	return allocate(base, filepath.Clean(wd))
}

// allocate 给 workspace 分配/复用实例编号:已有登记 → 复用登记编号(被占则报错);
// 新 workspace → 递增分配(max 已有编号 + 1,不复用已释放的编号,保证项目间环境不串)
// 并写入登记表。调用方须已持有分配锁。
func allocate(base, wd string) error {
	reg := loadRegistry(base)

	if name, ok := reg[wd]; ok {
		n, ok := dirNum(name)
		if !ok {
			return fmt.Errorf("登记表 %s 中 %s 的编号 %q 非法", registryFile, wd, name)
		}
		acquired, err := acquireOne(base, n)
		if err != nil {
			return err
		}
		if !acquired {
			return fmt.Errorf("项目 %s 的实例 %s 正被另一个进程运行(锁 %s)。"+
				"如需并行开多个实例,请用 DEEPX_INSTANCE=<n> 指定不同编号", wd, name, lockPath(base, n))
		}
		return nil
	}

	n := nextInstanceNum(base, reg)
	if _, err := acquireOne(base, n); err != nil {
		return err
	}
	reg[wd] = instanceDir(n)
	if err := saveRegistry(base, reg); err != nil {
		return err
	}
	return nil
}

// dirNum 解析实例目录名("deepx_05")为编号;非 deepx_<数字> 返回 (0, false)。
func dirNum(name string) (int, bool) {
	rest, ok := strings.CutPrefix(name, "deepx_")
	if !ok || rest == "" {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// nextInstanceNum 给新 workspace 算编号:取「登记表 + 父根现存目录」中的最大编号 + 1。
// 递增而不复用已释放的编号 → 新项目永远拿到全新实例环境,不会继承别的项目留下的配置/会话。
func nextInstanceNum(base string, reg map[string]string) int {
	max := 0
	for _, v := range reg {
		if n, ok := dirNum(v); ok && n > max {
			max = n
		}
	}
	entries, err := os.ReadDir(base)
	if err == nil {
		for _, e := range entries {
			if n, ok := dirNum(e.Name()); ok && n > max {
				max = n
			}
		}
	}
	return max + 1
}

// loadRegistry 读登记表;文件缺失/损坏一律视为空表(损坏不阻塞启动,下次分配会重建)。
func loadRegistry(base string) map[string]string {
	data, err := os.ReadFile(filepath.Join(base, registryFile))
	if err != nil {
		return map[string]string{}
	}
	var m map[string]string
	if json.Unmarshal(data, &m) != nil || m == nil {
		return map[string]string{}
	}
	return m
}

// saveRegistry 原子写登记表(权限 0600,含绝对路径无密钥,保守起见照旧)。失败返回 err,
// 由调用方决定是否中断(登记失败 = 下次启动重复分配,可容忍,但不静默吞)。
func saveRegistry(base string, reg map[string]string) error {
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(base, registryFile)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// acquire 给指定编号固定分配(显式 DEEPX_INSTANCE 路径)。锁被占 → 报错,不做 fallback。
func acquire(base string, n int) error {
	ok, err := acquireOne(base, n)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("实例 deepx_%02d 正被另一个进程占用(锁 %s)。"+
			"请关闭该实例、或改用其它编号(DEEPX_INSTANCE=<n>)", n, lockPath(base, n))
	}
	return nil
}

// acquireOne 尝试为编号 n 加锁并准备实例目录。成功返回 (true, nil) 并设置 currentRoot;
// 锁被占用返回 (false, nil)(由调用方决定报错还是试下一个)。
//
// 首次创建(目录不存在)且有旧环境 → 原子迁移(任意编号,不只是 01 —— 每个新实例
// 都应继承父根旧配置,否则新项目要重新配 provider/key)。无旧环境 → 建空目录。
func acquireOne(base string, n int) (bool, error) {
	root := filepath.Join(base, instanceDir(n))
	first := !dirExists(root)

	f, err := tryLock(lockPath(base, n))
	if err != nil {
		return false, nil // 被占用/加锁失败
	}

	if first {
		if hasLegacyEnv(base) {
			if err := migrateLegacy(base, root); err != nil {
				releaseLock(f)
				return false, fmt.Errorf("迁移旧环境到 %s 失败: %w", root, err)
			}
		} else {
			if err := os.MkdirAll(root, 0o700); err != nil {
				releaseLock(f)
				return false, fmt.Errorf("创建实例目录 %s: %w", root, err)
			}
		}
	}

	heldLocks = append(heldLocks, f)
	runtime.KeepAlive(f) // 防 GC 在锁存活期间回收句柄
	currentRoot = root
	currentID = instanceDir(n)
	return true, nil
}

// Home 返回当前实例环境根。未 Init 时回退父根(旧行为,兼容测试);父根取不到返回空串。
// 生产入口必须在读配置前 Init,否则隔离会静默失效。
func Home() string {
	if currentRoot != "" {
		return currentRoot
	}
	base, _ := baseRoot()
	return base
}

// BaseRoot 返回父根(DEEPX_HOME 或 ~/.deepx)。取不到返回空串。
func BaseRoot() string {
	base, _ := baseRoot()
	return base
}

// SharedDir 返回跨实例共享的资产目录(父根/name)。name 不在共享清单内返回空串。
func SharedDir(name string) string {
	if !sharedAssets[name] {
		return ""
	}
	base, _ := baseRoot()
	if base == "" {
		return ""
	}
	return filepath.Join(base, name)
}

// InstanceID 返回当前实例编号("01"...);未 Init 返回空串。
func InstanceID() string { return currentID }

// InstanceNum 返回当前实例编号的数字(deepx_01→1, deepx_05→5);
// 未 Init 返回 1(默认实例,与 Home 回退父根口径一致,端口派生 50529)。
func InstanceNum() int {
	if n, ok := dirNum(currentID); ok {
		return n
	}
	return 1
}

// baseRoot 计算父根:DEEPX_HOME(绝对化 + ~ 展开)或 ~/.deepx。
func baseRoot() (string, error) {
	if h := strings.TrimSpace(os.Getenv("DEEPX_HOME")); h != "" {
		return normalizeDir(h)
	}
	base, err := userDataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ".deepx"), nil
}

// userDataHome 解析用户主目录,兼容平台与测试:
//   - HOME 优先:Linux/macOS 惯例;Windows 上 Git Bash 设 HOME=真实主目录;测试用
//     t.Setenv("HOME", t.TempDir()) 隔离时也能生效(os.UserHomeDir 在 Windows 只读 USERPROFILE,
//     导致旧写法在 Windows 本地测到真实 ~/.deepx)。
//   - USERPROFILE 其次:Windows cmd / PowerShell 的主目录。
//   - os.UserHomeDir 兜底。
func userDataHome() (string, error) {
	for _, k := range []string{"HOME", "USERPROFILE"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v, nil
		}
	}
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h, nil
	}
	return "", fmt.Errorf("无法获取用户主目录(未设置 HOME/USERPROFILE)")
}

// normalizeDir 把路径绝对化,并展开前导 ~/ 或 ~(PowerShell 用户设 DEEPX_HOME=~/x 不会
// 由 shell 展开,直接写会得到字面 "~" 目录)。
func normalizeDir(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := userDataHome()
		if err != nil {
			return "", err
		}
		if p == "~" {
			p = home
		} else {
			p = home + string(os.PathSeparator) + p[2:]
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("解析路径 %s: %w", p, err)
	}
	return abs, nil
}

// parseInstanceNum 校验并解析 DEEPX_INSTANCE:1..maxInstances 的十进制数字。
func parseInstanceNum(raw string) (int, error) {
	if len(raw) == 0 || len(raw) > 4 {
		return 0, fmt.Errorf("DEEPX_INSTANCE=%q 非法:需要 1..%d 的数字", raw, maxInstances)
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("DEEPX_INSTANCE=%q 非法:需要 1..%d 的数字", raw, maxInstances)
		}
	}
	n := 0
	for _, c := range raw {
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > maxInstances {
		return 0, fmt.Errorf("DEEPX_INSTANCE=%q 非法:需要 1..%d 的数字", raw, maxInstances)
	}
	return n, nil
}

// instanceDir 返回编号对应的实例目录名(两位补零,>99 自然扩位)。
func instanceDir(n int) string { return fmt.Sprintf("deepx_%02d", n) }

// lockPath 返回编号对应的锁文件路径(父根下,不随实例目录迁移/清理)。
func lockPath(base string, n int) string {
	return filepath.Join(base, instanceDir(n)+".lock")
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// releaseLock 关闭并释放锁句柄(迁移失败等异常路径用;正常退出由 OS 自动释放)。
func releaseLock(f *os.File) {
	for i, h := range heldLocks {
		if h == f {
			heldLocks = append(heldLocks[:i], heldLocks[i+1:]...)
			break
		}
	}
	_ = f.Close()
}
