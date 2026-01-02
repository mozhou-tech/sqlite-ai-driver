package imagesearch

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"
)

// mockEmbedder 用于测试的简单嵌入生成器
type mockEmbedder struct {
	dimensions int
	err        error
}

func newMockEmbedder(dims int) *mockEmbedder {
	if dims <= 0 {
		dims = 128
	}
	return &mockEmbedder{dimensions: dims}
}

func (e *mockEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if e.err != nil {
		return nil, e.err
	}
	vec := make([]float64, e.dimensions)
	// 简单实现：基于文本内容生成固定向量
	for i := 0; i < len(text) && i < e.dimensions; i++ {
		vec[i] = float64(text[i]) / 255.0
	}
	// 填充剩余维度
	for i := len(text); i < e.dimensions; i++ {
		vec[i] = 0.1
	}
	return vec, nil
}

func (e *mockEmbedder) Dimensions() int {
	return e.dimensions
}

// mockOCR 用于测试的OCR实现
type mockOCR struct {
	text string
	err  error
}

func newMockOCR() *mockOCR {
	return &mockOCR{}
}

func (o *mockOCR) ExtractText(ctx context.Context, imagePath string) (string, error) {
	if o.err != nil {
		return "", o.err
	}
	if o.text != "" {
		return o.text, nil
	}
	// 返回基于文件名的模拟文本
	return fmt.Sprintf("OCR text from %s", filepath.Base(imagePath)), nil
}

func (o *mockOCR) ExtractTextFromReader(ctx context.Context, reader io.Reader) (string, error) {
	if o.err != nil {
		return "", o.err
	}
	return o.ExtractText(ctx, "")
}

// createTestImage 创建测试用的图片文件
func createTestImage(t *testing.T, path string) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}
	defer file.Close()

	// 使用PNG格式
	err = png.Encode(file, img)
	if err != nil {
		t.Fatalf("Failed to encode test image: %v", err)
	}
}

// waitForEmbeddings 等待embedding处理完成
func waitForEmbeddings(search *ImageSearch, maxWait time.Duration) {
	ctx := context.Background()
	deadline := time.Now().Add(maxWait)

	for time.Now().Before(deadline) {
		// 手动触发embedding处理
		if search.textsVector != nil {
			search.textsVector.processPendingEmbeddings(ctx)
		}
		if search.imagesVector != nil {
			search.imagesVector.processPendingEmbeddings(ctx)
		}

		// 检查是否还有pending的embedding（使用表前缀推断表名）
		var pendingCount int
		tablePrefix := search.tablePrefix
		if tablePrefix == "" {
			tablePrefix = "imagesearch_"
		}

		if search.textsVector != nil {
			querySQL := fmt.Sprintf(`
				SELECT COUNT(*) FROM %stexts 
				WHERE text_embedding IS NULL AND embedding_status = 'pending'
			`, tablePrefix)
			_ = search.db.QueryRowContext(ctx, querySQL).Scan(&pendingCount)
		}
		if search.imagesVector != nil {
			querySQL := fmt.Sprintf(`
				SELECT COUNT(*) FROM %simages 
				WHERE image_embedding IS NULL AND embedding_status = 'pending'
			`, tablePrefix)
			var imgPending int
			_ = search.db.QueryRowContext(ctx, querySQL).Scan(&imgPending)
			pendingCount += imgPending
		}

		if pendingCount == 0 {
			// 再等待一小段时间确保所有状态都已更新
			time.Sleep(200 * time.Millisecond)
			return
		}

		time.Sleep(200 * time.Millisecond)
	}
}

func TestNew(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	// 测试默认配置
	search := New(Options{
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
		WorkingDir:    "./testdata",
	})

	if search == nil {
		t.Fatal("New returned nil")
	}
	if search.textEmbedder != textEmbedder {
		t.Error("textEmbedder not set correctly")
	}
	if search.imageEmbedder != imageEmbedder {
		t.Error("imageEmbedder not set correctly")
	}
	if search.ocr == nil {
		t.Error("ocr not set correctly")
	}
	if search.tablePrefix != "imagesearch_" {
		t.Errorf("expected table prefix 'imagesearch_', got '%s'", search.tablePrefix)
	}
	if search.workingDir != "./testdata" {
		t.Errorf("expected working dir './testdata', got '%s'", search.workingDir)
	}
	if search.initialized {
		t.Error("search should not be initialized after New")
	}

	// 测试自定义配置
	search2 := New(Options{
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
		TablePrefix:   "custom_",
		WorkingDir:    "./testdata",
	})

	if search2.tablePrefix != "custom_" {
		t.Errorf("expected table prefix 'custom_', got '%s'", search2.tablePrefix)
	}
	if search2.workingDir != "./testdata" {
		t.Errorf("expected working dir './testdata', got '%s'", search2.workingDir)
	}
}

func TestInitializeStorages(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
		WorkingDir:    "./testdata",
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}

	if !search.initialized {
		t.Error("search should be initialized after InitializeStorages")
	}
	if search.images == nil {
		t.Error("images collection should be created")
	}
	if search.texts == nil {
		t.Error("texts collection should be created")
	}
	if search.imagesVector == nil {
		t.Error("imagesVector should be created")
	}
	if search.textsVector == nil {
		t.Error("textsVector should be created")
	}

	// 测试重复初始化
	err = search.InitializeStorages(ctx)
	if err != nil {
		t.Errorf("repeated InitializeStorages should not fail: %v", err)
	}

	// 清理
	search.Close(ctx)
}

func TestInitializeStorages_WithoutEmbedders(t *testing.T) {
	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  nil,
		ImageEmbedder: nil,
		OCR:           nil,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages should work without embedders: %v", err)
	}

	if search.imagesVector != nil {
		t.Error("imagesVector should be nil when imageEmbedder is nil")
	}
	if search.textsVector != nil {
		t.Error("textsVector should be nil when textEmbedder is nil")
	}

	search.Close(ctx)
}

func TestInsertImage_WithoutInitialization(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InsertImage(ctx, "test.jpg", nil)
	if err == nil {
		t.Error("InsertImage should fail when not initialized")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got: %v", err)
	}
}

func TestInsertImage_Basic(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	// 创建测试图片
	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "test.png")
	createTestImage(t, imagePath)

	// 插入图片
	err = search.InsertImage(ctx, imagePath, nil)
	if err != nil {
		t.Fatalf("InsertImage failed: %v", err)
	}

	// 等待embedding处理完成
	time.Sleep(500 * time.Millisecond)
}

func TestInsertImage_WithMetadata(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	// 创建测试图片
	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "test.png")
	createTestImage(t, imagePath)

	// 创建测试用的 Snowflake 生成器
	testSnowflake, _ := snowflake.NewNode(99)
	metadata := map[string]any{
		"source":   "test",
		"category": "example",
		"id":       testSnowflake.Generate(),
	}

	err = search.InsertImage(ctx, imagePath, metadata)
	if err != nil {
		t.Fatalf("InsertImage failed: %v", err)
	}

	// 等待embedding处理完成
	time.Sleep(500 * time.Millisecond)
}

func TestInsertImage_WithOCRError(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()
	ocr.err = fmt.Errorf("OCR error")

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	// 创建测试图片
	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "test.png")
	createTestImage(t, imagePath)

	// OCR错误不应该阻止插入
	err = search.InsertImage(ctx, imagePath, nil)
	if err != nil {
		t.Fatalf("InsertImage should continue even with OCR error: %v", err)
	}

	// 等待embedding处理完成
	time.Sleep(500 * time.Millisecond)
}

func TestInsertImage_InvalidPath(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	// 插入不存在的图片
	err = search.InsertImage(ctx, "/nonexistent/image.png", nil)
	if err == nil {
		t.Error("InsertImage should fail with invalid path")
	}
}

func TestInsertText_WithoutInitialization(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InsertText(ctx, "test text", nil)
	if err == nil {
		t.Error("InsertText should fail when not initialized")
	}
	if !strings.Contains(err.Error(), "not initialized") {
		t.Errorf("expected 'not initialized' error, got: %v", err)
	}
}

func TestInsertText_Basic(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	err = search.InsertText(ctx, "This is a test document with enough characters", nil)
	if err != nil {
		t.Fatalf("InsertText failed: %v", err)
	}

	// 等待embedding处理完成
	time.Sleep(500 * time.Millisecond)
}

func TestInsertText_TooShort(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	// 插入太短的文本（应该被跳过）
	err = search.InsertText(ctx, "short", nil)
	if err != nil {
		t.Fatalf("InsertText should skip short text without error: %v", err)
	}
}

func TestInsertText_WithMetadata(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	// 创建测试用的 Snowflake 生成器
	testSnowflake, _ := snowflake.NewNode(99)
	metadata := map[string]any{
		"source":   "test",
		"category": "document",
		"id":       testSnowflake.Generate(),
	}

	err = search.InsertText(ctx, "This is a test document with enough characters to be inserted", metadata)
	if err != nil {
		t.Fatalf("InsertText failed: %v", err)
	}

	// 等待embedding处理完成
	time.Sleep(500 * time.Millisecond)
}

func TestSearch_WithoutInitialization(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	results, err := search.Search(ctx, "query", 10, nil)
	if err == nil {
		t.Error("Search should fail when not initialized")
	}
	if results != nil {
		t.Error("Search should return nil results when failed")
	}
}

func TestSearch_WithoutEmbedder(t *testing.T) {
	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  nil,
		ImageEmbedder: nil,
		OCR:           nil,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	results, err := search.Search(ctx, "query", 10, nil)
	if err == nil {
		t.Error("Search should fail without embedder")
	}
	if !strings.Contains(err.Error(), "no embedder available") {
		t.Errorf("expected 'no embedder available' error, got: %v", err)
	}
	if results != nil {
		t.Error("Search should return nil results when failed")
	}
}

func TestSearch_Basic(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	// 插入文本
	err = search.InsertText(ctx, "The capital of France is Paris", nil)
	if err != nil {
		t.Fatalf("InsertText failed: %v", err)
	}

	err = search.InsertText(ctx, "The capital of Germany is Berlin", nil)
	if err != nil {
		t.Fatalf("InsertText failed: %v", err)
	}

	// 等待embedding处理完成
	waitForEmbeddings(search, 5*time.Second)

	// 搜索
	results, err := search.Search(ctx, "France capital", 10, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("Search should return at least one result")
	}

	// 验证结果格式
	for _, result := range results {
		if result.ID == "" {
			t.Error("result ID should not be empty")
		}
		if result.Content == "" {
			t.Error("result Content should not be empty")
		}
		if result.Score < 0 || result.Score > 1 {
			t.Errorf("similarity score should be between 0 and 1, got: %f", result.Score)
		}
	}
}

func TestSearch_WithMetadataFilter(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	// 插入带metadata的文本
	metadata1 := map[string]any{
		"category": "geography",
		"country":  "France",
	}
	err = search.InsertText(ctx, "The capital of France is Paris", metadata1)
	if err != nil {
		t.Fatalf("InsertText failed: %v", err)
	}

	metadata2 := map[string]any{
		"category": "geography",
		"country":  "Germany",
	}
	err = search.InsertText(ctx, "The capital of Germany is Berlin", metadata2)
	if err != nil {
		t.Fatalf("InsertText failed: %v", err)
	}

	// 等待embedding处理完成
	waitForEmbeddings(search, 5*time.Second)

	// 使用metadata过滤搜索
	filter := MetadataFilter{
		"country": "France",
	}
	results, err := search.Search(ctx, "capital", 10, filter)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// 验证结果都符合过滤条件
	for _, result := range results {
		if result.Data != nil {
			if country, ok := result.Data["country"].(string); ok {
				if country != "France" {
					t.Errorf("result should have country='France', got: %s", country)
				}
			}
		}
	}
}

func TestSearch_Limit(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	// 插入多个文档
	for i := 0; i < 5; i++ {
		err = search.InsertText(ctx, fmt.Sprintf("Document %d content with enough characters", i), nil)
		if err != nil {
			t.Fatalf("InsertText failed: %v", err)
		}
	}

	// 等待embedding处理完成
	waitForEmbeddings(search, 5*time.Second)

	// 测试limit
	results, err := search.Search(ctx, "document", 3, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}

	// 测试默认limit（当limit <= 0时）
	results, err = search.Search(ctx, "document", 0, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) > 10 {
		t.Errorf("expected at most 10 results (default), got %d", len(results))
	}
}

func TestSearch_WithImageEmbedderOnly(t *testing.T) {
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  nil,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	// 创建测试图片
	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "test.png")
	createTestImage(t, imagePath)

	err = search.InsertImage(ctx, imagePath, nil)
	if err != nil {
		t.Fatalf("InsertImage failed: %v", err)
	}

	// 等待embedding处理完成
	waitForEmbeddings(search, 5*time.Second)

	// 搜索（应该使用imageEmbedder）
	results, err := search.Search(ctx, "test query", 10, nil)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// 应该能找到图片
	if len(results) > 0 {
		// 验证结果
		for _, result := range results {
			if result.ID == "" {
				t.Error("result ID should not be empty")
			}
		}
	}
}

func TestHybridSearch_WithoutInitialization(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	results, err := search.HybridSearch(ctx, "query", 10, nil)
	if err == nil {
		t.Error("HybridSearch should fail when not initialized")
	}
	if results != nil {
		t.Error("HybridSearch should return nil results when failed")
	}
}

func TestHybridSearch_WithoutTextEmbedder(t *testing.T) {
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  nil,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	results, err := search.HybridSearch(ctx, "query", 10, nil)
	if err == nil {
		t.Error("HybridSearch should fail without textEmbedder")
	}
	if !strings.Contains(err.Error(), "text embedder is required") {
		t.Errorf("expected 'text embedder is required' error, got: %v", err)
	}
	if results != nil {
		t.Error("HybridSearch should return nil results when failed")
	}
}

func TestHybridSearch_WithoutImageEmbedder(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: nil,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	results, err := search.HybridSearch(ctx, "query", 10, nil)
	if err == nil {
		t.Error("HybridSearch should fail without imageEmbedder")
	}
	if !strings.Contains(err.Error(), "image embedder is required") {
		t.Errorf("expected 'image embedder is required' error, got: %v", err)
	}
	if results != nil {
		t.Error("HybridSearch should return nil results when failed")
	}
}

func TestHybridSearch_Basic(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	// 插入文本
	err = search.InsertText(ctx, "The capital of France is Paris", nil)
	if err != nil {
		t.Fatalf("InsertText failed: %v", err)
	}

	// 创建并插入图片
	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "test.png")
	createTestImage(t, imagePath)

	err = search.InsertImage(ctx, imagePath, nil)
	if err != nil {
		t.Fatalf("InsertImage failed: %v", err)
	}

	// 等待embedding处理完成
	waitForEmbeddings(search, 5*time.Second)

	// 混合搜索
	results, err := search.HybridSearch(ctx, "France", 10, nil)
	if err != nil {
		t.Fatalf("HybridSearch failed: %v", err)
	}

	// 验证结果
	if len(results) > 0 {
		for _, result := range results {
			if result.ID == "" {
				t.Error("result ID should not be empty")
			}
			if result.Score < 0 {
				t.Errorf("result score should be non-negative, got: %f", result.Score)
			}
			// Source 应该是 "text", "image", 或 "hybrid"
			if result.Source != "text" && result.Source != "image" && result.Source != "hybrid" {
				t.Errorf("unexpected source: %s", result.Source)
			}
		}
	}
}

func TestHybridSearch_WithMetadataFilter(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	// 插入带metadata的文本
	metadata1 := map[string]any{
		"category": "geography",
		"country":  "France",
	}
	err = search.InsertText(ctx, "The capital of France is Paris", metadata1)
	if err != nil {
		t.Fatalf("InsertText failed: %v", err)
	}

	// 插入带metadata的图片
	tmpDir := t.TempDir()
	imagePath := filepath.Join(tmpDir, "test.png")
	createTestImage(t, imagePath)

	metadata2 := map[string]any{
		"category": "geography",
		"country":  "France",
	}
	err = search.InsertImage(ctx, imagePath, metadata2)
	if err != nil {
		t.Fatalf("InsertImage failed: %v", err)
	}

	// 等待embedding处理完成
	waitForEmbeddings(search, 5*time.Second)

	// 使用metadata过滤的混合搜索
	filter := MetadataFilter{
		"country": "France",
	}
	results, err := search.HybridSearch(ctx, "capital", 10, filter)
	if err != nil {
		t.Fatalf("HybridSearch failed: %v", err)
	}

	// 验证结果都符合过滤条件
	for _, result := range results {
		if result.Data != nil {
			if country, ok := result.Data["country"].(string); ok {
				if country != "France" {
					t.Errorf("result should have country='France', got: %s", country)
				}
			}
		}
	}
}

func TestHybridSearch_Limit(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}
	defer search.Close(ctx)

	// 插入多个文档
	for i := 0; i < 5; i++ {
		err = search.InsertText(ctx, fmt.Sprintf("Document %d content with enough characters", i), nil)
		if err != nil {
			t.Fatalf("InsertText failed: %v", err)
		}
	}

	// 等待embedding处理完成
	waitForEmbeddings(search, 5*time.Second)

	// 测试limit
	results, err := search.HybridSearch(ctx, "document", 3, nil)
	if err != nil {
		t.Fatalf("HybridSearch failed: %v", err)
	}

	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}

	// 测试默认limit（当limit <= 0时）
	results, err = search.HybridSearch(ctx, "document", 0, nil)
	if err != nil {
		t.Fatalf("HybridSearch failed: %v", err)
	}

	if len(results) > 10 {
		t.Errorf("expected at most 10 results (default), got %d", len(results))
	}
}

func TestClose(t *testing.T) {
	textEmbedder := newMockEmbedder(128)
	imageEmbedder := newMockEmbedder(128)
	ocr := newMockOCR()

	search := New(Options{
		WorkingDir:    "./testdata",
		TextEmbedder:  textEmbedder,
		ImageEmbedder: imageEmbedder,
		OCR:           ocr,
	})

	ctx := context.Background()
	err := search.InitializeStorages(ctx)
	if err != nil {
		t.Fatalf("InitializeStorages failed: %v", err)
	}

	err = search.Close(ctx)
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// 测试重复关闭
	err = search.Close(ctx)
	if err != nil {
		t.Errorf("repeated Close should not fail: %v", err)
	}
}

func TestGetContentFromDoc(t *testing.T) {
	// 测试优先返回OCR文本
	doc1 := map[string]any{
		"ocr_text":   "OCR extracted text",
		"content":    "regular content",
		"image_path": "/path/to/image.jpg",
	}
	content := getContentFromDoc(doc1)
	if content != "OCR extracted text" {
		t.Errorf("expected 'OCR extracted text', got '%s'", content)
	}

	// 测试返回content字段
	doc2 := map[string]any{
		"content":    "regular content",
		"image_path": "/path/to/image.jpg",
	}
	content = getContentFromDoc(doc2)
	if content != "regular content" {
		t.Errorf("expected 'regular content', got '%s'", content)
	}

	// 测试返回图片路径
	doc3 := map[string]any{
		"image_path": "/path/to/image.jpg",
	}
	content = getContentFromDoc(doc3)
	if content != "/path/to/image.jpg" {
		t.Errorf("expected '/path/to/image.jpg', got '%s'", content)
	}

	// 测试空文档
	doc4 := map[string]any{}
	content = getContentFromDoc(doc4)
	if content != "" {
		t.Errorf("expected empty string, got '%s'", content)
	}
}
