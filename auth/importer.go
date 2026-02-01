package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
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

	credentialsList, parseErrors := parseCredentialsFromJSON(data)
	if len(parseErrors) > 0 {
		errors = append(errors, parseErrors...)
	}
	if len(credentialsList) == 0 {
		if len(errors) == 0 {
			errors = append(errors, "unsupported account file format")
		}
		return 0, 0, errors
	}

	store := GetOAuthTokenStore()

	for _, creds := range credentialsList {
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

func parseCredentialsFromJSON(data []byte) ([]Credentials, []string) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, []string{fmt.Sprintf("failed to parse JSON: %v", err)}
	}
	return extractCredentials(root)
}

func extractCredentials(value any) ([]Credentials, []string) {
	switch v := value.(type) {
	case []any:
		var creds []Credentials
		var errs []string
		for i, item := range v {
			itemCreds, itemErrs := extractCredentials(item)
			if len(itemCreds) > 0 {
				creds = append(creds, itemCreds...)
			}
			for _, err := range itemErrs {
				errs = append(errs, fmt.Sprintf("item %d: %s", i, err))
			}
		}
		return creds, errs
	case map[string]any:
		if accounts, ok := v["accounts"]; ok {
			return extractCredentials(accounts)
		}
		if account, ok := v["account"]; ok {
			return extractCredentials(account)
		}
		if credValue, ok := v["credentials"]; ok {
			return extractCredentials(credValue)
		}
		if cred, ok := mapToCredentials(v); ok {
			return []Credentials{cred}, nil
		}
		return nil, []string{"unsupported account object format"}
	default:
		return nil, []string{"unsupported JSON structure"}
	}
}

func mapToCredentials(m map[string]any) (Credentials, bool) {
	accessToken := pickString(m, "accessToken", "access_token", "accessTokenValue")
	refreshToken := pickString(m, "refreshToken", "refresh_token")
	clientID := pickString(m, "clientId", "clientID", "client_id")
	clientSecret := pickString(m, "clientSecret", "client_secret")
	region := pickString(m, "region", "awsRegion", "aws_region")
	authMethod := pickString(m, "authMethod", "auth_method", "auth", "authType", "auth_type")
	provider := pickString(m, "provider")
	expiresAt := pickInt64(m, "expiresAt", "expires_at", "expiresAtMs", "expires_at_ms")

	if accessToken == "" && refreshToken == "" && clientID == "" && clientSecret == "" {
		return Credentials{}, false
	}
	if authMethod != "" {
		authMethod = normalizeAuthType(authMethod)
	}
	if expiresAt > 0 {
		expiresAt = normalizeEpochMillis(expiresAt)
	}

	return Credentials{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ClientId:     clientID,
		ClientSecret: clientSecret,
		Region:       region,
		AuthMethod:   authMethod,
		Provider:     provider,
		ExpiresAt:    expiresAt,
	}, true
}

func pickString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			switch v := value.(type) {
			case string:
				if v != "" {
					return v
				}
			case fmt.Stringer:
				str := v.String()
				if str != "" {
					return str
				}
			}
		}
	}
	return ""
}

func pickInt64(m map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			switch v := value.(type) {
			case float64:
				return int64(v)
			case int64:
				return v
			case int:
				return int64(v)
			case json.Number:
				if n, err := v.Int64(); err == nil {
					return n
				}
			case string:
				if n, err := strconv.ParseInt(v, 10, 64); err == nil {
					return n
				}
			}
		}
	}
	return 0
}

func normalizeEpochMillis(value int64) int64 {
	if value <= 0 {
		return 0
	}
	if value < 1_000_000_000_000 {
		return value * 1000
	}
	return value
}
