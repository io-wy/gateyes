package rbac

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/gateyes/gateway/internal/repository"
)

// Service resolves user permissions with an in-process + Redis cache layer.
type Service struct {
	store  repository.RoleStore
	rdb    *redis.Client
	mu     sync.RWMutex
	local  map[string]map[string]struct{} // userID -> permission code set
	ttl    time.Duration
}

// New creates an RBAC service.
func New(store repository.RoleStore, rdb *redis.Client, ttl time.Duration) *Service {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Service{
		store: store,
		rdb:   rdb,
		local: make(map[string]map[string]struct{}),
		ttl:   ttl,
	}
}

// HasPermission reports whether the user has the given permission code.
func (s *Service) HasPermission(ctx context.Context, userID, perm string) bool {
	if userID == "" {
		return false
	}

	// Normalize permission code.
	perm = strings.ToLower(strings.TrimSpace(perm))

	// 1. In-process cache.
	if perms := s.loadLocal(userID); perms != nil {
		_, ok := perms[perm]
		return ok
	}

	// 2. Redis cache.
	if s.rdb != nil {
		if perms, ok := s.loadRedis(ctx, userID); ok {
			s.storeLocal(userID, perms)
			_, has := perms[perm]
			return has
		}
	}

	// 3. Database.
	records, err := s.store.GetUserPermissions(ctx, userID)
	if err != nil {
		return false
	}
	perms := make(map[string]struct{}, len(records))
	for _, r := range records {
		perms[strings.ToLower(strings.TrimSpace(r.Code))] = struct{}{}
	}
	s.storeLocal(userID, perms)
	s.storeRedis(ctx, userID, perms)
	_, has := perms[perm]
	return has
}

// Invalidate drops cached permissions for a user.
func (s *Service) Invalidate(userID string) {
	s.mu.Lock()
	delete(s.local, userID)
	s.mu.Unlock()
}

func (s *Service) loadLocal(userID string) map[string]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.local[userID]
}

func (s *Service) storeLocal(userID string, perms map[string]struct{}) {
	s.mu.Lock()
	s.local[userID] = perms
	s.mu.Unlock()

	// Lazy eviction: schedule cleanup after TTL. Real production may use LRU.
	time.AfterFunc(s.ttl, func() {
		s.Invalidate(userID)
	})
}

func (s *Service) redisKey(userID string) string {
	return fmt.Sprintf("rbac:perms:%s", userID)
}

func (s *Service) loadRedis(ctx context.Context, userID string) (map[string]struct{}, bool) {
	if s.rdb == nil {
		return nil, false
	}
	members, err := s.rdb.SMembers(ctx, s.redisKey(userID)).Result()
	if err != nil || len(members) == 0 {
		return nil, false
	}
	perms := make(map[string]struct{}, len(members))
	for _, m := range members {
		perms[m] = struct{}{}
	}
	return perms, true
}

func (s *Service) storeRedis(ctx context.Context, userID string, perms map[string]struct{}) {
	if s.rdb == nil || len(perms) == 0 {
		return
	}
	members := make([]any, 0, len(perms))
	for p := range perms {
		members = append(members, p)
	}
	key := s.redisKey(userID)
	s.rdb.Del(ctx, key)
	s.rdb.SAdd(ctx, key, members...)
	s.rdb.Expire(ctx, key, s.ttl)
}
