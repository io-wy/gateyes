package prefixaware

import (
	"sort"
	"sync"

	"github.com/cespare/xxhash/v2"
)

const vllmDefaultChunkSize = 128

type trieNode struct {
	children  map[uint64]*trieNode
	endpoints map[string]struct{}
}

type HashTrie struct {
	mu        sync.RWMutex
	root      *trieNode
	chunkSize int
}

func NewHashTrie(chunkSize int) *HashTrie {
	if chunkSize <= 0 {
		chunkSize = vllmDefaultChunkSize
	}
	return &HashTrie{root: newTrieNode(), chunkSize: chunkSize}
}

func newTrieNode() *trieNode {
	return &trieNode{children: make(map[uint64]*trieNode), endpoints: make(map[string]struct{})}
}

func (t *HashTrie) ChunkSize() int { return t.chunkSize }

func (t *HashTrie) Insert(request, endpoint string) {
	if endpoint == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	node := t.root
	node.endpoints[endpoint] = struct{}{}
	for _, chunkHash := range t.chunkHashes(request) {
		child := node.children[chunkHash]
		if child == nil {
			child = newTrieNode()
			node.children[chunkHash] = child
		}
		node = child
		node.endpoints[endpoint] = struct{}{}
	}
}

func (t *HashTrie) LongestPrefixMatch(request string, available map[string]struct{}) (int, []string) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	node := t.root
	matchLength := 0
	selected := cloneSet(available)
	for _, chunkHash := range t.chunkHashes(request) {
		node = node.children[chunkHash]
		if node == nil {
			break
		}
		intersection := intersect(node.endpoints, selected)
		if len(intersection) == 0 {
			break
		}
		matchLength += t.chunkSize
		selected = intersection
	}
	return matchLength, sortedKeys(selected)
}

func (t *HashTrie) chunkHashes(request string) []uint64 {
	runes := []rune(request)
	hashes := make([]uint64, 0, (len(runes)+t.chunkSize-1)/t.chunkSize)
	for start := 0; start < len(runes); start += t.chunkSize {
		end := min(start+t.chunkSize, len(runes))
		hashes = append(hashes, xxhash.Sum64String(string(runes[start:end])))
	}
	return hashes
}

func cloneSet(source map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{}, len(source))
	for value := range source {
		result[value] = struct{}{}
	}
	return result
}

func intersect(left, right map[string]struct{}) map[string]struct{} {
	result := make(map[string]struct{})
	for value := range left {
		if _, ok := right[value]; ok {
			result[value] = struct{}{}
		}
	}
	return result
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
