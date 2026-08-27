package credential

import "time"

// Token is the sensitive OAuth state stored in the operating system's native
// credential store. It must never be serialized into config.toml or logs.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type,omitempty"`
	ClientID     string    `json:"client_id"`
	Site         string    `json:"site"`
	ExpiresAt    time.Time `json:"expires_at"`
	RefreshedAt  time.Time `json:"refreshed_at"`
}
