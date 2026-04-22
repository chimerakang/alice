package security

// 全域安全管理器實例
var globalManager *SecurityManager

// Init 初始化全域安全管理器
func Init(config SecurityConfig) error {
	var err error
	globalManager, err = NewSecurityManager(config)
	return err
}

// Global 回傳當前全域安全管理器（未初始化時回傳 nil）
func Global() *SecurityManager {
	return globalManager
}
