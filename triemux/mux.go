// Package triemux implements an HTTP multiplexer, or URL router, which can be
// used to serve responses from multiple distinct handlers within a single URL
// hierarchy.
package triemux

import (
	"crypto/sha1"
	"github.com/alphagov/router/trie"
	"hash"
	"log"
	"net/http"
	"strings"
	"sync"
)

type Mux struct {
	mu         sync.RWMutex
	exactTrie  *trie.Trie
	prefixTrie *trie.Trie
	suffixTrie *trie.Trie
	count      int
	checksum   hash.Hash
}

type routeType int

const (
	PrefixRoute routeType = iota
	SuffixRoute
	ExactRoute
)

// NewMux makes a new empty Mux.
func NewMux() *Mux {
	return &Mux{
		exactTrie:  trie.NewTrie(),
		prefixTrie: trie.NewTrie(),
		suffixTrie: trie.NewTrie(),
		checksum:   sha1.New(),
	}
}

// ServeHTTP dispatches the request to a backend with a registered route
// matching the request path, or 404s.
func (mux *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler, ok := mux.lookup(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	handler.ServeHTTP(w, r)
}

// lookup takes a path and looks up its registered entry in the mux trie,
// returning the handler for that path, if any matches.
func (mux *Mux) lookup(path string) (handler http.Handler, ok bool) {
	mux.mu.RLock()
	defer mux.mu.RUnlock()

	pathSegments := splitpath(path)
	reversePathSegments := make([]string, len(pathSegments))
	last := len(pathSegments) - 1
	for i, val := range pathSegments {
		reversePathSegments[last-i] = val
	}

	val, ok := mux.suffixTrie.GetLongestPrefix(reversePathSegments)
	if !ok {
		val, ok = mux.exactTrie.Get(pathSegments)
	}
	if !ok {
		val, ok = mux.prefixTrie.GetLongestPrefix(pathSegments)
	}
	if !ok {
		return nil, false
	}

	entry, ok := val.(http.Handler)
	if !ok {
		log.Printf("lookup: got value (%v) from trie that wasn't a http.Handler!", val)
		return nil, false
	}

	return entry, ok
}

// Handle registers the specified route (either an exact or a prefix route)
// and associates it with the specified handler. Requests through the mux for
// paths matching the route will be passed to that handler.
func (mux *Mux) Handle(path string, rtype routeType, handler http.Handler) {
	mux.mu.Lock()
	defer mux.mu.Unlock()

	mux.addToStats(path, rtype)
	if rtype == PrefixRoute {
		mux.prefixTrie.Set(splitpath(path), handler)
	} else if rtype == SuffixRoute {
		mux.suffixTrie.Set(splitpath(path), handler)
	} else {
		mux.exactTrie.Set(splitpath(path), handler)
	}
}

func (mux *Mux) addToStats(path string, rtype routeType) {
	mux.count++
	mux.checksum.Write([]byte(path))
	if rtype == PrefixRoute {
		mux.checksum.Write([]byte("(prefix)"))
	} else if rtype == SuffixRoute {
		mux.checksum.Write([]byte("(suffix)"))
	} else {
		mux.checksum.Write([]byte("(exact)"))
	}
}

func (mux *Mux) RouteCount() int {
	return mux.count
}

func (mux *Mux) RouteChecksum() []byte {
	return mux.checksum.Sum(nil)
}

// splitpath turns a slash-delimited string into a lookup path (a slice
// containing the strings between slashes). Empty items produced by
// leading, trailing, or adjacent slashes are removed.
func splitpath(path string) []string {
	partsWithBlanks := strings.Split(path, "/")

	parts := make([]string, 0, len(partsWithBlanks))
	for _, part := range partsWithBlanks {
		if part != "" {
			parts = append(parts, part)
		}
	}

	return parts
}
