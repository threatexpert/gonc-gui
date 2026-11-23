package nearbystore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

// keyBytes is the size of an Options.Key.
const keyBytes = 32

// sealMagic marks a key-sealed store file and is bound in as associated data.
var sealMagic = []byte("gonc-nearby-store-v1\x00")

func seal(plain, key []byte) ([]byte, error) {
	gcm, err := sealCipher(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append(append([]byte(nil), sealMagic...), nonce...)
	return gcm.Seal(out, nonce, plain, sealMagic), nil
}

func openSealed(sealed, key []byte) ([]byte, error) {
	gcm, err := sealCipher(key)
	if err != nil {
		return nil, err
	}
	if len(sealed) < len(sealMagic)+gcm.NonceSize() || string(sealed[:len(sealMagic)]) != string(sealMagic) {
		return nil, errors.New("nearby store has an unrecognized format")
	}
	rest := sealed[len(sealMagic):]
	plain, err := gcm.Open(nil, rest[:gcm.NonceSize()], rest[gcm.NonceSize():], sealMagic)
	if err != nil {
		return nil, errors.New("nearby store could not be opened with this key")
	}
	return plain, nil
}

func sealCipher(key []byte) (cipher.AEAD, error) {
	if len(key) != keyBytes {
		return nil, errors.New("nearby store key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
