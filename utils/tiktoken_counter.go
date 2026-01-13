package utils

import (
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

var (
	encMu    sync.Mutex
	encCache = map[string]*tiktoken.Tiktoken{}
)

// CountTokensWithTiktoken counts tokens using tiktoken. If model is unknown, it falls back to cl100k_base.
// This is an approximation for Claude models but is typically closer than naive char-based heuristics.
func CountTokensWithTiktoken(text string, model string) (tokens int) {
	if text == "" {
		return 0
	}
	enc := getEncodingForModel(model)
	if enc == nil {
		// Worst-case fallback: ~4 chars per token.
		return (len([]rune(text)) + 3) / 4
	}
	fallback := (len([]rune(text)) + 3) / 4
	defer func() {
		if r := recover(); r != nil {
			tokens = fallback
		}
	}()
	ids := enc.Encode(text, nil, nil)
	return len(ids)
}

func getEncodingForModel(model string) *tiktoken.Tiktoken {
	if model == "" {
		model = "cl100k_base"
	}

	encMu.Lock()
	defer encMu.Unlock()

	if enc, ok := encCache[model]; ok {
		return enc
	}

	enc, err := tiktoken.EncodingForModel(model)
	if err != nil {
		enc, err = tiktoken.GetEncoding("cl100k_base")
		if err != nil {
			return nil
		}
		encCache[model] = enc
		return enc
	}

	encCache[model] = enc
	return enc
}
