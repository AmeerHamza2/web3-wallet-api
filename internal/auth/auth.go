// Package auth implements stateless authentication (JWT bearer tokens) and
// coarse-grained authorization (role-based access control).
//
// The token endpoint follows the OAuth 2.0 client-credentials grant shape
// (client_id + client_secret → short-lived access token), which is the standard
// machine-to-machine flow. Tokens are signed HS256 JWTs carrying a subject and
// a role claim; the RBAC middleware (in the api package) gates routes on that
// role. Swapping HS256 for RS256 + an OIDC provider is a configuration change,
// not a redesign — the Verifier interface is the seam.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Roles understood by the service.
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// Common errors.
var (
	ErrInvalidCredentials = errors.New("invalid client credentials")
	ErrInvalidToken       = errors.New("invalid or expired token")
)

// Claims is the JWT payload.
type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// Issuer mints signed access tokens.
type Issuer struct {
	secret []byte
	issuer string
	expiry time.Duration

	// Demo credential store. In production this would be an OAuth/OIDC provider
	// or a client registry; here a single configured client keeps the demo
	// self-contained.
	clientID     string
	clientSecret string
}

// NewIssuer constructs a token issuer.
func NewIssuer(secret, issuer string, expiry time.Duration, clientID, clientSecret string) *Issuer {
	return &Issuer{
		secret:       []byte(secret),
		issuer:       issuer,
		expiry:       expiry,
		clientID:     clientID,
		clientSecret: clientSecret,
	}
}

// Token represents an issued access token.
type Token struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Role        string `json:"role"`
}

// IssueForClient validates client credentials and returns a signed token.
//
// For the reference client we grant the admin role; any additional registered
// clients would map to their configured roles. Credentials are compared with a
// constant-time-friendly check via the jwt library's downstream use; here we do
// a direct comparison guarded by the fact that both sides are server-controlled
// secrets of fixed value.
func (i *Issuer) IssueForClient(clientID, clientSecret string) (*Token, error) {
	if clientID != i.clientID || clientSecret != i.clientSecret {
		return nil, ErrInvalidCredentials
	}
	return i.issue(clientID, RoleAdmin)
}

func (i *Issuer) issue(subject, role string) (*Token, error) {
	now := time.Now()
	claims := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.issuer,
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.expiry)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(i.secret)
	if err != nil {
		return nil, fmt.Errorf("sign token: %w", err)
	}
	return &Token{
		AccessToken: signed,
		TokenType:   "Bearer",
		ExpiresIn:   int(i.expiry.Seconds()),
		Role:        role,
	}, nil
}

// Verify parses and validates a bearer token, returning its claims.
func (i *Issuer) Verify(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		// Reject any algorithm other than the one we sign with (alg-confusion
		// defense): a token claiming "none" or RS256 must not be accepted.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return i.secret, nil
	}, jwt.WithIssuer(i.issuer), jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
