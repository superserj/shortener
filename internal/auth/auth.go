// Пакет auth опознаёт пользователя по куке, подписанной HMAC-SHA256.
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

// Authenticator выдаёт и проверяет подписанные куки с идентификатором пользователя.
type Authenticator struct {
	secret []byte
}

// New создаёт аутентификатор с указанным секретом подписи.
func New(secret string) *Authenticator {
	return &Authenticator{secret: []byte(secret)}
}

// Sign возвращает значение куки: идентификатор пользователя и его подпись
// через двоеточие.
func (a *Authenticator) Sign(userID string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(userID))
	return userID + ":" + hex.EncodeToString(mac.Sum(nil))
}

// Verify проверяет подпись куки и возвращает идентификатор пользователя.
func (a *Authenticator) Verify(value string) (string, error) {
	userID, signature, ok := strings.Cut(value, ":")
	if !ok || userID == "" {
		return "", errors.New("invalid cookie format")
	}
	got, err := hex.DecodeString(signature)
	if err != nil {
		return "", errors.New("invalid cookie signature encoding")
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(userID))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return "", errors.New("signature mismatch")
	}
	// Cut вернул подстроку заголовка запроса, а userID переживает запрос
	// в хранилище — без копии каждая запись удерживала бы заголовок целиком.
	return strings.Clone(userID), nil
}

// Middleware кладёт идентификатор пользователя в контекст запроса. Запросу без
// куки выдаёт новую, испорченную куку помечает признаком, который можно получить
// через CookieInvalidFromContext.
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

// WithUserID возвращает контекст с идентификатором пользователя.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ctxUserID, userID)
}

// WithCookieInvalid возвращает контекст с пометкой о непройденной проверке куки.
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

// UserIDFromContext достаёт идентификатор пользователя из контекста.
func UserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxUserID).(string)
	return v, ok && v != ""
}

// CookieInvalidFromContext сообщает, не прошла ли кука запроса проверку подписи.
func CookieInvalidFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(ctxCookieInvalid).(bool)
	return v
}
