package cert

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelfSignedFields(t *testing.T) {
	certPEM, keyPEM, err := selfSigned("short.test")
	require.NoError(t, err)

	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)
	assert.Equal(t, certBlockType, block.Type)

	parsed, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	assert.Contains(t, parsed.DNSNames, "localhost")
	assert.Contains(t, parsed.DNSNames, "short.test")
	assert.True(t, parsed.NotAfter.After(time.Now()), "сертификат не должен быть просрочен")
	assert.Contains(t, parsed.ExtKeyUsage, x509.ExtKeyUsageServerAuth)

	var found bool
	for _, ip := range parsed.IPAddresses {
		if ip.String() == "127.0.0.1" {
			found = true
			break
		}
	}
	assert.True(t, found, "в сертификате должен быть адрес обратной петли")

	keyBlock, _ := pem.Decode(keyPEM)
	require.NotNil(t, keyBlock)
	assert.Equal(t, keyBlockType, keyBlock.Type)
}

func TestSelfSignedAcceptsIPHost(t *testing.T) {
	certPEM, _, err := selfSigned("192.168.0.1")
	require.NoError(t, err)

	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)
	parsed, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	var found bool
	for _, ip := range parsed.IPAddresses {
		if ip.String() == "192.168.0.1" {
			found = true
			break
		}
	}
	assert.True(t, found, "адрес должен попасть в список IP, а не доменов")
	assert.NotContains(t, parsed.DNSNames, "192.168.0.1")
}

func TestCertificateServesHTTPS(t *testing.T) {
	certificate, err := Certificate("localhost")
	require.NoError(t, err)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()

	// клиент проверяет цепочку по нашему же сертификату, а не пропускает проверку
	pool := x509.NewCertPool()
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	require.NoError(t, err)
	pool.AddCert(leaf)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
	}
	res, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "ok", string(body))
}
