package oidc

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"math/big"
	"testing"
)

// IdPs sign id_tokens with whatever algorithm they like. Only RSA PKCS#1 v1.5 used to
// be accepted, so an ES384 provider answered "unsupported id_token alg" and nobody
// could sign in (reported 2026-11-08). This pins the accepted set and, just as
// important, the refused one.
func TestVerifyJWTSignatureAcceptsTheCommonAlgorithms(t *testing.T) {
	signed := []byte("header.payload")

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	digest256 := sha256.Sum256(signed)
	rsaSig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, digest256[:])
	if err != nil {
		t.Fatalf("rsa sign: %v", err)
	}
	digest384 := sha512.Sum384(signed)
	rsaSig384, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA384, digest384[:])
	if err != nil {
		t.Fatalf("rsa sign 384: %v", err)
	}

	ecKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("ec key: %v", err)
	}
	ecDigest := sha512.Sum384(signed)
	r, s, err := ecdsa.Sign(rand.Reader, ecKey, ecDigest[:])
	if err != nil {
		t.Fatalf("ec sign: %v", err)
	}
	// JOSE carries the raw r||s pair, each padded to the curve size.
	size := (ecKey.Curve.Params().BitSize + 7) / 8
	ecSig := make([]byte, size*2)
	r.FillBytes(ecSig[:size])
	s.FillBytes(ecSig[size:])

	edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519 key: %v", err)
	}
	edSig := ed25519.Sign(edPriv, signed)

	cases := []struct {
		name string
		alg  string
		key  crypto.PublicKey
		sig  []byte
	}{
		{"RS256", "RS256", &rsaKey.PublicKey, rsaSig},
		{"RS384", "RS384", &rsaKey.PublicKey, rsaSig384},
		{"ES384", "ES384", &ecKey.PublicKey, ecSig},
		{"EdDSA", "EdDSA", edPub, edSig},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := verifyJWTSignature(testCase.alg, testCase.key, signed, testCase.sig); err != nil {
				t.Fatalf("%s was refused: %v", testCase.alg, err)
			}
		})
	}
}

// The refusals matter as much as the acceptances: HS* with a public client is the
// classic key confusion attack, and an algorithm must match its key type.
func TestVerifyJWTSignatureRefusesWhatItShould(t *testing.T) {
	signed := []byte("header.payload")
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	ecKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	digest := sha256.Sum256(signed)
	sig, _ := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, digest[:])

	cases := []struct {
		name string
		alg  string
		key  crypto.PublicKey
		sig  []byte
	}{
		{"none", "none", &rsaKey.PublicKey, []byte{}},
		{"HS256", "HS256", &rsaKey.PublicKey, sig},
		{"ES384 with an RSA key", "ES384", &rsaKey.PublicKey, sig},
		{"RS256 with an EC key", "RS256", &ecKey.PublicKey, sig},
		{"unknown", "XX999", &rsaKey.PublicKey, sig},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := verifyJWTSignature(testCase.alg, testCase.key, signed, testCase.sig); err == nil {
				t.Fatalf("%s was accepted, want a refusal", testCase.alg)
			}
		})
	}
}

// An ES384 token signed over a different curve must not verify.
func TestVerifyJWTSignatureRejectsAWrongCurve(t *testing.T) {
	signed := []byte("a.b")
	p256, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	digest := sha512.Sum384(signed)
	r, s, _ := ecdsa.Sign(rand.Reader, p256, digest[:])
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	if err := verifyJWTSignature("ES384", &p256.PublicKey, signed, sig); err == nil {
		t.Fatal("ES384 accepted a P-256 key")
	}
	_ = big.NewInt(0)
}
