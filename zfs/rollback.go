package zfs

import (
	"bytes"

	"github.com/cerana/cerana/pkg/errors"
	"github.com/cerana/cerana/zfs/nv"
)

func rollback(name string) (string, error) {
	m := map[string]interface{}{
		"cmd":     "zfs_rollback",
		"version": uint64(0),
	}

	encoded := &bytes.Buffer{}
	err := nv.NewNativeEncoder(encoded).Encode(m)
	if err != nil {
		return "", errors.Wrapv(err, map[string]interface{}{"name": name, "args": m})
	}

	out := make([]byte, 1024)
	err = ioctl(zfs(), name, encoded.Bytes(), out)

	var snapName string
	if err == nil {
		var results map[string]string
		if err = nv.NewNativeDecoder(bytes.NewReader(out)).Decode(&results); err == nil {
			snapName = results["target"]
		} else {
			err = errors.Wrapv(err, map[string]interface{}{"name": name, "args": m})
		}
	}
	return snapName, err
}
