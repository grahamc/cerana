package systemd_test

import (
	"fmt"

	"github.com/cerana/cerana/acomm"
	systemdp "github.com/cerana/cerana/providers/systemd"
)

func (s *systemd) TestGet() {
	tests := []struct {
		name string
		err  string
	}{
		{"", "missing arg: name"},
		{"doesnotexist.service", "unit not found"},
		{"dbus.service", ""},
	}

	for _, test := range tests {
		args := &systemdp.GetArgs{Name: test.name}
		argsS := fmt.Sprintf("%+v", test)

		req, err := acomm.NewRequest(acomm.RequestOptions{
			Task:         "systemd-get",
			ResponseHook: s.responseHook,
			Args:         args,
		})
		s.Require().NoError(err, argsS)

		res, streamURL, err := s.systemd.Get(req)
		s.Nil(streamURL, argsS)
		if test.err == "" {
			if !s.NoError(err, argsS) {
				continue
			}
			result, ok := res.(*systemdp.GetResult)
			if !s.True(ok, argsS) {
				continue
			}
			if !s.NotNil(result.Unit, argsS) {
				continue
			}
			s.Equal(test.name, result.Unit.Name, argsS)
		} else {
			s.EqualError(err, test.err, argsS)
		}
	}
}
