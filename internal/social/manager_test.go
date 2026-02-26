package social

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/curve25519"
)

func TestIdentitySaveAndUnlock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	id, err := generateIdentity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}

	m := &Manager{cfg: Config{DataDir: dir}, identity: id}
	if err := m.saveIdentity("pass-123"); err != nil {
		t.Fatalf("save identity: %v", err)
	}

	m2 := &Manager{cfg: Config{DataDir: dir}}
	if err := m2.unlock("pass-123"); err != nil {
		t.Fatalf("unlock identity: %v", err)
	}
	if m2.identity == nil {
		t.Fatalf("identity should be loaded")
	}
	if m2.identity.UserID != id.UserID {
		t.Fatalf("user id mismatch: %s != %s", m2.identity.UserID, id.UserID)
	}
	if string(m2.identity.SignPublicKey) != string(id.SignPublicKey) {
		t.Fatalf("sign public key mismatch")
	}
}

func TestInviteTokenParseAndTamper(t *testing.T) {
	t.Parallel()
	id, err := generateIdentity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}

	m := &Manager{
		identity: id,
		profile: &Profile{
			UserID:        id.UserID,
			Username:      "alice",
			SignPublicKey: base64.RawStdEncoding.EncodeToString(id.SignPublicKey),
			BoxPublicKey:  base64.RawStdEncoding.EncodeToString(id.BoxPublicKey[:]),
		},
	}

	token, err := m.CreateInviteLink()
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	payload, err := parseInvite(token)
	if err != nil {
		t.Fatalf("parse invite: %v", err)
	}
	if got, _ := payload["user_id"].(string); got != id.UserID {
		t.Fatalf("unexpected user_id: %s", got)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Fatalf("unexpected token format")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode sig: %v", err)
	}
	sig[0] ^= 0x01
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString(sig)
	if _, err := parseInvite(tampered); err == nil {
		t.Fatalf("expected tampered token to fail")
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Parallel()

	alice, err := generateIdentity()
	if err != nil {
		t.Fatalf("generate alice: %v", err)
	}
	bob, err := generateIdentity()
	if err != nil {
		t.Fatalf("generate bob: %v", err)
	}

	plain := []byte("secret dm payload")
	ct, nonce, err := encryptForPeer(alice.BoxPrivateKey, bob.BoxPublicKey, plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, err := decryptFromPeer(bob.BoxPrivateKey, alice.BoxPublicKey, nonce, ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(got) != string(plain) {
		t.Fatalf("decrypt mismatch: %q", string(got))
	}
}

func TestNormalizeSettingsDefaults(t *testing.T) {
	t.Parallel()
	in := Settings{Discoverable: false, AllowStrangerRequests: false, IsAdmin: true, ContractAddress: " 0xabc "}
	out := normalizeSettings(in)
	if !out.Discoverable || !out.AllowStrangerRequests {
		t.Fatalf("discovery defaults should stay enabled")
	}
	if !out.IsAdmin {
		t.Fatalf("is_admin should be preserved")
	}
	if out.ContractAddress != "0xabc" {
		t.Fatalf("contract address should be trimmed")
	}
}

func TestIdentityHasValidCurve25519PublicKey(t *testing.T) {
	t.Parallel()
	id, err := generateIdentity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	pub, err := curve25519.X25519(id.BoxPrivateKey[:], curve25519.Basepoint)
	if err != nil {
		t.Fatalf("derive pub: %v", err)
	}
	if string(pub) != string(id.BoxPublicKey[:]) {
		t.Fatalf("box public key does not match private key")
	}
}

func TestVerifyWalletChallenge(t *testing.T) {
	t.Parallel()

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	addr := crypto.PubkeyToAddress(key.PublicKey).Hex()
	hash := accounts.TextHash([]byte(walletChallenge))
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sigHex := hexutil.Encode(sig)

	if !verifyWalletChallenge(addr, sigHex) {
		t.Fatalf("expected signature verification to pass")
	}

	tampered := append([]byte(nil), sig...)
	tampered[5] ^= 0x01
	if verifyWalletChallenge(addr, hexutil.Encode(tampered)) {
		t.Fatalf("expected tampered signature to fail")
	}
}

func TestWalletLoginAutoInitAndRelogin(t *testing.T) {
	t.Parallel()

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate wallet key: %v", err)
	}
	addr := crypto.PubkeyToAddress(key.PublicKey).Hex()

	dir := t.TempDir()
	m, err := NewManager(Config{DataDir: dir, RPCSocketPath: "/tmp/does-not-exist.sock"})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	p, err := m.LoginWithWallet(addr, Settings{Discoverable: true, AllowStrangerRequests: true})
	if err != nil {
		t.Fatalf("wallet login init: %v", err)
	}
	if !strings.EqualFold(p.Username, addr) {
		t.Fatalf("username should equal wallet address")
	}

	m2, err := NewManager(Config{DataDir: dir, RPCSocketPath: "/tmp/does-not-exist.sock"})
	if err != nil {
		t.Fatalf("new manager #2: %v", err)
	}
	p2, err := m2.LoginWithWallet(addr, Settings{})
	if err != nil {
		t.Fatalf("wallet relogin: %v", err)
	}
	if p2.UserID == "" {
		t.Fatalf("expected persistent user id")
	}
}
