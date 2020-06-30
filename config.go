package main

// Config ... The main config for Octyne.
type Config struct {
	HTTPS   HTTPSConfig             `json:"https"`
	Redis   RedisConfig             `json:"redis"`
	Servers map[string]ServerConfig `json:"servers"`
}

// RedisConfig ... Whether or not Redis is enabled, and if so, how to connect.
type RedisConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

// HTTPSConfig ... Whether or not HTTPS is enabled, and if so, path to cert and key.
type HTTPSConfig struct {
	Enabled bool   `json:"enabled"`
	Cert    string `json:"cert"`
	Key     string `json:"key"`
}

// ServerConfig ... The config for individual servers.
type ServerConfig struct {
	Directory string `json:"directory"`
	Command   string `json:"command"`
}
