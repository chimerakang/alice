package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

func (sm *SecurityManager) initEncryption(keyStr string) error {
	key, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil {
		hash := sha256.Sum256([]byte(keyStr))
		key = hash[:]
	}

	if len(key) != 32 {
		return fmt.Errorf("encryption key must be 32 bytes (got %d)", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	sm.cipher, err = cipher.NewGCM(block)
	if err != nil {
		return err
	}

	return nil
}

// Encrypt 加密文字
func (sm *SecurityManager) Encrypt(plaintext string) (string, error) {
	if sm.cipher == nil {
		return plaintext, fmt.Errorf("encryption not configured")
	}

	nonce := make([]byte, sm.cipher.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := sm.cipher.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密文字
func (sm *SecurityManager) Decrypt(ciphertext string) (string, error) {
	if sm.cipher == nil {
		return ciphertext, fmt.Errorf("encryption not configured")
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	nonceSize := sm.cipher.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, cipherBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := sm.cipher.Open(nil, nonce, cipherBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
