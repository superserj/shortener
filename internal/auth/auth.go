package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

const (
	cookieName    = "user_id"
	cookieMaxAge  = 60 * 60 * 24 * 30
	userIDByteLen = 16
)

type ctxKey int

const (
	ctxUserID ctxKey = iota
	ctxCookieInvalid
)

type Authenticator struct {
	secret []byte
}

func New(secret string) *Authenticator {
	return &Authenticator{secret: []byte(secret)}
}

func (a *Authenticator) Sign(userID string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(userID))
	return userID + ":" + hex.EncodeToString(mac.Sum(nil))
}

func (a *Authenticator) Verify(value string) (string, error) {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", errors.New("invalid cookie format")
	}
	got, err := hex.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("invalid cookie signature encoding")
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return "", errors.New("signature mismatch")
	}
	return parts[0], nil
}

func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		cookie, err := r.Cookie(cookieName)
		switch {
		case errors.Is(err, http.ErrNoCookie):
			userID, issueErr := newUserID()
			if issueErr != nil {
				http.Error(w, "failed to issue user id", http.StatusInternalServerError)
				return
			}
			http.SetCookie(w, a.makeCookie(userID))
			ctx = WithUserID(ctx, userID)
		case err != nil:
			http.Error(w, "failed to read cookie", http.StatusBadRequest)
			return
		default:
			userID, verifyErr := a.Verify(cookie.Value)
			if verifyErr != nil {
				ctx = WithCookieInvalid(ctx)
			} else {
				ctx = WithUserID(ctx, userID)
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ctxUserID, userID)
}

func WithCookieInvalid(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxCookieInvalid, true)
}

func (a *Authenticator) makeCookie(userID string) *http.Cookie {
	return &http.Cookie{
		Name:     cookieName,
		Value:    a.Sign(userID),
		Path:     "/",
		MaxAge:   cookieMaxAge,
		HttpOnly: true,
	}
}

func newUserID() (string, error) {
	buf := make([]byte, userIDByteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func UserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxUserID).(string)
	return v, ok && v != ""
}

func CookieInvalidFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(ctxCookieInvalid).(bool)
	return v
}
