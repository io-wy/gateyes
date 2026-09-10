package prefixaware

import (
	"reflect"
	"strings"
	"testing"
)

func TestHashTrieFindsLongestAvailablePrefixBy128CharacterChunks(t *testing.T) {
	trie := NewHashTrie(128)
	base := strings.Repeat("a", 128)
	trie.Insert(base+strings.Repeat("b", 128), "p1")
	trie.Insert(base+strings.Repeat("c", 128), "p2")

	length, endpoints := trie.LongestPrefixMatch(base+strings.Repeat("b", 64), map[string]struct{}{"p1": {}, "p2": {}})
	if length != 128 || !reflect.DeepEqual(endpoints, []string{"p1", "p2"}) {
		t.Fatalf("match = (%d,%v), want (128,[p1 p2])", length, endpoints)
	}

	length, endpoints = trie.LongestPrefixMatch(base+strings.Repeat("b", 128)+"tail", map[string]struct{}{"p1": {}})
	if length != 256 || !reflect.DeepEqual(endpoints, []string{"p1"}) {
		t.Fatalf("match = (%d,%v), want (256,[p1])", length, endpoints)
	}
}

func TestHashTrieCountsUnicodeCharactersLikePython(t *testing.T) {
	trie := NewHashTrie(128)
	prefix := strings.Repeat("你", 128)
	trie.Insert(prefix+"old", "p1")
	length, endpoints := trie.LongestPrefixMatch(prefix+"new", map[string]struct{}{"p1": {}})
	if length != 128 || !reflect.DeepEqual(endpoints, []string{"p1"}) {
		t.Fatalf("unicode match = (%d,%v), want (128,[p1])", length, endpoints)
	}
}

func TestHashTrieStopsWhenOnlyUnavailableEndpointsMatch(t *testing.T) {
	trie := NewHashTrie(128)
	trie.Insert(strings.Repeat("x", 256), "gone")
	length, endpoints := trie.LongestPrefixMatch(strings.Repeat("x", 256), map[string]struct{}{"live": {}})
	if length != 0 || !reflect.DeepEqual(endpoints, []string{"live"}) {
		t.Fatalf("unavailable match = (%d,%v), want (0,[live])", length, endpoints)
	}
}

func TestHashTrieZeroChunkSizeUsesVLLMDefault(t *testing.T) {
	if got := NewHashTrie(0).ChunkSize(); got != 128 {
		t.Fatalf("ChunkSize() = %d, want 128", got)
	}
}
