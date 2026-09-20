package session

// 孤儿会话清理:workspace 目录已经不在了的 session,整个目录删掉。
//
// 删的是用户数据且不可逆(jsonl 明文历史、history.gob、summary 全在里面),所以判据一律
// 往"不删"的方向倒 —— 少删一个只是留下点垃圾,多删一个是永久丢历史。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
)

// sessionDirRe 匹配本程序自己建的 session 目录名(sha1 取前 16 hex,见 rawSessionID)。
// 只有名字长这样的才考虑删 —— 用户手动放进 sessions/ 的任何东西都不碰。
var sessionDirRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// OrphanRemoval 记录被清掉的一个 session,供调用方如实汇报删了什么。
type OrphanRemoval struct {
	SessionID string // 目录名
	Workspace string // meta.json 里记的、已经不存在的那个路径
}

// SessionsRoot 返回 ~/.deepx/sessions 绝对路径。
func SessionsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".deepx", "sessions"), nil
}

// CleanupOrphaned 扫描 ~/.deepx/sessions/,删掉 workspace 已不存在的会话目录,返回删了哪些。
// keepID 是当前会话的 id,永不删(它的 workspace 就是当前工作目录,正在用)。
//
// 出错一律跳过而不是继续删:读不到目录、读不出 meta.json、判断不了路径在不在,
// 都属于"不确定",而不确定时删掉是不可接受的。
func CleanupOrphaned(keepID string) []OrphanRemoval {
	root, err := SessionsRoot()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	var removed []OrphanRemoval
	for _, e := range entries {
		if !e.IsDir() || e.Name() == keepID || !sessionDirRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(root, e.Name())
		ws, ok := metaWorkspace(dir)
		if !ok || !workspaceGone(ws) {
			continue
		}
		// 删的路径是我们自己拼的 root+目录名,不是 meta.json 里的字符串 —— meta.json 是可编辑的
		// 文件,拿它的内容去 RemoveAll 等于把删除目标交给了外部输入。
		if err := os.RemoveAll(dir); err != nil {
			continue
		}
		removed = append(removed, OrphanRemoval{SessionID: e.Name(), Workspace: ws})
	}
	return removed
}

// metaWorkspace 读一个 session 目录里 meta.json 记的 workspace。
// 文件缺失 / 解析失败 / 字段为空 / 不是绝对路径,一律返回 ok=false —— 判断不了就不动它。
func metaWorkspace(dir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return "", false
	}
	var info metaFile
	if json.Unmarshal(data, &info) != nil {
		return "", false
	}
	if info.Workspace == "" || !filepath.IsAbs(info.Workspace) {
		return "", false
	}
	return info.Workspace, true
}

// workspaceGone 判断这个 workspace 是不是**真的**被删了,而不是暂时看不见。
//
// 三个条件都要满足,每一条挡的是一类误删:
//
//  1. workspace 本身 stat 报 NotExist —— 其它错误(权限、I/O、路径过长)都是"不确定",
//     不能当成"没了"。
//  2. 父目录存在 —— 整个卷掉线时父目录链也一起消失(外置盘拔了、网络盘断了),
//     这时不该把盘上所有项目的历史都清掉。
//  3. 父目录非空 —— 挂载点在没挂载时表现为一个**空目录**,光靠条件 2 抓不住这种。
//     父目录里还有别的东西 = 这个位置是活的,workspace 确实是被删了。
//
// 代价是有个漏网场景:workspace 是父目录里最后一项时不会被清理(条件 3 不满足)。
// 这是刻意选的方向 —— 留个垃圾目录,好过把还在用的历史删掉。
func workspaceGone(ws string) bool {
	if _, err := os.Stat(ws); !os.IsNotExist(err) {
		return false
	}
	parent := filepath.Dir(ws)
	if parent == ws { // 已到根("/" 或 "C:\"),没有父目录可查 → 不删
		return false
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return false // 父目录不存在 / 读不了 → 不确定 → 不删
	}
	return len(entries) > 0
}
