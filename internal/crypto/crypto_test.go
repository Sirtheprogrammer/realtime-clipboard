package crypto

import (
	"crypto/rand"
	"testing"
)

func TestPasswordHashing(t *testing.T) {
	password := "Correct-Horse-Battery-Staple-42!"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if !VerifyPassword(password, hash) {
		t.Fatal("VerifyPassword failed for matching password")
	}

	if VerifyPassword("Wrong-Password", hash) {
		t.Fatal("VerifyPassword should not match wrong password")
	}
}

func TestEnvelopeEncryption(t *testing.T) {
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatal(err)
	}

	secret := "sk-live-1234567890abcdef-very-secret-token"
	encrypted, err := EncryptSecret(secret, key)
	if err != nil {
		t.Fatalf("EncryptSecret failed: %v", err)
	}

	if encrypted == secret {
		t.Fatal("ciphertext should not equal plaintext")
	}

	decrypted, err := DecryptSecret(encrypted, key)
	if err != nil {
		t.Fatalf("DecryptSecret failed: %v", err)
	}

	if decrypted != secret {
		t.Fatalf("expected %q, got %q", secret, decrypted)
	}

	// Wrong key fails
	var wrongKey [32]byte
	wrongKey[0] = key[0] ^ 0xff
	if _, err := DecryptSecret(encrypted, wrongKey); err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}
