package utils_test

import (
	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch/utils"
	"testing"

	"github.com/mozhou-tech/sqlite-ai-driver/pkg/graphsearch"
)

func TestValidateQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{
			name:    "空查询",
			query:   "",
			wantErr: true,
		},
		{
			name:    "正常查询",
			query:   "查找信息",
			wantErr: false,
		},
		{
			name:    "超长查询",
			query:   string(make([]byte, 10001)),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := utils.ValidateQuery(tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateQuery() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRetrievalOptions(t *testing.T) {
	tests := []struct {
		name    string
		options *graphsearch.RetrievalOptions
		wantErr bool
	}{
		{
			name:    "有效选项",
			options: utils.DefaultRetrievalOptions(),
			wantErr: false,
		},
		{
			name: "负数 Limit",
			options: &graphsearch.RetrievalOptions{
				Limit:    -1,
				MaxDepth: 2,
			},
			wantErr: true,
		},
		{
			name: "负数 MaxDepth",
			options: &graphsearch.RetrievalOptions{
				Limit:    10,
				MaxDepth: -1,
			},
			wantErr: true,
		},
		{
			name: "无效相似度阈值",
			options: &graphsearch.RetrievalOptions{
				Limit:               10,
				MaxDepth:            2,
				SimilarityThreshold: 1.5,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := utils.ValidateRetrievalOptions(tt.options)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRetrievalOptions() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateGenerationOptions(t *testing.T) {
	tests := []struct {
		name    string
		options *graphsearch.GenerationOptions
		wantErr bool
	}{
		{
			name:    "有效选项",
			options: utils.DefaultGenerationOptions(),
			wantErr: false,
		},
		{
			name: "负数 MaxLength",
			options: &graphsearch.GenerationOptions{
				MaxLength:   -1,
				Temperature: 0.7,
			},
			wantErr: true,
		},
		{
			name: "无效温度",
			options: &graphsearch.GenerationOptions{
				MaxLength:   500,
				Temperature: 3.0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := utils.ValidateGenerationOptions(tt.options)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGenerationOptions() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultOptions(t *testing.T) {
	// 测试默认选项不为 nil
	ragOpts := utils.DefaultGraphRAGOptions()
	if ragOpts == nil {
		t.Error("DefaultGraphRAGOptions() 返回 nil")
	}

	retOpts := utils.DefaultRetrievalOptions()
	if retOpts == nil {
		t.Error("DefaultRetrievalOptions() 返回 nil")
	}

	orgOpts := utils.DefaultOrganizationOptions()
	if orgOpts == nil {
		t.Error("DefaultOrganizationOptions() 返回 nil")
	}

	genOpts := utils.DefaultGenerationOptions()
	if genOpts == nil {
		t.Error("DefaultGenerationOptions() 返回 nil")
	}
}

func TestBuildGraphRAGOptions(t *testing.T) {
	opts := utils.BuildGraphRAGOptions(
		utils.WithRetrievalStrategy(graphsearch.StrategyHybrid),
		utils.WithRetrievalLimit(20),
		utils.WithGenerationMethod(graphsearch.MethodHybrid),
	)

	if opts == nil {
		t.Fatal("BuildGraphRAGOptions() 返回 nil")
	}
	if opts.RetrievalOptions == nil {
		t.Error("RetrievalOptions 为 nil")
	} else {
		if opts.RetrievalOptions.Strategy != graphsearch.StrategyHybrid {
			t.Errorf("期望 Strategy = StrategyHybrid, 实际 = %v", opts.RetrievalOptions.Strategy)
		}
		if opts.RetrievalOptions.Limit != 20 {
			t.Errorf("期望 Limit = 20, 实际 = %d", opts.RetrievalOptions.Limit)
		}
	}
	if opts.GenerationOptions == nil {
		t.Error("GenerationOptions 为 nil")
	} else {
		if opts.GenerationOptions.Method != graphsearch.MethodHybrid {
			t.Errorf("期望 Method = MethodHybrid, 实际 = %v", opts.GenerationOptions.Method)
		}
	}
}
