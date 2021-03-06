package metrics

import (
	"encoding/json"
	"net/url"
	"os/exec"

	"github.com/cerana/cerana/acomm"
	"github.com/cerana/cerana/pkg/errors"
)

// Hardware returns information about the hardware.
func (m *Metrics) Hardware(req *acomm.Request) (interface{}, *url.URL, error) {
	// Note: json output from lshw is broken when specifying classes with `-C`
	out, err := exec.Command("lshw", "-json").Output()
	if err != nil {
		return nil, nil, errors.Wrap(err)
	}

	var outI interface{}
	if err := json.Unmarshal(out, &outI); err != nil {
		return nil, nil, errors.Wrapv(err, map[string]interface{}{"output": string(out)})
	}

	return outI, nil, nil
}
