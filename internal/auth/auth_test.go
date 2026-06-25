package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func newTestIssuer() *Issuer {
	return NewIssuer("test-secret", "test-issuer", time.Hour, "client-1", "secret-1")
}

func TestIssueAndVerifyRoundTrip(t *testing.T) {
	iss := newTestIssuer()
	tok, err := iss.IssueForClient("client-1", "secret-1")
	if err != nil {
		t.Fatalf("IssueForClient: %v", err)
	}
	if tok.TokenType != "Bearer" {
		t.Errorf("TokenType = %q, want Bearer", tok.TokenType)
	}
	claims, err := iss.Verify(tok.AccessToken)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Role != RoleAdmin {
		t.Errorf("Role = %q, want %q", claims.Role, RoleAdmin)
	}
	if claims.Subject != "client-1" {
		t.Errorf("Subject = %q, want client-1", claims.Subject)
	}
}

func TestIssueRejectsBadCredentials(t *testing.T) {
	iss := newTestIssuer()
	if _, err := iss.IssueForClient("client-1", "wrong"); err == nil {
		t.Error("expected error for wrong secret")
	}
	if _, err := iss.IssueForClient("unknown", "secret-1"); err == nil {
		t.Error("expected error for unknown client")
	}
}

func TestVerifyRejectsTamperedToken(t *testing.T) {
	iss := newTestIssuer()
	tok, _ := iss.IssueForClient("client-1", "secret-1")
	tampered := tok.AccessToken[:len(tok.AccessToken)-2] + "xx"
	if _, err := iss.Verify(tampered); err == nil {
		t.Error("expected error for tampered token")
	}
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	iss := newTestIssuer()
	tok, _ := iss.IssueForClient("client-1", "secret-1")
	other := NewIssuer("DIFFERENT-secret", "test-issuer", time.Hour, "client-1", "secret-1")
	if _, err := other.Verify(tok.AccessToken); err == nil {
		t.Error("expected verification to fail under a different signing key")
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	iss := NewIssuer("test-secret", "test-issuer", -time.Minute, "client-1", "secret-1")
	tok, _ := iss.IssueForClient("client-1", "secret-1")
	if _, err := iss.Verify(tok.AccessToken); err == nil {
		t.Error("expected expired token to be rejected")
	}
}

// TestVerifyRejectsAlgNone guards against the classic JWT "alg=none" / algorithm
// confusion attack: a token signed with no/another algorithm must be refused.
func TestVerifyRejectsAlgNone(t *testing.T) {
	iss := newTestIssuer()
	claims := Claims{Role: RoleAdmin, RegisteredClaims: jwt.RegisteredClaims{Issuer: "test-issuer"}}
	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	raw, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none: %v", err)
	}
	if _, err := iss.Verify(raw); err == nil {
		t.Error("expected alg=none token to be rejected")
	}
	if !strings.HasPrefix(raw, "ey") {
		t.Fatal("sanity: token should be a JWT")
	}
}
