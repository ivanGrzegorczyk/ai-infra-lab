package domain

type APIKeyConfig struct {
	Key              string   `json:"key"`
	Name             string   `json:"name"`
	AllowedProviders []string `json:"allowed_providers"`
}

type KeyConfigList struct {
	Keys []APIKeyConfig `json:"keys"`
}
