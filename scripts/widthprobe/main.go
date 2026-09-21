// widthprobe 直接问终端:某个字符串实际占了多少列。
//
// 做法是 DSR(Device Status Report):把光标打到行首,输出待测字符串,再发 ESC[6n,
// 终端回一条 ESC[row;colR —— col-1 就是这串字符实际吃掉的列数。这是唯一能拿到
// "终端真实宽度"的办法,任何 Go 宽度库给的都只是它自己那张表的意见。
//
// 用法: go run ./scripts/widthprobe
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

type probe struct {
	name string
	s    string
}

var probes = []probe{
	{"数字基准 1234567890", "1234567890"},
	{"全角括号 （×5", strings.Repeat("（", 5)},
	{"全角括号 ）×5", strings.Repeat("）", 5)},
	{"半角括号 ()×5", strings.Repeat("()", 5)},
	{"汉字 中×5", strings.Repeat("中", 5)},
	{"全角逗号 ，×5", strings.Repeat("，", 5)},
	{"句号 。×5", strings.Repeat("。", 5)},
	{"破折号 —×10", strings.Repeat("—", 10)},
	{"项目符号 •×10", strings.Repeat("•", 10)},
	{"省略号 …×10", strings.Repeat("…", 10)},
	{"粗竖线 ┃×10", strings.Repeat("┃", 10)},
	{"细竖线 │×10", strings.Repeat("│", 10)},
	{"NBSP ×10", strings.Repeat(" ", 10)},
	{"混合(截图那行)", "表单加白色面板（圆角+阴影）——草坪背景上保证文字可读。"},
}

func main() {
	fd := os.Stdout.Fd()
	if !term.IsTerminal(fd) {
		fmt.Fprintln(os.Stderr, "需要在真实终端里直接运行(不能重定向/管道)")
		os.Exit(1)
	}
	old, err := term.MakeRaw(os.Stdin.Fd())
	if err != nil {
		fmt.Fprintln(os.Stderr, "进 raw 模式失败:", err)
		os.Exit(1)
	}
	defer term.Restore(os.Stdin.Fd(), old)

	type row struct {
		name      string
		got, want int
	}
	var rows []row
	for _, p := range probes {
		got, err := measure(p.s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\r\x1b[2K%s: 探测失败 %v\r\n", p.name, err)
			continue
		}
		rows = append(rows, row{p.name, got, ansi.StringWidth(p.s)})
	}
	term.Restore(os.Stdin.Fd(), old)

	fmt.Print("\r\x1b[2K")
	fmt.Printf("%-24s %8s %8s   %s\n", "样本", "终端实测", "deepx算", "结论")
	fmt.Println(strings.Repeat("-", 62))
	bad := 0
	for _, r := range rows {
		verdict := "一致"
		if r.got != r.want {
			verdict = fmt.Sprintf("★ 差 %+d", r.got-r.want)
			bad++
		}
		fmt.Printf("%-24s %8d %8d   %s\n", r.name, r.got, r.want, verdict)
	}
	fmt.Println(strings.Repeat("-", 62))
	if bad == 0 {
		fmt.Println("全部一致 —— 宽度模型不是错位的原因,问题在别处。")
	} else {
		fmt.Printf("有 %d 项不一致 —— 带这些字符的行就会把分隔线顶歪。\n", bad)
	}
}

// measure 回行首 → 打印 s → 问光标列 → 清行。返回 s 实际占用的列数。
func measure(s string) (int, error) {
	if _, err := fmt.Fprintf(os.Stdout, "\r\x1b[2K%s\x1b[6n", s); err != nil {
		return 0, err
	}
	col, err := readCPR()
	fmt.Fprint(os.Stdout, "\r\x1b[2K")
	if err != nil {
		return 0, err
	}
	return col - 1, nil
}

// readCPR 读一条 ESC[row;colR,返回 col。
func readCPR() (int, error) {
	_ = os.Stdin.SetReadDeadline(time.Now().Add(2 * time.Second))
	var buf []byte
	b := make([]byte, 1)
	for len(buf) < 32 {
		n, err := os.Stdin.Read(b)
		if err != nil {
			return 0, err
		}
		if n == 0 {
			continue
		}
		buf = append(buf, b[0])
		if b[0] == 'R' {
			break
		}
	}
	i := strings.LastIndexByte(string(buf), '[')
	j := strings.LastIndexByte(string(buf), ';')
	k := strings.LastIndexByte(string(buf), 'R')
	if i < 0 || j < 0 || k < 0 || j < i || k < j {
		return 0, fmt.Errorf("看不懂的回复 %q", string(buf))
	}
	return strconv.Atoi(string(buf[j+1 : k]))
}
