package repository

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

type UserRepository struct {
	users map[string]*User
	mu    sync.RWMutex
}

type User struct {
	ID        string    `json:"id"`
	APIKey    string    `json:"api_key"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Quota     int       `json:"quota"`      // total tokens allowed
	Used      int       `json:"used"`       // tokens used
	QPS       int       `json:"qps"`        // requests per second limit
	Models    []string  `json:"models"`     // allowed models
	Status    string    `json:"status"`     // active, suspended
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewUserRepository() *UserRepository {
	return &UserRepository{
		users: make(map[string]*User),
	}
}

func (r *UserRepository) Create(name, email string, quota, qps int, models []string) *User {
	r.mu.Lock()
	defer r.mu.Unlock()

	apiKey := "gk-" + uuid.New().String()[:12]
	user := &User{
		ID:        uuid.New().String(),
		APIKey:    apiKey,
		Name:      name,
		Email:     email,
		Quota:     quota,
		Used:      0,
		QPS:       qps,
		Models:    models,
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	r.users[user.ID] = user

	// 同时用 apiKey 作为 key存储，方便查找
	r.users[apiKey] = user

	return user
}

func (r *UserRepository) Get(id string) (*User, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	user, ok := r.users[id]
	return user, ok
}

func (r *UserRepository) GetByAPIKey(apiKey string) (*User, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	user, ok := r.users[apiKey]
	return user, ok
}

func (r *UserRepository) List() []*User {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*User, 0, len(r.users))
	seen := make(map[string]bool)
	for _, u := range r.users {
		if seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		result = append(result, u)
	}
	return result
}

func (r *UserRepository) Update(id string, quota, qps int, models []string, status string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if user, ok := r.users[id]; ok {
		if quota >= 0 {
			user.Quota = quota
		}
		if qps >= 0 {
			user.QPS = qps
		}
		if models != nil {
			user.Models = models
		}
		if status != "" {
			user.Status = status
		}
		user.UpdatedAt = time.Now()
		return true
	}
	return false
}

func (r *UserRepository) Delete(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if user, ok := r.users[id]; ok {
		delete(r.users, user.ID)
		delete(r.users, user.APIKey)
		return true
	}
	return false
}

func (r *UserRepository) Use(apiKey string, tokens int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if user, ok := r.users[apiKey]; ok {
		if user.Status != "active" {
			return false
		}
		if user.Quota > 0 && user.Used+tokens > user.Quota {
			return false
		}
		user.Used += tokens
		user.UpdatedAt = time.Now()
		return true
	}
	return false
}

func (r *UserRepository) Remaining(apiKey string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if user, ok := r.users[apiKey]; ok {
		if user.Quota <= 0 {
			return -1 // unlimited
		}
		return user.Quota - user.Used
	}
	return 0
}

func (r *UserRepository) IsAllowedModel(apiKey, model string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if user, ok := r.users[apiKey]; ok {
		if user.Status != "active" {
			return false
		}
		if len(user.Models) == 0 {
			return true // no restriction
		}
		for _, m := range user.Models {
			if m == model {
				return true
			}
		}
	}
	return false
}

func (r *UserRepository) Stats() (totalUsers, activeUsers, totalQuota, totalUsed int) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	for _, u := range r.users {
		if seen[u.ID] {
			continue
		}
		seen[u.ID] = true
		totalUsers++
		totalQuota += u.Quota
		totalUsed += u.Used
		if u.Status == "active" {
			activeUsers++
		}
	}
	return
}
