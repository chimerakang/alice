package app

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// I18nManager 管理多國語系訊息
type I18nManager struct {
	mu        sync.RWMutex
	languages map[string]*LanguageBundle
	default_  string // 預設語言
}

// LanguageBundle 語系包
type LanguageBundle struct {
	Lang     string            `json:"lang"`
	Name     string            `json:"name"`
	Messages map[string]string `json:"messages"`
}

// NewI18nManager 建立新的 i18n 管理器
func NewI18nManager(localesDir string, defaultLang string) (*I18nManager, error) {
	manager := &I18nManager{
		languages: make(map[string]*LanguageBundle),
		default_:  defaultLang,
	}

	// 從目錄載入所有語系檔案
	entries, err := os.ReadDir(localesDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("[i18n] Locales directory not found: %s", localesDir)
			return manager, nil
		}
		return nil, fmt.Errorf("failed to read locales directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(localesDir, entry.Name())
		langCode := strings.TrimSuffix(entry.Name(), ".json")

		bundle, err := loadLanguageBundle(filePath)
		if err != nil {
			log.Printf("[i18n] Failed to load language file %s: %v", filePath, err)
			continue
		}

		manager.languages[langCode] = bundle
		log.Printf("[i18n] Loaded language: %s (%s)", langCode, bundle.Name)
	}

	if len(manager.languages) == 0 {
		return nil, fmt.Errorf("no language bundles loaded from %s", localesDir)
	}

	return manager, nil
}

// loadLanguageBundle 載入語系檔案
func loadLanguageBundle(filePath string) (*LanguageBundle, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var bundle LanguageBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return nil, err
	}

	return &bundle, nil
}

// GetMessage 取得訊息
// 支援模板變數，例如 "Hello {name}" 會被替換為 "Hello John"
func (m *I18nManager) GetMessage(lang, key string, vars map[string]string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 優先使用指定語言，否則使用預設語言
	bundle, ok := m.languages[lang]
	if !ok {
		bundle, ok = m.languages[m.default_]
		if !ok {
			// 如果都找不到，回傳 key
			return key
		}
	}

	message, ok := bundle.Messages[key]
	if !ok {
		// 如果找不到 key，嘗試預設語言
		if lang != m.default_ {
			defaultBundle := m.languages[m.default_]
			if defaultBundle != nil {
				if msg, ok := defaultBundle.Messages[key]; ok {
					message = msg
				}
			}
		}

		if message == "" {
			return key
		}
	}

	// 替換模板變數
	if vars != nil {
		for k, v := range vars {
			message = strings.ReplaceAll(message, "{"+k+"}", v)
		}
	}

	return message
}

// GetAvailableLanguages 取得可用的語言清單
func (m *I18nManager) GetAvailableLanguages() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]string)
	for code, bundle := range m.languages {
		result[code] = bundle.Name
	}
	return result
}

// IsLanguageSupported 檢查語言是否支援
func (m *I18nManager) IsLanguageSupported(lang string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.languages[lang]
	return ok
}

// GetDefaultLanguage 取得預設語言
func (m *I18nManager) GetDefaultLanguage() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.default_
}

// SetDefaultLanguage 設定預設語言
func (m *I18nManager) SetDefaultLanguage(lang string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.languages[lang]; !ok {
		return fmt.Errorf("language %s not supported", lang)
	}

	m.default_ = lang
	return nil
}

// GetLanguageName 取得語言名稱
func (m *I18nManager) GetLanguageName(lang string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	bundle, ok := m.languages[lang]
	if !ok {
		return lang
	}

	return bundle.Name
}
