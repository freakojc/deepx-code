package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"deepx/agent"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// newAskInput 造「其他」行的输入框。窄一点(46)是为了连同行首标记一起塞进 54 宽的卡片内容区。
func newAskInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "" // 去掉默认 "> ",行首已有 ▸ / ✎ 标记
	ti.Placeholder = "其他（直接输入你的答案）"
	ti.CharLimit = 500
	ti.SetWidth(46)
	ti.Focus()
	return ti
}

// askCustomIdx 是「其他」输入行在选项列表里的下标 —— 恒为真实选项之后的那一行。
func askCustomIdx(q agent.AskQuestion) int { return len(q.Options) }

// askQuestionAnswered 判断某题是否已作答:勾了任一预设项,或「其他」里写了字(issue #242)。
func askQuestionAnswered(sel []bool, custom string) bool {
	if strings.TrimSpace(custom) != "" {
		return true
	}
	for _, on := range sel {
		if on {
			return true
		}
	}
	return false
}

// askCustomText 取某题「其他」的文本:当前题以输入框为准(用户正在打字,还没回写),其余取存档。
func (m model) askCustomText(qi int) string {
	if qi == m.askQIdx {
		return strings.TrimSpace(m.askInput.Value())
	}
	if qi < len(m.askCustom) {
		return strings.TrimSpace(m.askCustom[qi])
	}
	return ""
}

// syncAskCustom 把输入框内容回写到当前题的存档。切题 / 提交前必须调,否则刚打的字会丢。
func (m *model) syncAskCustom() {
	if m.askQIdx < len(m.askCustom) {
		m.askCustom[m.askQIdx] = m.askInput.Value()
	}
}

// loadAskCustom 把目标题的存档装回输入框(切题时用)。
func (m *model) loadAskCustom(qi int) {
	v := ""
	if qi < len(m.askCustom) {
		v = m.askCustom[qi]
	}
	m.askInput.SetValue(v)
}

// clearAskPresets 清掉当前题所有预设项的勾选。单选题里用户往「其他」打字时调 ——
// 单选就该互斥,自由作答同样是"一个答案"。
func (m *model) clearAskPresets() {
	for i := range m.askSelected[m.askQIdx] {
		m.askSelected[m.askQIdx][i] = false
	}
}

// === AskUser 选择题弹窗 ===
//
// LLM 调用 AskUser 工具时,agent 循环发来 AskUserMsg 并阻塞等待;TUI 弹此框让用户勾选,
// 提交后把结果 JSON 写回 channel(buildAskAnswer),agent 拿到后作为工具结果回传给模型。
// 键盘:↑↓ 移光标,空格 勾选(单选互斥/多选可叠加),←→ 切题,Enter 下一题或最后一题提交,Esc 取消。

// buildAskAnswer 把当前各题勾选态组装成回传给 LLM 的 JSON:
//
//	{"answers":[{"question":"...","selected":["value", ...]}, ...]}
func (m model) buildAskAnswer() string {
	type ans struct {
		Question string   `json:"question"`
		Selected []string `json:"selected"`
	}
	out := struct {
		Answers []ans `json:"answers"`
	}{}
	for qi, q := range m.askQuestions {
		sel := []string{}
		for oi, on := range m.askSelected[qi] {
			if on {
				sel = append(sel, q.Options[oi].Value)
			}
		}
		// 「其他」的自由文本作为一个普通答案值混进 selected —— 模型侧不需要知道它是自由输入的,
		// 拿到的就是用户的答案本身(issue #242)。
		if c := m.askCustomText(qi); c != "" {
			sel = append(sel, c)
		}
		out.Answers = append(out.Answers, ans{Question: q.Question, Selected: sel})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// askRecord 把作答结果折叠成一段紧凑的 markdown 档案,作为 kindSystem 段写进对话流留痕(issue #134):
// 每题一行「❓ 问题 → **已选答案**」(多选用顿号连接);skipped=true 时记「（已跳过）」。
// 待答时的完整交互卡片(askUserBlock)消失后,scrollback 里仍能看到问了什么、选了什么。
func (m model) askRecord(skipped bool) string {
	lines := make([]string, 0, len(m.askQuestions))
	for qi, q := range m.askQuestions {
		if skipped {
			lines = append(lines, fmt.Sprintf("❓ %s —（已跳过）", q.Question))
			continue
		}
		var sel []string
		for oi, on := range m.askSelected[qi] {
			if on {
				sel = append(sel, q.Options[oi].Label)
			}
		}
		if c := m.askCustomText(qi); c != "" {
			sel = append(sel, c)
		}
		ans := "（未选）"
		if len(sel) > 0 {
			ans = strings.Join(sel, "、")
		}
		lines = append(lines, fmt.Sprintf("❓ %s → **%s**", q.Question, ans))
	}
	return strings.Join(lines, "\n\n")
}

// askUserBlock 渲染 AskUser 选择题弹窗(当前题)。
func (m model) askUserBlock() string {
	if len(m.askQuestions) == 0 || m.askQIdx >= len(m.askQuestions) {
		return ""
	}
	q := m.askQuestions[m.askQIdx]

	dim := lipgloss.NewStyle().Foreground(subtleColor)
	on := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(highlightColor)

	// 标题:多题时带进度;标注单/多选
	head := "请选择"
	if len(m.askQuestions) > 1 {
		head = fmt.Sprintf("需求确认  %d/%d", m.askQIdx+1, len(m.askQuestions))
	}
	kind := "单选"
	if q.Multiple {
		kind = "多选"
	}
	title := titleStyle.Render(head) + dim.Render("   ("+kind+")")

	question := lipgloss.NewStyle().Bold(true).Foreground(softFgColor).Width(54).Render(q.Question)

	var rows []string
	for i, opt := range q.Options {
		selected := m.askSelected[m.askQIdx][i]
		var box string
		if q.Multiple {
			box = "☐"
			if selected {
				box = "☑"
			}
		} else {
			box = "○"
			if selected {
				box = "●"
			}
		}
		marker := "  "
		if i == m.askOptIdx {
			marker = "▸ "
		}
		seg := marker + box + " " + opt.Label
		switch {
		case selected:
			seg = on.Render(seg)
		case i == m.askOptIdx:
			seg = lipgloss.NewStyle().Foreground(softFgColor).Render(seg)
		default:
			seg = dim.Render(seg)
		}
		rows = append(rows, seg)
	}

	// 末行:「其他」自由输入(issue #242)。预设选项覆盖不到时,用户在这里直接写答案。
	// 光标停在这行时渲染真输入框(带光标);不在这行时渲染已写的文本或提示语。
	custom := m.askCustomText(m.askQIdx)
	onCustom := m.askOptIdx == askCustomIdx(q)
	var box, body string
	if custom != "" {
		box = "●"
		if q.Multiple {
			box = "☑"
		}
	} else {
		box = "○"
		if q.Multiple {
			box = "☐"
		}
	}
	if onCustom {
		body = m.askInput.View()
	} else if custom != "" {
		body = custom
	} else {
		body = "其他（移到这里直接输入）"
	}
	marker := "  "
	if onCustom {
		marker = "▸ "
	}
	seg := marker + box + " " + body
	switch {
	case custom != "":
		seg = on.Render(marker+box+" ") + body // 文本本身不上色,免得盖掉输入框自己的光标样式
	case onCustom:
		seg = lipgloss.NewStyle().Foreground(softFgColor).Render(marker+box+" ") + body
	default:
		seg = dim.Render(seg)
	}
	rows = append(rows, seg)

	// 操作提示:预设项只认空格,「其他」行直接打字;Enter 仅用于前进/提交。
	nextLabel := "Enter 提交"
	if m.askQIdx < len(m.askQuestions)-1 {
		nextLabel = "Enter 下一题"
	}
	hint := "↑↓ 移动 · 空格 选择 · " + nextLabel
	if len(m.askQuestions) > 1 {
		// 光标在「其他」行时 ←→ 归输入框移动光标,切题改用 Tab(见按键处理)。
		hint += " · Tab 切题"
	}
	footer := dim.Render(hint + " · Esc 取消")

	parts := []string{title, "", question, ""}
	parts = append(parts, rows...)
	// 没选就回车 → 红色闪烁警告(用空格选),提醒用户下次记住正确操作。
	if m.askWarn {
		blinkOn := (time.Now().UnixMilli()/350)%2 == 0
		c := lipgloss.Color("196") // 亮红
		if !blinkOn {
			c = lipgloss.Color("88") // 暗红,形成闪烁
		}
		warn := lipgloss.NewStyle().Bold(true).Foreground(c).Render("⚠ 请先用【空格】选中,或在「其他」行输入答案")
		parts = append(parts, "", warn)
	}
	parts = append(parts, "", footer)
	content := lipgloss.JoinVertical(lipgloss.Left, parts...)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(highlightColor).
		Padding(1, 2).
		Width(60).
		Render(content)
}
