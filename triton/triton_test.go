package triton

import (
	"testing"

	docker "github.com/fsouza/go-dockerclient"
	tu "github.com/hashicorp/nomad/testutil"
	"github.com/stretchr/testify/require"
)

func TestDockerDriver_AuthFromTaskConfig(t *testing.T) {
	if !tu.IsCI() {
		t.Parallel()
	}

	cases := []struct {
		Auth       DockerAuth
		AuthConfig *docker.AuthConfiguration
		Desc       string
	}{
		{
			Auth:       DockerAuth{},
			AuthConfig: nil,
			Desc:       "Empty Config",
		},
		{
			Auth: DockerAuth{
				Username:   "foo",
				Password:   "bar",
				Email:      "foo@bar.com",
				ServerAddr: "www.foobar.com",
			},
			AuthConfig: &docker.AuthConfiguration{
				Username:      "foo",
				Password:      "bar",
				Email:         "foo@bar.com",
				ServerAddress: "www.foobar.com",
			},
			Desc: "All fields set",
		},
		{
			Auth: DockerAuth{
				Username:   "foo",
				Password:   "bar",
				ServerAddr: "www.foobar.com",
			},
			AuthConfig: &docker.AuthConfiguration{
				Username:      "foo",
				Password:      "bar",
				ServerAddress: "www.foobar.com",
			},
			Desc: "Email not set",
		},
	}

	for _, c := range cases {
		t.Run(c.Desc, func(t *testing.T) {
			act, err := authFromTaskConfig(&TaskConfig{Docker: DockerAPI{Auth: c.Auth}})("test")
			require.NoError(t, err)
			require.Exactly(t, c.AuthConfig, act)
		})
	}
}
