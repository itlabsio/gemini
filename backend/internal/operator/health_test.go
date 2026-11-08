package operator

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
)

func TestDependencyCheckLogsFailureAndRecovery(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	var pingErr error
	d := &dependencyCheck{name: "s3", ping: func(context.Context) error { return pingErr }}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, ""},
		{errors.New("boom"), "unavailable"},
		{errors.New("boom"), "unavailable"},
		{nil, "recovered"},
		{nil, ""},
	} {
		buf.Reset()
		pingErr = tc.err
		d.run(context.Background())
		if tc.want == "" && buf.Len() != 0 {
			t.Errorf("unexpected log: %s", buf.String())
		}
		if !strings.Contains(buf.String(), tc.want) {
			t.Errorf("log %q does not contain %q", buf.String(), tc.want)
		}
	}
}
