package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// okHandler отмечает, что запрос дошёл до защищённого обработчика.
func okHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestTrustedSubnetAllowsOwnSubnet(t *testing.T) {
	mw, err := TrustedSubnet("192.168.1.0/24")
	require.NoError(t, err)

	called := false
	req := httptest.NewRequest(http.MethodGet, "/api/internal/stats", nil)
	req.Header.Set(realIPHeader, "192.168.1.15")
	res := httptest.NewRecorder()

	mw(okHandler(&called)).ServeHTTP(res, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, res.Code)
}

func TestTrustedSubnetRejects(t *testing.T) {
	mw, err := TrustedSubnet("192.168.1.0/24")
	require.NoError(t, err)

	tests := []struct {
		name string
		ip   string
	}{
		{name: "адрес из чужой сети", ip: "10.0.0.1"},
		{name: "заголовок не заполнен", ip: ""},
		{name: "адрес не разбирается", ip: "not-an-ip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			req := httptest.NewRequest(http.MethodGet, "/api/internal/stats", nil)
			if tt.ip != "" {
				req.Header.Set(realIPHeader, tt.ip)
			}
			res := httptest.NewRecorder()

			mw(okHandler(&called)).ServeHTTP(res, req)

			assert.False(t, called, "запрос не должен доходить до обработчика")
			assert.Equal(t, http.StatusForbidden, res.Code)
		})
	}
}

func TestTrustedSubnetEmptyGivesNoMiddleware(t *testing.T) {
	mw, err := TrustedSubnet("")
	require.NoError(t, err)
	assert.Nil(t, mw, "сверять адрес не с чем, мидлварь не нужна")
}

func TestTrustedSubnetRejectsBrokenCIDR(t *testing.T) {
	_, err := TrustedSubnet("192.168.1.0/33")
	assert.Error(t, err, "неразбираемая подсеть останавливает запуск")
}

func TestTrustedSubnetIPv6(t *testing.T) {
	mw, err := TrustedSubnet("2001:db8::/32")
	require.NoError(t, err)

	called := false
	req := httptest.NewRequest(http.MethodGet, "/api/internal/stats", nil)
	req.Header.Set(realIPHeader, "2001:db8::1")
	res := httptest.NewRecorder()

	mw(okHandler(&called)).ServeHTTP(res, req)

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, res.Code)
}
