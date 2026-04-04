package backend

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// SettingsStore 负责读取和保存本地设置。
type SettingsStore struct {
	path string
	mu   sync.RWMutex
	data SettingsFile
}

// NewSettingsStore 创建设置仓库。
func NewSettingsStore(path string) *SettingsStore {
	return &SettingsStore{
		path: path,
		data: SettingsFile{},
	}
}

// Load 从磁盘加载设置。
func (s *SettingsStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("创建设置目录失败: %w", err)
	}
	if !FileExists(s.path) {
		return s.saveLocked()
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("读取设置失败: %w", err)
	}
	if len(raw) == 0 {
		return s.saveLocked()
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return fmt.Errorf("解析设置失败: %w", err)
	}
	if s.data.ManagementKey == "" {
		if _, err := s.ensureManagementKeyLocked(); err != nil {
			return err
		}
		return s.saveLocked()
	}
	return nil
}

// NetworkSettings 返回当前网络设置副本。
func (s *SettingsStore) NetworkSettings() NetworkSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Network
}

// SaveNetworkSettings 保存网络设置。
func (s *SettingsStore) SaveNetworkSettings(settings NetworkSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Network = settings
	return s.saveLocked()
}

// EnsureManagementKey 返回已持久化的管理密钥。
func (s *SettingsStore) EnsureManagementKey() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, err := s.ensureManagementKeyLocked()
	if err != nil {
		return "", err
	}
	if err := s.saveLocked(); err != nil {
		return "", err
	}
	return key, nil
}

// ManagementKey 返回当前管理密钥。
func (s *SettingsStore) ManagementKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.ManagementKey
}

// SaveManagementKey 保存管理密钥。
func (s *SettingsStore) SaveManagementKey(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("管理密钥不能为空")
	}
	s.data.ManagementKey = key
	return s.saveLocked()
}

// saveLocked 在持锁状态下落盘。
func (s *SettingsStore) saveLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("编码设置失败: %w", err)
	}
	return os.WriteFile(s.path, raw, 0o644)
}

// ensureManagementKeyLocked 在持锁状态下生成或返回管理密钥。
func (s *SettingsStore) ensureManagementKeyLocked() (string, error) {
	if s.data.ManagementKey != "" {
		return s.data.ManagementKey, nil
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成管理密钥失败: %w", err)
	}
	s.data.ManagementKey = hex.EncodeToString(buf)
	return s.data.ManagementKey, nil
}
