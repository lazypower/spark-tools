package auth

// UserInfo holds identity information from HuggingFace.
type UserInfo struct {
	Username    string `json:"username"`
	FullName    string `json:"fullname,omitempty"`
	Email       string `json:"email,omitempty"`
	AccountType string `json:"accountType,omitempty"`
}

// TokenResult pairs a resolved token with its source for debugging.
type TokenResult struct {
	Token  string // The raw token value
	Source string // "flag", "env", "config", "hf-compat", "none"
}
