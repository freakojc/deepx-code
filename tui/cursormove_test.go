package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestTabBreaksColumnLock 回归:含 \t 的行必须在补白前被展开。
//
// 没展开时 ansi.StringWidth 把 \t 算 0 列,padLinesToWidth 会按"还差很多"继续补空格,
// 终端却把 \t 画成"跳到下一个 8 列制表位" —— 这一行实际比 w 宽,右栏 ┃ 分隔线被顶偏。
func TestTabBreaksColumnLock(t *testing.T) {
	const w = 20
	got := padLinesToWidth("abc\tdef", w)
	if strings.ContainsRune(got, '\t') {
		t.Fatalf("padLinesToWidth 没展开 \\t: %q", got)
	}
	if n := ansi.StringWidth(got); n != w {
		t.Fatalf("补白后宽度 = %d, want %d: %q", n, w, got)
	}
	// 展开后终端画出来的列数 == 库算的列数,这才是列锁成立的前提。
	if got != "abc     def         " {
		t.Fatalf("制表位展开不对: %q", got)
	}
}

// TestExpandTabsStopsAndANSI 制表位按 8 列对齐,且不把 ANSI 转义算进列宽。
func TestExpandTabsStopsAndANSI(t *testing.T) {
	cases := []struct{ in, want string }{
		{"\tx", "        x"},                 // 行首 tab → 补满 8 列
		{"1234567\tx", "1234567 x"},          // 第 7 列 → 补 1 格到第 8 列
		{"12345678\tx", "12345678        x"}, // 正好在制表位上 → 整跳 8 格
		{"中中\tx", "中中    x"},                 // 宽字符按 2 列算,4 列 → 补 4 格
	}
	for _, c := range cases {
		if got := expandTabsLine(c.in); got != c.want {
			t.Errorf("expandTabsLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// 带颜色的文本:ANSI 序列不占列,制表位应按可见字符算。
	colored := "\x1b[31mabc\x1b[0m\tx"
	got := expandTabsLine(colored)
	if ansi.StringWidth(got) != ansi.StringWidth("abc     x") {
		t.Errorf("ANSI 行制表位算错: %q (宽 %d)", got, ansi.StringWidth(got))
	}
}

// TestDropCursorMovers \b \v \f \a \x7f 会挪光标或换行,必须删掉;ESC 与 \n 必须留下。
func TestDropCursorMovers(t *testing.T) {
	in := "a\bb\vc\fd\ae\x7ff\x1b[31mg\x1b[0m\nh"
	got := dropCursorMovers(in)
	for _, bad := range []string{"\b", "\v", "\f", "\a", "\x7f"} {
		if strings.Contains(got, bad) {
			t.Errorf("没删掉 %q: %q", bad, got)
		}
	}
	if !strings.Contains(got, "\x1b[31m") {
		t.Errorf("ANSI 被误删: %q", got)
	}
	if !strings.Contains(got, "\n") {
		t.Errorf("换行被误删: %q", got)
	}
	if want := "abcdef\x1b[31mg\x1b[0m\nh"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestSanitizeNoAllocFastPath 干净文本原样返回(快路径),避免每帧无谓分配。
func TestSanitizeNoAllocFastPath(t *testing.T) {
	clean := "┃ 表单加白色面板（圆角+阴影）——草坪背景上保证文字可读。\n┃ 第二行"
	if got := sanitizeCursorMovers(clean); got != clean {
		t.Errorf("干净文本被改动了: %q", got)
	}
}

// TestFrameColumnLockWithTabs 端到端:整帧锁宽对含 \t 的行同样成立。
func TestFrameColumnLockWithTabs(t *testing.T) {
	const w, h = 40, 3
	frame := normalizeFrame("a\tb\n\tc\nplain", w, h)
	for i, ln := range strings.Split(frame, "\n") {
		if strings.ContainsRune(ln, '\t') {
			t.Fatalf("行%d 仍含 \\t: %q", i, ln)
		}
		if n := ansi.StringWidth(ln); n != w {
			t.Errorf("行%d 宽 = %d, want %d: %q", i, n, w, ln)
		}
	}
}
