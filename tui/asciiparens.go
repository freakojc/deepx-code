package tui

import (
	"os"
	"strings"
)

// 全角标点折半:显示层把全角标点换成对应的半角 ASCII。
//
// 起因是 VS Code 终端里,含宽字符的行右侧分栏会往左缩,而 deepx 侧所有可测量的东西都证明
// 列位置是对的 —— 帧自检逐行等宽、字节流重建分割线全在同一列、DSR 逐行实测同列、
// 静态打印正常;换分割线字符 / 关 customGlyphs / 关差分 / 全量重绘 / 关优化序列均无效。
// 根因在 VS Code 侧,没能定位;实测把全角标点折成半角可以消除错位,这里是绕开。
//
// 触发源横跨两个 Unicode 区,实测都会引发错位:
//
//	Fullwidth Forms   U+FF01–FF5E   （）：，；！？ 等,减 0xFEE0 即为对应 ASCII
//	CJK 符号标点       U+3000–303F   。、「」『』【】《》 等,无算术规律,逐个列表
//
// 折不掉的只有汉字本身(U+4E00+,同样是 2 列宽)。所以这是缓解不是修复:
// 若汉字也参与触发,再怎么折都堵不干净。
//
// **必须在 markdown 渲染之前做** —— glamour 按列宽折行,全角 2 列、半角 1 列,
// 换完再渲染折行才算得对;渲染完再换,行会凭空变短,排好的版就废了。
//
// 只作用于**显示**:chatContent 是展示用副本,发给模型的历史、落盘的会话文件都不受影响。
// 代价是拖选复制出来的也是半角(复制与显示同源,见 renderChatDisplayContent)。
//
// DEEPX_ASCII_PUNCT:
//
//	未设          仅 VS Code 下开启,折掉全部能折的
//	1 / on / all  强制开启,折掉全部能折的
//	min           保守集:只折成对括号与冒号,保留 。、，；！？(中文观感更自然)
//	0 / off       关闭
var (
	asciiPunctOn  bool
	asciiPunctMin *strings.Replacer // 非 nil 表示走保守集
)

// minFoldPairs 是保守集:折了之后中文观感仍然自然的那些。
// 成对括号类换成 ASCII 括号/引号影响不大;句号、顿号、逗号折半最难看,不在此列。
var minFoldPairs = []string{
	"（", "(", "）", ")",
	"：", ":",
	"「", "\"", "」", "\"",
	"『", "\"", "』", "\"",
	"【", "[", "】", "]",
	"〔", "[", "〕", "]",
	"〖", "[", "〗", "]",
	"《", "<", "》", ">",
	"〈", "<", "〉", ">",
}

// cjkPunctPairs 是 CJK 符号标点区(U+3000–303F)里能折的全部。
// 这个区没有 Fullwidth Forms 那种减 0xFEE0 的规律,只能逐个列。
var cjkPunctPairs = append([]string{
	"　", " ", // 表意空格
	"。", ".",
	"、", ",",
	"〜", "~",
	"・", ".",
	"〝", "\"", "〞", "\"",
}, minFoldPairs[6:]...) // 复用 minFoldPairs 里的成对括号(前 6 项是 FF 区的,由算术映射覆盖)

var (
	minFolder = strings.NewReplacer(minFoldPairs...)
	cjkFolder = strings.NewReplacer(cjkPunctPairs...)
)

func init() {
	switch strings.TrimSpace(os.Getenv("DEEPX_ASCII_PUNCT")) {
	case "0", "off", "false":
		asciiPunctOn = false
	case "min":
		asciiPunctOn, asciiPunctMin = true, minFolder
	case "1", "on", "true", "all", "full":
		asciiPunctOn = true
	default:
		asciiPunctOn = os.Getenv("TERM_PROGRAM") == "vscode"
	}
}

// foldFullwidthPunct 在开启时把全角标点折成半角;关闭时原样返回,零开销。
func foldFullwidthPunct(s string) string {
	if !asciiPunctOn || s == "" {
		return s
	}
	if asciiPunctMin != nil {
		return asciiPunctMin.Replace(s)
	}
	s = cjkFolder.Replace(s)
	if !strings.ContainsFunc(s, isFullwidthForm) {
		return s
	}
	// Fullwidth Forms:减 0xFEE0 即为对应 ASCII。
	return strings.Map(func(r rune) rune {
		if isFullwidthForm(r) {
			return r - 0xFEE0
		}
		return r
	}, s)
}

func isFullwidthForm(r rune) bool { return r >= 0xFF01 && r <= 0xFF5E }
