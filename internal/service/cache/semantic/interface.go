package semantic

import "time"

// CacheResult 缓存命中结果
type CacheResult struct {
	Response   string
	Similarity float64
	IsHit      bool
}

// Cache 语义缓存接口
type Cache interface {
	// Get 获取缓存，返回命中的响应和相似度
	Get(prompt string) (CacheResult, error)

	// Set 存储缓存
	Set(prompt, response string) error

	// Delete 删除缓存
	Delete(prompt string) error

	// Close 关闭连接
	Close() error
}

// Embedder 向量化接口
type Embedder interface {
	// Embed 生成文本的向量表示
	Embed(text string) ([]float64, error)

	// EmbedBatch 批量生成向量
	EmbedBatch(texts []string) ([][]float64, error)

	// Dimension 返回向量维度
	Dimension() int
}

// Config 语义缓存配置
type Config struct {
	Enabled        bool
	Threshold      float64   // 相似度阈值
	TTL            time.Duration
	Namespace      string
	RedisAddr      string
	RedisPassword  string
	RedisDB        int
	EmbeddingModel string
	Provider       string // "openai" or "simple"
	APIKey         string
}
