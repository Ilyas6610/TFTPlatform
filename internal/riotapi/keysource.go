package riotapi

import (
	"fmt"
	"os"
	"strings"
)

// KeySource abstracts where the Riot API key comes from. FileKeySource is
// preferred over EnvKeySource because a personal key expires every 24h:
// reading from a file (or a Kubernetes Secret mounted as a file) lets an
// operator rotate the key by editing the file, with no process restart and
// no redeploy required.
type KeySource interface {
	CurrentKey() (string, error)
}

// EnvKeySource holds a key fixed at process start. Rotating it requires a
// restart — acceptable for quick local scripts, not recommended for the API
// server or a long-running worker.
type EnvKeySource struct {
	Key string
}

func (s EnvKeySource) CurrentKey() (string, error) {
	if s.Key == "" {
		return "", fmt.Errorf("riot api key not set")
	}
	return s.Key, nil
}

// FileKeySource re-reads the key from disk on every call, so an updated
// Kubernetes Secret volume (or an edited local file in docker-compose) takes
// effect on the next request with no restart.
type FileKeySource struct {
	Path string
}

func (s FileKeySource) CurrentKey() (string, error) {
	b, err := os.ReadFile(s.Path)
	if err != nil {
		return "", fmt.Errorf("read riot api key file %s: %w", s.Path, err)
	}
	key := strings.TrimSpace(string(b))
	if key == "" {
		return "", fmt.Errorf("riot api key file %s is empty", s.Path)
	}
	return key, nil
}
