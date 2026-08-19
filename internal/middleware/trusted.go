package middleware

import (
	"fmt"
	"net"
	"net/http"
)

// realIPHeader — заголовок, в котором прокси передаёт адрес клиента. Доверять
// ему можно только там, где прокси заполняет его безусловно, затирая значение
// из запроса.
const realIPHeader = "X-Real-IP"

// TrustedSubnet пропускает дальше только запросы с адресом из подсети subnet,
// заданной в формате CIDR. Адрес берётся из заголовка X-Real-IP: запрос без
// него, с неразборчивым адресом или с адресом из чужой сети получает 403.
// Пустая подсеть закрывает доступ всем.
func TrustedSubnet(subnet string) (func(http.Handler) http.Handler, error) {
	if subnet == "" {
		return denyAll, nil
	}

	_, network, err := net.ParseCIDR(subnet)
	if err != nil {
		return nil, fmt.Errorf("parse trusted subnet %q: %w", subnet, err)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := net.ParseIP(r.Header.Get(realIPHeader))
			if ip == nil || !network.Contains(ip) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

// denyAll закрывает доступ к обработчику: подсеть не настроена, доверять
// некому.
func denyAll(http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
}
