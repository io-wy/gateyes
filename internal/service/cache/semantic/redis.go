package semantic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// IndexName Redis 索引名称
	IndexName = "semantic_cache"

	// VectorField 向量字段名
	VectorField = "embedding"

	// TextField 文本字段名
	TextField = "text"

	// ResponseField 响应字段名
	ResponseField = "response"

	// TenantField 租户字段名
	TenantField = "tenant_id"

	// DefaultThreshold 默认相似度阈值
	DefaultThreshold = 0.85
)

// RedisCache Redis 向量语义缓存
type RedisCache struct {
	client    *redis.Client
	embedder  Embedder
	cfg       Config
	namespace string
}

// NewRedisCache 创建 Redis 语义缓存
func NewRedisCache(cfg Config, embedder Embedder) (*RedisCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	// 初始化索引
	if err := createIndex(ctx, client, embedder.Dimension()); err != nil {
		return nil, fmt.Errorf("failed to create index: %w", err)
	}

	if cfg.Threshold == 0 {
		cfg.Threshold = DefaultThreshold
	}

	return &RedisCache{
		client:    client,
		embedder:  embedder,
		cfg:       cfg,
		namespace: cfg.Namespace,
	}, nil
}

// createIndex 创建 Redis 搜索索引
func createIndex(ctx context.Context, client *redis.Client, dim int) error {
	// 使用正确的 FT.CREATE 命令
	indexName := IndexName
	_, err := client.Do(ctx,
		"FT.CREATE", indexName,
		"ON", "HASH",
		"SCHEMA",
		TenantField, "TEXT",
		TextField, "TEXT",
		ResponseField, "TEXT",
		VectorField, "VECTOR", "HNSW", "TYPE", "FLOAT32", "DIM", dim, "DISTANCE_METRIC", "COSINE",
	).Result()

	if err != nil {
		// 索引已存在则忽略
		if strings.Contains(err.Error(), "Index already exists") {
			return nil
		}
		return err
	}
	return nil
}

// cacheKey 生成缓存 key
func (r *RedisCache) cacheKey(prompt string) string {
	h := sha256.Sum256([]byte(prompt))
	return r.namespace + ":" + hex.EncodeToString(h[:])
}

// float64ToBytes 将 float64 转换为 4 字节 big-endian
func float64ToBytes(f float64) []byte {
	bits := uint64(f)
	return []byte{
		byte(bits >> 24),
		byte(bits >> 16),
		byte(bits >> 8),
		byte(bits),
	}
}

// bytesToFloat64 将 4 字节 big-endian 转换回 float64
func bytesToFloat64(b []byte) float64 {
	var bits uint64
	bits = uint64(b[0]) << 24
	bits |= uint64(b[1]) << 16
	bits |= uint64(b[2]) << 8
	bits |= uint64(b[3])
	return float64(bits)
}

// embedToBytes 将 embedding 转换为 Redis 能识别的 bytes 格式
func embedToBytes(embedding []float64) []byte {
	result := make([]byte, len(embedding)*4)
	for i, v := range embedding {
		bits := float64ToBytes(v)
		result[i*4] = bits[0]
		result[i*4+1] = bits[1]
		result[i*4+2] = bits[2]
		result[i*4+3] = bits[3]
	}
	return result
}

// Get 获取缓存
func (r *RedisCache) Get(prompt string) (CacheResult, error) {
	ctx := context.Background()

	// 1. 先尝试精确匹配
	key := r.cacheKey(prompt)
	existing, err := r.client.HGet(ctx, key, ResponseField).Result()
	if err == nil {
		// 精确命中
		return CacheResult{
			Response:   existing,
			Similarity: 1.0,
			IsHit:      true,
		}, nil
	}

	if err != redis.Nil && !strings.Contains(err.Error(), "not found") {
		return CacheResult{}, err
	}

	// 2. 语义相似度搜索
	if r.embedder == nil {
		return CacheResult{IsHit: false}, nil
	}

	embedding, err := r.embedder.Embed(prompt)
	if err != nil {
		return CacheResult{}, err
	}

	// 将 embedding 转换为 Redis blob 格式
	embeddingBytes := embedToBytes(embedding)

	// KNN 搜索
	result, err := r.client.Do(ctx,
		"FT.SEARCH", IndexName,
		"*",
		"RETURN", "3", TextField, ResponseField, "DISTANCE", VectorField,
		"KNN", "10", "@"+VectorField, "BLOB", embeddingBytes,
		"PARAMS", "2", "DISTANCE_THRESH", r.cfg.Threshold,
	).Result()

	if err != nil {
		if strings.Contains(err.Error(), "Index not found") {
			return CacheResult{IsHit: false}, nil
		}
		return CacheResult{}, err
	}

	// 解析结果
	// FT.SEARCH 返回格式: [total_results, key1, [field1, value1, field2, value2, ...], key2, ...]
	results, ok := result.([]any)
	if !ok || len(results) < 2 {
		return CacheResult{IsHit: false}, nil
	}

	// 遍历结果找第一个匹配的
	for i := 1; i < len(results)-1; i += 2 {
		// results[i] 是 key, results[i+1] 是 fields 数组
		fields, ok := results[i+1].([]any)
		if !ok {
			continue
		}

		var response string
		var distance float64

		for j := 0; j < len(fields)-1; j += 2 {
			fieldName, _ := fields[j].(string)
			fieldValue, _ := fields[j+1].(string)

			switch fieldName {
			case ResponseField:
				response = fieldValue
			case "DISTANCE":
				// distance 越小越相似
				fmt.Sscanf(fieldValue, "%f", &distance)
			}
		}

		if response != "" {
			// 转换 distance 为相似度 (cosine distance: 0 = identical, 2 = opposite)
			similarity := 1.0 - distance
			if similarity >= r.cfg.Threshold {
				return CacheResult{
					Response:   response,
					Similarity: similarity,
					IsHit:      true,
				}, nil
			}
		}
	}

	return CacheResult{IsHit: false}, nil
}

// Set 存储缓存
func (r *RedisCache) Set(prompt, response string) error {
	ctx := context.Background()
	key := r.cacheKey(prompt)

	// 准备数据
	data := map[string]any{
		TextField:     prompt,
		ResponseField: response,
	}

	// 添加 embedding 向量
	if r.embedder != nil {
		embedding, err := r.embedder.Embed(prompt)
		if err != nil {
			// embedding 失败不影响写入其他数据
			fmt.Printf("Warning: failed to get embedding: %v\n", err)
		} else {
			// 转换为 bytes 格式存储
			data[VectorField] = embedToBytes(embedding)
		}
	}

	// 使用 HSet 存储
	if err := r.client.HSet(ctx, key, data).Err(); err != nil {
		return err
	}

	// 设置 TTL
	if r.cfg.TTL > 0 {
		r.client.Expire(ctx, key, r.cfg.TTL)
	}

	return nil
}

// Delete 删除缓存
func (r *RedisCache) Delete(prompt string) error {
	ctx := context.Background()
	key := r.cacheKey(prompt)
	return r.client.Del(ctx, key).Err()
}

// Close 关闭连接
func (r *RedisCache) Close() error {
	return r.client.Close()
}
