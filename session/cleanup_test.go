package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mkSession 在 sessions/ 下造一个 session 目录:meta.json 指向 ws,外加一个占位文件用来
// 确认删除是整目录删干净。
func mkSession(t *testing.T, root, sid, ws string) string {
	t.Helper()
	dir := filepath.Join(root, sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if ws != "" {
		data, _ := json.MarshalIndent(metaFile{
			Workspace: ws, CreatedAt: time.Now(), LastSeenAt: time.Now(),
		}, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "history.gob"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// liveWorkspace 造一个"活着的"父目录 + workspace,返回 workspace 路径。
// 父目录里另放一个兄弟目录 —— workspaceGone 要求父目录非空(空父目录是未挂载的挂载点)。
func liveWorkspace(t *testing.T, base, name string) string {
	t.Helper()
	ws := filepath.Join(base, name)
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "sibling"), 0o755); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestCleanupOrphaned(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := filepath.Join(home, ".deepx", "sessions")
	projects := filepath.Join(t.TempDir(), "projects")

	// 1. 活着的 workspace → 保留
	alive := liveWorkspace(t, projects, "alive")
	aliveDir := mkSession(t, root, "1111111111111111", alive)

	// 2. workspace 被删了(父目录还在且非空)→ 清理
	gone := liveWorkspace(t, projects, "gone")
	goneDir := mkSession(t, root, "2222222222222222", gone)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	// 3. 当前会话 → 即使 workspace 没了也不删
	cur := liveWorkspace(t, projects, "current")
	curDir := mkSession(t, root, "3333333333333333", cur)
	if err := os.RemoveAll(cur); err != nil {
		t.Fatal(err)
	}

	// 4. 没有 meta.json → 判断不了,不动
	noMeta := mkSession(t, root, "4444444444444444", "")

	// 5. 整个父目录都没了(相当于卷掉线)→ 不删
	detached := filepath.Join(t.TempDir(), "unmounted", "proj")
	detachedDir := mkSession(t, root, "5555555555555555", detached)

	// 6. 父目录存在但是空的(未挂载的挂载点长这样)→ 不删
	emptyParent := filepath.Join(t.TempDir(), "mountpoint")
	if err := os.MkdirAll(emptyParent, 0o755); err != nil {
		t.Fatal(err)
	}
	emptyDir := mkSession(t, root, "6666666666666666", filepath.Join(emptyParent, "proj"))

	// 7. 目录名不是 16 位 hex → 不是我们建的,不碰
	foreign := mkSession(t, root, "my-notes", filepath.Join(projects, "nope"))

	removed := CleanupOrphaned("3333333333333333")

	if len(removed) != 1 || removed[0].SessionID != "2222222222222222" {
		t.Fatalf("只应清掉 2222…, got %+v", removed)
	}
	if removed[0].Workspace != gone {
		t.Fatalf("汇报的 workspace 应是 %q, got %q", gone, removed[0].Workspace)
	}
	if dirExists(goneDir) {
		t.Fatal("孤儿目录应被整个删掉")
	}
	for name, d := range map[string]string{
		"活着的":         aliveDir,
		"当前会话":        curDir,
		"无 meta.json": noMeta,
		"父目录整个不在":     detachedDir,
		"父目录为空":       emptyDir,
		"非本程序目录":      foreign,
	} {
		if !dirExists(d) {
			t.Fatalf("%s 的 session 不该被删: %s", name, d)
		}
	}
}

// TestCleanupSkipsUnparsableMeta meta.json 坏了 / workspace 是相对路径 → 判断不了,一律不删。
func TestCleanupSkipsUnparsableMeta(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := filepath.Join(home, ".deepx", "sessions")

	broken := mkSession(t, root, "aaaaaaaaaaaaaaaa", "")
	if err := os.WriteFile(filepath.Join(broken, "meta.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	relative := mkSession(t, root, "bbbbbbbbbbbbbbbb", "")
	data, _ := json.Marshal(metaFile{Workspace: "relative/path"})
	if err := os.WriteFile(filepath.Join(relative, "meta.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	if removed := CleanupOrphaned(""); len(removed) != 0 {
		t.Fatalf("meta 不可信时不该删任何东西, got %+v", removed)
	}
	if !dirExists(broken) || !dirExists(relative) {
		t.Fatal("两个目录都应保留")
	}
}
