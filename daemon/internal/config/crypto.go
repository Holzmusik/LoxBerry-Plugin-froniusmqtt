package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
)

// Verschluesselung der in config.json abgelegten Passwoerter (mqtt.password,
// fronius.battery_control.password) - AES-256-GCM aus der Go-Standardbibliothek,
// identisches Schema wie beim Schwester-Plugin EaseeMQTT (siehe dessen
// daemon/internal/config/crypto.go). Schluessel ist zufaellig, pro Installation
// einmalig erzeugt und liegt lazy neben der jeweiligen config.json (Datei
// "secret.key", Modus 0600).
const keyFileName = "secret.key"

func keyPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), keyFileName)
}

func loadOrCreateKey(configPath string) ([]byte, error) {
	path := keyPath(configPath)
	if data, err := os.ReadFile(path); err == nil && len(data) == 32 {
		return data, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("Schluessel konnte nicht erzeugt werden: %w", err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("Schluesseldatei %s konnte nicht geschrieben werden: %w", path, err)
	}
	return key, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt verschluesselt plaintext und liefert Base64(Nonce||Ciphertext||Tag).
func Encrypt(configPath, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	key, err := loadOrCreateKey(configPath)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt entschluesselt einen von Encrypt() erzeugten String. ok=false
// bedeutet "kein gueltiges Chiffrat" - der Aufrufer entscheidet dann selbst,
// ob der Rohwert stattdessen als (Alt-)Klartext behandelt wird.
func Decrypt(configPath, stored string) (plaintext string, ok bool) {
	if stored == "" {
		return "", true
	}
	key, err := loadOrCreateKey(configPath)
	if err != nil {
		return "", false
	}
	data, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return "", false
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", false
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", false
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	out, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", false
	}
	return string(out), true
}
