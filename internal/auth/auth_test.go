package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	a := New("secret")
	signed := a.Sign("user-1")

	got, err := a.Verify(signed)
	require.NoError(t, err)
	assert.Equal(t, "user-1", got)
}

func TestVerifyRejectsTamperedSignature(t *testing.T) {
	a := New("secret")
	signed := a.Sign("user-1")

	_, err := a.Verify(signed + "ff")
	assert.Error(t, err)
}

func TestVerifyRejectsForeignSecret(t *testing.T) {
	signed := New("secret-a").Sign("user-1")

	_, err := New("secret-b").Verify(signed)
	assert.Error(t, err)
}

func TestMiddlewareIssuesCookieWhenAbsent(t *testing.T) {
	a := New("secret")

	var observedUserID string
	var observedInvalid bool
	handler := a.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		observedUserID, _ = UserIDFromContext(r.Context())
		observedInvalid = CookieInvalidFromContext(r.Context())
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	res := rec.Result()
	defer res.Body.Close()

	require.NotEmpty(t, res.Cookies(), "expected Set-Cookie when no cookie was sent")
	assert.Equal(t, cookieName, res.Cookies()[0].Name)
	assert.NotEmpty(t, observedUserID)
	assert.False(t, observedInvalid)
}

func TestMiddlewareKeepsValidCookie(t *testing.T) {
	a := New("secret")

	var observedUserID string
	handler := a.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		observedUserID, _ = UserIDFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: a.Sign("user-42")})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "user-42", observedUserID)
	assert.Empty(t, rec.Result().Cookies(), "no new cookie expected for valid request")
}

func TestMiddlewareFlagsTamperedCookie(t *testing.T) {
	a := New("secret")

	var (
		observedUserID  string
		observedInvalid bool
	)
	handler := a.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		observedUserID, _ = UserIDFromContext(r.Context())
		observedInvalid = CookieInvalidFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "garbage:deadbeef"})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.True(t, observedInvalid)
	assert.Empty(t, observedUserID)
}
