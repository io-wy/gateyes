package repository

import (
	"github.com/gateyes/gateway/internal/config"
	"sync"
)

type APIKeyRepository struct {
	keys map[string]*APIKeyInfo
	mu   sync.RWMutex
}

type APIKeyInfo struct {
	Key      string
	Secret   string
	Quota    int
	Used     int
	QPS      int
	Models   []string
}

func NewAPIKeyRepository(keys []config.APIKeyConfig) *APIKeyRepository {
	repo := &APIKeyRepository{
		keys: make(map[string]*APIKeyInfo),
	}
	for _, k := range keys {
		repo.keys[k.Key] = &APIKeyInfo{
			Key:    k.Key,
			Secret: k.Secret,
			Quota:  k.Quota,
			QPS:    k.QPS,
			Models: k.Models,
		}
	}
	return repo
}

func (r *APIKeyRepository) Get(key string) (*APIKeyInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.keys[key]
	return info, ok
}

func (r *APIKeyRepository) Use(key string, tokens int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if info, ok := r.keys[key]; ok {
		if info.Quota <= 0 || info.Used+tokens <= info.Quota {
			info.Used += tokens
			return true
		}
	}
	return false
}

func (r *APIKeyRepository) Remaining(key string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if info, ok := r.keys[key]; ok {
		return info.Quota - info.Used
	}
	return 0
}

func (r *APIKeyRepository) IsAllowedModel(key, model string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if info, ok := r.keys[key]; ok {
		if len(info.Models) == 0 {
			return true // no restriction
		}
		for _, m := range info.Models {
			if m == model {
				return true
			}
		}
	}
	return false
}
