package credential

import "time"

// Token 保存于私有凭据文件，不得写入普通配置或日志。
type Token struct {
	LoginID      string    `json:"login_id,omitempty"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type,omitempty"`
	ClientID     string    `json:"client_id"`
	Site         string    `json:"site"`
	ExpiresAt    time.Time `json:"expires_at"`
	RefreshedAt  time.Time `json:"refreshed_at"`
}
