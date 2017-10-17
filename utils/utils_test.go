package utils

import "testing"

func TestGenerateKey(t *testing.T) {
	pri, pub, err := GenerateKey()
	if err != nil {
		panic(err)
	}
	priK, err := ParsePrivateKey(pri)
	if err != nil {
		panic(err)
	}
	pubK, err := ParsePublicKey(pub)
	if err != nil {
		panic(err)
	}
	pubFrPriv := PublicKeyFromPrivate(priK)
	if pubK.E != pubFrPriv.E {
		panic("key gen pub key not match")
	}
}

func TestVerifySign(t *testing.T) {
	pri, pub, err := GenerateKey()
	if err != nil {
		panic(err)
	}
	priK, err := ParsePrivateKey(pri)
	if err != nil {
		panic(err)
	}
	pubK, err := ParsePublicKey(pub)
	if err != nil {
		panic(err)
	}
	pubFrPriv := PublicKeyFromPrivate(priK)
	data := []byte("test signature")
	sig := Sign(priK, data)
	if err := VerifySign(pubK, sig, data); err != nil {
		panic(err)
	}
	if err := VerifySign(pubFrPriv, sig, data); err != nil {
		panic(err)
	}
}
