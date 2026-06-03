package peer

import (
	"crypto/rand"
	"fmt"
	"os"

	"github.com/libp2p/go-libp2p/core/crypto"
)

func loadPrivateKey(keyFile string) (crypto.PrivKey, error) {
	const fn = "peer.loadPrivateKey"

	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		privKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("%s: failed to generate private key: %w", fn, err)
		}

		keyAsBytes, err := crypto.MarshalPrivateKey(privKey)
		if err != nil {
			return nil, fmt.Errorf("%s: failed to marshal private key: %w", fn, err)
		}

		err = os.WriteFile(keyFile, keyAsBytes, 0600)
		if err != nil {
			return nil, fmt.Errorf("%s: failed to write private key to file: %w", fn, err)
		}

		return privKey, nil
	}

	privKey, err := os.ReadFile(keyFile) // load existing key
	if err != nil {
		return nil, fmt.Errorf("%s: failed to read private key from file: %w", fn, err)
	}

	return crypto.UnmarshalPrivateKey(privKey)
}
