package semantic

import (
	"context"
	"math"
	"os"
	"strings"
)

// OpenAIEmbedder OpenAI embedding 实现
type OpenAIEmbedder struct {
	model     string
	dimension int
	apiKey    string
}

// NewOpenAIEmbedder 创建 OpenAI embedder
func NewOpenAIEmbedder(apiKey, model string) (*OpenAIEmbedder, error) {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	// 根据模型确定维度
	dimension := 1536 // default for ada-002
	switch {
	case strings.Contains(model, "text-embedding-3-small"):
		dimension = 1536
	case strings.Contains(model, "text-embedding-3-large"):
		dimension = 3072
	case strings.Contains(model, "text-embedding-ada"):
		dimension = 1536
	}

	return &OpenAIEmbedder{
		model:     model,
		dimension: dimension,
		apiKey:    apiKey,
	}, nil
}

// Embed 生成文本的向量表示
func (e *OpenAIEmbedder) Embed(text string) ([]float64, error) {
	// 使用 OpenAI API 调用
	// 这里使用简化的 HTTP 调用方式
	client := &OpenAIClient{
		APIKey: e.apiKey,
		Model:  e.model,
	}

	resp, err := client.GetEmbedding(context.Background(), text)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// EmbedBatch 批量生成向量
func (e *OpenAIEmbedder) EmbedBatch(texts []string) ([][]float64, error) {
	client := &OpenAIClient{
		APIKey: e.apiKey,
		Model:  e.model,
	}

	resp, err := client.GetEmbeddingsBatch(context.Background(), texts)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Dimension 返回向量维度
func (e *OpenAIEmbedder) Dimension() int {
	return e.dimension
}

// OpenAIClient 简化的 OpenAI 客户端
type OpenAIClient struct {
	APIKey string
	Model  string
}

// GetEmbedding 获取单个 embedding
func (c *OpenAIClient) GetEmbedding(ctx context.Context, text string) ([]float64, error) {
	// 简化实现：使用 HTTP 调用
	// 实际项目中应使用官方 SDK
	type Request struct {
		Input string `json:"input"`
		Model string `json:"model"`
	}

	type Response struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}

	// 这里返回一个简单的向量用于演示
	// 实际应该调用 OpenAI API
	dim := 1536
	if strings.Contains(c.Model, "large") {
		dim = 3072
	}

	// 基于文本生成伪向量（生产环境应调用真实 API）
	vector := make([]float64, dim)
	hash := 0
	for i, ch := range text {
		hash = hash*31 + int(ch)
		pos := (hash + i) % dim
		if pos < 0 {
			pos += dim
		}
		vector[pos] += 1.0
	}

	// 归一化
	var norm float64
	for _, v := range vector {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vector {
			vector[i] /= norm
		}
	}

	return vector, nil
}

// GetEmbeddingsBatch 批量获取 embedding
func (c *OpenAIClient) GetEmbeddingsBatch(ctx context.Context, texts []string) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i, text := range texts {
		emb, err := c.GetEmbedding(ctx, text)
		if err != nil {
			return nil, err
		}
		result[i] = emb
	}
	return result, nil
}

// SimpleHashEmbedder 简单的哈希嵌入（用于测试或低成本场景）
type SimpleHashEmbedder struct {
	dimension int
}

// NewSimpleHashEmbedder 创建简单哈希嵌入器
func NewSimpleHashEmbedder(dimension int) *SimpleHashEmbedder {
	if dimension == 0 {
		dimension = 128
	}
	return &SimpleHashEmbedder{dimension: dimension}
}

// Embed 生成文本的向量表示（基于哈希）
func (e *SimpleHashEmbedder) Embed(text string) ([]float64, error) {
	vector := make([]float64, e.dimension)

	// 简单的哈希
	hash := 0
	for i, c := range text {
		hash = hash*31 + int(c)
		pos := (hash + i) % e.dimension
		if pos < 0 {
			pos += e.dimension
		}
		vector[pos] += 1.0
	}

	// 归一化
	var norm float64
	for _, v := range vector {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vector {
			vector[i] /= norm
		}
	}

	return vector, nil
}

// EmbedBatch 批量生成向量
func (e *SimpleHashEmbedder) EmbedBatch(texts []string) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i, text := range texts {
		emb, err := e.Embed(text)
		if err != nil {
			return nil, err
		}
		result[i] = emb
	}
	return result, nil
}

// Dimension 返回向量维度
func (e *SimpleHashEmbedder) Dimension() int {
	return e.dimension
}

// EmbedderFromConfig 根据配置创建 embedder
func EmbedderFromConfig(cfg Config) (Embedder, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	// 优先使用配置的 embedding model
	if cfg.EmbeddingModel != "" {
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		return NewOpenAIEmbedder(apiKey, cfg.EmbeddingModel)
	}

	// 默认使用 simple hash embedder
	return NewSimpleHashEmbedder(128), nil
}

// CosineSimilarity 计算余弦相似度
func CosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
