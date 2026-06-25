package discovery

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/grandcat/zeroconf"
	"github.com/mohammad-safakhou/stalker/internal/store"
)

const ServiceType = "_stalker._tcp"

type Advertiser struct {
	server *zeroconf.Server
}

func Advertise(addr string, s *store.Store) (*Advertiser, error) {
	if disabled := strings.TrimSpace(os.Getenv("STALKER_BONJOUR")); disabled == "0" || strings.EqualFold(disabled, "false") {
		return nil, nil
	}
	_, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("parse listen address for bonjour: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, fmt.Errorf("parse listen port for bonjour: %w", err)
	}
	device, err := s.SyncDevice()
	if err != nil {
		return nil, err
	}
	instance := "Stalker " + device.Name
	text := []string{
		"path=/api/v1/sync",
		"snapshot=/api/v1/sync/snapshot",
		"stream=/api/v1/sync/stream",
		"device_id=" + device.ID,
		"platform=" + device.Platform,
	}
	server, err := zeroconf.Register(instance, ServiceType, "local.", port, text, nil)
	if err != nil {
		return nil, err
	}
	return &Advertiser{server: server}, nil
}

func (a *Advertiser) Shutdown() {
	if a != nil && a.server != nil {
		a.server.Shutdown()
	}
}
