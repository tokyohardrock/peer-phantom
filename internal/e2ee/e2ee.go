package e2ee

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"

	"github.com/libp2p/go-libp2p/core/crypto"
	"golang.org/x/crypto/hkdf"
)

// GenerateSessionKeys generates current session keys using the elliptic curve Diffie-Hellman algorithm
func GenerateSessionKeys(peerPrivKey crypto.PrivKey) (*ecdh.PrivateKey, *ecdh.PublicKey, []byte, error) {
	curve := ecdh.X25519()

	sessionPrivKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}

	sessionPubKey := sessionPrivKey.PublicKey()

	signedSessionPubKey, err := peerPrivKey.Sign(sessionPubKey.Bytes())
	if err != nil {
		return nil, nil, nil, err
	}

	return sessionPrivKey, sessionPubKey, signedSessionPubKey, nil
}

// VerifySessionPubKey verifies that "signature" was signed via sender's private peer id
func VerifySessionPubKey(senderPubKey crypto.PubKey, sessionPubBytes []byte, signature []byte) error {
	valid, err := senderPubKey.Verify(sessionPubBytes, signature)
	if err != nil {
		return err
	}

	if !valid {
		return errors.New("Invalid session key signature!")
	}

	return nil
}

// ComputeSharedSecret computes Diffie-Hellman shared secret
func ComputeSharedSecret(localPriv *ecdh.PrivateKey, remotePub []byte) ([]byte, error) {
	curve := ecdh.X25519()

	peerPub, err := curve.NewPublicKey(remotePub)
	if err != nil {
		panic(err)
	}

	sharedSecret, err := localPriv.ECDH(peerPub)
	if err != nil {
		return nil, err
	}

	return sharedSecret, nil
}

// DeriveAESKey derives Diffie-Hellman shared secret and returns a 32-byte key for the AES-256 encryption algorithm
func DeriveAESKey(sharedSecret []byte) ([]byte, error) {
	hkdf := hkdf.New(sha256.New, sharedSecret, nil, nil)
	key := make([]byte, 32)
	if _, err := io.ReadFull(hkdf, key); err != nil {
		return nil, err
	}

	return key, nil
}

// EncryptMessage encrypts given sequence of bytes via AES algorithm with GCM mode using given aesKey
func EncryptMessage(aesKey, plaintext []byte) ([]byte, error) {
	cphr, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(cphr)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// DecryptMessage decrypts given sequence of bytes via AES algorithm with GCM mode using given aesKey
func DecryptMessage(aesKey, ciphertext []byte) ([]byte, error) {
	cphr, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(cphr)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("Invalid ciphertext size!")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}
