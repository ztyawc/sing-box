package option

import (
	"testing"

	"github.com/sagernet/sing/common/json"

	"github.com/stretchr/testify/require"
)

func TestSOCKSOutboundPrivateAuthMethodJSON(t *testing.T) {
	var options SOCKSOutboundOptions
	err := json.Unmarshal([]byte(`{
		"server": "127.0.0.1",
		"server_port": 1080,
		"version": "5",
		"username": "1234567890123456789",
		"password": "password",
		"private_auth_method": "0x82"
	}`), &options)
	require.NoError(t, err)
	require.Equal(t, "0x82", options.PrivateAuthMethod)

	content, err := json.Marshal(options)
	require.NoError(t, err)
	require.Contains(t, string(content), `"private_auth_method":"0x82"`)
}
