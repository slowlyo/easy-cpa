package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigManager 负责维护托管核心配置。
type ConfigManager struct {
	paths    ManagedPaths
	settings *SettingsStore
}

// NewConfigManager 创建配置管理器。
func NewConfigManager(paths ManagedPaths, settings *SettingsStore) *ConfigManager {
	return &ConfigManager{
		paths:    paths,
		settings: settings,
	}
}

// EnsureConfig 创建或补齐托管配置。
func (m *ConfigManager) EnsureConfig() (ConfigState, error) {
	if err := EnsureManagedDirectories(m.paths); err != nil {
		return ConfigState{}, err
	}
	if !FileExists(m.paths.ConfigPath) {
		return m.createDefaultConfig()
	}
	return m.patchExistingConfig()
}

// LoadConfigState 读取当前配置中的关键字段。
func (m *ConfigManager) LoadConfigState() (ConfigState, error) {
	if !FileExists(m.paths.ConfigPath) {
		return ConfigState{}, fmt.Errorf("配置文件不存在: %s", m.paths.ConfigPath)
	}
	raw, err := os.ReadFile(m.paths.ConfigPath)
	if err != nil {
		return ConfigState{}, fmt.Errorf("读取配置失败: %w", err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return ConfigState{}, fmt.Errorf("解析配置失败: %w", err)
	}

	host := "127.0.0.1"
	port := defaultPort
	secret := m.settings.ManagementKey()
	if root := documentMap(&node); root != nil {
		if value := mapValue(root, "host"); value != nil && value.Value != "" {
			host = value.Value
		}
		if value := mapValue(root, "port"); value != nil {
			fmt.Sscanf(value.Value, "%d", &port)
		}
		remote := ensureMapValue(root, "remote-management")
		if value := mapValue(remote, "secret-key"); value != nil && value.Value != "" {
			// 优先跟随用户手动改成的明文密钥；哈希值无法直接用于登录，仍保留 settings 中的明文。
			if !isHashedManagementSecret(value.Value) {
				secret = value.Value
			} else if secret == "" {
				secret = value.Value
			}
		}
	}
	if secret == "" {
		return ConfigState{}, fmt.Errorf("remote-management.secret-key 为空")
	}
	return ConfigState{
		Host:          host,
		Port:          port,
		ManagementKey: secret,
		ConfigPath:    m.paths.ConfigPath,
	}, nil
}

// createDefaultConfig 创建默认配置文件。
func (m *ConfigManager) createDefaultConfig() (ConfigState, error) {
	secret, err := m.settings.EnsureManagementKey()
	if err != nil {
		return ConfigState{}, err
	}

	root := &yaml.Node{Kind: yaml.MappingNode}
	setScalar(root, "host", "127.0.0.1")
	setScalar(root, "port", fmt.Sprintf("%d", defaultPort))
	setScalar(root, "auth-dir", filepath.Join(m.paths.RootDir, "auth"))
	setScalar(root, "logging-to-file", "true")
	remote := ensureMapValue(root, "remote-management")
	setScalar(remote, "allow-remote", "false")
	setScalar(remote, "secret-key", secret)
	setScalar(remote, "disable-control-panel", "true")

	doc := yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	raw, err := yaml.Marshal(&doc)
	if err != nil {
		return ConfigState{}, fmt.Errorf("编码配置失败: %w", err)
	}
	if err := os.WriteFile(m.paths.ConfigPath, raw, 0o644); err != nil {
		return ConfigState{}, fmt.Errorf("写入配置失败: %w", err)
	}
	return ConfigState{
		Host:          "127.0.0.1",
		Port:          defaultPort,
		ManagementKey: secret,
		ConfigPath:    m.paths.ConfigPath,
	}, nil
}

// patchExistingConfig 为已有配置补齐必要字段。
func (m *ConfigManager) patchExistingConfig() (ConfigState, error) {
	raw, err := os.ReadFile(m.paths.ConfigPath)
	if err != nil {
		return ConfigState{}, fmt.Errorf("读取配置失败: %w", err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return ConfigState{}, fmt.Errorf("解析配置失败: %w", err)
	}
	root := documentMap(&node)
	if root == nil {
		root = &yaml.Node{Kind: yaml.MappingNode}
		node = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	}

	if mapValue(root, "host") == nil {
		setScalar(root, "host", "127.0.0.1")
	}
	if mapValue(root, "port") == nil {
		setScalar(root, "port", fmt.Sprintf("%d", defaultPort))
	}
	if mapValue(root, "auth-dir") == nil {
		setScalar(root, "auth-dir", filepath.Join(m.paths.RootDir, "auth"))
	}
	if mapValue(root, "logging-to-file") == nil {
		setScalar(root, "logging-to-file", "true")
	}

	remote := ensureMapValue(root, "remote-management")
	if mapValue(remote, "allow-remote") == nil {
		setScalar(remote, "allow-remote", "false")
	}
	if mapValue(remote, "disable-control-panel") == nil {
		setScalar(remote, "disable-control-panel", "true")
	}
	secretNode := mapValue(remote, "secret-key")
	if secretNode == nil || secretNode.Value == "" {
		secret, err := m.settings.EnsureManagementKey()
		if err != nil {
			return ConfigState{}, err
		}
		setScalar(remote, "secret-key", secret)
	} else if !isHashedManagementSecret(secretNode.Value) && secretNode.Value != m.settings.ManagementKey() {
		// 用户手动改了明文密钥时，同步回 settings，确保内嵌管理页与健康检查继续可用。
		if err := m.settings.SaveManagementKey(secretNode.Value); err != nil {
			return ConfigState{}, err
		}
	}

	updatedRaw, err := yaml.Marshal(&node)
	if err != nil {
		return ConfigState{}, fmt.Errorf("编码配置失败: %w", err)
	}
	if err := os.WriteFile(m.paths.ConfigPath, updatedRaw, 0o644); err != nil {
		return ConfigState{}, fmt.Errorf("写入配置失败: %w", err)
	}
	return m.LoadConfigState()
}

// documentMap 获取 YAML 文档根映射。
func documentMap(node *yaml.Node) *yaml.Node {
	if node == nil || len(node.Content) == 0 {
		return nil
	}
	return node.Content[0]
}

// mapValue 获取映射节点中的值节点。
func mapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index < len(node.Content)-1; index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

// ensureMapValue 确保某个键存在映射值。
func ensureMapValue(node *yaml.Node, key string) *yaml.Node {
	value := mapValue(node, key)
	if value != nil && value.Kind == yaml.MappingNode {
		return value
	}
	if value != nil {
		value.Kind = yaml.MappingNode
		value.Tag = "!!map"
		value.Value = ""
		value.Content = []*yaml.Node{}
		return value
	}
	mapNode := &yaml.Node{Kind: yaml.MappingNode}
	node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, mapNode)
	return mapNode
}

// setScalar 设置标量键值。
func setScalar(node *yaml.Node, key, value string) {
	if existing := mapValue(node, key); existing != nil {
		existing.Kind = yaml.ScalarNode
		existing.Tag = detectScalarTag(value)
		existing.Value = value
		return
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: detectScalarTag(value), Value: value},
	)
}

// detectScalarTag 根据值推断 YAML 标量类型。
func detectScalarTag(value string) string {
	if value == "true" || value == "false" {
		return "!!bool"
	}
	isNumber := true
	for _, char := range value {
		if char < '0' || char > '9' {
			isNumber = false
			break
		}
	}
	if isNumber {
		return "!!int"
	}
	return "!!str"
}

// isHashedManagementSecret 判断密钥是否已经是哈希形式。
func isHashedManagementSecret(secret string) bool {
	secret = strings.TrimSpace(secret)
	return strings.HasPrefix(secret, "$2a$") || strings.HasPrefix(secret, "$2b$") || strings.HasPrefix(secret, "$2y$")
}
