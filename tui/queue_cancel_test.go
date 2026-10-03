package tui

import (
	"errors"
	"strings"
	"testing"

	"deepx/web"

	tea "charm.land/bubbletea/v2"
)

// fakeClipboard 替换 clipboardCopy:记录收到的文本,按 wantErr 决定成败。测试结束自动还原。
func fakeClipboard(t *testing.T, wantErr error) (got *string, calls *int) {
	t.Helper()
	got = new(string)
	calls = new(int)
	old := clipboardCopy
	clipboardCopy = func(text string) error {
		*calls++
		*got = text
		return wantErr
	}
	t.Cleanup(func() { clipboardCopy = old })
	return got, calls
}

// newQueueModel 造一个有排队消息、能收 appendChat 的最小 model。
func newQueueModel(msgs ...string) (*model, *web.Hub) {
	hub := web.NewHub("f", "p", "/tmp/ws", "zh")
	m := &model{chatContent: newChatLog(1 << 20), hub: hub, queuedInput: msgs}
	// 队列初态推进 hub,模拟 TUI 排队时的 broadcastQueued。
	m.broadcastQueued()
	return m, hub
}

// TestCancelQueuedInputCopiesAllThenClears 核心诉求:多条消息整份(按顺序、空行分隔)复制进
// 剪贴板后才清空队列,并经 queued 事件把清空同步给 web。
func TestCancelQueuedInputCopiesAllThenClears(t *testing.T) {
	got, calls := fakeClipboard(t, nil)
	m, hub := newQueueModel("第一条 待发送", "第二条\n带换行", "第三条")

	m.cancelQueuedInput()

	if *calls != 1 {
		t.Fatalf("应恰好写一次剪贴板, got %d", *calls)
	}
	if want := "第一条 待发送\n\n第二条\n带换行\n\n第三条"; *got != want {
		t.Fatalf("剪贴板内容不符\nwant %q\ngot  %q", want, *got)
	}
	if len(m.queuedInput) != 0 {
		t.Fatalf("复制成功后队列应清空, got %v", m.queuedInput)
	}
	if s := hub.SnapshotCopy(); len(s.Queued) != 0 {
		t.Fatalf("web 待发送区应同步清空, got %v", s.Queued)
	}
	if log := m.chatContent.String(); !strings.Contains(log, "3") || !strings.Contains(log, "剪贴板") {
		t.Fatalf("回执应写明条数与已复制, got %q", log)
	}
}

// TestCancelQueuedInputKeepsQueueOnCopyFailure 复制失败必须保留原文 —— 打好的话不能丢,
// 也不能把失败说成已取消。
func TestCancelQueuedInputKeepsQueueOnCopyFailure(t *testing.T) {
	got, calls := fakeClipboard(t, errors.New("no display"))
	m, hub := newQueueModel("a", "b")

	m.cancelQueuedInput()

	if *calls != 1 || *got != "a\n\nb" {
		t.Fatalf("应尝试复制整份队列, calls=%d text=%q", *calls, *got)
	}
	if len(m.queuedInput) != 2 {
		t.Fatalf("复制失败不该清队列, got %v", m.queuedInput)
	}
	if s := hub.SnapshotCopy(); len(s.Queued) != 2 {
		t.Fatalf("队列未取消, web 展示不该被动, got %v", s.Queued)
	}
	if log := m.chatContent.String(); !strings.Contains(log, "失败") {
		t.Fatalf("应提示复制失败, got %q", log)
	}
}

// TestCancelQueuedInputEmptyIsNoop 空队列按 Ctrl+Q:不碰剪贴板,只给一条提示。
func TestCancelQueuedInputEmptyIsNoop(t *testing.T) {
	got, calls := fakeClipboard(t, nil)
	m, _ := newQueueModel()

	m.cancelQueuedInput()

	if *calls != 0 {
		t.Fatalf("空队列不该写剪贴板, text=%q", *got)
	}
	if log := m.chatContent.String(); !strings.Contains(log, "没有待发送") {
		t.Fatalf("空队列应有提示, got %q", log)
	}
}

// TestCtrlQKeyBindingCancelsQueue Update 层接线:流式中按 ctrl+q 走 cancelQueuedInput,
// 且不打断正在跑的 stream(streaming / streamCh 原样保留)。
// Update 是值接收者(按键改的是副本),断言一律读返回的那个 model。
func TestCtrlQKeyBindingCancelsQueue(t *testing.T) {
	got, calls := fakeClipboard(t, nil)
	m, hub := newQueueModel("x", "y", "z")
	m.streaming = true
	m.streamCh = make(chan tea.Msg, 1) // 占位:断言没被清空

	mm, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: 'q', Mod: tea.ModCtrl}))
	out := mm.(model)

	if *calls != 1 || *got != "x\n\ny\n\nz" {
		t.Fatalf("ctrl+q 应复制整份队列, calls=%d text=%q", *calls, *got)
	}
	if len(out.queuedInput) != 0 {
		t.Fatalf("队列应已取消, got %v", out.queuedInput)
	}
	if !out.streaming || out.streamCh == nil {
		t.Fatalf("取消队列不能打断运行中的任务: streaming=%v streamCh=%v", out.streaming, out.streamCh)
	}
	if s := hub.SnapshotCopy(); len(s.Queued) != 0 {
		t.Fatalf("web 待发送区应同步清空, got %v", s.Queued)
	}
}
