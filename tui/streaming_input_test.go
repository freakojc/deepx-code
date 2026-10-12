package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// 运行中(streaming)的输入与排队:打字应实时显示在输入框,Enter 应进入排队队列
// (不打断当前轮,本轮结束后自动续发)。
func TestStreamingTypingShowsAndEnterQueues(t *testing.T) {
	m := initModel()
	m.input.SetWidth(40)
	m.input.SetHeight(inputTextRows)
	m.streaming = true

	// 打字 "hi"
	for _, r := range []rune{'h', 'i'} {
		mm, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)}))
		m = mm.(model)
	}
	if m.input.Value() != "hi" {
		t.Fatalf("streaming 中输入应显示在输入框, got %q", m.input.Value())
	}

	// Enter → 排队
	mm, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = mm.(model)
	if len(m.queuedInput) != 1 || m.queuedInput[0] != "hi" {
		t.Fatalf("streaming 中 Enter 应排队, got %v", m.queuedInput)
	}
	if m.input.Value() != "" {
		t.Errorf("排队后输入框应清空, got %q", m.input.Value())
	}

	// 再打字 + Enter → 第二条排队(FIFO)
	for _, r := range []rune{'w' } {
		mm, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)}))
		m = mm.(model)
	}
	mm, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = mm.(model)
	if len(m.queuedInput) != 2 || m.queuedInput[1] != "w" {
		t.Fatalf("第二条应排到队尾, got %v", m.queuedInput)
	}

	// 空白 Enter 不应排队
	mm, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = mm.(model)
	if len(m.queuedInput) != 2 {
		t.Errorf("空白 Enter 不应排队, got %v", m.queuedInput)
	}
}

// 渲染层:streaming 进行中输入的文字必须在 View 输出里可见(输入框回显正常)。
func TestStreamingInputVisibleInView(t *testing.T) {
	m := initModel()
	m.streaming = true
	// 输入 "hi"
	for _, r := range []rune{'h', 'i'} {
		mm, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)}))
		m = mm.(model)
	}
	if m.input.Value() != "hi" {
		t.Fatalf("前置失败:输入未进入输入框, got %q", m.input.Value())
	}
	out := ansi.Strip(m.View().Content)
	if !strings.Contains(out, "hi") {
		t.Errorf("streaming 时输入文字应在渲染输出中可见。View:\n%s", out)
	}
}
