package common

import (
	"os"

	"github.com/joho/godotenv"
)

const (
	envPathStdin = "stdin"
)

type EnvMap struct {
	path   string
	envMap map[string]string
}

func (em *EnvMap) GetEx(key string) (string, bool) {
	if em.envMap == nil {
		value := os.Getenv(key)
		return value, len(value) > 0
	}

	v, ok := em.envMap[key]
	return v, ok && len(v) > 0
}

func (em *EnvMap) Get(key string) string {
	v, _ := em.GetEx(key)
	return v
}

func (em *EnvMap) Update() error {
	if (len(em.path) > 0) && (em.path != envPathStdin) {
		envMap, err := godotenv.Read(em.path)
		if err != nil {
			return err
		}

		em.envMap = envMap
	}

	return nil
}

func NewEnvMap(path string) (*EnvMap, error) {
	var envMap map[string]string

	if path == envPathStdin {
		var err error
		envMap, err = godotenv.Parse(os.Stdin)
		if err != nil {
			return nil, err
		}
	} else if len(path) > 0 {
		var err error
		envMap, err = godotenv.Read(path)
		if err != nil {
			return nil, err
		}
	}

	return &EnvMap{envMap: envMap, path: path}, nil
}
