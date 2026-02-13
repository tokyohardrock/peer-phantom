package peer

import (
	"crypto/rand"
	"os"

	"github.com/libp2p/go-libp2p/core/crypto"
)

func loadPrivateKey(keyFile string) (crypto.PrivKey, error) {
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		privKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			return nil, err
		}

		keyAsBytes, err := crypto.MarshalPrivateKey(privKey)
		if err != nil {
			return nil, err
		}

		err = os.WriteFile(keyFile, keyAsBytes, 0600)
		if err != nil {
			return nil, err
		}

		return privKey, nil
	}

	privKey, err := os.ReadFile(keyFile) // load existing key
	if err != nil {
		return nil, err
	}

	return crypto.UnmarshalPrivateKey(privKey)
}
