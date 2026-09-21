package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// 本文件处理一族"库算 0 列、终端却会照着挪光标"的裸控制字符。
//
// 它们是"补白明明算对了、画出来却歪"的唯一剩余解释:padLinesToWidth / normalizeFrame
// 把每行锁到精确 N 列用的是 ansi.StringWidth,而这些字符在它眼里一律 0 列 ——
//
//	\t  终端跳到下一个 8 列制表位(往右多吃 1~8 列)
//	\b  光标左退 1 列
//	\v \f  下移一行,整帧行数跟着错
//	\a  响铃;其余 C0 / DEL 各终端行为不一
//
// 于是那一行实际画出来不是 N 列,右栏 ┃ 分隔线在这一行被顶偏;每行含的控制字符个数
// 不同,偏移量也不同,整条分隔线就成了参差的锯齿。
//
// 裸 \r 早先单独兜过(见 padLinesToWidth 与 refreshViewport 的注释),那次只修了这一族里
// 的一个成员 —— \t 才是 LLM 输出里最常见的那个(缩进、对齐、工具结果原样透传)。
//
// 处理分两类:
//   - \t 展开成空格(按 8 列制表位,与终端行为一致):保留视觉对齐,不丢内容;
//   - 其余一律删除 —— 已排版的展示文本里本就不该有它们,留着只会让光标乱跑。
//     ESC(0x1B)必须保留,ANSI 颜色全靠它;\n 保留,它是分行依据。
const tabStop = 8

// isCursorMover 判断 r 是否属于上述"会挪光标但不占列宽"的字符。
func isCursorMover(r rune) bool {
	switch r {
	case '\n', 0x1B:
		return false
	}
	return r < 0x20 || r == 0x7F
}

// expandTabsLine 把单行里的 \t 展开成到下一个制表位的空格。
// 列数用 ansi.StringWidth 累计,所以行内已有的 ANSI 转义不会被算进列宽。
func expandTabsLine(line string) string {
	if !strings.ContainsRune(line, '\t') {
		return line
	}
	parts := strings.Split(line, "\t")
	var sb strings.Builder
	sb.Grow(len(line) + len(parts)*tabStop)
	col := 0
	for i, p := range parts {
		sb.WriteString(p)
		col += ansi.StringWidth(p)
		if i == len(parts)-1 {
			break
		}
		pad := tabStop - col%tabStop
		sb.WriteString(strings.Repeat(" ", pad))
		col += pad
	}
	return sb.String()
}

// dropCursorMovers 删掉除 \n / ESC 之外的全部 C0 控制字符与 DEL。
// 调用方应先展开 \t(否则这里会把它直接删掉,丢掉缩进)。
func dropCursorMovers(s string) string {
	if !strings.ContainsFunc(s, isCursorMover) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if isCursorMover(r) {
			return -1
		}
		return r
	}, s)
}

// sanitizeCursorMovers 是给"已排版、准备上屏"的多行文本用的总入口:
// 逐行展开 \t,再删掉其余会挪光标的控制字符。
//
// 只读不改的快路径:绝大多数帧一个这类字符都没有,直接原样返回,不产生任何分配。
func sanitizeCursorMovers(s string) string {
	if s == "" || !strings.ContainsFunc(s, func(r rune) bool { return r == '\t' || isCursorMover(r) }) {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = dropCursorMovers(expandTabsLine(ln))
	}
	return strings.Join(lines, "\n")
}
