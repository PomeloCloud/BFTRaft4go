package server

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"hash/fnv"
)

func GenerateKey() ([]byte, []byte, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err == nil {
		return nil, nil, err
	}
	publicKey := &privateKey.PublicKey
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err == nil {
		return nil, nil, err
	}
	return privateKeyBytes, publicKeyBytes, nil
}

func ParsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	return x509.ParsePKCS1PrivateKey(data)
}

func ParsePublicKey(data []byte) (*rsa.PublicKey, error) {
	key, err := x509.ParsePKIXPublicKey(data)
	return key.(*rsa.PublicKey), err
}

func PublicKeyFromPrivate(key *rsa.PrivateKey) *rsa.PublicKey {
	return &key.PublicKey
}

func HashPublicKeyBytes(keyData []byte) uint64 {
	fnv_hasher := fnv.New64a()
	fnv_hasher.Write(keyData)
	return fnv_hasher.Sum64()
}

func HashPublicKey(key *rsa.PublicKey) uint64 {
	keyData, _ := x509.MarshalPKIXPublicKey(key)
	return HashPublicKeyBytes(keyData)
}

func (s *BFTRaftServer) Sign(data []byte) []byte {
	hashed, hash := SHA1Hash(data)
	signature, _ := rsa.SignPKCS1v15(rand.Reader, s.PrivateKey, hash, hashed)
	return signature
}

func VerifySign(publicKey *rsa.PublicKey, signature []byte, data []byte) error {
	hashed, hash := SHA1Hash(data)
	return rsa.VerifyPKCS1v15(publicKey, hash, hashed, signature)
}

func SHA1Hash(data []byte) ([]byte, crypto.Hash) {
	hash := crypto.SHA1
	h := hash.New()
	h.Write(data)
	return h.Sum(nil), hash
}

func LogHash(prevHash []byte, index uint64, funcId uint64, args []byte) ([]byte, crypto.Hash) {
	// combine index, funcId and args to prevent unordered log sequences
	return SHA1Hash(append(append(append(prevHash, U64Bytes(index)...), U64Bytes(funcId)...), args...))
}
