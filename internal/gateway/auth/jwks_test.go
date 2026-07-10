package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"testing"
)

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func rsaJWK(kid string, pub *rsa.PublicKey) jwk {
	return jwk{
		Kty: "RSA",
		Kid: kid,
		Use: "sig",
		N:   b64url(pub.N.Bytes()),
		E:   b64url(big.NewInt(int64(pub.E)).Bytes()),
	}
}

func ecJWK(t *testing.T, kid string, pub *ecdsa.PublicKey) jwk {
	t.Helper()
	// pub.Bytes() is the maintained SEC1 uncompressed-point encoder
	// (0x04 || X || Y); FillBytes on the raw X/Y fields is deprecated.
	raw, err := pub.Bytes()
	if err != nil {
		t.Fatalf("PublicKey.Bytes: %v", err)
	}
	size := (len(raw) - 1) / 2
	return jwk{
		Kty: "EC",
		Kid: kid,
		Use: "sig",
		Crv: crvName(pub.Curve),
		X:   b64url(raw[1 : 1+size]),
		Y:   b64url(raw[1+size:]),
	}
}

func crvName(c elliptic.Curve) string {
	switch c {
	case elliptic.P256():
		return "P-256"
	case elliptic.P384():
		return "P-384"
	case elliptic.P521():
		return "P-521"
	default:
		return "unknown"
	}
}

func marshalJWKS(t *testing.T, keys ...jwk) []byte {
	t.Helper()
	b, err := json.Marshal(jwkSet{Keys: keys})
	if err != nil {
		t.Fatalf("marshal jwkSet: %v", err)
	}
	return b
}

func TestParseJWKS_RSA(t *testing.T) {
	kp := newRSA(t)
	body := marshalJWKS(t, rsaJWK("k1", kp.pub))

	keys, skipped, err := ParseJWKS(body)
	if err != nil {
		t.Fatalf("ParseJWKS: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none", skipped)
	}
	got, ok := keys["k1"].(*rsa.PublicKey)
	if !ok {
		t.Fatalf("keys[k1] type = %T, want *rsa.PublicKey", keys["k1"])
	}
	if got.E != kp.pub.E || got.N.Cmp(kp.pub.N) != 0 {
		t.Errorf("parsed RSA key does not match: got E=%d N=%s", got.E, got.N.String())
	}

	// Round-trip: the parsed key must actually verify a token signed by the
	// matching private key — proving the parse is not just structurally
	// well-typed but cryptographically correct.
	v := verifierFor(t, keys)
	if _, err := v.Verify(signRS256(t, kp.priv, "k1", claims())); err != nil {
		t.Errorf("Verify with JWKS-parsed key failed: %v", err)
	}
}

func TestParseJWKS_EC(t *testing.T) {
	priv := newECDSA(t)
	body := marshalJWKS(t, ecJWK(t, "k2", &priv.PublicKey))

	keys, _, err := ParseJWKS(body)
	if err != nil {
		t.Fatalf("ParseJWKS: %v", err)
	}
	v := verifierFor(t, keys)
	if _, err := v.Verify(signES256(t, priv, "k2", claims())); err != nil {
		t.Errorf("Verify with JWKS-parsed EC key failed: %v", err)
	}
}

func TestParseJWKS_MultipleKeys(t *testing.T) {
	rsaKP := newRSA(t)
	ecPriv := newECDSA(t)
	body := marshalJWKS(t, rsaJWK("rk", rsaKP.pub), ecJWK(t, "ek", &ecPriv.PublicKey))

	keys, skipped, err := ParseJWKS(body)
	if err != nil {
		t.Fatalf("ParseJWKS: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none", skipped)
	}
	if len(keys) != 2 {
		t.Fatalf("len(keys) = %d, want 2", len(keys))
	}
}

func TestParseJWKS_SkipsNonSigUse(t *testing.T) {
	kp := newRSA(t)
	enc := rsaJWK("enc1", kp.pub)
	enc.Use = "enc"
	body := marshalJWKS(t, enc)

	_, _, err := ParseJWKS(body)
	if !errors.Is(err, ErrEmptyJWKS) {
		t.Fatalf("err = %v, want ErrEmptyJWKS (the only key present is use=enc)", err)
	}
}

func TestParseJWKS_SkipsMissingKid(t *testing.T) {
	kp := newRSA(t)
	noKid := rsaJWK("", kp.pub)
	goodKey := newRSA(t)
	body := marshalJWKS(t, noKid, rsaJWK("good", goodKey.pub))

	keys, skipped, err := ParseJWKS(body)
	if err != nil {
		t.Fatalf("ParseJWKS: %v", err)
	}
	if len(keys) != 1 || keys["good"] == nil {
		t.Fatalf("keys = %v, want only {good}", keys)
	}
	if len(skipped) != 1 {
		t.Errorf("skipped = %v, want 1 entry", skipped)
	}
}

func TestParseJWKS_SkipsDuplicateKid(t *testing.T) {
	kp1 := newRSA(t)
	kp2 := newRSA(t)
	body := marshalJWKS(t, rsaJWK("dup", kp1.pub), rsaJWK("dup", kp2.pub))

	keys, skipped, err := ParseJWKS(body)
	if err != nil {
		t.Fatalf("ParseJWKS: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	got, _ := keys["dup"].(*rsa.PublicKey)
	if got.N.Cmp(kp1.pub.N) != 0 {
		t.Errorf("duplicate kid should keep the FIRST entry")
	}
	if len(skipped) != 1 {
		t.Errorf("skipped = %v, want 1 entry (the duplicate)", skipped)
	}
}

func TestParseJWKS_SkipsUnsupportedKty(t *testing.T) {
	body := marshalJWKS(t, jwk{Kty: "oct", Kid: "sym", Use: "sig"})

	_, skipped, err := ParseJWKS(body)
	if !errors.Is(err, ErrEmptyJWKS) {
		t.Fatalf("err = %v, want ErrEmptyJWKS", err)
	}
	if len(skipped) != 1 {
		t.Errorf("skipped = %v, want 1 entry", skipped)
	}
}

func TestParseJWKS_SkipsUnsupportedCurve(t *testing.T) {
	body := marshalJWKS(t, jwk{Kty: "EC", Kid: "weird", Use: "sig", Crv: "secp256k1", X: "AA", Y: "AA"})

	_, skipped, err := ParseJWKS(body)
	if !errors.Is(err, ErrEmptyJWKS) {
		t.Fatalf("err = %v, want ErrEmptyJWKS", err)
	}
	if len(skipped) != 1 {
		t.Errorf("skipped = %v, want 1 entry", skipped)
	}
}

func TestParseJWKS_RejectsPointNotOnCurve(t *testing.T) {
	// A structurally-decodable but bogus (x, y) pair must not silently
	// produce an *ecdsa.PublicKey the parser trusts.
	body := marshalJWKS(t, jwk{
		Kty: "EC", Kid: "bogus", Use: "sig", Crv: "P-256",
		X: b64url([]byte{1, 2, 3, 4}),
		Y: b64url([]byte{5, 6, 7, 8}),
	})

	_, skipped, err := ParseJWKS(body)
	if !errors.Is(err, ErrEmptyJWKS) {
		t.Fatalf("err = %v, want ErrEmptyJWKS", err)
	}
	if len(skipped) != 1 {
		t.Errorf("skipped = %v, want 1 entry", skipped)
	}
}

func TestParseJWKS_EmptyKeysArray(t *testing.T) {
	body := marshalJWKS(t)
	_, _, err := ParseJWKS(body)
	if !errors.Is(err, ErrEmptyJWKS) {
		t.Fatalf("err = %v, want ErrEmptyJWKS", err)
	}
}

func TestParseJWKS_MalformedJSON(t *testing.T) {
	_, _, err := ParseJWKS([]byte("not json"))
	if err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
	if errors.Is(err, ErrEmptyJWKS) {
		t.Error("malformed JSON should be a parse error, not ErrEmptyJWKS")
	}
}

func TestParseJWKS_RejectsDegenerateRSAExponent(t *testing.T) {
	kp := newRSA(t)
	for _, e := range []string{"AA", "AQ"} { // 0x00 (e=0), 0x01 (e=1)
		k := rsaJWK("bad-e", kp.pub)
		k.E = e
		body := marshalJWKS(t, k)

		_, skipped, err := ParseJWKS(body)
		if !errors.Is(err, ErrEmptyJWKS) {
			t.Fatalf("e=%q: err = %v, want ErrEmptyJWKS", e, err)
		}
		if len(skipped) != 1 {
			t.Errorf("e=%q: skipped = %v, want 1 entry", e, skipped)
		}
	}
}

func TestParseJWKS_MalformedBase64(t *testing.T) {
	body := marshalJWKS(t, jwk{Kty: "RSA", Kid: "bad", Use: "sig", N: "not-valid-base64!!!", E: "AQAB"})
	_, skipped, err := ParseJWKS(body)
	if !errors.Is(err, ErrEmptyJWKS) {
		t.Fatalf("err = %v, want ErrEmptyJWKS", err)
	}
	if len(skipped) != 1 {
		t.Errorf("skipped = %v, want 1 entry", skipped)
	}
}

func TestSetKeys_HotSwap(t *testing.T) {
	kp1 := newRSA(t)
	kp2 := newRSA(t)
	v := verifierFor(t, map[string]crypto.PublicKey{"k1": kp1.pub})

	tok1 := signRS256(t, kp1.priv, "k1", claims())
	tok2 := signRS256(t, kp2.priv, "k2", claims())

	if _, err := v.Verify(tok1); err != nil {
		t.Fatalf("Verify(tok1) before swap: %v", err)
	}
	if _, err := v.Verify(tok2); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("Verify(tok2) before swap: err = %v, want ErrUnknownKey", err)
	}

	if err := v.SetKeysWithInfo(
		map[string]crypto.PublicKey{"k2": kp2.pub},
		map[string]KeyInfo{"k2": {Spec: BindingSpec{Mode: ModeNanoID}}},
	); err != nil {
		t.Fatalf("SetKeysWithInfo: %v", err)
	}

	if _, err := v.Verify(tok2); err != nil {
		t.Fatalf("Verify(tok2) after swap: %v", err)
	}
	if _, err := v.Verify(tok1); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("Verify(tok1) after swap: err = %v, want ErrUnknownKey (k1 was retired by the swap)", err)
	}
}
