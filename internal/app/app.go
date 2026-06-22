package app

import (
	"net/http"
	"strings"

	"github.com/mohammad-safakhou/stalker/internal/proxy"
	"github.com/mohammad-safakhou/stalker/internal/store"
	"github.com/mohammad-safakhou/stalker/internal/ui"
)

type App struct {
	proxy *proxy.Proxy
	ui    *ui.Handler
}

func New(s *store.Store) *App {
	return &App{
		proxy: &proxy.Proxy{Store: s},
		ui:    ui.New(s),
	}
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/":
		http.Redirect(w, r, "/ui/", http.StatusFound)
	case r.URL.Path == "/ui" || strings.HasPrefix(r.URL.Path, "/ui/") ||
		r.URL.Path == "/api/exchanges" || strings.HasPrefix(r.URL.Path, "/api/exchanges/"):
		a.ui.ServeHTTP(w, r)
	default:
		a.proxy.ServeHTTP(w, r)
	}
}
