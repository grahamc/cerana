package clusterconf_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"time"

	"github.com/cerana/cerana/acomm"
	"github.com/cerana/cerana/providers/clusterconf"
)

func (s *clusterConf) setupDHCP(config clusterconf.DHCPConfig) {
	buf, err := json.Marshal(config)
	s.Require().NoError(err)
	s.Require().NoError(s.kv.Set("dhcp", string(buf)))
}

type DHCPConfigStrs struct {
	DNS      []string
	Duration string
	Gateway  string
	Net      string
}

func (d DHCPConfigStrs) toConfig() clusterconf.DHCPConfig {
	c := clusterconf.DHCPConfig{
		DNS:     make([]net.IP, len(d.DNS)),
		Gateway: net.ParseIP(d.Gateway),
	}

	for i, dns := range d.DNS {
		c.DNS[i] = net.ParseIP(dns)
	}

	c.Duration, _ = time.ParseDuration(d.Duration)
	_, subnet, _ := net.ParseCIDR(d.Net)
	if subnet != nil {
		c.Net.IP = subnet.IP.To16()
		c.Net.Mask = subnet.Mask
	}

	return c
}

func (s *clusterConf) TestGetDHCP() {
	sconf := DHCPConfigStrs{
		DNS: []string{
			fmt.Sprintf("10.10.1.%d", rand.Intn(255)),
			fmt.Sprintf("10.10.2.%d", rand.Intn(255)),
		},
		Duration: fmt.Sprintf("%dh", 1+rand.Intn(24)),
		Gateway:  fmt.Sprintf("10.10.10.%d", rand.Intn(255)),
		Net:      "10.10.10.0/24",
	}
	conf := sconf.toConfig()

	_, _, err := s.clusterConf.GetDHCP(nil)
	s.Error(err, "expected no dhcp settings")

	s.setupDHCP(conf)

	resp, _, err := s.clusterConf.GetDHCP(nil)
	s.NoError(err, "expected some dhcp settings")

	got := resp.(clusterconf.DHCPConfig)
	s.Equal(conf, got)
}

func (s *clusterConf) TestSetDHCP() {
	s.setupDHCP(clusterconf.DHCPConfig{
		DNS: []net.IP{
			net.IPv4(10, 10, 1, byte(rand.Intn(255))),
			net.IPv4(10, 10, 2, byte(rand.Intn(255))),
		},
		Duration: 5*time.Hour + time.Hour*time.Duration(rand.Intn(11)+1),
		Gateway:  net.IPv4(10, 10, 10, byte(rand.Intn(255))),
		Net: net.IPNet{
			IP:   net.IPv4(10, 10, 10, 0),
			Mask: net.IPMask{255, 255, 255, 0},
		},
	})

	tests := []struct {
		desc string
		err  string
		conf DHCPConfigStrs
	}{
		{desc: "duration too short",
			err: "duration is invalid",
			conf: DHCPConfigStrs{
				Duration: "30m",
			},
		},
		{desc: "duration too long",
			err: "duration is invalid",
			conf: DHCPConfigStrs{
				Duration: "25h",
			},
		},
		{desc: "missing network",
			err: "net.IP is required",
			conf: DHCPConfigStrs{
				Duration: "1h",
			},
		},
		{desc: "IPv6",
			err: "net.IP must be IPv4",
			conf: DHCPConfigStrs{
				Duration: "1h",
				Net:      "::1/128",
			},
		},
		{desc: "IPv4zero",
			err: "net.IP must not be 0.0.0.0",
			conf: DHCPConfigStrs{
				Duration: "1h",
				Net:      "0.0.0.0/0",
			},
		},
		{desc: "missing netmask",
			err: "net.IP is required",
			conf: DHCPConfigStrs{
				Duration: "1h",
				Net:      "10.100.10.0",
			},
		},
		{desc: "unreachable gateway",
			err: "gateway is unreachable",
			conf: DHCPConfigStrs{
				Duration: "1h",
				Gateway:  fmt.Sprintf("10.0.10.%d", rand.Intn(255)),
				Net:      "10.100.10.0/24",
			},
		},
		{desc: "good",
			conf: DHCPConfigStrs{
				DNS: []string{
					fmt.Sprintf("10.100.1.%d", rand.Intn(255)),
					fmt.Sprintf("10.100.2.%d", rand.Intn(255)),
				},
				Duration: "1h",
				Gateway:  fmt.Sprintf("10.100.10.%d", rand.Intn(255)),
				Net:      "10.100.10.0/24",
			},
		},
	}

	for _, t := range tests {
		req, err := acomm.NewRequest(acomm.RequestOptions{
			Task: "clusterconf-set-dhcp",
			Args: t.conf.toConfig(),
		})
		s.Require().NoError(err, t.desc)

		resp, url, err := s.clusterConf.SetDHCP(req)
		s.Nil(resp, t.desc)
		s.Nil(url, t.desc)
		if t.err != "" {
			s.Contains(err.Error(), t.err, t.desc)
			continue
		}
		if !s.NoError(err, t.desc) {
			continue
		}

		resp, url, err = s.clusterConf.GetDHCP(nil)
		s.Require().NoError(err, t.desc)
		s.Nil(url)

		got := resp.(clusterconf.DHCPConfig)
		s.Equal(t.conf.toConfig(), got, t.desc)
	}
}
