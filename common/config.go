package common

import (
	"encoding/json"
	"os"
)

type RepositoryConfig struct {
	Endpoint string `json:"endpoint"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type Config struct {
	Repositories map[string]*RepositoryConfig `json:"repositories"`
}

func ReadConfig(filename string) (*Config, error) {
	input, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	config := &Config{}
	if err = json.Unmarshal(input, config); err != nil {
		return nil, err
	}
	return config, err
}
