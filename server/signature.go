package server

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
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