// Пакет cert выпускает самоподписанный TLS-сертификат для режима HTTPS.
// Сертификат живёт только в памяти процесса и создаётся заново при каждом
// запуске: сервису не нужен ни внешний удостоверяющий центр, ни файлы с ключами.
package cert

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

const (
	// keyBits — длина ключа RSA. 2048 бит достаточно для самоподписанного
	// сертификата и заметно быстрее генерируется при старте сервиса.
	keyBits = 2048
	// validity — срок действия сертификата.
	validity = 365 * 24 * time.Hour
	// serialBits — разрядность случайного серийного номера.
	serialBits = 128
	// clockSkew — запас на расхождение часов клиента и сервера: без него
	// свежевыпущенный сертификат отвергается как «ещё не действительный».
	clockSkew = time.Hour

	certBlockType = "CERTIFICATE"
	keyBlockType  = "RSA PRIVATE KEY"
)

// selfSigned выпускает сертификат и закрытый ключ в формате PEM. В сертификат
// попадают переданные имена хостов, адреса добавляются в список IP, а имена —
// в список доменов. Адрес обратной петли добавляется всегда, иначе клиент не
// сможет проверить сертификат при обращении на localhost.
func selfSigned(hosts ...string) (certPEM, keyPEM []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), serialBits))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial number: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Shortener"},
			Country:      []string{"RU"},
		},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	addHosts(template, hosts)

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: certBlockType, Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: keyBlockType, Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM, nil
}

// Certificate выпускает самоподписанный сертификат и сразу разбирает его в вид,
// пригодный для tls.Config.
func Certificate(hosts ...string) (tls.Certificate, error) {
	certPEM, keyPEM, err := selfSigned(hosts...)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

func addHosts(template *x509.Certificate, hosts []string) {
	template.IPAddresses = []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	template.DNSNames = []string{"localhost"}

	for _, host := range hosts {
		if host == "" {
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
			continue
		}
		if host != "localhost" {
			template.DNSNames = append(template.DNSNames, host)
		}
	}
}
