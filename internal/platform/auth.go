package platform

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Identity struct {
	UserID string
	Email  string
}

type SessionClaims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

func HashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func MintToken(cfg Config, userID, email string) (string, error) {
	now := time.Now().UTC()
	claims := SessionClaims{
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    cfg.JWTIssuer,
			Audience:  jwt.ClaimStrings{cfg.JWTAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(12 * time.Hour)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(cfg.JWTSecret))
}

func Verify(cfg Config, raw string, keys *JWKS) (Identity, error) {
	local := cfg.Environment == "local" || cfg.Environment == "test"
	parser := jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg(), jwt.SigningMethodRS256.Alg(), jwt.SigningMethodES256.Alg()}))
	claims := &SessionClaims{}
	token, err := parser.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		switch token.Method.Alg() {
		case jwt.SigningMethodHS256.Alg():
			if !local || cfg.JWTSecret == "" || cfg.JWTSecret == DevJWTSecret {
				return nil, fmt.Errorf("hmac tokens are not accepted")
			}
			return []byte(cfg.JWTSecret), nil
		case jwt.SigningMethodRS256.Alg(), jwt.SigningMethodES256.Alg():
			if keys == nil {
				return nil, fmt.Errorf("jwks is not configured")
			}
			return keys.Key(fmt.Sprint(token.Header["kid"]))
		default:
			return nil, fmt.Errorf("unsupported signing method")
		}
	})
	if err != nil || !token.Valid {
		return Identity{}, Fail(http.StatusUnauthorized, "unauthenticated", "Invalid or expired session")
	}
	if claims.Issuer != cfg.JWTIssuer {
		return Identity{}, Fail(http.StatusUnauthorized, "unauthenticated", "Invalid or expired session")
	}
	if cfg.Environment == "production" && devIssuer(claims.Issuer) {
		return Identity{}, Fail(http.StatusUnauthorized, "unauthenticated", "Invalid or expired session")
	}
	audienceOK := false
	for _, audience := range claims.Audience {
		if audience == cfg.JWTAudience {
			audienceOK = true
		}
	}
	if !audienceOK || claims.Subject == "" {
		return Identity{}, Fail(http.StatusUnauthorized, "unauthenticated", "Invalid or expired session")
	}
	return Identity{UserID: claims.Subject, Email: claims.Email}, nil
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type jwksDoc struct {
	Keys []jwk `json:"keys"`
}

type JWKS struct {
	url  string
	mu   sync.Mutex
	keys map[string]any
	at   time.Time
}

func NewJWKS(url string) *JWKS {
	if url == "" {
		return nil
	}
	return &JWKS{url: url, keys: map[string]any{}}
}

func (j *JWKS) Key(kid string) (any, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if time.Since(j.at) > 5*time.Minute || len(j.keys) == 0 {
		if err := j.refresh(); err != nil {
			return nil, err
		}
	}
	key, ok := j.keys[kid]
	if !ok {
		if err := j.refresh(); err != nil {
			return nil, err
		}
		key, ok = j.keys[kid]
	}
	if !ok {
		return nil, fmt.Errorf("unknown signing key")
	}
	return key, nil
}

func (j *JWKS) refresh() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var doc jwksDoc
	if err := json.NewDecoder(response.Body).Decode(&doc); err != nil {
		return err
	}
	next := map[string]any{}
	for _, item := range doc.Keys {
		key, err := item.publicKey()
		if err != nil {
			continue
		}
		next[item.Kid] = key
	}
	j.keys = next
	j.at = time.Now()
	return nil
}

func (item jwk) publicKey() (any, error) {
	switch item.Kty {
	case "RSA":
		nBytes, err := base64.RawURLEncoding.DecodeString(item.N)
		if err != nil {
			return nil, err
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(item.E)
		if err != nil {
			return nil, err
		}
		n := new(big.Int).SetBytes(nBytes)
		e := 0
		for _, b := range eBytes {
			e = e<<8 + int(b)
		}
		return &rsa.PublicKey{N: n, E: e}, nil
	case "EC":
		if item.Crv != "P-256" {
			return nil, fmt.Errorf("unsupported curve")
		}
		xBytes, err := base64.RawURLEncoding.DecodeString(item.X)
		if err != nil {
			return nil, err
		}
		yBytes, err := base64.RawURLEncoding.DecodeString(item.Y)
		if err != nil {
			return nil, err
		}
		return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(xBytes), Y: new(big.Int).SetBytes(yBytes)}, nil
	default:
		return nil, fmt.Errorf("unsupported key type")
	}
}

func Bearer(header string) (string, error) {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", Fail(http.StatusUnauthorized, "unauthenticated", "Authentication required")
	}
	return strings.TrimSpace(parts[1]), nil
}
