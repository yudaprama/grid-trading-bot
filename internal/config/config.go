package config

import (
	"github.com/yudaprama/grid-trading-bot/internal/models"
	"encoding/json"
	"os"
)

// LoadConfig load the JSON configuration file from the specified path and parse it into the Config structure
func LoadConfig(path string) (*models.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	config := &models.Config{}
	err = decoder.Decode(config)
	if err != nil {
		return nil, err
	}

	return config, nil
}
