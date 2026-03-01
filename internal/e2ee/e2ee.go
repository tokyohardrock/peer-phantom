package e2ee

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"

	"github.com/libp2p/go-libp2p/core/crypto"
	"golang.org/x/crypto/hkdf"
)

type sessionPubKeys struct {
	PubKey       []byte `json:"pubKey"`
	SignedPubKey []byte `json:"signedPubKey"`
}

// GenerateSessionKeys generates current session keys using the elliptic curve Diffie-Hellman algorithm
func GenerateSessionKeys(peerPrivKey crypto.PrivKey) (*ecdh.PrivateKey, []byte, error) {
	const fn = "e2ee.GenerateSessionKeys"

	curve := ecdh.X25519()

	sessionPrivKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: failed to generate session private key: %w", fn, err)
	}

	sessionPubKey := sessionPrivKey.PublicKey().Bytes()

	signedSessionPubKey, err := peerPrivKey.Sign(sessionPubKey)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: failed to sign session public key: %w", fn, err)
	}

	keys, err := json.Marshal(sessionPubKeys{
		PubKey:       sessionPubKey,
		SignedPubKey: signedSessionPubKey,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%s: failed to marshal session keys: %w", fn, err)
	}

	return sessionPrivKey, keys, nil
}

// VerifySessionPubKey verifies that "signedSessionPubBytes" was signed via sender's private peer id
func VerifySessionPubKey(senderPubKey crypto.PubKey, keysBytes []byte) ([]byte, error) {
	const fn = "e2ee.VerifySessionPubKey"

	var keys sessionPubKeys

	err := json.Unmarshal(keysBytes, &keys)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to unmarshal session keys: %w", fn, err)
	}

	valid, err := senderPubKey.Verify(keys.PubKey, keys.SignedPubKey)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to verify session public key signature: %w", fn, err)
	}

	if !valid {
		return nil, fmt.Errorf("%s: invalid session public key signature", fn)
	}

	return keys.PubKey, nil
}

// ComputeSharedSecret computes Diffie-Hellman shared secret
func ComputeSharedSecret(localPriv *ecdh.PrivateKey, remotePub []byte) ([]byte, error) {
	const fn = "e2ee.ComputeSharedSecret"

	curve := ecdh.X25519()

	peerPub, err := curve.NewPublicKey(remotePub)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to create public key from bytes: %w", fn, err)
	}

	sharedSecret, err := localPriv.ECDH(peerPub)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to compute shared secret: %w", fn, err)
	}

	return sharedSecret, nil
}

// DeriveAESKey derives Diffie-Hellman shared secret and returns a 32-byte key for the AES-256 encryption algorithm
func DeriveAESKey(sharedSecret []byte) ([]byte, error) {
	const fn = "e2ee.DeriveAESKey"

	hkdf := hkdf.New(sha256.New, sharedSecret, nil, nil)
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf, key); err != nil {
		return nil, fmt.Errorf("%s: failed to derive AES key: %w", fn, err)
	}

	return key, nil
}

// EncryptMessage encrypts given sequence of bytes via AES algorithm with GCM mode using given aesKey
func EncryptMessage(aesKey, plaintext []byte) ([]byte, error) {
	const fn = "e2ee.EncryptMessage"

	cphr, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to create AES cipher: %w", fn, err)
	}

	gcm, err := cipher.NewGCM(cphr)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to create GCM cipher mode: %w", fn, err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("%s: failed to generate nonce: %w", fn, err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// DecryptMessage decrypts given sequence of bytes via AES algorithm with GCM mode using given aesKey
func DecryptMessage(aesKey, ciphertext []byte) ([]byte, error) {
	const fn = "e2ee.DecryptMessage"

	cphr, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to create AES cipher: %w", fn, err)
	}

	gcm, err := cipher.NewGCM(cphr)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to create GCM cipher mode: %w", fn, err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("%s: ciphertext too short", fn)
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: failed to decrypt message: %w", fn, err)
	}

	return plaintext, nil
}
