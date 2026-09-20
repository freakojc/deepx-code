package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"deepx/agent"
)

// askModel 造一个待答状态的 model:两题(一单选一多选),askCustom / askInput 已就绪。
func askModel() *model {
	qs := []agent.AskQuestion{
		{Question: "要不要同时更新 README?", Options: []agent.AskOption{{Label: "更新", Value: "update"}, {Label: "先不更新", Value: "skip"}}},
		// Value 显式写上 —— 真实流程里 parseAskUserArgs 会把空 Value 回填成 Label,这里照它的产物造。
		{Question: "启用哪些特性?", Multiple: true, Options: []agent.AskOption{{Label: "缓存", Value: "缓存"}, {Label: "压缩", Value: "压缩"}}},
	}
	m := &model{askPending: true, askQuestions: qs, askQIdx: 0, askCustom: make([]string, 2), askInput: newAskInput()}
	m.askSelected = [][]bool{make([]bool, 2), make([]bool, 2)}
	return m
}

// selectedFor 从回传 JSON 里取第 qi 题的 selected。
func selectedFor(t *testing.T, raw string, qi int) []string {
	t.Helper()
	var out struct {
		Answers []struct {
			Question string   `json:"question"`
			Selected []string `json:"selected"`
		} `json:"answers"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("回传的不是合法 JSON: %v\n%s", err, raw)
	}
	if qi >= len(out.Answers) {
		t.Fatalf("答案里没有第 %d 题: %s", qi, raw)
	}
	return out.Answers[qi].Selected
}

// TestAskCustomIndexIsLast 「其他」行恒在真实选项之后一行 —— 光标边界和渲染都靠这个下标。
func TestAskCustomIndexIsLast(t *testing.T) {
	q := agent.AskQuestion{Options: []agent.AskOption{{Label: "A"}, {Label: "B"}}}
	if got := askCustomIdx(q); got != 2 {
		t.Fatalf("两个选项时「其他」应在下标 2, got %d", got)
	}
	if got := askCustomIdx(agent.AskQuestion{}); got != 0 {
		t.Fatalf("无选项时「其他」应在下标 0, got %d", got)
	}
}

// TestAskAnsweredCountsCustom 自由文本非空即算已作答(不必再勾任何预设项);纯空白不算。
func TestAskAnsweredCountsCustom(t *testing.T) {
	none := []bool{false, false}
	if askQuestionAnswered(none, "") {
		t.Fatal("什么都没选不该算已作答")
	}
	if askQuestionAnswered(none, "   ") {
		t.Fatal("只打了空白不该算已作答")
	}
	if !askQuestionAnswered(none, "走第三条路") {
		t.Fatal("写了自由文本就该算已作答")
	}
	if !askQuestionAnswered([]bool{true, false}, "") {
		t.Fatal("勾了预设项就该算已作答")
	}
}

// TestAskCustomFlowsIntoAnswer 自由文本要原样进回传 JSON 和作答档案 —— 模型侧看到的就是用户的原话。
func TestAskCustomFlowsIntoAnswer(t *testing.T) {
	m := askModel()
	m.askOptIdx = askCustomIdx(m.askQuestions[0])
	m.askInput.SetValue("只更新 README.md,别动其它语言版本")
	m.syncAskCustom()

	sel := selectedFor(t, m.buildAskAnswer(), 0)
	if len(sel) != 1 || sel[0] != "只更新 README.md,别动其它语言版本" {
		t.Fatalf("自由文本应原样作为答案值, got %v", sel)
	}
	if rec := m.askRecord(false); !strings.Contains(rec, "→ **只更新 README.md,别动其它语言版本**") {
		t.Fatalf("作答档案应记下自由文本, got:\n%s", rec)
	}
}

// TestAskCustomCoexistsWithPresets 多选题里,自由文本与勾选的预设项并存,一起回传。
func TestAskCustomCoexistsWithPresets(t *testing.T) {
	m := askModel()
	m.askSelected[1][0] = true // 勾「缓存」
	m.askCustom[1] = "还要限流"

	sel := selectedFor(t, m.buildAskAnswer(), 1)
	if len(sel) != 2 || sel[0] != "缓存" || sel[1] != "还要限流" {
		t.Fatalf("多选应同时带上预设项和自由文本, got %v", sel)
	}
}

// TestAskCustomTrimmedAndSkippedWhenBlank 只有空白的自由文本不该混进答案(否则模型会收到一个空字符串答案)。
func TestAskCustomTrimmedAndSkippedWhenBlank(t *testing.T) {
	m := askModel()
	m.askSelected[0][0] = true
	m.askInput.SetValue("   ")
	m.syncAskCustom()

	sel := selectedFor(t, m.buildAskAnswer(), 0)
	if len(sel) != 1 || sel[0] != "update" {
		t.Fatalf("空白自由文本应被丢掉, got %v", sel)
	}
}

// TestAskCustomSurvivesQuestionSwitch 切题要把输入框内容存下来、再把目标题的装回来,
// 否则用户在第一题打的字一切题就没了。
func TestAskCustomSurvivesQuestionSwitch(t *testing.T) {
	m := askModel()
	m.askOptIdx = askCustomIdx(m.askQuestions[0])
	m.askInput.SetValue("第一题的自由答案")

	// 切到第二题:存第一题、装第二题(空)
	m.syncAskCustom()
	m.askQIdx = 1
	m.loadAskCustom(1)
	if m.askInput.Value() != "" {
		t.Fatalf("切到没写过的题,输入框应为空, got %q", m.askInput.Value())
	}
	m.askInput.SetValue("第二题的自由答案")

	// 切回第一题
	m.syncAskCustom()
	m.askQIdx = 0
	m.loadAskCustom(0)
	if m.askInput.Value() != "第一题的自由答案" {
		t.Fatalf("切回来应拿到原文, got %q", m.askInput.Value())
	}

	// 两题的文本各自独立地进了答案
	raw := m.buildAskAnswer()
	if s := selectedFor(t, raw, 0); len(s) != 1 || s[0] != "第一题的自由答案" {
		t.Fatalf("第一题答案错了: %v", s)
	}
	if s := selectedFor(t, raw, 1); len(s) != 1 || s[0] != "第二题的自由答案" {
		t.Fatalf("第二题答案错了: %v", s)
	}
}

// TestAskCustomClearsPresetsOnSingle 单选题里往「其他」写字 = 放弃预设项,两者必须互斥,
// 否则会回传两个答案给一道单选题。
func TestAskCustomClearsPresetsOnSingle(t *testing.T) {
	m := askModel()
	m.askSelected[0][0] = true
	m.askOptIdx = askCustomIdx(m.askQuestions[0])
	m.askInput.SetValue("两个都不对")
	m.clearAskPresets() // 按键路径里输入框内容一变就会调它
	m.syncAskCustom()

	sel := selectedFor(t, m.buildAskAnswer(), 0)
	if len(sel) != 1 || sel[0] != "两个都不对" {
		t.Fatalf("单选题应只剩自由文本, got %v", sel)
	}
}

// TestAskBlockRendersCustomRow 卡片必须渲染出「其他」这一行,否则用户根本不知道能自己输入。
func TestAskBlockRendersCustomRow(t *testing.T) {
	m := askModel()
	if got := m.askUserBlock(); !strings.Contains(got, "其他") {
		t.Fatalf("待答卡片应有「其他」行, got:\n%s", got)
	}
	// 写了字之后,卡片上应显示写的内容
	m.askOptIdx = askCustomIdx(m.askQuestions[0])
	m.askInput.SetValue("自定义答案XYZ")
	m.syncAskCustom()
	if got := m.askUserBlock(); !strings.Contains(got, "自定义答案XYZ") {
		t.Fatalf("卡片应显示已输入的自由文本, got:\n%s", got)
	}
}
