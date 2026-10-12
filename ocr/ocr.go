// Package ocr 用 PaddleOCR PP-OCRv5_mobile 的 ONNX 模型做中文/英文 OCR。
// ONNX 推理走 github.com/getcharzp/onnxruntime_purego (purego dlopen,无需 cgo)。
// 检测后处理是 MVP 版:阈值二值化 + 连通域 + 轴对齐 bbox + 简单外扩,
// 没做 unclip / 多边形 / 旋转矫正,旋转/弯曲文字识别效果较差,
// 屏幕截图、文档、网页/对话框这类水平文字场景足够好。
package ocr

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"deepx/instance"

	ort "github.com/getcharzp/onnxruntime_purego"
)

// OCR 实例。线程不安全,Recognize 内部加锁,可多协程并发调用同一实例。
type OCR struct {
	mu       sync.Mutex
	cacheDir string

	engine  *ort.Engine
	det     *ort.Session
	rec     *ort.Session
	charset []string

	loaded  bool
	loadErr error
}

// global 默认实例。整个进程共享同一份模型/引擎,避免重复加载。
var (
	global     *OCR
	globalOnce sync.Once
)

// DefaultCacheDir 默认资产缓存目录:<配置根>/ocr(跨实例共享,只放 ORT 库/模型等可重建资产;
// 粘贴图片缓存另见 tools.PasteCacheDir——那是实例隔离的 ocr/cache,两者只差一层)。
func DefaultCacheDir() string {
	if d := instance.SharedDir("ocr"); d != "" {
		return d
	}
	return filepath.Join(os.TempDir(), "deepx", "ocr")
}

// Global 返回进程全局 OCR 实例 (惰性初始化,资产未就绪时返回未 Load 的实例)。
func Global() *OCR {
	globalOnce.Do(func() {
		global = New(DefaultCacheDir())
	})
	return global
}

// New 构造一个 OCR 实例,模型先不加载,首次 Recognize 时才加载。
func New(cacheDir string) *OCR {
	return &OCR{cacheDir: cacheDir}
}

// IsReady 资产是否已下载齐备 (有 .ready 标记)。未就绪时调用方应先 EnsureAssets。
func (o *OCR) IsReady() bool { return isReady(o.cacheDir) }

// CacheDir 返回缓存目录,外部可据此告知用户首次下载位置。
func (o *OCR) CacheDir() string { return o.cacheDir }

// EnsureAssets 暴露给调用方的下载入口。包装顶层 EnsureAssets。
func (o *OCR) EnsureAssets(onProgress ProgressFunc) error {
	return EnsureAssets(o.cacheDir, onProgress)
}

// Result OCR 单条结果。
type Result struct {
	Text string
	Box  [4]int // 原图坐标系 [x0, y0, x1, y1]
}

// Recognize 对单张图片做端到端 OCR,返回每个文本块的文字与原图坐标。
// 第一次调用会加载模型 (慢,~几百毫秒),后续调用复用同一 session。
func (o *OCR) Recognize(imagePath string) ([]Result, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.loaded {
		o.load()
	}
	if o.loadErr != nil {
		return nil, o.loadErr
	}

	img, err := loadImage(imagePath)
	if err != nil {
		return nil, fmt.Errorf("load image: %w", err)
	}

	boxes, err := o.runDet(img)
	if err != nil {
		return nil, err
	}

	out := make([]Result, 0, len(boxes))
	for _, b := range boxes {
		crop := cropRect(img, b)
		text, err := o.runRec(crop)
		if err != nil {
			// 单个 box 失败不让整张图阵亡,记录空文本继续
			text = ""
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		out = append(out, Result{
			Text: text,
			Box:  [4]int{b.Min.X, b.Min.Y, b.Max.X, b.Max.Y},
		})
	}
	return out, nil
}

// RecognizeText 拼接 Recognize 返回的所有文字成一个字符串,
// 同一行的 box 用空格分隔,跨行用换行。供 tool calling 直接拿。
func (o *OCR) RecognizeText(imagePath string) (string, error) {
	results, err := o.Recognize(imagePath)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "", nil
	}
	// 估算行高,同行合并空格
	var lines []string
	var currentLine []string
	prevYCenter := -1
	for _, r := range results {
		y := (r.Box[1] + r.Box[3]) / 2
		if prevYCenter < 0 || abs(y-prevYCenter) < (r.Box[3]-r.Box[1])/2+1 {
			currentLine = append(currentLine, r.Text)
		} else {
			lines = append(lines, strings.Join(currentLine, " "))
			currentLine = []string{r.Text}
		}
		prevYCenter = y
	}
	if len(currentLine) > 0 {
		lines = append(lines, strings.Join(currentLine, " "))
	}
	return strings.Join(lines, "\n"), nil
}

// load 内部加载逻辑。要求 cacheDir 已含全套资产 (调用前确保 EnsureAssets)。
func (o *OCR) load() {
	o.loaded = true

	if !o.IsReady() {
		o.loadErr = fmt.Errorf("OCR 资产未就绪,请先调用 EnsureAssets() 下载到: %s", o.cacheDir)
		return
	}

	libPath := filepath.Join(o.cacheDir, ortLibName)
	engine, err := ort.NewEngine(libPath)
	if err != nil {
		o.loadErr = fmt.Errorf("初始化 onnxruntime 失败 (%s): %w", libPath, err)
		return
	}
	o.engine = engine

	opts, err := engine.NewSessionOptions()
	if err != nil {
		o.loadErr = fmt.Errorf("session options: %w", err)
		return
	}
	defer opts.Destroy()
	// 默认线程数 = 物理核数的一半,避免抢资源
	threads := int32(runtime.NumCPU() / 2)
	if threads < 1 {
		threads = 1
	}
	_ = opts.SetIntraOpNumThreads(threads)
	_ = opts.SetCpuMemArena(true)

	detSess, err := engine.NewSession(filepath.Join(o.cacheDir, detModelFile), opts)
	if err != nil {
		o.loadErr = fmt.Errorf("加载 det 模型: %w", err)
		return
	}
	o.det = detSess

	recSess, err := engine.NewSession(filepath.Join(o.cacheDir, recModelFile), opts)
	if err != nil {
		o.loadErr = fmt.Errorf("加载 rec 模型: %w", err)
		return
	}
	o.rec = recSess

	charset, err := loadDict(filepath.Join(o.cacheDir, dictFile))
	if err != nil {
		o.loadErr = fmt.Errorf("加载字典: %w", err)
		return
	}
	o.charset = charset
}

// Close 释放所有 native 资源。一般进程退出前调用,长时间空闲也可以主动收回内存。
func (o *OCR) Close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.rec != nil {
		o.rec.Destroy()
		o.rec = nil
	}
	if o.det != nil {
		o.det.Destroy()
		o.det = nil
	}
	if o.engine != nil {
		o.engine.Destroy()
		o.engine = nil
	}
	o.loaded = false
	o.loadErr = nil
}
