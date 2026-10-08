package daemon

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/benyamin-git/kumiho/internal/fxa"
	"github.com/benyamin-git/kumiho/internal/settings"
)

// loadTokens reads tokens.json; a missing file means "not signed in".
func loadTokens(path string) (*fxa.Tokens, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("tokens %s: %w", path, err)
	}
	var t fxa.Tokens
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("tokens %s: %w", path, err)
	}
	if t.AccessToken == "" {
		return nil, nil
	}
	return &t, nil
}

func saveTokens(path string, t *fxa.Tokens) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return settings.WriteFileAtomic(path, data, 0o600)
}

func clearTokens(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
