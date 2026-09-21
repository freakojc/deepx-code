package tui

import (
	"strings"
	"unicode/utf8"

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
	var sb strings.Builder
	sb.Grow(len(line) + tabStop)
	col := 0
	for i := 0; i < len(line); {
		// 转义序列原样带过,且不计列宽 —— OSC 载荷(如超链接 URL)里若含 \t 也不该被展开。
		if line[i] == 0x1B {
			n := escSeqLen(line[i:])
			sb.WriteString(line[i : i+n])
			i += n
			continue
		}
		if line[i] == '\t' {
			pad := tabStop - col%tabStop
			sb.WriteString(strings.Repeat(" ", pad))
			col += pad
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		sb.WriteString(line[i : i+size])
		col += ansi.StringWidth(string(r))
		i += size
	}
	return sb.String()
}

// dropCursorMovers 删掉正文里除 \n 之外的 C0 控制字符与 DEL,但**整条转义序列原样保留**。
// 调用方应先展开 \t(否则这里会把它直接删掉,丢掉缩进)。
//
// **必须跳过转义序列内部,不能按字符一刀切。** OSC 序列(ESC ] … )以 BEL(0x07)或 ST 终止,
// 而 BEL 本身是 C0 —— 按字符删会把终止符抹掉,OSC 变成未结束状态,终端会把紧随其后的
// 正文一并吞掉。glamour 把链接渲染成 OSC 8 超链接(ESC]8;id=…;URL BEL 链接文字 ),
// 这个坑的表现就是"本地 Web 面板已就绪"显示出来了、后面的地址整段消失。
func dropCursorMovers(s string) string {
	if !strings.ContainsFunc(s, isCursorMover) {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1B {
			n := escSeqLen(s[i:])
			sb.WriteString(s[i : i+n])
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isCursorMover(r) {
			sb.WriteString(s[i : i+size])
		}
		i += size
	}
	return sb.String()
}

// escSeqLen 返回 s 开头那条转义序列的字节长度(含终止符);s[0] 必须是 ESC。
// 序列不完整时返回 len(s) —— 宁可整段原样带过,也不要半途截断。
func escSeqLen(s string) int {
	if len(s) < 2 {
		return len(s)
	}
	switch s[1] {
	case '[': // CSI: ESC [ 参数 中间字节 final(0x40–0x7E)
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7E {
				return i + 1
			}
		}
		return len(s)
	case ']', 'P', 'X', '^', '_': // OSC / DCS / SOS / PM / APC:BEL 或 ST(ESC \\) 终止
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1B && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return len(s)
	}
	return 2 // ESC + 单字节(如 ESC = / ESC >)
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
