package templates

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
)

type Generator struct {
	catalog *Catalog
	rand    *rand.Rand
	mu      sync.Mutex
}

func NewGenerator(catalog *Catalog) *Generator {
	return &Generator{
		catalog: catalog,
		rand:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (g *Generator) BuildMessage() string {
	g.mu.Lock()
	defer g.mu.Unlock()

	first := g.rand.Intn(len(g.catalog.Sentences))
	second := g.rand.Intn(len(g.catalog.Sentences))
	for second == first {
		second = g.rand.Intn(len(g.catalog.Sentences))
	}

	parts := []string{
		normalizeSentence(g.catalog.Sentences[first]),
		normalizeSentence(g.catalog.Sentences[second]),
	}

	return fmt.Sprintf("%s %s", parts[0], parts[1])
}

func normalizeSentence(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if strings.HasSuffix(trimmed, ".") || strings.HasSuffix(trimmed, "!") || strings.HasSuffix(trimmed, "?") {
		return trimmed
	}
	return trimmed + "."
}
