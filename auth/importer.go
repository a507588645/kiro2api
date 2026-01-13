package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"kiro2api/logger"
)

// AccountExport matches the structure of kiro-accounts-*.json
type AccountExport struct {
	Accounts []Account `json:"accounts"`
}

type Account struct {
	Credentials Credentials `json:"credentials"`
}

type Credentials struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ClientId     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	Region       string `json:"region"`
	AuthMethod   string `json:"authMethod"`
	Provider     string `json:"provider"`
	ExpiresAt    int64  `json:"expiresAt"` // Milliseconds
}

// ImportAccounts imports accounts from a JSON file
func ImportAccounts(filePath string) error {
	logger.Info("开始导入账户", logger.String("file", filePath))

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	imported, _, _ := ImportAccountsFromReader(file)
	logger.Info("账户导入完成", logger.Int("imported_count", imported))
	return nil
}

// ImportAccountsFromReader imports accounts from an io.Reader
func ImportAccountsFromReader(r io.Reader) (imported int, skipped int, errors []string) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, 0, []string{fmt.Sprintf("failed to read data: %v", err)}
	}

	var export AccountExport
	if err := json.Unmarshal(data, &export); err != nil {
		return 0, 0, []string{fmt.Sprintf("failed to parse JSON: %v", err)}
	}

	store := GetOAuthTokenStore()

	for _, acc := range export.Accounts {
		creds := acc.Credentials
		if creds.RefreshToken == "" {
			skipped++
			continue
		}

		token := &OAuthToken{
			AccessToken:  creds.AccessToken,
			RefreshToken: creds.RefreshToken,
			ClientID:     creds.ClientId,
			ClientSecret: creds.ClientSecret,
			Region:       creds.Region,
			AuthMethod:   creds.AuthMethod,
			Provider:     creds.Provider,
			ExpiresAt:    time.UnixMilli(creds.ExpiresAt),
		}

		if !token.ExpiresAt.IsZero() {
			token.ExpiresIn = int(time.Until(token.ExpiresAt).Seconds())
		}

		if err := store.AddToken(token); err != nil {
			errors = append(errors, fmt.Sprintf("failed to add token: %v", err))
			skipped++
		} else {
			imported++
		}
	}

	return imported, skipped, errors
}