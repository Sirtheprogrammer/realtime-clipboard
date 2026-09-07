package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	pbkdf2Iterations = 600000
	saltLen          = 16
	keyLen           = 32
)

// HashPassword hashes a plain password with PBKDF2-HMAC-SHA256 and a cryptographically secure salt.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	dk := derivePBKDF2([]byte(password), salt, pbkdf2Iterations, keyLen)
	saltB64 := base64.RawURLEncoding.EncodeToString(salt)
	dkB64 := base64.RawURLEncoding.EncodeToString(dk)

	return fmt.Sprintf("$pbkdf2-sha256$i=%d$%s$%s", pbkdf2Iterations, saltB64, dkB64), nil
}

// VerifyPassword verifies whether a plain password matches the encoded PBKDF2-HMAC-SHA256 hash.
func VerifyPassword(password, encodedHash string) bool {
	parts := strings.Split(encodedHash, "$")
	// Expected format: ["", "pbkdf2-sha256", "i=600000", "<salt>", "<hash>"]
	if len(parts) != 5 || parts[1] != "pbkdf2-sha256" {
		return false
	}

	iterStr := strings.TrimPrefix(parts[2], "i=")
	iter, err := strconv.Atoi(iterStr)
	if err != nil || iter <= 0 {
		return false
	}

	salt, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}

	expectedDK, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}

	actualDK := derivePBKDF2([]byte(password), salt, iter, len(expectedDK))
	return subtle.ConstantTimeCompare(actualDK, expectedDK) == 1
}

func derivePBKDF2(password, salt []byte, iter, kLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	numBlocks := (kLen + hashLen - 1) / hashLen
	var buf [4]byte
	dk := make([]byte, 0, numBlocks*hashLen)
	u := make([]byte, hashLen)

	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		buf[0] = byte(block >> 24)
		buf[1] = byte(block >> 16)
		buf[2] = byte(block >> 8)
		buf[3] = byte(block)
		prf.Write(buf[:4])
		dkBlock := prf.Sum(nil)
		copy(u, dkBlock)

		for i := 2; i <= iter; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for k := 0; k < hashLen; k++ {
				dkBlock[k] ^= u[k]
			}
		}
		dk = append(dk, dkBlock...)
	}
	return dk[:kLen]
}

// EncryptSecret encrypts plaintext using AES-256-GCM envelope encryption with a 32-byte key.
// Returns base64(nonce + ciphertext + tag).
func EncryptSecret(plaintext string, key [32]byte) (string, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptSecret decrypts ciphertext created by EncryptSecret using AES-256-GCM.
func DecryptSecret(encrypted string, key [32]byte) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("new gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt failed: %w", err)
	}

	return string(plaintext), nil
}

// GenerateSessionToken generates a cryptographically random session token.
func GenerateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
