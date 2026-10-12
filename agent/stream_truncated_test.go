package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// streamAttempt 对流被服务端掐断(有内容但从未收到 [DONE])必须返回 errStreamTruncated,
// 而不是把半截回复当正常完成(根因:AskUser 提交后连接被掐,只回半截 reasoning 却显示"完成")。
func TestStreamAttempt_DetectsTruncatedStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter 不支持 Flush")
			return
		}
		f.Flush()
		// 只给一段 reasoning(半截思考),不给 [DONE] → 连接被掐的场景
		fmt.Fprint(w, `data: {"choices":[{"delta":{"reasoning_content":"正在分析…"}}]}`+"\n\n")
		f.Flush()
		// handler 返回 → 连接干净关闭,但流缺 [DONE] 结束标记
	}))
	t.Cleanup(srv.Close)

	ch := make(chan tea.Msg, 16)
	content, reasoning, _, _, _, err := streamAttempt(
		context.Background(), "sk-test", srv.URL, "test-model",
		nil, 0, nil, "", "", ch,
	)
	if !errors.Is(err, errStreamTruncated) {
		t.Fatalf("应返回 errStreamTruncated, got %v (content=%q reasoning=%q)", err, content, reasoning)
	}
	if reasoning != "正在分析…" {
		t.Errorf("半截 reasoning 应原样返回, got %q", reasoning)
	}
}

// 对照:正常流(带 [DONE])不应误报截断。
func TestStreamAttempt_NormalDoneOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter 不支持 Flush")
			return
		}
		f.Flush()
		fmt.Fprint(w, `data: {"choices":[{"delta":{"reasoning_content":"思考"}}]}`+"\n\n")
		f.Flush()
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"你好"}}]}`+"\n\n")
		f.Flush()
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`+"\n\n")
		f.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		f.Flush()
	}))
	t.Cleanup(srv.Close)

	ch := make(chan tea.Msg, 16)
	content, _, toolCalls, fr, _, err := streamAttempt(
		context.Background(), "sk-test", srv.URL, "test-model",
		nil, 0, nil, "", "", ch,
	)
	if err != nil {
		t.Fatalf("正常流不应报错: %v", err)
	}
	if content != "你好" {
		t.Errorf("content = %q, want 你好", content)
	}
	if fr != "stop" || len(toolCalls) != 0 {
		t.Errorf("finish_reason/toolCalls 不符: fr=%q calls=%d", fr, len(toolCalls))
	}
}

// 主循环集成:上游连续返回「有内容但无 [DONE]」的截断流 → agent 自动发送继续(maxGateNudges 次),
// 仍拿不到完整 SSE 流则抛出可读错误结束(不再静默当"完成"、也不无限空转)。
func TestStreamTruncated_RetriesThenErrors(t *testing.T) {
	var reqs atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f, _ := w.(http.Flusher)
		f.Flush()
		// 半截 reasoning,不发 [DONE] → 每次都掐断
		fmt.Fprint(w, `data: {"choices":[{"delta":{"reasoning_content":"思考中"}}]}`+"\n\n")
		f.Flush()
	}))
	t.Cleanup(srv.Close)

	cfg := ModelConfig{Flash: ModelEntry{BaseURL: srv.URL, Model: "m", APIKey: "k", MaxTokens: 256}}
	hist := []ChatMessage{{Role: "user", Content: "请完成这个任务"}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, ch := StartStream(ctx, cfg, hist, AgentMode_Auto, t.TempDir(), "", "", "flash", WorkingModeDefault)

	var gotErr error
	for msg := range ch {
		if em, ok := msg.(StreamErrMsg); ok {
			gotErr = em.Err
		}
	}

	if !errors.Is(gotErr, errStreamTruncatedLoop) {
		t.Fatalf("连续截断后应抛 errStreamTruncatedLoop, got %v", gotErr)
	}
	// 1 次初始请求 + maxGateNudges(3) 次自动"发送继续"重试 = 4 次请求
	if n := reqs.Load(); n != maxGateNudges+1 {
		t.Errorf("请求次数应 = maxGateNudges+1 = %d, got %d", maxGateNudges+1, n)
	}
}
