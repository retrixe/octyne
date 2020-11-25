package main

import "encoding/json"

// Config ... The main config for Octyne.
type Config struct {
	Port    uint16                  `json:"port"`
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
	Enabled   bool   `json:"enabled"`
	Directory string `json:"directory"`
	Command   string `json:"command"`
}

// UnmarshalJSON ... Unmarshals and sets default value for the enabled property.
func (c *ServerConfig) UnmarshalJSON(data []byte) error {
	type alias ServerConfig
	conf := &alias{Enabled: true}
	err := json.Unmarshal(data, conf)
	*c = ServerConfig(*conf)
	return err
}
