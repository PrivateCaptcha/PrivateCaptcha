package common

import (
	"log/slog"
	"os"
	"sync"

	"github.com/joho/godotenv"
)

const (
	envPathStdin = "stdin"
)

type EnvMap struct {
	path   string
	envMap map[string]string
	lock   sync.Mutex
}

func (em *EnvMap) GetEx(key string) (string, bool) {
	em.lock.Lock()
	defer em.lock.Unlock()

	if em.envMap == nil {
		return os.LookupEnv(key)
	}

	v, ok := em.envMap[key]
	return v, ok
}

func (em *EnvMap) Get(key string) string {
	v, ok := em.GetEx(key)
	if !ok {
		slog.Warn("Environment variable is not set", "key", key)
	}

	return v
}

func (em *EnvMap) Update() error {
	if (len(em.path) > 0) && (em.path != envPathStdin) {
		envMap, err := godotenv.Read(em.path)
		if err != nil {
			return err
		}

		em.lock.Lock()
		em.envMap = envMap
		em.lock.Unlock()
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
