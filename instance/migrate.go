package instance

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// hasLegacyEnv 报告父根顶层是否存在旧版环境(排除共享资产与 deepx_* 后仍有条目)。
// 与复制黑名单同源:只要还有东西可迁,就触发迁移,不限于 model.yaml/meta.json 等已知文件。
func hasLegacyEnv(base string) bool {
	entries, err := os.ReadDir(base)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if isExcluded(e.Name()) {
			continue
		}
		return true
	}
	return false
}

// isExcluded 迁移时跳过:共享资产目录(ocr/router)与一切 deepx_ 前缀
// (实例目录、残留 tmp、锁文件)——前者共享、后者归新体系管,都不属于旧环境。
func isExcluded(name string) bool {
	if sharedAssets[name] {
		return true
	}
	return strings.HasPrefix(name, "deepx_")
}

// migrateLegacy 把父根顶层的旧环境复制到实例根(目标必须不存在)。
// 先写 <父根>/deepx_01.tmp,全部完成后再 rename —— 中断/失败不留半成品目录,
// 也不会让"deepx_01 已存在"的幂等判据被残缺目录污染。
func migrateLegacy(base, root string) error {
	tmp := filepath.Join(base, filepath.Base(root)+".tmp")
	_ = os.RemoveAll(tmp) // 清上次中断残留
	if err := copyTree(base, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, root); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("迁移目录就位失败: %w", err)
	}
	// 复制进来的 meta.json 清掉 web_port:端口改由实例编号派生(50528+N),
	// 旧全局固定端口被每个实例继承会让多实例端口冲突(02 带着 01 的 50529 即此问题)。
	sanitizeMigratedMeta(root)
	return nil
}

// sanitizeMigratedMeta 读实例内的 meta.json,删除 web_port 字段后写回。
// 其它字段(web_host/lang/sandbox 等)原样保留;文件缺失/解析失败静默跳过。
func sanitizeMigratedMeta(root string) {
	p := filepath.Join(root, "meta.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return
	}
	if _, ok := m["web_port"]; !ok {
		return
	}
	delete(m, "web_port")
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(p, out, 0o644)
}

// copyTree 黑名单复制 base 顶层到 dst:文件按原权限复制,目录递归。
// 复制范围与 hasLegacyEnv/isExcluded 完全一致。
func copyTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if isExcluded(name) {
			continue
		}
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)
		if e.IsDir() {
			if err := copyTree(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

// copyFile 复制单个文件并保持源权限位(model.yaml 0600 含 api key,不能丢成 0644)。
func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// 显式 chmod 兜底(OpenFile 的 mode 受 umask 影响,保权限位必须再设一次)。
	return os.Chmod(dst, info.Mode().Perm())
}
