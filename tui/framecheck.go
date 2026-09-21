package tui

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// 帧自检:DEEPX_DUMP_FRAME=<path> 打开后,每帧在锁宽之前体检一遍,
// **只把异常帧**追加写进该文件 —— 正常帧一个字节都不写,所以跑完文件还是空的,
// 就等于 deepx 交给终端的每一行都精确等于终端宽度,错位不在排版这一层。
//
// 体检三项,对应"分割线歪掉"的三种可能成因:
//  1. 行宽 != width  —— 上游补白算错(normalizeFrame 会兜住,但被 Cut 的那种要丢字)
//  2. 行数 != height —— 整帧行数错位,分隔线整条往下/往上挪
//  3. 含挪光标的裸控制字符 —— 宽度算对了也会画歪,见 cursormove.go
//
// 关掉时(环境变量为空)全部调用都是一次字符串判空,无任何开销。
var frameDumpPath = os.Getenv("DEEPX_DUMP_FRAME")

var (
	frameDumpMu    sync.Mutex
	frameDumpCount int
)

// frameDumpMax 限制最多记多少个异常帧 —— 一旦出问题往往每帧都出,不设上限会把磁盘写满。
const frameDumpMax = 20

func checkFrame(s string, width, height int) {
	if frameDumpPath == "" {
		return
	}
	lines := strings.Split(s, "\n")
	var probs []string
	if len(lines) != height {
		probs = append(probs, fmt.Sprintf("  行数=%d 期望=%d", len(lines), height))
	}
	for i, ln := range lines {
		if w := ansi.StringWidth(ln); w != width {
			probs = append(probs, fmt.Sprintf("  行%-3d 宽=%-4d 期望=%-4d 内容=%q", i, w, width, ansi.Strip(ln)))
		}
		if r := firstCursorMover(ln); r != 0 {
			probs = append(probs, fmt.Sprintf("  行%-3d 含裸控制字符 0x%02X 内容=%q", i, r, ansi.Strip(ln)))
		}
	}
	if len(probs) == 0 {
		return
	}

	frameDumpMu.Lock()
	defer frameDumpMu.Unlock()
	if frameDumpCount >= frameDumpMax {
		return
	}
	frameDumpCount++
	f, err := os.OpenFile(frameDumpPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "=== 异常帧 #%d  终端 %dx%d ===\n%s\n",
		frameDumpCount, width, height, strings.Join(probs, "\n"))
}

// firstCursorMover 返回行内第一个"会挪光标但不占列宽"的字符,没有则返回 0。
// \t 也算 —— 展开之前它就是元凶。
func firstCursorMover(s string) rune {
	for _, r := range s {
		if r == '\t' || isCursorMover(r) {
			return r
		}
	}
	return 0
}
